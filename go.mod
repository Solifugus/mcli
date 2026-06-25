module github.com/Solifugus/mcli

// Go 1.25 is the floor set by Bubble Tea v2 (see docs/mcli-design.md §4).
// With GOTOOLCHAIN=auto, `go` downloads 1.25 into the user cache on demand —
// no system install or sudo required.
go 1.25.0
