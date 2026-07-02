package adapter

import "context"

// AdapterTableFunctions is the optional interface for discovering table-valued
// functions — functions that return a rowset and can therefore be read as a
// tabular data source (design §29, Data area). An adapter that implements it must
// advertise CapTableFunctions. Not every engine has TVFs (MySQL has none), so
// this stays optional; the core probes for it and returns ErrUnsupported when
// absent.
type AdapterTableFunctions interface {
	// SearchTableFunctions returns table-valued functions whose name contains
	// substr (case-insensitive; empty = all), each with Type == KindTableFunction.
	SearchTableFunctions(ctx context.Context, substr string) ([]ObjectRef, error)
}

// TabularQuery builds a ready-to-run (or ready-to-edit) SELECT that reads the
// given tabular object as rows. Tables and views select directly; a table
// function uses the dialect's invocation syntax and leaves an argument
// placeholder for the user to fill. It is a pure string builder — no connection
// needed — so both front-ends and tests share one source of truth for how each
// dialect is queried.
func TabularQuery(d Dialect, ref ObjectRef) string {
	name := ref.Name
	if ref.Schema != "" {
		name = ref.Schema + "." + ref.Name
	}
	if ObjectKind(ref.Type) != KindTableFunction {
		return "SELECT * FROM " + name
	}
	switch d {
	case DialectOracle, DialectDB2:
		// Pipelined / table functions are read through TABLE(...).
		return "SELECT * FROM TABLE(" + name + "(/* args */))"
	default:
		// Postgres, T-SQL inline TVFs, and the generic case call the function
		// directly in the FROM clause.
		return "SELECT * FROM " + name + "(/* args */)"
	}
}
