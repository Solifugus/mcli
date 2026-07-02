package core

import (
	"context"
	"errors"
	"testing"
)

func TestSourceOpsRequireConnection(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	if _, err := c.Source(ctx, "v"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Source without connection = %v, want ErrNotConnected", err)
	}
	if _, err := c.SearchRoutines(ctx, "x"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("SearchRoutines without connection = %v, want ErrNotConnected", err)
	}
}
