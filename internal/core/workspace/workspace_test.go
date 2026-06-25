package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDefaultCreatesWorkspace(t *testing.T) {
	m := NewManager(t.TempDir())
	ws, err := m.EnsureDefault()
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if ws.Name != DefaultName {
		t.Errorf("name = %q, want %q", ws.Name, DefaultName)
	}
	for _, sub := range []string{"", "imports", "exports"} {
		p := filepath.Join(m.Dir(DefaultName), sub)
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			t.Errorf("missing dir %s: err=%v", p, err)
		}
	}
}

func TestEnsureDefaultIdempotent(t *testing.T) {
	m := NewManager(t.TempDir())
	if _, err := m.EnsureDefault(); err != nil {
		t.Fatalf("first EnsureDefault: %v", err)
	}
	// Mutate and persist, then ensure again — must not overwrite.
	ws, _ := m.Load(DefaultName)
	ws.CurrentDatabase = "ETLDB"
	if err := m.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := m.EnsureDefault()
	if err != nil {
		t.Fatalf("second EnsureDefault: %v", err)
	}
	if again.CurrentDatabase != "ETLDB" {
		t.Errorf("EnsureDefault overwrote existing workspace: %+v", again)
	}
}

func TestCreateRejectsDuplicate(t *testing.T) {
	m := NewManager(t.TempDir())
	if _, err := m.Create("lending"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := m.Create("lending"); err == nil {
		t.Fatal("expected error creating duplicate workspace")
	}
}

func TestRoundTrip(t *testing.T) {
	m := NewManager(t.TempDir())
	if _, err := m.Create("lending"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	ws, _ := m.Load("lending")
	ws.CurrentServer = "etl_sqlserver"
	ws.CurrentDatabase = "ETLDB"
	ws.AutoConnect = true
	if err := m.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := m.Load("lending")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != ws {
		t.Errorf("round trip = %+v, want %+v", got, ws)
	}
}

func TestList(t *testing.T) {
	m := NewManager(t.TempDir())
	for _, n := range []string{"b-work", "a-work", "default"} {
		if _, err := m.Create(n); err != nil {
			t.Fatalf("Create %q: %v", n, err)
		}
	}
	names, err := m.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"a-work", "b-work", "default"}
	if len(names) != len(want) {
		t.Fatalf("List = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("List not sorted: %v", names)
		}
	}
}

func TestRename(t *testing.T) {
	m := NewManager(t.TempDir())
	if _, err := m.Create("old"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Rename("old", "new"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if m.Exists("old") {
		t.Error("old workspace still exists after rename")
	}
	ws, err := m.Load("new")
	if err != nil {
		t.Fatalf("Load new: %v", err)
	}
	if ws.Name != "new" {
		t.Errorf("renamed workspace name = %q, want new", ws.Name)
	}
}

func TestDelete(t *testing.T) {
	m := NewManager(t.TempDir())
	if _, err := m.Create("temp"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Delete("temp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if m.Exists("temp") {
		t.Error("workspace still exists after delete")
	}
}

func TestDefaultIsProtected(t *testing.T) {
	m := NewManager(t.TempDir())
	if _, err := m.EnsureDefault(); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if err := m.Delete(DefaultName); err == nil {
		t.Error("expected error deleting default workspace")
	}
	if err := m.Rename(DefaultName, "other"); err == nil {
		t.Error("expected error renaming default workspace")
	}
}

func TestInvalidNamesRejected(t *testing.T) {
	m := NewManager(t.TempDir())
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "../escape", "x..y"} {
		if _, err := m.Create(bad); err == nil {
			t.Errorf("Create(%q) should fail", bad)
		}
	}
}

func TestLoadMissingWorkspace(t *testing.T) {
	m := NewManager(t.TempDir())
	if _, err := m.Load("nope"); err == nil {
		t.Error("expected error loading missing workspace")
	}
}
