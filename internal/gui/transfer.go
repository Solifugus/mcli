//go:build gui

package gui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// browseRow lays a path entry beside its Browse button.
func browseRow(entry, btn fyne.CanvasObject) fyne.CanvasObject {
	return container.NewBorder(nil, nil, nil, btn, entry)
}

// showImport loads a file into a table via core.ImportFile. The GUI only
// gathers the source path, target table, and (for spreadsheets) a sheet name;
// the parsing, type handling, and safety all live in core/transfer.
func (g *App) showImport() {
	if !g.core.Connected() {
		dialog.ShowInformation("Import", "Connect to a server first.", g.win)
		return
	}

	path := widget.NewEntry()
	path.SetPlaceHolder("/path/to/data.csv")
	browse := widget.NewButton("Browse…", func() {
		dialog.ShowFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil || rc == nil {
				return
			}
			path.SetText(rc.URI().Path())
			_ = rc.Close()
		}, g.win)
	})
	table := widget.NewEntry()
	table.SetPlaceHolder("target table")
	sheet := widget.NewEntry()
	sheet.SetPlaceHolder("sheet (spreadsheets only, optional)")

	form := dialog.NewForm("Import", "Import", "Cancel", []*widget.FormItem{
		widget.NewFormItem("File", browseRow(path, browse)),
		widget.NewFormItem("Table", table),
		widget.NewFormItem("Sheet", sheet),
	}, func(ok bool) {
		if !ok {
			return
		}
		if path.Text == "" || table.Text == "" {
			dialog.ShowInformation("Import", "File and Table are required.", g.win)
			return
		}
		go func() {
			n, err := g.core.ImportFile(g.ctx(), path.Text, table.Text, sheet.Text)
			if err != nil {
				g.bgErr("import", err)
				return
			}
			onUI(func() {
				dialog.ShowInformation("Import",
					fmt.Sprintf("Imported %d row(s) into %s.", n, table.Text), g.win)
			})
		}()
	}, g.win)
	form.Resize(fyne.NewSize(520, 260))
	form.Show()
}

// showExport writes the current result grid to a file via core.ExportRows. It
// exports what the user is looking at — run a query first.
func (g *App) showExport() {
	m := g.editor.model
	if len(m.cols) == 0 || len(m.rows) == 0 {
		dialog.ShowInformation("Export", "Run a query first — there are no results to export.", g.win)
		return
	}

	path := widget.NewEntry()
	path.SetPlaceHolder("/path/to/out.csv")
	browse := widget.NewButton("Browse…", func() {
		dialog.ShowFileSave(func(wc fyne.URIWriteCloser, err error) {
			if err != nil || wc == nil {
				return
			}
			path.SetText(wc.URI().Path())
			_ = wc.Close()
		}, g.win)
	})

	form := dialog.NewForm("Export results", "Export", "Cancel", []*widget.FormItem{
		widget.NewFormItem("File", browseRow(path, browse)),
	}, func(ok bool) {
		if !ok || path.Text == "" {
			return
		}
		go func() {
			n, err := g.core.ExportRows(m.cols, m.rows, path.Text)
			if err != nil {
				g.bgErr("export", err)
				return
			}
			onUI(func() {
				dialog.ShowInformation("Export",
					fmt.Sprintf("Wrote %d row(s) to %s.", n, path.Text), g.win)
			})
		}()
	}, g.win)
	form.Resize(fyne.NewSize(520, 200))
	form.Show()
}
