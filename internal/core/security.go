package core

import (
	"context"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// ListPrincipals returns security principals (users/roles) on the connected
// server. kind filters by adapter.PrincipalKindUser / PrincipalKindRole (or "" for
// both); substr filters by name. When the adapter does not expose security
// introspection it returns adapter.ErrUnsupported, so front-ends should gate on
// Supports(adapter.CapSecurity) first. Read-only catalog query.
func (c *Core) ListPrincipals(ctx context.Context, kind, substr string) ([]adapter.PrincipalRef, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	s, ok := c.conn.(adapter.AdapterSecurity)
	if !ok {
		return nil, adapter.ErrUnsupported
	}
	return s.ListPrincipals(ctx, kind, substr)
}

// DescribePrincipal returns a single principal's configuration (attributes, role
// membership, grants). Like ListPrincipals it depends on adapter.CapSecurity and
// returns adapter.ErrUnsupported when unavailable. Read-only.
func (c *Core) DescribePrincipal(ctx context.Context, name string) (adapter.Principal, error) {
	if c.conn == nil {
		return adapter.Principal{}, ErrNotConnected
	}
	s, ok := c.conn.(adapter.AdapterSecurity)
	if !ok {
		return adapter.Principal{}, adapter.ErrUnsupported
	}
	return s.DescribePrincipal(ctx, name)
}
