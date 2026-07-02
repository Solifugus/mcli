package core

import (
	"context"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// ListJobs returns scheduled jobs on the connected server whose name contains
// substr. When the adapter does not expose a scheduler it returns
// adapter.ErrUnsupported, so front-ends should gate on Supports(adapter.CapJobs)
// first. Read-only catalog query.
func (c *Core) ListJobs(ctx context.Context, substr string) ([]adapter.JobRef, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	j, ok := c.conn.(adapter.AdapterJobs)
	if !ok {
		return nil, adapter.ErrUnsupported
	}
	return j.ListJobs(ctx, substr)
}

// DescribeJob returns the design of a single scheduled job. Like ListJobs it
// depends on adapter.CapJobs and returns adapter.ErrUnsupported when unavailable.
// Read-only.
func (c *Core) DescribeJob(ctx context.Context, name string) (adapter.Job, error) {
	if c.conn == nil {
		return adapter.Job{}, ErrNotConnected
	}
	j, ok := c.conn.(adapter.AdapterJobs)
	if !ok {
		return adapter.Job{}, adapter.ErrUnsupported
	}
	return j.DescribeJob(ctx, name)
}

// JobHistory returns recent run records for a job, newest first, up to limit
// (limit <= 0 = adapter default). Like ListJobs it depends on adapter.CapJobs and
// returns adapter.ErrUnsupported when unavailable. Read-only.
func (c *Core) JobHistory(ctx context.Context, name string, limit int) ([]adapter.JobRun, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	j, ok := c.conn.(adapter.AdapterJobs)
	if !ok {
		return nil, adapter.ErrUnsupported
	}
	return j.JobHistory(ctx, name, limit)
}
