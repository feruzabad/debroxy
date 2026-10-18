// Package in holds the driving ports of the identity context: its commands,
// queries, their results and handler interfaces (ddd-rules A-2, A-7).
package in

// Actor is the authenticated user performing an action. HTTP adapters build
// it from the common auth principal (DEP-6).
type Actor struct {
	UserID    string
	Role      string
	SessionID string
}
