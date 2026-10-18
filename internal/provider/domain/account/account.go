// Package account is the Provider Account aggregate (spec §6.1): one
// configured account of a provider and the capability state learned for it.
// Everything except the capability states comes from configuration.
package account

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// notEnabledInV1 lists capabilities a provider has but debroxy v1 does not
// offer (spec §6.3, C8-01, DEC-16).
func notEnabledInV1(kind Kind, c Capability) bool {
	switch c {
	case CapabilitySubmitNZBFile, CapabilitySubmitNZBURL:
		return true
	case CapabilitySubmitWebURL:
		return kind == KindRealDebrid
	}
	return false
}

// initialState: usenet capabilities start unverified, all others available
// (ADR 0003 rule 8). Stored states override it.
func initialState(c Capability) CapabilityState {
	if c == CapabilitySubmitNZBFile || c == CapabilitySubmitNZBURL {
		return Unverified()
	}
	return Available(time.Time{})
}

// ProviderAccount is one configured account of a provider.
type ProviderAccount struct {
	id                   ID
	kind                 Kind
	displayName          string
	enabled              bool
	webhooksEnabled      bool
	kindCapabilities     []Capability
	disabledCapabilities []Capability
	states               map[Capability]CapabilityState
}

// Definition is the configured part of an account. KindCapabilities is the
// static table of the provider kind (ADR 0003 rule 7), supplied by its adapter.
type Definition struct {
	ID                   ID
	Kind                 Kind
	DisplayName          string
	Enabled              bool
	WebhooksEnabled      bool
	KindCapabilities     []Capability
	DisabledCapabilities []Capability
}

// NewProviderAccount checks the definition. A disabled capability the kind
// does not have, or webhooks on a kind without them, fail startup (spec §11).
func NewProviderAccount(def Definition, stored map[Capability]CapabilityState) (*ProviderAccount, error) {
	switch {
	case def.ID.IsZero():
		return nil, errors.New("empty provider ID")
	case def.Kind.IsZero():
		return nil, errors.New("empty provider kind")
	case len(def.KindCapabilities) == 0:
		return nil, fmt.Errorf("provider %s: kind %s has no capabilities", def.ID, def.Kind)
	}
	for _, c := range def.DisabledCapabilities {
		if !slices.Contains(def.KindCapabilities, c) {
			return nil, fmt.Errorf("provider %s: cannot disable %s, which kind %s does not have", def.ID, c, def.Kind)
		}
	}
	if def.WebhooksEnabled && !slices.Contains(def.KindCapabilities, CapabilityWebhooks) {
		return nil, fmt.Errorf("provider %s: kind %s offers no webhooks", def.ID, def.Kind)
	}
	a := &ProviderAccount{
		id:                   def.ID,
		kind:                 def.Kind,
		displayName:          def.DisplayName,
		enabled:              def.Enabled,
		webhooksEnabled:      def.WebhooksEnabled,
		kindCapabilities:     slices.Clone(def.KindCapabilities),
		disabledCapabilities: slices.Clone(def.DisabledCapabilities),
		states:               map[Capability]CapabilityState{},
	}
	for _, c := range a.kindCapabilities {
		a.states[c] = initialState(c)
		if s, ok := stored[c]; ok {
			a.states[c] = s
		}
	}
	return a, nil
}

func (a *ProviderAccount) ID() ID              { return a.id }
func (a *ProviderAccount) Kind() Kind          { return a.kind }
func (a *ProviderAccount) DisplayName() string { return a.displayName }
func (a *ProviderAccount) IsEnabled() bool     { return a.enabled }

// Clone returns an independent copy, for repositories that keep accounts in memory.
func (a *ProviderAccount) Clone() *ProviderAccount {
	copied := *a
	copied.kindCapabilities = slices.Clone(a.kindCapabilities)
	copied.disabledCapabilities = slices.Clone(a.disabledCapabilities)
	copied.states = make(map[Capability]CapabilityState, len(a.states))
	for c, s := range a.states {
		copied.states[c] = s
	}
	return &copied
}

// HasCapability: the provider kind offers c at all.
func (a *ProviderAccount) HasCapability(c Capability) bool {
	return slices.Contains(a.kindCapabilities, c)
}

// IsCapabilityEnabled: the kind has c and neither debroxy v1 nor the
// operator disabled it.
func (a *ProviderAccount) IsCapabilityEnabled(c Capability) bool {
	return a.HasCapability(c) && !notEnabledInV1(a.kind, c) && !slices.Contains(a.disabledCapabilities, c)
}

// CapabilityState returns the learned state; ok is false when the kind lacks c.
func (a *ProviderAccount) CapabilityState(c Capability) (CapabilityState, bool) {
	s, ok := a.states[c]
	return s, ok
}

// CheckCapability applies ADR 0003 rule 7 in order: not supported, not
// enabled (the account state is not consulted), unavailable for the plan.
// An unavailable state older than UnavailableRetryAfter lets one call test
// it again.
func (a *ProviderAccount) CheckCapability(c Capability, now time.Time) error {
	if !a.HasCapability(c) {
		return CapabilityNotSupportedError{Provider: a.id.String(), Capability: c}
	}
	if !a.IsCapabilityEnabled(c) {
		return CapabilityNotEnabledError{Provider: a.id.String(), Capability: c}
	}
	if a.states[c].blocksAt(now) {
		return CapabilityUnavailableForPlanError{Provider: a.id.String(), Capability: c}
	}
	return nil
}

// WebhooksAccepted: the kind has webhooks and the operator enabled them.
func (a *ProviderAccount) WebhooksAccepted() bool {
	return a.webhooksEnabled && a.HasCapability(CapabilityWebhooks)
}

// RecordCapabilitySuccess marks c available after a successful call. It
// reports whether the state changed and must be stored.
func (a *ProviderAccount) RecordCapabilitySuccess(c Capability, now time.Time) bool {
	current, ok := a.states[c]
	if !ok || current.status == StatusAvailable {
		return false
	}
	a.states[c] = Available(now)
	return true
}

// RecordPlanRestriction marks c unavailable{plan}. A repeated restriction
// after the retry period restarts the period. It reports whether the state
// changed.
func (a *ProviderAccount) RecordPlanRestriction(c Capability, now time.Time) bool {
	current, ok := a.states[c]
	if !ok {
		return false
	}
	if current.status == StatusUnavailable && current.reason == ReasonPlan && current.blocksAt(now) {
		return false
	}
	a.states[c] = Unavailable(ReasonPlan, now)
	return true
}

// AllowCapabilityProbes lets one call test every unavailable capability
// again (the admin operation ProbeCapabilities, ADR 0003 rule 8). It
// returns the capabilities whose state changed.
func (a *ProviderAccount) AllowCapabilityProbes() []Capability {
	var changed []Capability
	for _, c := range a.kindCapabilities {
		if a.states[c].status == StatusUnavailable {
			a.states[c] = Unverified()
			changed = append(changed, c)
		}
	}
	return changed
}

// CapabilityView is one row of the capability list shown to clients.
type CapabilityView struct {
	Capability Capability
	Supported  bool
	Enabled    bool
	State      CapabilityState
}

// Capabilities lists every known capability with its support, enablement
// and learned state.
func (a *ProviderAccount) Capabilities() []CapabilityView {
	views := make([]CapabilityView, 0, len(AllCapabilities))
	for _, c := range AllCapabilities {
		state, supported := a.states[c]
		views = append(views, CapabilityView{
			Capability: c, Supported: supported, Enabled: a.IsCapabilityEnabled(c), State: state,
		})
	}
	return views
}
