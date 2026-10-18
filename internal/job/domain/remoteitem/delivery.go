package remoteitem

import commonerrors "github.com/feruzabad/debroxy/internal/common/errors"

// ErrTargetNotAvailable: the item is not ready, the file is not known, or
// the item offers no such target (spec §5.9).
var ErrTargetNotAvailable = commonerrors.NewConflictError("this download target is not available", "target-not-available")

// CheckDeliveryTarget applies spec §5.9: a file needs a ready item with
// per-file delivery and the file reported; a bundle needs a ready item
// with a bundle.
func CheckDeliveryTarget(state State, perFileDelivery bool, fileKnown bool, hasBundle bool, wantBundle bool) error {
	switch {
	case state != StateReady:
		return ErrTargetNotAvailable
	case wantBundle && !hasBundle:
		return ErrTargetNotAvailable
	case !wantBundle && (!perFileDelivery || !fileKnown):
		return ErrTargetNotAvailable
	}
	return nil
}
