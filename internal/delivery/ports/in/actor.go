// Package in holds the driving ports of the delivery context (ddd-rules
// A-2, A-7).
package in

// Actor is the authenticated user performing an action (DEP-6).
type Actor struct {
	UserID string
	Role   string
}
