//go:build db2

package adapters

// The Db2 adapter is CGo-free but still gated behind the "db2" build tag because
// it is not part of the default driver set. Build with `go build -tags db2` to
// register it. Kept in a separate tagged file so the default adapters.go stays
// pure and tag-free.
import (
	_ "github.com/Solifugus/mcli/internal/adapters/db2"
)
