package command

import (
	"fmt"
	"math"
	"time"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

// errWrongCurrentPassword is not invalid-credentials: the actor is logged
// in, and a 401 would make clients drop their session.
var errWrongCurrentPassword = commonerrors.NewIncorrectInputError("the current password is wrong", "wrong-current-password")

// LoginThrottledError: too many failed logins for this username or client IP (DEC-05).
type LoginThrottledError struct {
	Wait time.Duration
}

func (e LoginThrottledError) Error() string {
	return fmt.Sprintf("too many failed logins; retry in %d seconds", int(math.Ceil(e.Wait.Seconds())))
}
func (e LoginThrottledError) Slug() string { return "login-throttled" }
func (e LoginThrottledError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeQuotaExceeded
}
func (e LoginThrottledError) RetryAfter() time.Duration { return e.Wait }
