package realdebrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/feruzabad/debroxy/internal/common/httpclient"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// defaultCDNProbeHosts is one concrete host of the Real-Debrid CDN domain,
// from the fixtures (ADR 0012 rule 6).
var defaultCDNProbeHosts = []string{"20-4.download.real-debrid.com"}

const pathUnrestrict = "/unrestrict/link"

// deliveryHeaders is the whitelist of ADR 0010 rule 18.
var deliveryHeaders = []string{"Content-Length", "Content-Range", "Content-Type", "Last-Modified", "ETag"}

// OpenDeliveryStream resolves the item's single hoster link through
// unrestrict (cached) and opens it. Files honour Range; RAR bundles are
// assumed to (ASM-R15) and fall back to rule 20 when they do not. CDN
// downloads have no rate class (ADR 0013 rule 2).
func (g *Gateway) OpenDeliveryStream(ctx context.Context, request in.OpenDeliveryStreamRequest) (in.DeliveryStream, error) {
	if request.Kind != in.KindTorrent {
		return in.DeliveryStream{}, fmt.Errorf("delivering %s items is not supported by this adapter", request.Kind)
	}
	header := http.Header{}
	if request.Range != "" {
		header.Set("Range", request.Range)
		if request.IfRange != "" {
			header.Set("If-Range", request.IfRange)
		}
	}
	response, err := g.client.OpenLink(ctx, g.links, httpclient.LinkRequest{
		CacheKey: linkCacheKey(request.ProviderItemID) + targetKey(request.Target),
		Resolve:  func(ctx context.Context) (httpclient.SecretURL, error) { return g.resolveLink(ctx, request) },
		Method:   request.Method,
		Header:   header,
	})
	if err != nil {
		return in.DeliveryStream{}, g.neutralOrTransport(err)
	}
	return g.deliveryStream(response, request.Range != "")
}

type unrestrictAnswer struct {
	Download string `json:"download"`
}

// resolveLink: one selected file gives a direct link to it; more than one
// gives one RAR link (realdebrid.md §4). The target must match what the
// selection offers (ADR 0011 rule 10). Each unrestrict adds a /downloads
// history entry, which the daily sweep removes (DEC-25).
func (g *Gateway) resolveLink(ctx context.Context, request in.OpenDeliveryStreamRequest) (httpclient.SecretURL, error) {
	info, err := g.fetchInfo(ctx, request.ProviderItemID)
	if err != nil {
		return httpclient.SecretURL{}, err
	}
	var selected []torrentFile
	for _, f := range info.Files {
		if f.Selected == 1 {
			selected = append(selected, f)
		}
	}
	switch {
	case len(info.Links) == 0:
		return httpclient.SecretURL{}, in.ProviderRejectedError{Provider: g.accountID, Detail: "the item has no download link yet"}
	case request.Target.Bundle && len(selected) < 2:
		return httpclient.SecretURL{}, in.ProviderRejectedError{Provider: g.accountID, Detail: "a single-file item has no bundle"}
	case !request.Target.Bundle && (len(selected) != 1 || strconv.FormatInt(selected[0].ID, 10) != request.Target.FileID):
		return httpclient.SecretURL{}, in.ProviderRejectedError{Provider: g.accountID, Detail: "per-file delivery needs exactly this one selected file"}
	}
	response, err := g.send(ctx, apiRequest{
		method: http.MethodPost, path: pathUnrestrict,
		body: []byte(url.Values{"link": {info.Links[0]}}.Encode()), contentType: "application/x-www-form-urlencoded", idempotent: true,
	})
	if err != nil {
		return httpclient.SecretURL{}, g.transportError(err)
	}
	if err := g.check(response); err != nil {
		return httpclient.SecretURL{}, err
	}
	var answer unrestrictAnswer
	if err := json.Unmarshal(response.body, &answer); err != nil {
		return httpclient.SecretURL{}, in.ProviderRejectedError{Provider: g.accountID, Detail: "unexpected unrestrict answer"}
	}
	secret, err := httpclient.ParseSecretURL(answer.Download)
	if err != nil {
		return httpclient.SecretURL{}, in.ProviderRejectedError{Provider: g.accountID, Detail: "unrestrict returned no usable link"}
	}
	return secret, nil
}

// deliveryStream: every Real-Debrid target is treated as range-capable
// unless a ranged request comes back as 200 (ADR 0010 rule 20).
func (g *Gateway) deliveryStream(response *http.Response, rangeRequested bool) (in.DeliveryStream, error) {
	switch response.StatusCode {
	case http.StatusOK, http.StatusPartialContent, http.StatusRequestedRangeNotSatisfiable:
	default:
		_ = response.Body.Close()
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			g.logOnce("cdn-redirect", "the CDN answered with a redirect, which debroxy does not follow")
		}
		return in.DeliveryStream{}, in.ProviderUnavailableError{Provider: g.accountID, Cause: fmt.Sprintf("CDN answered %d", response.StatusCode)}
	}
	headers := map[string]string{}
	for _, name := range deliveryHeaders {
		if value := response.Header.Get(name); value != "" {
			headers[name] = value
		}
	}
	rangeSupported := !rangeRequested || response.StatusCode != http.StatusOK
	return in.DeliveryStream{Status: response.StatusCode, Headers: headers, RangeSupported: rangeSupported, Body: response.Body}, nil
}

func (g *Gateway) neutralOrTransport(err error) error {
	var unavailable in.ProviderUnavailableError
	var rejected in.ProviderRejectedError
	if errors.As(err, &unavailable) || errors.As(err, &rejected) || errors.Is(err, in.ErrRemoteItemNotFound) {
		return err
	}
	return g.transportError(err)
}

// CheckEgress opens a TCP connection to the API host and each CDN probe
// host through the pinned dialer (ADR 0012 rule 6).
func (g *Gateway) CheckEgress(ctx context.Context) error {
	hosts := append([]string{hostWithPort(g.baseURL)}, g.probes...)
	for _, host := range hosts {
		if _, _, err := net.SplitHostPort(host); err != nil {
			host = net.JoinHostPort(host, "443")
		}
		if err := g.client.Probe(ctx, host); err != nil {
			return fmt.Errorf("egress check for %s failed: %w", host, err)
		}
	}
	return nil
}

func linkCacheKey(providerItemID string) string { return providerItemID + "/" }

func targetKey(target in.DeliveryTarget) string {
	if target.Bundle {
		return "bundle"
	}
	return "file:" + target.FileID
}

func hostWithPort(u *url.URL) string {
	if u.Port() != "" {
		return u.Host
	}
	if u.Scheme == "http" {
		return net.JoinHostPort(u.Hostname(), "80")
	}
	return net.JoinHostPort(u.Hostname(), "443")
}
