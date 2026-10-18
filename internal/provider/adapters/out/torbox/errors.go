package torbox

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/feruzabad/debroxy/internal/common/httpclient"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

// envelope is TorBox's usual answer {success, error, detail, data}.
type envelope struct {
	Success *bool           `json:"success"`
	Error   *string         `json:"error"`
	Detail  json.RawMessage `json:"detail"`
	Data    json.RawMessage `json:"data"`
}

var errorCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)

// Error codes TorBox uses (torbox.md T02, spec §6.4).
const (
	codeAuth           = "AUTH_ERROR"
	codePlanRestricted = "PLAN_RESTRICTED_FEATURE"
	codeUnsupportedURL = "UNSUPPORTED_SITE"
	codeNotFound       = "ITEM_NOT_FOUND"
	codeDatabase       = "DATABASE_ERROR"
)

// decode classifies a response by its body first and by HTTP status only
// when no body can be classified (ADR 0003 rule 4). On success it returns
// the envelope's data.
func (g *Gateway) decode(response apiResponse) (json.RawMessage, error) {
	var env envelope
	if err := json.Unmarshal(response.body, &env); err == nil && env.Success != nil {
		code := ""
		if env.Error != nil {
			code = *env.Error
		}
		detail := detailText(env.Detail)
		// Some 500 answers swap error and detail (torbox.md T02 d).
		if !errorCode.MatchString(code) && errorCode.MatchString(detail) {
			code, detail = detail, code
		}
		if code == "" && *env.Success && response.status < 300 {
			return env.Data, nil
		}
		return nil, g.errorForCode(code, detail, response)
	}
	return nil, g.errorForStatus(response)
}

func (g *Gateway) errorForCode(code, detail string, response apiResponse) error {
	switch code {
	case codeAuth:
		g.logger.Error("TorBox rejected the API key; check the account credentials", "operator_alert", "credentials")
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: "credentials rejected"}
	case codePlanRestricted:
		return out.ErrPlanRestricted
	case codeUnsupportedURL:
		return in.UnsupportedHosterError{Provider: g.accountID}
	case codeNotFound:
		return in.ErrRemoteItemNotFound
	case codeDatabase:
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: "provider database error"}
	case "":
		return g.errorForStatus(response)
	}
	g.logOnce("code:"+code, "unknown TorBox error code", "code", code)
	return in.ProviderRejectedError{Provider: g.accountID, Detail: truncate(detail, 200)}
}

// errorForStatus handles answers without an envelope: FastAPI bodies
// ({"detail": ...}), rate limiting and server errors.
func (g *Gateway) errorForStatus(response apiResponse) error {
	switch {
	case response.status == http.StatusUnauthorized || response.status == http.StatusForbidden:
		g.logger.Error("TorBox refused authentication; check the account credentials", "operator_alert", "credentials", "status", response.status)
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: "authentication refused"}
	case response.status == http.StatusUnprocessableEntity:
		g.logger.Error("TorBox rejected a request as malformed; this is an adapter bug", "body", truncate(string(response.body), 300))
		return in.ProviderRejectedError{Provider: g.accountID, Detail: "request rejected as malformed"}
	case response.status == http.StatusTooManyRequests:
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: "rate limited", RetryAfter: retryAfter(response.header)}
	case response.status >= 500:
		return in.ProviderUnavailableError{Provider: g.accountID, Cause: "server error " + strconv.Itoa(response.status)}
	case response.status >= 300:
		return in.ProviderRejectedError{Provider: g.accountID, Detail: "HTTP " + strconv.Itoa(response.status)}
	}
	return in.ProviderRejectedError{Provider: g.accountID, Detail: "unexpected answer"}
}

// transportError translates errors where no response arrived.
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

// createError classifies a failed create (spec §5.5 step 4, ADR 0013 rule 8):
// not sent, ambiguous, or a definite failure.
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
	_, decodeErr := g.decode(*response)
	var unavailable in.ProviderUnavailableError
	if errors.As(decodeErr, &unavailable) && response.status >= 500 {
		// A server error without a definite code may have created the item.
		return in.AmbiguousCreateError{Provider: g.accountID, Cause: unavailable.Cause}
	}
	return decodeErr
}

func detailText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return string(raw)
}

func retryAfter(header http.Header) time.Duration {
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
