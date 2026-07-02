//go:build gui

package gui

import (
	"fmt"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// kindToggle pairs a type checkbox with the ObjectKind it filters on.
type kindToggle struct {
	kind  adapter.ObjectKind
	check *widget.Check
}

// browserPane is the object finder the user asked for (§27): a row of type
// checkboxes (Tables / Views / Procedures / Functions) plus a search box, over
// a result list. Every query is core.SearchObjects — the same typed finder the
// TUI's .objects command and the MCP search_objects tool call.
type browserPane struct {
	app *App

	toggles []kindToggle
	search  *widget.Entry
	count   *widget.Label
	list    *widget.List

	results  []adapter.ObjectRef
	lastPick *adapter.ObjectRef // most recent selection (Fyne List exposes none)
	root     fyne.CanvasObject
}

func newBrowserPane(a *App) *browserPane {
	b := &browserPane{app: a}

	// One checkbox per kind, all on by default (empty selection would mean "all"
	// to the core, but showing them checked makes the filter legible). Callbacks
	// are attached *after* every widget exists (see below): SetChecked fires
	// OnChanged, which runs a search that touches count/list, so those must be
	// built first.
	for _, k := range adapter.AllObjectKinds() {
		kk := k
		chk := widget.NewCheck(kindLabel(kk), nil)
		chk.SetChecked(true)
		b.toggles = append(b.toggles, kindToggle{kind: kk, check: chk})
	}

	b.search = widget.NewEntry()
	b.search.SetPlaceHolder("filter by name…")

	b.count = widget.NewLabel("")

	b.list = widget.NewList(
		func() int { return len(b.results) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id < len(b.results) {
				o.(*widget.Label).SetText(formatRef(b.results[id]))
			}
		},
	)
	b.list.OnSelected = func(id widget.ListItemID) {
		if id < len(b.results) {
			ref := b.results[id]
			b.lastPick = &ref
			b.onPick(ref)
		}
	}

	checks := container.NewHBox()
	for _, t := range b.toggles {
		checks.Add(t.check)
	}

	// Selecting a row drops "SELECT * FROM <obj>" into the editor; the button
	// describes it. Both are core calls (query path / core.Describe).
	describeBtn := widget.NewButton("Describe", b.describeSelected)

	header := container.NewVBox(
		widget.NewLabelWithStyle("Objects", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		checks,
		b.search,
		container.NewBorder(nil, nil, nil, describeBtn, b.count),
	)
	b.root = container.NewBorder(header, nil, nil, nil, b.list)

	// Now that count/list exist, wire the change handlers and paint the initial
	// (disconnected) hint.
	for i := range b.toggles {
		b.toggles[i].check.OnChanged = func(bool) { b.runSearch() }
	}
	b.search.OnChanged = func(string) { b.runSearch() }
	b.runSearch()
	return b
}

func (b *browserPane) object() fyne.CanvasObject { return b.root }

// selectedKinds returns the checked kinds. If none are checked we pass the
// explicit full set rather than an empty slice, so an all-unchecked panel shows
// nothing (matching the checkboxes) instead of the core's empty=all default.
func (b *browserPane) selectedKinds() []adapter.ObjectKind {
	var ks []adapter.ObjectKind
	for _, t := range b.toggles {
		if t.check.Checked {
			ks = append(ks, t.kind)
		}
	}
	return ks
}

// runSearch re-queries the core with the current checkboxes + substring. It runs
// off the UI thread and repaints the list when done. Not connected → empty list
// with a hint, never a modal (the panel updates on every keystroke).
func (b *browserPane) runSearch() {
	if !b.app.core.Connected() {
		b.setResults(nil, "connect to a server to browse objects")
		return
	}
	kinds := b.selectedKinds()
	if len(kinds) == 0 {
		b.setResults(nil, "no object types selected")
		return
	}
	substr := strings.TrimSpace(b.search.Text)
	go func() {
		refs, err := b.app.core.SearchObjects(b.app.ctx(), kinds, substr)
		if err != nil {
			onUI(func() { b.setResults(nil, "error: "+err.Error()) })
			return
		}
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].Schema != refs[j].Schema {
				return refs[i].Schema < refs[j].Schema
			}
			return refs[i].Name < refs[j].Name
		})
		onUI(func() { b.setResults(refs, fmt.Sprintf("%d object(s)", len(refs))) })
	}()
}

func (b *browserPane) setResults(refs []adapter.ObjectRef, status string) {
	b.results = refs
	b.count.SetText(status)
	b.list.UnselectAll()
	b.list.Refresh()
}

// refresh re-runs the current filter — called after connect/disconnect/use.
func (b *browserPane) refresh() { b.runSearch() }

// onPick stages a SELECT for tables/views in the editor. Procedures/functions
// aren't directly selectable, so picking one just describes it.
func (b *browserPane) onPick(ref adapter.ObjectRef) {
	switch adapter.ObjectKind(ref.Type) {
	case adapter.KindTable, adapter.KindView:
		b.app.editor.setText("SELECT * FROM " + qualify(ref) + "\n")
	default:
		b.describe(ref)
	}
}

func (b *browserPane) describeSelected() {
	if b.lastPick == nil {
		dialog.ShowInformation("Describe", "Select an object first.", b.app.win)
		return
	}
	b.describe(*b.lastPick)
}

// describe shows an object's columns in a dialog via core.Describe.
func (b *browserPane) describe(ref adapter.ObjectRef) {
	go func() {
		det, err := b.app.core.Describe(b.app.ctx(), qualify(ref))
		if err != nil {
			b.app.bgErr("describe", err)
			return
		}
		onUI(func() { b.showDescribe(det) })
	}()
}

func (b *browserPane) showDescribe(det adapter.ObjectDetail) {
	rows := make([]string, 0, len(det.Columns))
	for _, c := range det.Columns {
		null := "NOT NULL"
		if c.Nullable {
			null = "NULL"
		}
		key := ""
		if c.Key != "" {
			key = "  " + c.Key
		}
		rows = append(rows, fmt.Sprintf("%-28s %-18s %s%s", c.Name, c.DataType, null, key))
	}
	body := widget.NewLabel(strings.Join(rows, "\n"))
	body.TextStyle = fyne.TextStyle{Monospace: true}
	d := dialog.NewCustom(qualify(det.Ref), "Close",
		container.NewVScroll(body), b.app.win)
	d.Resize(fyne.NewSize(560, 420))
	d.Show()
}

func kindLabel(k adapter.ObjectKind) string {
	switch k {
	case adapter.KindTable:
		return "Tables"
	case adapter.KindView:
		return "Views"
	case adapter.KindProcedure:
		return "Procedures"
	case adapter.KindFunction:
		return "Functions"
	default:
		s := string(k)
		if s == "" {
			return "?"
		}
		return strings.ToUpper(s[:1]) + s[1:] + "s"
	}
}

func formatRef(r adapter.ObjectRef) string {
	return fmt.Sprintf("%s   (%s)", qualify(r), r.Type)
}

// qualify renders a schema-qualified name, omitting an empty schema.
func qualify(r adapter.ObjectRef) string {
	if r.Schema == "" {
		return r.Name
	}
	return r.Schema + "." + r.Name
}
