//go:build gui

package gui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/Solifugus/mcli/internal/core"
	"github.com/Solifugus/mcli/internal/core/adapter"
)

// newTestApp builds an App backed by a real (unconnected) core and Fyne's
// headless test driver, so pane construction and synchronous rendering can be
// exercised without a display or a database.
func newTestApp(t *testing.T) *App {
	t.Helper()
	c, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := test.NewApp()
	w := a.NewWindow("test")
	g := &App{core: c, fyneApp: a, win: w}
	g.build()
	return g
}

func TestEnvColor(t *testing.T) {
	// Disconnected is always grey regardless of label.
	if _, ok := envColor("prod", false).(color.Gray); !ok {
		t.Error("disconnected should be grey")
	}
	// Prod connected is the red danger color; dev is not.
	prod := envColor("prod", true)
	dev := envColor("dev", true)
	if prod == dev {
		t.Error("prod and dev must render as different colors")
	}
	r, _, _, _ := prod.RGBA()
	if r < 0x8000 {
		t.Errorf("prod color should be strongly red, got R=%x", r>>8)
	}
}

func TestIsQuery(t *testing.T) {
	cases := map[string]bool{
		"select 1":           true,
		"  SELECT * FROM t":  true,
		"with x as (…)":      true,
		"explain select 1":   true,
		"insert into t v(1)": false,
		"update t set a=1":   false,
		"delete from t":      false,
		"create table t(a)":  false,
		"selects_are_not":    false, // must be a keyword boundary, not a prefix
	}
	for sql, want := range cases {
		if got := isQuery(sql); got != want {
			t.Errorf("isQuery(%q) = %v, want %v", sql, got, want)
		}
	}
}

func TestToStrings(t *testing.T) {
	got := toStrings([]any{nil, 42, "x"})
	want := []string{"NULL", "42", "x"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("toStrings[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestColumnWidths(t *testing.T) {
	m := gridModel{
		cols: []string{"id", "a_very_long_column_name"},
		rows: [][]string{{"1", "short"}, {"2", "x"}},
	}
	w := columnWidths(m)
	if len(w) != 2 {
		t.Fatalf("want 2 widths, got %d", len(w))
	}
	// The wide header should size its column wider than the narrow "id" column,
	// and every width is clamped to the [min,max] band.
	if w[1] <= w[0] {
		t.Errorf("wide column should be wider: %v", w)
	}
	for i, px := range w {
		if px < 60 || px > 420 {
			t.Errorf("width[%d]=%v outside clamp band", i, px)
		}
	}
}

func TestFinderSelectedKinds(t *testing.T) {
	g := newTestApp(t)
	b := g.browser

	// All four kinds checked by default.
	if got := len(b.selectedKinds()); got != len(adapter.AllObjectKinds()) {
		t.Fatalf("default selectedKinds = %d, want %d", got, len(adapter.AllObjectKinds()))
	}

	// Unchecking Tables drops it from the query set.
	b.toggles[0].check.SetChecked(false)
	for _, k := range b.selectedKinds() {
		if k == adapter.KindTable {
			t.Error("KindTable should be excluded after unchecking Tables")
		}
	}
	if got, want := len(b.selectedKinds()), len(adapter.AllObjectKinds())-1; got != want {
		t.Errorf("after uncheck, selectedKinds = %d, want %d", got, want)
	}
}

func TestFinderNotConnectedHint(t *testing.T) {
	g := newTestApp(t)
	// With no connection, searching must not call the adapter; it shows a hint
	// and leaves the result list empty.
	g.browser.runSearch()
	if g.browser.count.Text == "" || len(g.browser.results) != 0 {
		t.Errorf("expected empty results + a hint when disconnected, got %q / %d rows",
			g.browser.count.Text, len(g.browser.results))
	}
}

func TestEditorSetModelAndText(t *testing.T) {
	g := newTestApp(t)
	e := g.editor

	g.browser.onPick(adapter.ObjectRef{Schema: "public", Name: "users", Type: string(adapter.KindTable)})
	if e.entry.Text != "SELECT * FROM public.users\n" {
		t.Errorf("picking a table should stage a SELECT, got %q", e.entry.Text)
	}

	e.setModel(gridModel{cols: []string{"a", "b"}, rows: [][]string{{"1", "2"}}})
	if len(e.model.cols) != 2 || len(e.model.rows) != 1 {
		t.Errorf("grid model not applied: %+v", e.model)
	}
}

func TestStatusReflectsDisconnected(t *testing.T) {
	g := newTestApp(t)
	g.refreshStatus()
	if g.statusLabel.Text == "" {
		t.Error("status label should be populated")
	}
	if g.readOnly.Checked != g.core.ReadOnly() {
		t.Error("read-only checkbox should track core policy")
	}
}
