package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/Solifugus/mcli/internal/adapters/postgres"
)

// TestLiveLintPostgres is a gated check of LiveLint against the local gbasic
// Postgres (see the test-postgres-db memory). Run with:
//
//	MCLI_PG_LIVE=1 go test ./internal/core/ -run TestLiveLintPostgres -v
func TestLiveLintPostgres(t *testing.T) {
	if os.Getenv("MCLI_PG_LIVE") == "" {
		t.Skip("set MCLI_PG_LIVE=1 to run the live Postgres lint test")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	c, err := Open(filepath.Join(home, ".mcli"))
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	ctx := context.Background()
	if err := c.Connect(ctx, "gbasic"); err != nil {
		t.Fatalf("connect gbasic: %v", err)
	}
	defer c.Disconnect()

	// A query against a table that does not exist: static lint is clean, but the
	// live check must catch the unknown relation.
	bad := "select * from no_such_table_xyz"
	if fs := c.Lint(bad); len(fs) != 0 {
		// SELECT * is a style finding; that's fine, just log it.
		t.Logf("static findings for bad query: %+v", fs)
	}
	live, err := c.LiveLint(ctx, bad)
	if err != nil {
		t.Fatalf("LiveLint: %v", err)
	}
	if len(live) == 0 {
		t.Fatal("expected a live-validation finding for an unknown table")
	}
	t.Logf("live finding: %s", live[0].Message)

	// A valid query must produce no live findings.
	good, err := c.LiveLint(ctx, "select 1 as one")
	if err != nil {
		t.Fatalf("LiveLint(good): %v", err)
	}
	if len(good) != 0 {
		t.Fatalf("valid query produced live findings: %+v", good)
	}
}
