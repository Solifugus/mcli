package core

import (
	"context"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// SearchTableFunctions returns table-valued functions on the connected server
// whose name contains substr. When the adapter does not implement table-function
// discovery it returns adapter.ErrUnsupported, so front-ends should gate on
// Supports(adapter.CapTableFunctions) first. Read-only catalog query.
func (c *Core) SearchTableFunctions(ctx context.Context, substr string) ([]adapter.ObjectRef, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	tf, ok := c.conn.(adapter.AdapterTableFunctions)
	if !ok {
		return nil, adapter.ErrUnsupported
	}
	return tf.SearchTableFunctions(ctx, substr)
}

// TabularQuery builds the dialect-correct SELECT that reads a tabular object
// (table, view, or table function) as rows, using the connected server's
// dialect. It is pure and always available (no connection required beyond the
// dialect, which is "" when disconnected — yielding the generic form).
func (c *Core) TabularQuery(ref adapter.ObjectRef) string {
	return adapter.TabularQuery(c.dialect, ref)
}
