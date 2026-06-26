package core

import (
	"context"
	"testing"

	"github.com/Solifugus/mcli/internal/core/config"
)

func TestResolvePassword(t *testing.T) {
	t.Setenv("MCLI_TEST_PW", "s3cret")

	cases := []struct {
		src     string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"none", "", false},
		{"env:MCLI_TEST_PW", "s3cret", false},
		{"env:MCLI_UNSET_VAR", "", false}, // unset -> empty, not an error
		{"env:", "", true},
		{"prompt", "", true},
		{"keyring", "", true},
		{"weird", "", true},
	}
	for _, c := range cases {
		got, err := resolvePassword("test", config.Server{PasswordSource: c.src})
		if (err != nil) != c.wantErr {
			t.Errorf("resolvePassword(%q) err = %v, wantErr %v", c.src, err, c.wantErr)
		}
		if got != c.want {
			t.Errorf("resolvePassword(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

func TestConnectUnknownServer(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.Connect(context.Background(), "ghost"); err == nil {
		t.Error("expected error connecting to unknown server")
	}
}

func TestDatabaseOpsRequireConnection(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	if _, err := c.ListTables(ctx); err == nil {
		t.Error("ListTables without connection should error")
	}
	if _, err := c.RunQuery(ctx, "select 1"); err == nil {
		t.Error("RunQuery without connection should error")
	}
	if err := c.Use(ctx, "x"); err == nil {
		t.Error("Use without connection should error")
	}
	if c.Connected() {
		t.Error("Connected() should be false")
	}
}
