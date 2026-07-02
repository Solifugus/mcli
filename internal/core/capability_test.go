package core

import (
	"context"
	"errors"
	"testing"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func TestCapabilitiesEmptyWhenDisconnected(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if caps := c.Capabilities(); len(caps) != 0 {
		t.Errorf("disconnected Capabilities should be empty, got %v", caps)
	}
	if c.Supports(adapter.CapExplain) {
		t.Error("disconnected Supports(CapExplain) should be false")
	}
}

func TestCapabilityOpsRequireConnection(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	if _, err := c.Explain(ctx, "select 1"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Explain without connection = %v, want ErrNotConnected", err)
	}
	if _, err := c.PreLineage(ctx, "t"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("PreLineage without connection = %v, want ErrNotConnected", err)
	}
	if _, err := c.PostLineage(ctx, "t"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("PostLineage without connection = %v, want ErrNotConnected", err)
	}
}
