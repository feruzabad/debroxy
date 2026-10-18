package in

// Actor is the authenticated user performing an action. HTTP adapters build
// it from the common auth principal (DEP-6).
type Actor struct {
	UserID string
	Role   string
}
