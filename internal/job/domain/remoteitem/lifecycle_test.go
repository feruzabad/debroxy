package remoteitem_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
)

var (
	sSubmitting = remoteitem.StateSubmitting
	sQueued     = remoteitem.StateQueued
	sResolving  = remoteitem.StateResolvingMetadata
	sAwaiting   = remoteitem.StateAwaitingFileSelection
	sDownload   = remoteitem.StateDownloading
	sProcessing = remoteitem.StateProcessing
	sReady      = remoteitem.StateReady
	sFailed     = remoteitem.StateFailed
	sGone       = remoteitem.StateGone
	sDeleting   = remoteitem.StateDeleting
	sDeleted    = remoteitem.StateDeleted
)

// allowed mirrors spec §5.2: sync table plus command transitions (SM-4).
var allowed = map[remoteitem.State][]remoteitem.State{
	sSubmitting: {sQueued, sResolving, sAwaiting, sDownload, sProcessing, sReady, sFailed, sDeleted, sDeleting},
	sQueued:     {sResolving, sAwaiting, sDownload, sProcessing, sReady, sFailed, sGone, sDeleting},
	sResolving:  {sQueued, sAwaiting, sDownload, sProcessing, sReady, sFailed, sGone, sDeleting},
	sAwaiting:   {sQueued, sDownload, sProcessing, sReady, sFailed, sGone, sDeleting},
	sDownload:   {sQueued, sProcessing, sReady, sFailed, sGone, sDeleting},
	sProcessing: {sDownload, sReady, sFailed, sGone, sDeleting},
	sReady:      {sGone, sDeleting},
	sFailed:     {sQueued, sResolving, sAwaiting, sDownload, sProcessing, sReady, sGone, sDeleting, sDeleted},
	sGone:       {sQueued, sResolving, sAwaiting, sDownload, sProcessing, sReady, sDeleted},
	sDeleting:   {sDeleted},
	sDeleted:    {},
}

// sync is the sync table of spec §5.2.
var sync = map[remoteitem.State][]remoteitem.State{
	sQueued:     {sResolving, sAwaiting, sDownload, sProcessing, sReady, sFailed},
	sResolving:  {sQueued, sAwaiting, sDownload, sProcessing, sReady, sFailed},
	sAwaiting:   {sQueued, sDownload, sProcessing, sReady, sFailed},
	sDownload:   {sQueued, sProcessing, sReady, sFailed},
	sProcessing: {sDownload, sReady, sFailed},
}

func TestLifecycle_TransitionTo_every_pair(t *testing.T) {
	t.Parallel()

	for _, from := range remoteitem.AllStates {
		for _, to := range remoteitem.AllStates {
			t.Run(fmt.Sprintf("%s_to_%s", from, to), func(t *testing.T) {
				t.Parallel()
				lifecycle, err := remoteitem.NewLifecycle(from)
				assert.NoError(t, err)

				next, err := lifecycle.TransitionTo(to)

				switch {
				case from == to:
					assert.NoError(t, err)
				case slices.Contains(allowed[from], to):
					assert.NoError(t, err)
					assert.Equal(t, to, next.State())
				default:
					assert.Equal(t, remoteitem.InvalidTransitionError{From: from, To: to}, err)
					assert.Equal(t, from, next.State())
				}
				assert.Equal(t, slices.Contains(sync[from], to), remoteitem.IsSyncTransition(from, to))
			})
		}
	}
}

func TestLifecycle_only_deleted_is_terminal(t *testing.T) {
	t.Parallel()

	for _, state := range remoteitem.AllStates {
		lifecycle, err := remoteitem.NewLifecycle(state)
		assert.NoError(t, err)
		assert.Equal(t, state == sDeleted, lifecycle.IsTerminal(), state.String())
	}
}

func TestState_classes(t *testing.T) {
	t.Parallel()

	for _, state := range remoteitem.AllStates {
		active := slices.Contains([]remoteitem.State{sSubmitting, sQueued, sResolving, sAwaiting, sDownload, sProcessing}, state)
		assert.Equal(t, active, state.IsActive(), state.String())
		assert.Equal(t, active || state == sReady, state.IsClaimable(), state.String())
	}
}
