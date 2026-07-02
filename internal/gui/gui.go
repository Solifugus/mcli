//go:build gui

// Package gui is mcli's native front-end (design §25), the third thin client of
// internal/core alongside the TUI and the MCP server. It pulls in a CGo toolkit
// (Fyne), so the whole package sits behind the `gui` build tag and is absent
// from the default pure-Go binary; only a `-tags gui` build carries `mcli gui`.
//
// Layout note: design §25 sketches app/ browser/ grid/ editor/ connect/
// transfer/ sub-packages. They are realized here as files in one package
// instead of separate packages, because every pane binds the same window and
// the same *core.Core — splitting them into packages would force the shared App
// state through import-cycle gymnastics for no isolation benefit. The
// separation of concerns the design asks for lives at the file boundary
// (gui.go / connect.go / browser.go / editor.go / transfer.go). Per §28 the GUI
// adds no domain logic of its own: every data path calls a core method and
// inherits the core's safety guards.
package gui

import (
	"context"
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Solifugus/mcli/internal/core"
)

// App is the GUI shell. It holds the one *core.Core and wires the panes into a
// single window. Like the TUI's root model it owns no domain logic — it renders
// core state and routes user actions back to core methods.
type App struct {
	core    *core.Core
	fyneApp fyne.App
	win     fyne.Window

	browser *browserPane
	editor  *editorPane

	// status bar
	statusLabel *widget.Label
	envRect     *canvas.Rectangle
	readOnly    *widget.Check
}

// Run opens the native window and blocks until it is closed. It is the GUI's
// entry point, called from `mcli gui` (cmd/mcli/gui_run.go).
func Run(c *core.Core, version string) error {
	a := app.NewWithID("com.solifugus.mcli")
	w := a.NewWindow("mcli — " + version)

	g := &App{core: c, fyneApp: a, win: w}
	g.build()

	w.SetOnClosed(func() {
		// Mirror the TUI's clean shutdown: drop any live connection.
		_ = c.Disconnect()
	})
	w.Resize(fyne.NewSize(1180, 760))
	w.CenterOnScreen()
	w.ShowAndRun()
	return nil
}

// build assembles the window: a menu, the object browser on the left, the
// editor + result grid on the right, and an environment-colored status bar.
func (g *App) build() {
	g.browser = newBrowserPane(g)
	g.editor = newEditorPane(g)

	g.win.SetMainMenu(g.mainMenu())

	// Left: object browser (finder). Right: editor over grid.
	split := container.NewHSplit(g.browser.object(), g.editor.object())
	split.SetOffset(0.28)

	content := container.NewBorder(nil, g.statusBar(), nil, nil, split)
	g.win.SetContent(content)
	g.refreshStatus()
}

// mainMenu maps the REPL command surface to menu items. Each item calls a core
// method (directly or via a dialog) — no capability the menu offers is
// implemented anywhere but the core.
func (g *App) mainMenu() *fyne.MainMenu {
	connection := fyne.NewMenu("Connection",
		fyne.NewMenuItem("Connect…", g.showConnect),
		fyne.NewMenuItem("Disconnect", func() {
			if err := g.core.Disconnect(); err != nil {
				dialog.ShowError(err, g.win)
			}
			g.refreshStatus()
			g.browser.refresh()
		}),
	)
	data := fyne.NewMenu("Data",
		fyne.NewMenuItem("Import…", g.showImport),
		fyne.NewMenuItem("Export query results…", g.showExport),
	)
	return fyne.NewMainMenu(connection, data)
}

// statusBar builds the bottom bar: an environment color swatch, a workspace /
// server / database label, and a read-only toggle bound to the core policy.
func (g *App) statusBar() fyne.CanvasObject {
	g.envRect = canvas.NewRectangle(color.Gray{Y: 0x60})
	g.envRect.SetMinSize(fyne.NewSize(14, 14))

	g.statusLabel = widget.NewLabel("")

	g.readOnly = widget.NewCheck("read-only", func(on bool) {
		g.core.SetReadOnly(on)
	})

	left := container.NewHBox(container.NewCenter(g.envRect), g.statusLabel)
	return container.NewBorder(nil, nil, left, g.readOnly)
}

// refreshStatus repaints the status bar from live core state: which workspace,
// server, and database are current, and the environment color (§17's
// environment-colored prompt, rendered here as a swatch). Prod is red, staging
// yellow, dev/local green, disconnected grey.
func (g *App) refreshStatus() {
	ws := g.core.Current()
	server := ws.CurrentServer
	if server == "" {
		server = "(no server)"
	}
	db := ws.CurrentDatabase
	if db == "" {
		db = "(no db)"
	}
	state := "disconnected"
	if g.core.Connected() {
		state = "connected"
	}
	g.statusLabel.SetText(fmt.Sprintf("workspace %s · %s · %s · %s", ws.Name, server, db, state))

	g.envRect.FillColor = envColor(g.core.Environment(), g.core.Connected())
	g.envRect.Refresh()

	g.readOnly.SetChecked(g.core.ReadOnly())
}

// envColor picks the status swatch color for an environment label. It mirrors
// the TUI's prompt coloring so both surfaces signal danger identically.
func envColor(env string, connected bool) color.Color {
	if !connected {
		return color.Gray{Y: 0x60}
	}
	switch env {
	case "prod", "production":
		return color.NRGBA{R: 0xd4, G: 0x2c, B: 0x2c, A: 0xff}
	case "stage", "staging", "uat":
		return color.NRGBA{R: 0xd9, G: 0xa8, B: 0x27, A: 0xff}
	case "test", "qa":
		return color.NRGBA{R: 0x3b, G: 0x82, B: 0xc4, A: 0xff}
	default: // dev, local, or unlabeled
		return color.NRGBA{R: 0x3a, G: 0xa8, B: 0x55, A: 0xff}
	}
}

// onUI runs f on Fyne's UI goroutine. Background core calls (queries,
// connections) run in their own goroutines and must marshal every widget update
// back through here.
func onUI(f func()) { fyne.Do(f) }

// bgErr shows an error dialog from a background goroutine.
func (g *App) bgErr(prefix string, err error) {
	onUI(func() { dialog.ShowError(fmt.Errorf("%s: %w", prefix, err), g.win) })
}

// ctx is the background context for a core call. Cancellation wiring (a Stop
// button per running query) is a later refinement; for now each call runs to
// completion or fails.
func (g *App) ctx() context.Context { return context.Background() }
