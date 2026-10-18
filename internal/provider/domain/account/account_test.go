package account_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/provider/domain/account"
)

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

var torboxCapabilities = []account.Capability{
	account.CapabilitySubmitMagnet, account.CapabilitySubmitTorrentFile, account.CapabilitySubmitNZBFile,
	account.CapabilitySubmitNZBURL, account.CapabilitySubmitWebURL, account.CapabilityPerFileDelivery,
	account.CapabilityBundleDelivery, account.CapabilityCachedCheck, account.CapabilityWebhooks,
	account.CapabilityProviderDedup,
}

var realDebridCapabilities = []account.Capability{
	account.CapabilitySubmitMagnet, account.CapabilitySubmitTorrentFile, account.CapabilitySubmitWebURL,
	account.CapabilityPerFileDelivery, account.CapabilityBundleDelivery,
}

func newAccount(t *testing.T, kind account.Kind, capabilities []account.Capability, disabled []account.Capability, stored map[account.Capability]account.CapabilityState) *account.ProviderAccount {
	t.Helper()
	id, err := account.NewID(kind.String())
	require.NoError(t, err)
	a, err := account.NewProviderAccount(account.Definition{
		ID: id, Kind: kind, DisplayName: kind.String(), Enabled: true,
		KindCapabilities: capabilities, DisabledCapabilities: disabled,
	}, stored)
	require.NoError(t, err)
	return a
}

func TestProviderAccount_CheckCapability_order(t *testing.T) {
	t.Parallel()
	plan := map[account.Capability]account.CapabilityState{
		account.CapabilityCachedCheck:   account.Unavailable(account.ReasonPlan, now.Add(-time.Hour)),
		account.CapabilitySubmitNZBFile: account.Unavailable(account.ReasonPlan, now.Add(-time.Hour)),
	}
	torbox := newAccount(t, account.KindTorBox, torboxCapabilities, []account.Capability{account.CapabilityWebhooks}, plan)
	realDebrid := newAccount(t, account.KindRealDebrid, realDebridCapabilities, nil, nil)

	testCases := []struct {
		name       string
		account    *account.ProviderAccount
		capability account.Capability
		want       error
	}{
		{"supported_and_available", torbox, account.CapabilitySubmitMagnet, nil},
		{"kind_lacks_it", realDebrid, account.CapabilityCachedCheck, account.CapabilityNotSupportedError{Provider: "realdebrid", Capability: account.CapabilityCachedCheck}},
		{"usenet_not_enabled_in_v1_whatever_the_state", torbox, account.CapabilitySubmitNZBFile, account.CapabilityNotEnabledError{Provider: "torbox", Capability: account.CapabilitySubmitNZBFile}},
		{"realdebrid_web_not_enabled", realDebrid, account.CapabilitySubmitWebURL, account.CapabilityNotEnabledError{Provider: "realdebrid", Capability: account.CapabilitySubmitWebURL}},
		{"operator_disabled", torbox, account.CapabilityWebhooks, account.CapabilityNotEnabledError{Provider: "torbox", Capability: account.CapabilityWebhooks}},
		{"plan_restricted", torbox, account.CapabilityCachedCheck, account.CapabilityUnavailableForPlanError{Provider: "torbox", Capability: account.CapabilityCachedCheck}},
		{"torbox_web_starts_available", torbox, account.CapabilitySubmitWebURL, nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.account.CheckCapability(tc.capability, now))
		})
	}
}

func TestProviderAccount_unavailable_is_retried_after_24_hours(t *testing.T) {
	t.Parallel()
	a := newAccount(t, account.KindTorBox, torboxCapabilities, nil, map[account.Capability]account.CapabilityState{
		account.CapabilityCachedCheck: account.Unavailable(account.ReasonPlan, now),
	})

	assert.Error(t, a.CheckCapability(account.CapabilityCachedCheck, now.Add(23*time.Hour)))
	assert.NoError(t, a.CheckCapability(account.CapabilityCachedCheck, now.Add(24*time.Hour)), "one call may test it again")

	assert.True(t, a.RecordPlanRestriction(account.CapabilityCachedCheck, now.Add(24*time.Hour)), "a new restriction restarts the period")
	assert.Error(t, a.CheckCapability(account.CapabilityCachedCheck, now.Add(25*time.Hour)))
}

func TestProviderAccount_learning(t *testing.T) {
	t.Parallel()
	a := newAccount(t, account.KindTorBox, torboxCapabilities, nil, nil)

	state, _ := a.CapabilityState(account.CapabilitySubmitNZBFile)
	assert.Equal(t, account.StatusUnverified, state.Status(), "usenet starts unverified")
	state, _ = a.CapabilityState(account.CapabilitySubmitMagnet)
	assert.Equal(t, account.StatusAvailable, state.Status())

	assert.False(t, a.RecordCapabilitySuccess(account.CapabilitySubmitMagnet, now), "already available: nothing to store")
	assert.True(t, a.RecordPlanRestriction(account.CapabilityCachedCheck, now))
	assert.False(t, a.RecordPlanRestriction(account.CapabilityCachedCheck, now.Add(time.Minute)), "unchanged within the period")
	assert.True(t, a.RecordCapabilitySuccess(account.CapabilityCachedCheck, now.Add(time.Hour)))
	state, _ = a.CapabilityState(account.CapabilityCachedCheck)
	assert.Equal(t, account.Available(now.Add(time.Hour)), state)
}

func TestProviderAccount_AllowCapabilityProbes(t *testing.T) {
	t.Parallel()
	a := newAccount(t, account.KindTorBox, torboxCapabilities, nil, map[account.Capability]account.CapabilityState{
		account.CapabilityCachedCheck: account.Unavailable(account.ReasonPlan, now),
	})

	changed := a.AllowCapabilityProbes()

	assert.Equal(t, []account.Capability{account.CapabilityCachedCheck}, changed)
	assert.NoError(t, a.CheckCapability(account.CapabilityCachedCheck, now))
}

func TestNewProviderAccount_rejects_impossible_configuration(t *testing.T) {
	t.Parallel()
	id, err := account.NewID("realdebrid")
	require.NoError(t, err)

	_, err = account.NewProviderAccount(account.Definition{
		ID: id, Kind: account.KindRealDebrid, KindCapabilities: realDebridCapabilities,
		DisabledCapabilities: []account.Capability{account.CapabilityCachedCheck},
	}, nil)
	assert.ErrorContains(t, err, "cannot disable CachedCheck")

	_, err = account.NewProviderAccount(account.Definition{
		ID: id, Kind: account.KindRealDebrid, KindCapabilities: realDebridCapabilities, WebhooksEnabled: true,
	}, nil)
	assert.ErrorContains(t, err, "offers no webhooks")
}

func TestProviderAccount_Capabilities_lists_all_in_order(t *testing.T) {
	t.Parallel()
	a := newAccount(t, account.KindRealDebrid, realDebridCapabilities, nil, nil)

	views := a.Capabilities()

	require.Len(t, views, len(account.AllCapabilities))
	assert.Equal(t, account.CapabilityView{Capability: account.CapabilitySubmitMagnet, Supported: true, Enabled: true, State: account.Available(time.Time{})}, views[0])
	assert.Equal(t, account.CapabilityView{Capability: account.CapabilitySubmitWebURL, Supported: true, Enabled: false, State: account.Available(time.Time{})}, views[4])
	assert.Equal(t, account.CapabilityView{Capability: account.CapabilityCachedCheck, Supported: false, Enabled: false}, views[7])
}

func TestProviderAccount_Clone_is_independent(t *testing.T) {
	t.Parallel()
	a := newAccount(t, account.KindTorBox, torboxCapabilities, nil, nil)

	clone := a.Clone()
	clone.RecordPlanRestriction(account.CapabilityCachedCheck, now)

	assert.NoError(t, a.CheckCapability(account.CapabilityCachedCheck, now))
}
