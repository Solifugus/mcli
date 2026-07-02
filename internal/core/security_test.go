package core

import (
	"context"
	"errors"
	"testing"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func TestSecurityOpsRequireConnection(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	if _, err := c.ListPrincipals(ctx, adapter.PrincipalKindUser, ""); !errors.Is(err, ErrNotConnected) {
		t.Errorf("ListPrincipals without connection = %v, want ErrNotConnected", err)
	}
	if _, err := c.DescribePrincipal(ctx, "bob"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("DescribePrincipal without connection = %v, want ErrNotConnected", err)
	}
}
