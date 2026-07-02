//go:build gui

package gui

import (
	"errors"

	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Solifugus/mcli/internal/core"
)

// showConnect presents the server list and connects the chosen one. It reuses
// the core's password sources exactly as the TUI does: Connect uses the
// server's configured source, and on ErrPasswordRequired (source "prompt", or a
// keyring/env miss) it falls back to a password dialog and ConnectWithPassword.
// No password handling is reimplemented here — the flow is the core's.
func (g *App) showConnect() {
	names := g.core.ServerNames()
	if len(names) == 0 {
		dialog.ShowInformation("Connect",
			"No servers are configured. Add one with the CLI (.server add) first.", g.win)
		return
	}

	sel := widget.NewSelect(names, nil)
	sel.SetSelectedIndex(0)

	form := dialog.NewForm("Connect", "Connect", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Server", sel)},
		func(ok bool) {
			if !ok || sel.Selected == "" {
				return
			}
			g.connectTo(sel.Selected)
		}, g.win)
	form.Show()
}

// connectTo runs core.Connect off the UI thread; if the core reports it needs a
// password interactively, it prompts and retries with ConnectWithPassword.
func (g *App) connectTo(name string) {
	go func() {
		err := g.core.Connect(g.ctx(), name)
		switch {
		case err == nil:
			g.afterConnect()
		case errors.Is(err, core.ErrPasswordRequired):
			onUI(func() { g.promptPassword(name) })
		default:
			g.bgErr("connect", err)
		}
	}()
}

// promptPassword asks for a password and retries the connection with it.
func (g *App) promptPassword(name string) {
	pw := widget.NewPasswordEntry()
	form := dialog.NewForm("Password for "+name, "Connect", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Password", pw)},
		func(ok bool) {
			if !ok {
				return
			}
			go func() {
				if err := g.core.ConnectWithPassword(g.ctx(), name, pw.Text); err != nil {
					g.bgErr("connect", err)
					return
				}
				g.afterConnect()
			}()
		}, g.win)
	form.Show()
}

// afterConnect refreshes every surface that reflects connection state. Safe to
// call from a background goroutine — it marshals onto the UI thread.
func (g *App) afterConnect() {
	onUI(func() {
		g.refreshStatus()
		g.browser.refresh()
	})
}
