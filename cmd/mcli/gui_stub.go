//go:build !gui

package main

import "errors"

// runGUI is the pure-Go stub. The native GUI pulls in a CGo toolkit (Fyne), so
// it lives behind the `gui` build tag and is absent from the default binary —
// keeping `go build ./cmd/mcli` pure-Go and cross-compilable. See design §25.
func runGUI() error {
	return errors.New("this build has no GUI support; rebuild with -tags gui (needs a C toolchain)")
}
