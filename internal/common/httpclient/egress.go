package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// Family is the IP family all connections of a client use (ADR 0012).
type Family struct{ network string }

var (
	FamilyIPv4 = Family{"tcp4"}
	FamilyIPv6 = Family{"tcp6"}
)

func NewFamilyFromString(s string) (Family, error) {
	switch s {
	case "ipv4":
		return FamilyIPv4, nil
	case "ipv6":
		return FamilyIPv6, nil
	}
	return Family{}, fmt.Errorf("unknown IP family %q; use ipv4 or ipv6", s)
}

func (f Family) String() string {
	if f == FamilyIPv6 {
		return "ipv6"
	}
	return "ipv4"
}

func (f Family) IsZero() bool { return f == Family{} }

// Egress pins outbound connections: always the family's network, never
// the other one, optionally from one local address.
type Egress struct {
	Family       Family
	LocalAddress netip.Addr // zero: chosen by the operating system
}

func (e Egress) validate() error {
	if e.Family.IsZero() {
		return errors.New("egress family not set")
	}
	if !e.LocalAddress.IsValid() {
		return nil
	}
	if (e.Family == FamilyIPv4) != e.LocalAddress.Unmap().Is4() {
		return fmt.Errorf("local address %s is not an %s address", e.LocalAddress, e.Family)
	}
	return nil
}

// pinnedDialer dials only the pinned family. Go then uses only resolved
// addresses of that family and never falls back to the other.
type pinnedDialer struct {
	egress Egress
	dialer net.Dialer
}

func newPinnedDialer(egress Egress) pinnedDialer {
	d := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if egress.LocalAddress.IsValid() {
		d.LocalAddr = &net.TCPAddr{IP: egress.LocalAddress.AsSlice()}
	}
	return pinnedDialer{egress: egress, dialer: d}
}

func (p pinnedDialer) DialContext(ctx context.Context, _ string, address string) (net.Conn, error) {
	conn, err := p.dialer.DialContext(ctx, p.egress.Family.network, address)
	if err != nil {
		host, _, _ := net.SplitHostPort(address)
		if isNoAddressInFamily(err) {
			return nil, NoAddressInFamilyError{Host: host, Family: p.egress.Family}
		}
		return nil, err
	}
	return conn, nil
}

// NoAddressInFamilyError: the host has no address in the pinned family. The
// call is never retried over the other family (ADR 0012 rule 7); operators
// should be alerted.
type NoAddressInFamilyError struct {
	Host   string
	Family Family
}

func (e NoAddressInFamilyError) Error() string {
	return fmt.Sprintf("host %s has no %s address (egress is pinned to %s)", e.Host, e.Family, e.Family)
}

func isNoAddressInFamily(err error) bool {
	var addrErr *net.AddrError
	return errors.As(err, &addrErr) && addrErr.Err == "no suitable address found"
}

// CheckLocalAddress reports whether addr is assigned to a local interface
// (ADR 0012 rule 6).
func CheckLocalAddress(addr netip.Addr) error {
	interfaceAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return fmt.Errorf("list interface addresses: %w", err)
	}
	for _, interfaceAddr := range interfaceAddrs {
		prefix, err := netip.ParsePrefix(interfaceAddr.String())
		if err == nil && prefix.Addr().Unmap() == addr.Unmap() {
			return nil
		}
	}
	return fmt.Errorf("local address %s is not assigned to any interface", addr)
}
