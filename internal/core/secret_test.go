package core

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/Solifugus/mcli/internal/core/config"

	_ "github.com/Solifugus/mcli/internal/adapters"
)

func TestKeyringRoundTrip(t *testing.T) {
	keyring.MockInit() // in-memory keyring for the test
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.AddServer("pg", config.Server{Type: "postgres", PasswordSource: "keyring"}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	// Before storing, a keyring source resolves to ErrPasswordRequired (miss).
	if _, err := resolvePassword("pg", c.servers.Servers["pg"]); !errors.Is(err, ErrPasswordRequired) {
		t.Errorf("keyring miss err = %v, want ErrPasswordRequired", err)
	}

	if err := c.SetServerPassword("pg", "s3cret"); err != nil {
		t.Fatalf("SetServerPassword: %v", err)
	}
	got, err := resolvePassword("pg", c.servers.Servers["pg"])
	if err != nil {
		t.Fatalf("resolvePassword after set: %v", err)
	}
	if got != "s3cret" {
		t.Errorf("resolved keyring secret = %q, want s3cret", got)
	}

	if err := c.DeleteServerPassword("pg"); err != nil {
		t.Fatalf("DeleteServerPassword: %v", err)
	}
	if _, err := resolvePassword("pg", c.servers.Servers["pg"]); !errors.Is(err, ErrPasswordRequired) {
		t.Errorf("after delete err = %v, want ErrPasswordRequired", err)
	}
	// Deleting a missing secret is not an error.
	if err := c.DeleteServerPassword("pg"); err != nil {
		t.Errorf("delete of missing secret should be a no-op, got %v", err)
	}
}

func TestPromptSourceNeedsPassword(t *testing.T) {
	if _, err := resolvePassword("x", config.Server{PasswordSource: "prompt"}); !errors.Is(err, ErrPasswordRequired) {
		t.Errorf("prompt source err = %v, want ErrPasswordRequired", err)
	}
}

func TestSetPasswordUnknownServer(t *testing.T) {
	keyring.MockInit()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.SetServerPassword("ghost", "x"); err == nil {
		t.Error("SetServerPassword on unknown server should error")
	}
}
