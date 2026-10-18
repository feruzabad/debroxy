package realdebrid

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/feruzabad/debroxy/internal/common/httpclient"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// errorBody is Real-Debrid's error answer {error, error_code} (RD-N08).
type errorBody struct {
	Error     string `json:"error"`
	ErrorCode *int   `json:"error_code"`
}

// Error codes Real-Debrid uses (spec §6.4).
const (
	codeUnknownResource   = 7
	codeBadToken          = 8
	codePermissionDenied  = 9
	codeHosterUnsupported = 16
	codeUnavailableFile   = 24
	codeTooManyRequests   = 34
	codeDisabledEndpoint  = 37
)

// check classifies a response by its error code first and by HTTP status
// only when the body has none (ADR 0003 rule 4).
func (g *Gateway) check(response apiResponse) error {
	if response.status < 300 {
		return nil
	}
	var body errorBody
	if err := json.Unmarshal(response.body, &body); err == nil && body.ErrorCode != nil {
		return g.errorForCode(*body.ErrorCode, body.Error, response)
	}
	return g.errorForStatus(response)
}

func (g *Gateway) errorForCode(code int, name string, response apiResponse) error {
	switch code {
	case codeBadToken, codePermissionDenied:
		g.logger.Error("Real-Debrid rejected the API token; check the account credentials", "operator_alert", "credentials", "error", name)
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: "credentials rejected"}
	case codeUnknownResource:
		return in.ErrRemoteItemNotFound
	case codeHosterUnsupported:
		return in.UnsupportedHosterError{Provider: g.accountID}
	case codeUnavailableFile:
		return in.ProviderRejectedError{Provider: g.accountID, Detail: "file unavailable"}
	case codeTooManyRequests:
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: "rate limited", RetryAfter: retryAfter(response.header)}
	case codeDisabledEndpoint:
		g.logOnce("disabled:"+name, "Real-Debrid disabled an endpoint; the capability is treated as absent", "error", name)
		return in.ProviderRejectedError{Provider: g.accountID, Detail: "endpoint disabled by the provider"}
	}
	g.logOnce("code:"+strconv.Itoa(code), "unknown Real-Debrid error code", "code", code, "error", name)
	return in.ProviderRejectedError{Provider: g.accountID, Detail: truncate(name, 200)}
}

func (g *Gateway) errorForStatus(response apiResponse) error {
	switch {
	case response.status == http.StatusUnauthorized || response.status == http.StatusForbidden:
		g.logger.Error("Real-Debrid refused authentication; check the account credentials", "operator_alert", "credentials", "status", response.status)
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: "authentication refused"}
	case response.status == http.StatusTooManyRequests:
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: "rate limited", RetryAfter: retryAfter(response.header)}
	case response.status >= 500:
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: "server error " + strconv.Itoa(response.status)}
	}
	return in.ProviderRejectedError{Provider: g.accountID, Detail: "HTTP " + strconv.Itoa(response.status)}
}

func (g *Gateway) transportError(err error) error {
	var budget httpclient.BudgetExhaustedError
	var noAddress httpclient.NoAddressInFamilyError
	switch {
	case errors.As(err, &budget):
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: "rate budget exhausted", RetryAfter: budget.RetryAfter}
	case errors.Is(err, httpclient.ErrCircuitOpen):
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: "circuit open after repeated failures", RetryAfter: g.openFor}
	case errors.As(err, &noAddress):
		g.logger.Error("provider host has no address in the pinned IP family", "operator_alert", "egress", "host", noAddress.Host, "family", noAddress.Family.String())
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: noAddress.Error()}
	}
	return in.ProviderUnavailableError{Provider: g.accountID, Cause: err.Error()}
}

// createError classifies a failed add like the TorBox adapter (Stage 5
// finding S5-F4): not sent, ambiguous, or definite.
func (g *Gateway) createError(response *apiResponse, err error) error {
	if response == nil {
		var budget httpclient.BudgetExhaustedError
		var noAddress httpclient.NoAddressInFamilyError
		var opErr *net.OpError
		switch {
		case errors.As(err, &budget):
			return in.CreateNotSentError{Provider: g.accountID, RetryAfter: budget.RetryAfter}
		case errors.Is(err, httpclient.ErrCircuitOpen):
			return in.CreateNotSentError{Provider: g.accountID, RetryAfter: g.openFor}
		case errors.As(err, &noAddress):
			_ = g.transportError(err) // operator alert
			return in.CreateNotSentError{Provider: g.accountID, RetryAfter: time.Minute}
		case errors.As(err, &opErr) && opErr.Op == "dial":
			return in.CreateNotSentError{Provider: g.accountID, RetryAfter: time.Minute}
		case errors.Is(err, context.Canceled):
			return in.AmbiguousCreateError{Provider: g.accountID, Cause: "cancelled while sending"}
		}
		return in.AmbiguousCreateError{Provider: g.accountID, Cause: err.Error()}
	}
	if response.status == http.StatusTooManyRequests {
		return in.AmbiguousCreateError{Provider: g.accountID, Cause: "rate limited (429)"}
	}
	checkErr := g.check(*response)
	var unavailable in.ProviderUnavailableError
	if errors.As(checkErr, &unavailable) && response.status >= 500 {
		return in.AmbiguousCreateError{Provider: g.accountID, Cause: unavailable.Cause}
	}
	return checkErr
}

func retryAfter(header http.Header) time.Duration {
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
