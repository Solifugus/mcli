package core

import (
	"errors"
	"testing"

	"github.com/Solifugus/mcli/internal/core/safety"
)

func TestSecurityEditRequiresConnection(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := c.BuildGrant([]string{"SELECT"}, "t", "bob", false); !errors.Is(err, ErrNotConnected) {
		t.Errorf("BuildGrant without connection = %v, want ErrNotConnected", err)
	}
	if _, err := c.BuildCreateUser("bob", "pw"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("BuildCreateUser without connection = %v, want ErrNotConnected", err)
	}
	if _, err := c.BuildDropUser("bob"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("BuildDropUser without connection = %v, want ErrNotConnected", err)
	}
}

// TestGeneratedDCLIsGuarded documents the contract that makes routing generated
// DCL through the normal execution path safe: the safety classifier treats a
// GRANT/CREATE as a non-read write (so read-only mode blocks it) and a DROP as
// dangerous (so it is confirmed / blocked on production).
func TestGeneratedDCLIsGuarded(t *testing.T) {
	if v := safety.Classify("GRANT SELECT ON t TO bob", nil); v.ReadOnly {
		t.Error("GRANT must not be classified read-only (read-only mode must block it)")
	}
	if v := safety.Classify("CREATE USER bob PASSWORD 'x'", nil); v.ReadOnly {
		t.Error("CREATE USER must not be classified read-only")
	}
	if v := safety.Classify("DROP ROLE bob", nil); !v.Dangerous {
		t.Error("DROP must be classified dangerous (confirmed / blocked on prod)")
	}
}
