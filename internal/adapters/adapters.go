// Package adapters blank-imports the default-build database adapters so that
// importing this one package registers all of them. CGo-only adapters (e.g. a
// DB2 build) live behind build tags and are added in tagged files, never here,
// so the default cross-platform build stays pure Go. See docs/mcli-design.md §6.
package adapters

import (
	_ "github.com/Solifugus/mcli/internal/adapters/postgres"
)
