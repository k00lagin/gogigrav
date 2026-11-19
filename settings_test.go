package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/checkbox"
	"github.com/gogpu/ui/core/dropdown"
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
)

func TestSettingsDialogInputAndPersistence(t *testing.T) {
	path := t.TempDir()
	testGit(t, path, "init", "-b", "main")
	testGit(t, path, "config", "user.name", "Tester")
	testGit(t, path, "config", "user.email", "tester@example.test")
	testGit(t, path, "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "First")
	w := testWorkbench(t, path)
	w.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	testToolbarAction(t, w, icon.ExpSettings.Name)
	testWaitForGit(t, w)

	win := w.ui.Window()
	draw := func() {
		if w.dirty {
			w.render()
		}
		w.ui.Frame()
		win.DrawOverlays(&uitest.MockCanvas{})
	}
	draw()
	overlays := win.OverlayContentWidgets()
	if len(overlays) != 1 || w.page != "changes" {
		t.Fatalf("settings should overlay the workspace, got %d overlays and page %q", len(overlays), w.page)
	}

	var dropdowns []*dropdown.Widget
	var checks []*checkbox.Widget
	var buttons []*button.Widget
	var scroll *scrollview.Widget
	walkWidgets(overlays[0], func(node widget.Widget) {
		switch v := node.(type) {
		case *dropdown.Widget:
			dropdowns = append(dropdowns, v)
		case *checkbox.Widget:
			checks = append(checks, v)
		case *button.Widget:
			buttons = append(buttons, v)
		case *scrollview.Widget:
			scroll = v
		}
	})
	if len(dropdowns) < 2 || len(checks) < 4 || len(buttons) < 4 || scroll == nil {
		t.Fatal("settings controls were not mounted")
	}
	testClick(w, dropdowns[1].ScreenBounds().Center())
	if win.OverlayCount() != 2 {
		t.Fatal("initial commit count did not open its dropdown inside settings")
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyUp, 0, event.ModNone))
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	if got := dropdowns[1].SelectedValue(); got != "100" || win.OverlayCount() != 1 {
		t.Fatalf("commit count after selection = %q, overlays = %d", got, win.OverlayCount())
	}
	if w.filter.Limit != 100 || !w.settingsRefresh {
		t.Fatal("commit count did not apply while settings were open")
	}
	w.refreshSettingsHistory()
	draw()
	testClick(w, checks[0].ScreenBounds().Center())
	testClick(w, checks[3].ScreenBounds().Center())
	draw()
	if w.table.GetRowCount() != 1 || w.filter.Remote || !w.settingsRefresh || win.OverlayCount() != 1 {
		t.Fatal("changes made during history reload did not update the open workspace")
	}
	testWaitForGit(t, w)
	if w.filter.Remote {
		t.Fatal("an older history reload overwrote the remote setting")
	}
	w.refreshSettingsHistory()
	testWaitForGit(t, w)

	point := scroll.ScreenBounds().Center()
	win.HandleEvent(event.NewWheelEvent(geometry.Pt(0, 100), point, point, event.ModNone))
	draw()
	_, offset := scroll.ScrollOffset()
	if offset <= 0 {
		t.Fatal("cannot scroll to the Repository section on a small window")
	}
	diffPicker := dropdowns[len(dropdowns)-2]
	diffPicker.Open(win.Context())
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyDown, 0, event.ModNone))
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	if !w.prefs.Settings.SplitDiff || win.OverlayCount() != 1 {
		t.Fatal("Diff view did not apply while settings were open")
	}
	themePicker := dropdowns[len(dropdowns)-1]
	testClick(w, themePicker.ScreenBounds().Center())
	if win.OverlayCount() != 2 {
		t.Fatal("theme menu did not open after scrolling")
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyUp, 0, event.ModNone))
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	if got := themePicker.SelectedValue(); got != "DevTools" {
		t.Fatalf("selected theme = %q", got)
	}
	draw()
	if _, got := scroll.ScrollOffset(); got != offset || win.OverlayCount() != 1 || w.theme.IsDark() {
		t.Fatal("theme did not update the open settings dialog in place")
	}
	canvas := &uitest.MockCanvas{}
	win.DrawOverlays(canvas)
	foundSurface := false
	for _, rect := range canvas.RoundRects {
		if rect.Color == w.theme.Colors.Surface {
			foundSurface = true
			break
		}
	}
	if !foundSurface {
		t.Fatal("open settings dialog was not painted with the new theme")
	}
	data, err := os.ReadFile(w.settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved preferences
	if err := json.Unmarshal(data, &saved); err != nil || !saved.Light || saved.Settings.InitialCommits != 100 ||
		!saved.Settings.HideRemotes || !saved.Settings.HideUncommitted || !saved.Settings.SplitDiff {
		t.Fatalf("preferences on disk before closing = %+v, err = %v", saved, err)
	}
	testClick(w, buttons[2].ScreenBounds().Center())
	draw()
	if win.OverlayCount() != 2 {
		t.Fatal("Edit did not open the repository user dialog")
	}
	var fields []*textfield.Widget
	var save *button.Widget
	walkWidgets(win.OverlayContentWidgets()[1], func(node widget.Widget) {
		switch v := node.(type) {
		case *textfield.Widget:
			fields = append(fields, v)
		case *button.Widget:
			save = v
		}
	})
	if len(fields) != 2 || save == nil {
		t.Fatal("repository user fields were not mounted")
	}
	fields[0].SetText("Local User")
	fields[1].SetText("local@example.test")
	testClick(w, save.ScreenBounds().Center())
	testWaitForGit(t, w)
	if got := testGit(t, path, "config", "--local", "user.email"); got != "local@example.test" {
		t.Fatalf("saved repository email = %q", got)
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))
	testActionFrame(w)
	if win.HasOverlays() || w.settingsRefresh || w.filter.Limit != 100 || w.filter.Remote ||
		w.table.GetRowCount() != 1 || w.theme.IsDark() {
		t.Fatalf("settings not applied: overlays=%d filter=%+v rows=%d", win.OverlayCount(), w.filter, w.table.GetRowCount())
	}
}

func TestAutoRefreshDetectsExternalCommit(t *testing.T) {
	path := t.TempDir()
	testGit(t, path, "init", "-b", "main")
	w := testWorkbench(t, path)
	testGit(t, path, "-c", "user.name=Tester", "-c", "user.email=tester@example.test",
		"-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "External commit")
	w.checkAutoRefresh()
	select {
	case apply := <-w.done:
		apply()
	case <-time.After(15 * time.Second):
		t.Fatal("auto refresh did not complete")
	}
	if len(w.rows) != 1 || w.rows[0].Subject != "External commit" {
		t.Fatalf("history after external commit: %+v", w.rows)
	}
}

func TestImportVSCodeJSONC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "example.jsonc")
	err := os.WriteFile(path, []byte(`{
		// workbench colors
		"name": "Sample // light",
		"type": "light",
		"colors": {"editor.background": "#ffeedd", "focusBorder": "#39f", /* accent */},
	}`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	custom, err := readTheme(path)
	if err != nil {
		t.Fatal(err)
	}
	w := &workbench{prefs: preferences{Settings: viewSettings{ImportedTheme: custom, UseImportedTheme: true}}}
	w.setTheme()
	if custom.Name != "Sample // light" || w.theme.IsDark() || w.theme.Colors.Background != widget.Hex(0xffeedd) ||
		w.theme.Colors.Primary != widget.Hex(0x3399ff) {
		t.Fatalf("imported colors: %+v", w.theme.Colors)
	}
}

func walkWidgets(root widget.Widget, visit func(widget.Widget)) {
	visit(root)
	for _, child := range root.Children() {
		walkWidgets(child, visit)
	}
}
