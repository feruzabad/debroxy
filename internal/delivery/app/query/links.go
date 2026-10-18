package query

import (
	"net/url"
	"strings"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/delivery/domain/signedlink"
	"github.com/feruzabad/debroxy/internal/delivery/ports/in"
	"github.com/feruzabad/debroxy/internal/delivery/ports/out"
)

// LinkConfig holds the Signed Link settings (spec §7.1).
type LinkConfig struct {
	// PublicBaseURL is the external base URL, e.g. https://debroxy.example.org.
	PublicBaseURL string
	TTL           time.Duration
	Keys          []signedlink.Key
}

// linker signs links for resolved targets.
type linker struct {
	signer  signedlink.Signer
	clock   clock.Clock
	baseURL string
	ttl     time.Duration
}

func newLinker(config LinkConfig, clk clock.Clock) (linker, error) {
	if clk == nil {
		panic("nil clock")
	}
	signer, err := signedlink.NewSigner(config.Keys)
	if err != nil {
		return linker{}, err
	}
	return linker{signer: signer, clock: clk, baseURL: strings.TrimSuffix(config.PublicBaseURL, "/"), ttl: config.TTL}, nil
}

func (l linker) link(userID, jobID string, target out.Target) (in.SignedLinkResult, error) {
	linkTarget := signedlink.BundleTarget()
	if !target.Bundle {
		var err error
		if linkTarget, err = signedlink.FileTarget(target.FileID); err != nil {
			return in.SignedLinkResult{}, err
		}
	}
	expiresAt := l.clock.Now().Add(l.ttl).Truncate(time.Second)
	token, err := l.signer.Sign(userID, jobID, linkTarget, expiresAt)
	if err != nil {
		return in.SignedLinkResult{}, err
	}
	name := signedlink.SanitizeName("", target.Name)
	return in.SignedLinkResult{
		URL: l.baseURL + "/dl/" + token + "/" + url.PathEscape(name), ExpiresAt: expiresAt,
		RangeSupported: target.RangeSupported, FileID: target.FileID, Bundle: target.Bundle,
	}, nil
}
