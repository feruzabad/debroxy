package account

import "fmt"

// ID identifies a Provider Account (config `providers[].id`).
type ID struct{ s string }

func NewID(s string) (ID, error) {
	if s == "" {
		return ID{}, fmt.Errorf("empty provider ID")
	}
	return ID{s: s}, nil
}

func (id ID) String() string { return id.s }
func (id ID) IsZero() bool   { return id == ID{} }

// Kind is the provider type (spec §2).
type Kind struct{ s string }

var (
	KindTorBox     = Kind{"torbox"}
	KindRealDebrid = Kind{"realdebrid"}
)

var allKinds = []Kind{KindTorBox, KindRealDebrid}

func NewKindFromString(s string) (Kind, error) {
	for _, k := range allKinds {
		if k.s == s {
			return k, nil
		}
	}
	return Kind{}, fmt.Errorf("unknown provider kind %q", s)
}

func (k Kind) String() string { return k.s }
func (k Kind) IsZero() bool   { return k == Kind{} }

// Capability is a Kind Capability (spec §6.3).
type Capability struct{ s string }

var (
	CapabilitySubmitMagnet      = Capability{"SubmitMagnet"}
	CapabilitySubmitTorrentFile = Capability{"SubmitTorrentFile"}
	CapabilitySubmitNZBFile     = Capability{"SubmitNZBFile"}
	CapabilitySubmitNZBURL      = Capability{"SubmitNZBURL"}
	CapabilitySubmitWebURL      = Capability{"SubmitWebURL"}
	CapabilityPerFileDelivery   = Capability{"PerFileDelivery"}
	CapabilityBundleDelivery    = Capability{"BundleDelivery"}
	CapabilityCachedCheck       = Capability{"CachedCheck"}
	CapabilityWebhooks          = Capability{"Webhooks"}
	CapabilityProviderDedup     = Capability{"ProviderDedup"}
)

// AllCapabilities is the order in which capabilities are listed.
var AllCapabilities = []Capability{
	CapabilitySubmitMagnet, CapabilitySubmitTorrentFile, CapabilitySubmitNZBFile, CapabilitySubmitNZBURL,
	CapabilitySubmitWebURL, CapabilityPerFileDelivery, CapabilityBundleDelivery, CapabilityCachedCheck,
	CapabilityWebhooks, CapabilityProviderDedup,
}

func NewCapabilityFromString(s string) (Capability, error) {
	for _, c := range AllCapabilities {
		if c.s == s {
			return c, nil
		}
	}
	return Capability{}, fmt.Errorf("unknown capability %q", s)
}

func (c Capability) String() string { return c.s }
func (c Capability) IsZero() bool   { return c == Capability{} }

// Status of a capability for one account (spec §2 Capability State).
type Status struct{ s string }

var (
	StatusAvailable   = Status{"available"}
	StatusUnavailable = Status{"unavailable"}
	StatusUnverified  = Status{"unverified"}
)

func NewStatusFromString(s string) (Status, error) {
	for _, st := range []Status{StatusAvailable, StatusUnavailable, StatusUnverified} {
		if st.s == s {
			return st, nil
		}
	}
	return Status{}, fmt.Errorf("unknown capability status %q", s)
}

func (s Status) String() string { return s.s }
func (s Status) IsZero() bool   { return s == Status{} }
