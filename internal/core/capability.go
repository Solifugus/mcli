package core

import (
	"context"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// Capabilities reports the optional features the connected adapter supports.
// When disconnected it returns an empty set — nothing is available — so a
// front-end can uniformly decide what to offer (the GUI's area dropdown, the
// CLI's .caps, the MCP capabilities tool) without special-casing the no-conn
// state. A capability reflects engine ability, not the current login's grants;
// a call can still fail with adapter.ErrUnauthorized. See design §22.
func (c *Core) Capabilities() adapter.CapabilitySet {
	if c.conn == nil {
		return adapter.CapabilitySet{}
	}
	return c.conn.Capabilities()
}

// Supports is a convenience over Capabilities for a single feature.
func (c *Core) Supports(cap adapter.Capability) bool {
	return c.Capabilities().Has(cap)
}

// Explain returns the execution plan for a query. When the connected engine has
// no EXPLAIN it returns adapter.ErrUnsupported; front-ends should gate on
// Supports(adapter.CapExplain) first. It is a read-only operation, so no safety
// guard applies.
func (c *Core) Explain(ctx context.Context, sql string) (adapter.Plan, error) {
	if c.conn == nil {
		return adapter.Plan{}, ErrNotConnected
	}
	return c.conn.ExplainQuery(ctx, sql)
}

// PreLineage returns the objects the named object depends on (its inputs), one
// hop only. For the assembled multi-level graph use Lineage; this single-hop
// accessor is what that walk is built on. Gate on Supports(CapLineage).
func (c *Core) PreLineage(ctx context.Context, name string) ([]adapter.ObjectRef, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	return c.conn.GetPreLineage(ctx, name)
}

// PostLineage returns the objects that depend on the named object (its
// consumers). See PreLineage.
func (c *Core) PostLineage(ctx context.Context, name string) ([]adapter.ObjectRef, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	return c.conn.GetPostLineage(ctx, name)
}
