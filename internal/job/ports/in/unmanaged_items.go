package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// UnmanagedItems lists provider items debroxy does not track (admin only, DEC-10).
type UnmanagedItems struct {
	Actor Actor
}

type UnmanagedItemsHandler decorator.QueryHandler[UnmanagedItems, []UnmanagedItemView]
