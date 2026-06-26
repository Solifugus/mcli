package core

import (
	"context"
	"testing"

	"github.com/Solifugus/mcli/internal/core/config"

	// Register the real adapters so server types validate in these tests.
	_ "github.com/Solifugus/mcli/internal/adapters"
)

func TestAddEditRemoveServer(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	srv := config.Server{Type: "postgres", Environment: "dev", Host: "localhost", Port: 5432}
	if err := c.AddServer("pg", srv); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	if _, ok := c.Server("pg"); !ok {
		t.Fatal("server not stored")
	}
	// Adding the same name again fails.
	if err := c.AddServer("pg", srv); err == nil {
		t.Error("duplicate AddServer should error")
	}
	// Unknown type is rejected.
	if err := c.AddServer("bad", config.Server{Type: "nodb"}); err == nil {
		t.Error("unknown type should error")
	}
	// Missing type is rejected.
	if err := c.AddServer("bad2", config.Server{}); err == nil {
		t.Error("missing type should error")
	}

	// Edit persists changes.
	srv.Environment = "prod"
	if err := c.EditServer("pg", srv); err != nil {
		t.Fatalf("EditServer: %v", err)
	}
	if got, _ := c.Server("pg"); got.Environment != "prod" {
		t.Errorf("edit not applied: env = %q", got.Environment)
	}
	// Editing a missing server fails.
	if err := c.EditServer("ghost", srv); err == nil {
		t.Error("EditServer on missing server should error")
	}

	// Changes survive a reopen (persisted to servers.json).
	c2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got, ok := c2.Server("pg"); !ok || got.Environment != "prod" {
		t.Errorf("server did not persist: %+v ok=%v", got, ok)
	}

	// Remove deletes it.
	if err := c2.RemoveServer("pg"); err != nil {
		t.Fatalf("RemoveServer: %v", err)
	}
	if _, ok := c2.Server("pg"); ok {
		t.Error("server still present after remove")
	}
	if err := c2.RemoveServer("pg"); err == nil {
		t.Error("removing missing server should error")
	}
}

func TestTestServerUnknown(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.TestServer(context.Background(), "ghost"); err == nil {
		t.Error("TestServer on unknown server should error")
	}
}
