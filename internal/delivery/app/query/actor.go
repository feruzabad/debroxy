// Package query implements the delivery queries.
package query

import (
	"errors"
	"time"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/delivery/ports/in"
)

var errNoActor = errors.New("actor without user ID")

func actorID(actor in.Actor) (string, error) {
	if actor.UserID == "" {
		return "", errNoActor
	}
	return actor.UserID, nil
}

// TooManyStreamsError: the user's or the global stream cap is reached (DEC-18).
type TooManyStreamsError struct{ Global bool }

// streamsRetryAfter is the fixed hint clients should wait before retrying a
// stream-capped download (ADR 0020).
const streamsRetryAfter = 5 * time.Second

func (e TooManyStreamsError) Error() string {
	if e.Global {
		return "too many downloads are running; try again later"
	}
	return "you have too many downloads running; finish one first"
}
func (e TooManyStreamsError) Slug() string { return "too-many-streams" }
func (e TooManyStreamsError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeServiceUnavailable
}
func (e TooManyStreamsError) RetryAfter() time.Duration { return streamsRetryAfter }

// errProviderUnavailable: the upstream could not be opened (ADR 0010 rule 14).
var errProviderUnavailable = commonerrors.NewProviderUnavailableError("the provider could not deliver the file; try again later", "provider-unavailable")
