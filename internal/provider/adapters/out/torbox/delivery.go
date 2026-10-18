package torbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/feruzabad/debroxy/internal/common/httpclient"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// defaultCDNProbeHosts are one concrete host per TorBox CDN domain, from
// the fixtures (ADR 0012 rule 6).
var defaultCDNProbeHosts = []string{"nexus-182.ceur.tb-cdn.st", "store-079.wnam.tb-cdn.io"}

const (
	pathTorrentRequestDL = "/torrents/requestdl"
	pathWebRequestDL     = "/webdl/requestdl"
)

// deliveryHeaders is the whitelist of ADR 0010 rule 18.
var deliveryHeaders = []string{"Content-Length", "Content-Range", "Content-Type", "Last-Modified", "ETag"}

// OpenDeliveryStream resolves the target through requestdl (cached) and
// opens the CDN link through the account's client. A torrent zip ignores
// Range (torbox.md §5), so Range is never forwarded for the bundle and a
// zip request takes a cdnZip token; file downloads have no rate class
// (ADR 0013 rule 2). user_ip is never sent (OQ-05).
func (g *Gateway) OpenDeliveryStream(ctx context.Context, request in.OpenDeliveryStreamRequest) (in.DeliveryStream, error) {
	rangeSupported := !request.Target.Bundle
	header := http.Header{}
	if rangeSupported && request.Range != "" {
		header.Set("Range", request.Range)
		if request.IfRange != "" {
			header.Set("If-Range", request.IfRange)
		}
	}
	var bucket *httpclient.BucketKey
	if request.Target.Bundle {
		bucket = &httpclient.BucketKey{Class: classCDNZip}
	}
	response, err := g.client.OpenLink(ctx, g.links, httpclient.LinkRequest{
		CacheKey: linkCacheKey(request.Kind, request.ProviderItemID) + targetKey(request.Target),
		Resolve:  func(ctx context.Context) (httpclient.SecretURL, error) { return g.resolveLink(ctx, request) },
		Method:   request.Method,
		Header:   header,
		Bucket:   bucket,
		MaxWait:  g.timeout,
	})
	if err != nil {
		return in.DeliveryStream{}, g.neutralOrTransport(err)
	}
	return g.deliveryStream(response, request.Range != "", rangeSupported)
}

// resolveLink asks requestdl for a CDN URL. The URL embeds the API key
// (TB-N01), so it exists only as a SecretURL.
func (g *Gateway) resolveLink(ctx context.Context, request in.OpenDeliveryStreamRequest) (httpclient.SecretURL, error) {
	query := url.Values{"token": {g.apiKey}}
	var path string
	switch request.Kind {
	case in.KindTorrent:
		path = pathTorrentRequestDL
		query.Set("torrent_id", request.ProviderItemID)
		if request.Target.Bundle {
			query.Set("zip_link", "true")
		} else {
			query.Set("file_id", request.Target.FileID)
		}
	case in.KindWeb:
		if request.Target.Bundle {
			return httpclient.SecretURL{}, in.ProviderRejectedError{Provider: g.accountID, Detail: "web items have no bundle"}
		}
		path = pathWebRequestDL
		query.Set("web_id", request.ProviderItemID)
		query.Set("file_id", request.Target.FileID)
	default:
		return httpclient.SecretURL{}, fmt.Errorf("delivering %s items is not supported by this adapter", request.Kind)
	}
	var link string
	if err := g.getData(ctx, path, query, &link); err != nil {
		return httpclient.SecretURL{}, err
	}
	secret, err := httpclient.ParseSecretURL(link)
	if err != nil {
		return httpclient.SecretURL{}, in.ProviderRejectedError{Provider: g.accountID, Detail: "requestdl returned no usable link"}
	}
	return secret, nil
}

// deliveryStream keeps 200, 206 and 416 and the whitelisted headers. A
// ranged request answered with 200 means the target did not honour Range
// (ADR 0010 rule 20). Redirects and other answers are provider-unavailable.
func (g *Gateway) deliveryStream(response *http.Response, rangeRequested, rangeSupported bool) (in.DeliveryStream, error) {
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
	if rangeRequested && rangeSupported && response.StatusCode == http.StatusOK {
		rangeSupported = false
	}
	return in.DeliveryStream{Status: response.StatusCode, Headers: headers, RangeSupported: rangeSupported, Body: response.Body}, nil
}

// neutralOrTransport keeps neutral errors from resolving and translates
// client errors.
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

func linkCacheKey(kind in.Kind, providerItemID string) string {
	return string(kind) + "/" + providerItemID + "/"
}

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
