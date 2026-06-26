package core

import (
	"testing"

	"github.com/Solifugus/mcli/internal/core/safety"
)

func TestGuardStatementUsesSettings(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Default settings confirm dangerous SQL; not connected, so env is "".
	if act, _, _ := c.GuardStatement("drop table t"); act != safety.Confirm {
		t.Errorf("dangerous with default settings = %v, want Confirm", act)
	}
	if act, _, _ := c.GuardStatement("select 1"); act != safety.Allow {
		t.Errorf("read = %v, want Allow", act)
	}
}

func TestReadOnlyToggleBlocksWrites(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if c.ReadOnly() {
		t.Fatal("read-only should default off")
	}
	c.SetReadOnly(true)
	if !c.ReadOnly() {
		t.Fatal("SetReadOnly(true) did not engage")
	}
	if act, _, _ := c.GuardStatement("insert into t values (1)"); act != safety.Block {
		t.Errorf("write under read-only = %v, want Block", act)
	}
	if act, _, _ := c.GuardStatement("select 1"); act != safety.Allow {
		t.Errorf("read under read-only = %v, want Allow", act)
	}
	c.SetReadOnly(false)
	if act, _, _ := c.GuardStatement("insert into t values (1)"); act == safety.Block {
		t.Error("write after read-only off should not Block")
	}
}

func TestReadOnlySeededFromSettings(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := c.Settings()
	s.ReadOnly = true
	if err := c.cfg.SaveSettings(s); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	c2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !c2.ReadOnly() {
		t.Error("read-only should be seeded from settings on Open")
	}
}
