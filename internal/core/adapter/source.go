package adapter

import "context"

// ObjectSource is an object's definition text — the CREATE / body a user reads or
// edits. It is returned by AdapterSource.Source for definition-bearing objects
// (views, procedures, functions). Tables carry no stored definition; use
// DescribeObject for their design instead.
type ObjectSource struct {
	Ref      ObjectRef
	Language string // best-effort ("sql", "plpgsql", "tsql", ...); "" if unknown
	Body     string // the definition / CREATE text
}

// AdapterSource is the optional interface for source retrieval and body search
// (design §22.1, §29). An adapter that implements it must also advertise
// CapSource from Capabilities(); the core probes for the interface and returns
// ErrUnsupported when an adapter does not implement it, so a front-end that
// checks Supports(CapSource) first never reaches an unsupported call.
type AdapterSource interface {
	// Source returns the definition text of a view, procedure, or function. The
	// name may be schema-qualified ("schema.name"). It returns ErrUnsupported for
	// object kinds that have no stored definition (e.g. tables — use
	// DescribeObject), and a not-found error when nothing matches.
	Source(ctx context.Context, name string) (ObjectSource, error)

	// SearchRoutines returns procedures and functions whose name or body contains
	// text (case-insensitive; empty = all). It is the Processing-area analog of
	// SearchViews, which greps view definitions.
	SearchRoutines(ctx context.Context, text string) ([]ObjectRef, error)
}
