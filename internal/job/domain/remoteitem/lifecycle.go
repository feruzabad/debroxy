package remoteitem

import (
	"fmt"
	"slices"
)

// State is a Lifecycle state of spec §5.2.
type State struct{ s string }

var (
	StateSubmitting            = State{"submitting"}
	StateQueued                = State{"queued"}
	StateResolvingMetadata     = State{"resolving_metadata"}
	StateAwaitingFileSelection = State{"awaiting_file_selection"}
	StateDownloading           = State{"downloading"}
	StateProcessing            = State{"processing"}
	StateReady                 = State{"ready"}
	StateFailed                = State{"failed"}
	StateGone                  = State{"gone"}
	StateDeleting              = State{"deleting"}
	StateDeleted               = State{"deleted"}
)

// AllStates in spec order.
var AllStates = []State{
	StateSubmitting, StateQueued, StateResolvingMetadata, StateAwaitingFileSelection, StateDownloading,
	StateProcessing, StateReady, StateFailed, StateGone, StateDeleting, StateDeleted,
}

func NewStateFromString(s string) (State, error) {
	for _, state := range AllStates {
		if state.s == s {
			return state, nil
		}
	}
	return State{}, UnknownStateError{Given: s}
}

func (s State) String() string { return s.s }
func (s State) IsZero() bool   { return s == State{} }

// activeStates hold an Active Run (spec §2).
var activeStates = []State{StateSubmitting, StateQueued, StateResolvingMetadata, StateAwaitingFileSelection, StateDownloading, StateProcessing}

// ClaimableStates accept new claims and are covered by the uniqueness
// indexes (ADR 0007 rule 1).
var ClaimableStates = append(slices.Clone(activeStates), StateReady)

func (s State) IsActive() bool    { return slices.Contains(activeStates, s) }
func (s State) IsClaimable() bool { return slices.Contains(ClaimableStates, s) }

// syncTransitions is the sync table of spec §5.2: the moves an observation
// may cause through Observe.
var syncTransitions = map[State][]State{
	StateQueued:                {StateResolvingMetadata, StateAwaitingFileSelection, StateDownloading, StateProcessing, StateReady, StateFailed},
	StateResolvingMetadata:     {StateQueued, StateAwaitingFileSelection, StateDownloading, StateProcessing, StateReady, StateFailed},
	StateAwaitingFileSelection: {StateQueued, StateDownloading, StateProcessing, StateReady, StateFailed},
	StateDownloading:           {StateQueued, StateProcessing, StateReady, StateFailed},
	StateProcessing:            {StateDownloading, StateReady, StateFailed},
	StateReady:                 {},
	StateFailed:                {},
}

// observableStates are the states a provider observation can map to.
var observableStates = []State{StateQueued, StateResolvingMetadata, StateAwaitingFileSelection, StateDownloading, StateProcessing, StateReady, StateFailed}

// allowedTransitions is every move of spec §5.2: the sync table plus the
// command transitions, each owned by one method of RemoteItem (SM-1).
var allowedTransitions = map[State][]State{
	// CompleteSubmission (observation's state), FailSubmission, DiscardSubmission, RequestDeletion.
	StateSubmitting: {StateQueued, StateResolvingMetadata, StateAwaitingFileSelection, StateDownloading, StateProcessing, StateReady, StateFailed, StateDeleted, StateDeleting},
	// Sync table plus MarkGone and RequestDeletion.
	StateQueued:                withCommands(syncTransitions[StateQueued]),
	StateResolvingMetadata:     withCommands(syncTransitions[StateResolvingMetadata]),
	StateAwaitingFileSelection: withCommands(syncTransitions[StateAwaitingFileSelection]),
	StateDownloading:           withCommands(syncTransitions[StateDownloading]),
	StateProcessing:            withCommands(syncTransitions[StateProcessing]),
	StateReady:                 withCommands(syncTransitions[StateReady]),
	// Revive (active or ready), MarkGone, RequestDeletion (with or without a provider item).
	StateFailed: {StateQueued, StateResolvingMetadata, StateAwaitingFileSelection, StateDownloading, StateProcessing, StateReady, StateGone, StateDeleting, StateDeleted},
	// Revive, RequestDeletion (no provider call).
	StateGone: {StateQueued, StateResolvingMetadata, StateAwaitingFileSelection, StateDownloading, StateProcessing, StateReady, StateDeleted},
	// ConfirmDeleted, SupersedeDeletion, FailSubmission (no provider item).
	StateDeleting: {StateDeleted},
	StateDeleted:  {},
}

func withCommands(sync []State) []State {
	return append(slices.Clone(sync), StateGone, StateDeleting)
}

// Lifecycle is the state machine value object (SM-1…SM-3).
type Lifecycle struct{ current State }

func NewLifecycle(state State) (Lifecycle, error) {
	if state.IsZero() {
		return Lifecycle{}, fmt.Errorf("empty lifecycle state")
	}
	return Lifecycle{current: state}, nil
}

func (l Lifecycle) State() State     { return l.current }
func (l Lifecycle) IsTerminal() bool { return len(allowedTransitions[l.current]) == 0 }

// TransitionTo is the only way to change state (SM-2). Staying in the same
// state is allowed and changes nothing.
func (l Lifecycle) TransitionTo(next State) (Lifecycle, error) {
	if next == l.current {
		return l, nil
	}
	if slices.Contains(allowedTransitions[l.current], next) {
		return Lifecycle{current: next}, nil
	}
	return l, InvalidTransitionError{From: l.current, To: next}
}

// IsSyncTransition reports whether an observation may move from to to.
func IsSyncTransition(from, to State) bool {
	return slices.Contains(syncTransitions[from], to)
}
