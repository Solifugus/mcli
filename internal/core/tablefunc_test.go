package core

import (
	"context"
	"errors"
	"testing"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func TestSearchTableFunctionsRequiresConnection(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := c.SearchTableFunctions(context.Background(), "x"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("SearchTableFunctions without connection = %v, want ErrNotConnected", err)
	}
}

func TestTabularQueryUsesDialect(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Disconnected → empty dialect → the generic form is fine and never panics.
	ref := adapter.ObjectRef{Schema: "s", Name: "t", Type: string(adapter.KindTable)}
	if got := c.TabularQuery(ref); got != "SELECT * FROM s.t" {
		t.Errorf("TabularQuery(table) = %q", got)
	}
}
