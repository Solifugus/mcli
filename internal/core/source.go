package core

import (
	"context"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// Source returns the definition text of a view, procedure, or function on the
// connected server. When the adapter does not implement source retrieval it
// returns adapter.ErrUnsupported, so front-ends should gate on
// Supports(adapter.CapSource) first. It is a read-only catalog query.
func (c *Core) Source(ctx context.Context, name string) (adapter.ObjectSource, error) {
	if c.conn == nil {
		return adapter.ObjectSource{}, ErrNotConnected
	}
	src, ok := c.conn.(adapter.AdapterSource)
	if !ok {
		return adapter.ObjectSource{}, adapter.ErrUnsupported
	}
	return src.Source(ctx, name)
}

// SearchRoutines returns procedures and functions whose name or body contains
// text (case-insensitive). Like Source it depends on adapter.CapSource and
// returns adapter.ErrUnsupported when unavailable. Read-only.
func (c *Core) SearchRoutines(ctx context.Context, text string) ([]adapter.ObjectRef, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	src, ok := c.conn.(adapter.AdapterSource)
	if !ok {
		return nil, adapter.ErrUnsupported
	}
	return src.SearchRoutines(ctx, text)
}
