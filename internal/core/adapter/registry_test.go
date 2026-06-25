package adapter

import (
	"context"
	"testing"
)

// fakeAdapter is a no-op adapter used to exercise the registry.
type fakeAdapter struct{}

func (fakeAdapter) Connect(context.Context, ConnectParams) error          { return nil }
func (fakeAdapter) Disconnect() error                                     { return nil }
func (fakeAdapter) ListDatabases(context.Context) ([]string, error)       { return nil, nil }
func (fakeAdapter) UseDatabase(context.Context, string) error             { return nil }
func (fakeAdapter) ListSchemas(context.Context) ([]string, error)         { return nil, nil }
func (fakeAdapter) ListTables(context.Context) ([]ObjectRef, error)       { return nil, nil }
func (fakeAdapter) ListViews(context.Context) ([]ObjectRef, error)        { return nil, nil }
func (fakeAdapter) DescribeObject(context.Context, string) (ObjectDetail, error) {
	return ObjectDetail{}, nil
}
func (fakeAdapter) RunQuery(context.Context, string) (RowStream, error) { return nil, nil }
func (fakeAdapter) RunStatement(context.Context, string) (Result, error) {
	return Result{}, nil
}
func (fakeAdapter) ExplainQuery(context.Context, string) (Plan, error)    { return Plan{}, nil }
func (fakeAdapter) SearchColumns(context.Context, string) ([]ColumnRef, error) { return nil, nil }
func (fakeAdapter) SearchViews(context.Context, string) ([]ObjectRef, error)   { return nil, nil }
func (fakeAdapter) GetPreLineage(context.Context, string) ([]ObjectRef, error) { return nil, nil }
func (fakeAdapter) GetPostLineage(context.Context, string) ([]ObjectRef, error) {
	return nil, nil
}
func (fakeAdapter) Dialect() Dialect { return DialectGenericSQL }

func TestRegisterAndNew(t *testing.T) {
	Register("fake", func() Adapter { return fakeAdapter{} })

	if !Registered("fake") {
		t.Fatal("fake not registered")
	}
	a, err := New("fake")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Dialect() != DialectGenericSQL {
		t.Errorf("dialect = %q", a.Dialect())
	}
}

func TestNewUnknownType(t *testing.T) {
	if _, err := New("nope"); err == nil {
		t.Error("expected error for unknown type")
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	Register("dup", func() Adapter { return fakeAdapter{} })
	defer func() {
		if recover() == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	Register("dup", func() Adapter { return fakeAdapter{} })
}
