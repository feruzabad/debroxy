// Package operation implements the provider operations (ddd-rules A-10).
package operation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/feruzabad/debroxy/internal/provider/domain/account"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

// accountAndGateway loads an account and its gateway. requireEnabled is
// set for operations that start new work (create, cached check, hosters);
// listing, describing and deleting keep working for a disabled account so
// existing items can still be refreshed and cleaned up.
func accountAndGateway(
	ctx context.Context, accounts out.ProviderAccountRepository, gateways out.ProviderAPIGateways,
	providerID string, requireEnabled bool,
) (*account.ProviderAccount, out.ProviderAPIGateway, error) {
	id, err := account.NewID(providerID)
	if err != nil {
		return nil, nil, account.NotFoundError{ID: providerID}
	}
	a, err := accounts.ProviderAccount(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if requireEnabled && !a.IsEnabled() {
		return nil, nil, account.DisabledError{ID: providerID}
	}
	gateway, ok := gateways[providerID]
	if !ok {
		return nil, nil, fmt.Errorf("no gateway wired for provider %s", providerID)
	}
	return a, gateway, nil
}

// recordCapabilityOutcome learns from a call that used capability c: a plan
// restriction makes it unavailable, a success makes it available
// (ADR 0003 rule 8). It returns the error the caller should report.
func recordCapabilityOutcome(
	ctx context.Context, accounts out.ProviderAccountRepository, a *account.ProviderAccount,
	c account.Capability, callErr error, now time.Time,
) error {
	switch {
	case errors.Is(callErr, out.ErrPlanRestricted):
		if err := accounts.UpdateProviderAccount(ctx, a.ID(), func(_ context.Context, stored *account.ProviderAccount) error {
			stored.RecordPlanRestriction(c, now)
			return nil
		}); err != nil {
			return errors.Join(account.CapabilityUnavailableForPlanError{Provider: a.ID().String(), Capability: c}, err)
		}
		return account.CapabilityUnavailableForPlanError{Provider: a.ID().String(), Capability: c}
	case callErr != nil:
		return callErr
	}
	if state, _ := a.CapabilityState(c); state.Status() == account.StatusAvailable {
		return nil
	}
	return accounts.UpdateProviderAccount(ctx, a.ID(), func(_ context.Context, stored *account.ProviderAccount) error {
		stored.RecordCapabilitySuccess(c, now)
		return nil
	})
}

// stamp sets debroxy's observation time, taken at request start (DEC-09b).
func stamp(observation *in.Observation, startedAt time.Time) {
	observation.ObservedAt = startedAt
}

func capabilityForSource(sourceType in.SourceType) (account.Capability, error) {
	switch sourceType {
	case in.SourceMagnet:
		return account.CapabilitySubmitMagnet, nil
	case in.SourceTorrentFile:
		return account.CapabilitySubmitTorrentFile, nil
	case in.SourceNZBFile:
		return account.CapabilitySubmitNZBFile, nil
	case in.SourceNZBURL:
		return account.CapabilitySubmitNZBURL, nil
	case in.SourceWebURL:
		return account.CapabilitySubmitWebURL, nil
	}
	return account.Capability{}, fmt.Errorf("unknown source type %q", sourceType)
}

// usableKinds are the kinds whose items the account can hold: a kind is
// listed when one of its submit capabilities is enabled and not blocked by
// the plan. Listing a kind the plan forbids would fail every refresh.
func usableKinds(a *account.ProviderAccount, now time.Time) []in.Kind {
	usable := func(capabilities ...account.Capability) bool {
		for _, c := range capabilities {
			if a.CheckCapability(c, now) == nil {
				return true
			}
		}
		return false
	}
	var kinds []in.Kind
	if usable(account.CapabilitySubmitMagnet, account.CapabilitySubmitTorrentFile) {
		kinds = append(kinds, in.KindTorrent)
	}
	if usable(account.CapabilitySubmitNZBFile, account.CapabilitySubmitNZBURL) {
		kinds = append(kinds, in.KindNZB)
	}
	if usable(account.CapabilitySubmitWebURL) {
		kinds = append(kinds, in.KindWeb)
	}
	return kinds
}

func capabilityViewsToDTO(views []account.CapabilityView) []in.CapabilityView {
	result := make([]in.CapabilityView, 0, len(views))
	for _, v := range views {
		state := ""
		if v.Supported {
			state = v.State.Status().String()
		}
		result = append(result, in.CapabilityView{Name: v.Capability.String(), Supported: v.Supported, Enabled: v.Enabled, State: state})
	}
	return result
}

func mustHave(accounts out.ProviderAccountRepository, gateways out.ProviderAPIGateways, clk interface{ Now() time.Time }) {
	switch {
	case accounts == nil:
		panic("nil accounts")
	case gateways == nil:
		panic("nil gateways")
	case clk == nil:
		panic("nil clock")
	}
}
