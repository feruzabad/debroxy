package remoteitem

import (
	"fmt"
	"strings"
	"unicode/utf8"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

// maxDisplayNameLength is the longest a display name may be, in Unicode
// characters (spec §10.3, C14-03).
const maxDisplayNameLength = 255

// NormalizeDisplayName trims a display name and enforces the length limit.
// An empty result is valid here: SubmitJob derives the name from the source
// (spec §5.5), while Rename rejects it.
func NormalizeDisplayName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if n := utf8.RuneCountInString(trimmed); n > maxDisplayNameLength {
		return "", InvalidDisplayNameError{Length: n}
	}
	return trimmed, nil
}

// InvalidDisplayNameError: a display name that is empty after trimming
// (rename) or longer than 255 Unicode characters (submit or rename)
// (spec §10.3, C14-03).
type InvalidDisplayNameError struct {
	// Length is the rune count of the offending name; 0 means it was empty.
	Length int
}

func (e InvalidDisplayNameError) Error() string {
	if e.Length == 0 {
		return "display name must not be empty"
	}
	return fmt.Sprintf("display name has %d characters; at most %d are allowed", e.Length, maxDisplayNameLength)
}
func (e InvalidDisplayNameError) Slug() string { return "invalid-display-name" }
func (e InvalidDisplayNameError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeIncorrectInput
}
