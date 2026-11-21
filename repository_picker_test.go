package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
)

func TestRepositoryFrecency(t *testing.T) {
	const frequent, newer, firstOther, secondOther = "frequent", "newer", "alpha", "zeta"
	now := time.Unix(1700000000, 0)
	prefs := preferences{Recent: []string{secondOther, newer, frequent, firstOther}}
	for range 3 {
		prefs.recordRepositoryVisit(frequent, now.Add(-repositoryVisitHalfLife))
	}
	prefs.recordRepositoryVisit(newer, now)
	recent, others := prefs.repositoryGroups(now)
	if !slices.Equal(recent, []string{frequent, newer}) || !slices.Equal(others, []string{firstOther, secondOther}) {
		t.Fatalf("recent %v, others %v", recent, others)
	}
	prefs.recordRepositoryVisit(newer, now)
	recent, _ = prefs.repositoryGroups(now)
	if !slices.Equal(recent, []string{newer, frequent}) {
		t.Fatalf("recent after second visit: %v", recent)
	}
}

func TestRepositoryPickerBrowsesAddsAndInitializes(t *testing.T) {
	parent := t.TempDir()
	alpha, docs, zeta := filepath.Join(parent, "alpha"), filepath.Join(parent, "docs"), filepath.Join(parent, "zeta")
	for _, path := range []string{alpha, docs, zeta} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if path != docs {
			testGit(t, path, "init", "-b", "main")
		}
	}
	w := testWorkbench(t, alpha)
	w.prefs.Recent = []string{alpha}
	w.prefs.recordRepositoryVisit(alpha, time.Now())
	win := w.ui.Window()
	paintOverlays := func() {
		w.ui.Frame()
		win.DrawOverlays(&uitest.MockCanvas{})
	}
	key := func(key event.Key, mods event.Modifiers) {
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, key, 0, mods))
	}
	move := func(at geometry.Point) {
		win.HandleEvent(event.NewMouseEvent(event.MouseMove, event.ButtonNone, 0, at, at, event.ModNone))
	}
	waitForOpen := func() {
		t.Helper()
		if !w.busy {
			t.Fatal("expected a repository job")
		}
		waitRepositoryUI(t, w, func() bool { return !w.busy })
		if w.status != "" {
			t.Fatal(w.status)
		}
		testActionFrame(w)
	}
	testClick(w, w.branchBar.ItemBounds(0).Translate(w.branchBar.ScreenOrigin()).Center())
	paintOverlays()
	search, _ := win.Context().FocusedWidget().(*textfield.Widget)
	var add *button.Widget
	var menu *choicePanelKeys
	for _, overlay := range win.OverlayContentWidgets() {
		walkWidgets(overlay, func(node widget.Widget) {
			switch v := node.(type) {
			case *button.Widget:
				add = v
			case *choicePanelKeys:
				menu = v
			}
		})
	}
	if search == nil || add == nil || menu == nil || add.ScreenBounds().Min.Y < search.ScreenBounds().Max.Y ||
		add.ScreenBounds().Max.Y > menu.list.ScreenBounds().Min.Y {
		t.Fatal("add button should sit between the filter and repository list")
	}
	if menu.list.GetItemCount() != 2 || menu.selection.Get() != 1 || !menu.sectionAt(0) {
		t.Fatal("recent group should have a heading and highlight its first repository")
	}
	testClick(w, add.ScreenBounds().Center())
	testApplyPopupAction(t, w)

	findPicker := func() *repositoryPicker {
		t.Helper()
		var picker *repositoryPicker
		for _, overlay := range win.OverlayContentWidgets() {
			walkWidgets(overlay, func(node widget.Widget) {
				if p, ok := node.(*repositoryPicker); ok {
					picker = p
				}
			})
		}
		if picker == nil || !win.Context().IsFocused(picker.field) {
			t.Fatal("modal picker should focus its path field")
		}
		return picker
	}
	picker := findPicker()
	waitRepositoryUI(t, w, func() bool { return len(picker.rows) == 3 })
	paintOverlays()
	if picker.field.Text() != directoryPath(parent) || picker.rows[0].path != alpha || picker.rows[1].path != docs {
		t.Fatalf("unexpected initial browse path %q or folder order", picker.field.Text())
	}
	assertFolderIcon := func(row interface{ ScreenBounds() geometry.Rect }, glyph widget.Widget) {
		t.Helper()
		if _, direct := glyph.(*icon.IconWidget); !direct {
			glyph = glyph.Children()[0]
		}
		bounds := glyph.(*icon.IconWidget).ScreenBounds()
		want := row.ScreenBounds().Min.Add(geometry.Pt(pickerInset+repositoryIconSize/2, row.ScreenBounds().Height()/2))
		if bounds.Width() != 16 || bounds.Height() != 16 || bounds.Center() != want {
			t.Errorf("folder icon bounds %v, want 16×16 centered at %v", bounds, want)
		}
	}
	initRow := picker.list.Children()[0]
	assertFolderIcon(initRow.(interface{ ScreenBounds() geometry.Rect }), initRow.Children()[0])
	folderRow := picker.rows[1]
	assertFolderIcon(folderRow, folderRow.BoxWidget.Children()[0])
	first := picker.rows[0]
	if first.add == nil || first.add.IsVisible() {
		t.Fatal("the Add button should be hidden before hovering the repository")
	}
	move(first.ScreenBounds().Center())
	if !first.add.IsVisible() {
		t.Fatal("hovering a repository should reveal its Add button")
	}
	paintOverlays()
	move(first.add.ScreenBounds().Center())
	if !first.add.IsVisible() {
		t.Fatal("Add disappeared when the pointer moved from the row onto the button")
	}
	move(picker.field.ScreenBounds().Center())
	if first.add.IsVisible() {
		t.Fatal("leaving the repository row should hide its Add button")
	}
	testClick(w, picker.rows[1].ScreenBounds().Center())
	waitRepositoryUI(t, w, func() bool { return picker.initDir == docs })
	if picker.field.Text() != directoryPath(docs) {
		t.Fatalf("clicking the folder row should browse into it, got %q", picker.field.Text())
	}
	key(event.KeyEnter, event.ModCtrl)
	waitRepositoryUI(t, w, func() bool { return !w.busy })
	if !picker.dialog.IsOpen() || !strings.Contains(picker.status.Get(), "no Git worktree found") {
		t.Fatalf("failed add should leave the picker open with an error, got %q", picker.status.Get())
	}
	key(event.KeyBackspace, event.ModNone)
	waitRepositoryUI(t, w, func() bool { return len(picker.rows) == 3 })
	key(event.KeyEnter, event.ModCtrl)
	waitForOpen()
	if picker.dialog.IsOpen() || w.path != alpha || w.selected.Get() != -1 ||
		!slices.Equal(w.prefs.Recent, []string{alpha, zeta}) ||
		w.prefs.RepositoryVisits[repositoryKey(zeta)].Score != 0 {
		t.Fatal("adding a folder should register both worktrees and open only the first")
	}

	testClick(w, w.branchBar.ItemBounds(0).Translate(w.branchBar.ScreenOrigin()).Center())
	paintOverlays()
	menu = nil
	for _, overlay := range win.OverlayContentWidgets() {
		walkWidgets(overlay, func(node widget.Widget) {
			if choices, ok := node.(*choicePanelKeys); ok {
				menu = choices
			}
		})
	}
	if menu == nil || menu.list.GetItemCount() != 4 || !menu.sectionAt(0) || !menu.sectionAt(2) {
		t.Fatal("expected a divider between the recent and other repositories")
	}
	key(event.KeyDown, event.ModNone)
	if menu.selection.Get() != 3 {
		t.Fatal("keyboard navigation should skip the group divider")
	}
	key(event.KeyEscape, event.ModNone)

	w.showRepositoryPicker()
	picker = findPicker()
	waitRepositoryUI(t, w, func() bool { return len(picker.rows) == 3 })
	key(event.KeyDown, event.ModNone)
	key(event.KeyEnter, event.ModNone)
	waitRepositoryUI(t, w, func() bool { return picker.initDir == docs })
	key(event.KeyEnter, event.ModAlt)
	waitForOpen()
	if picker.dialog.IsOpen() || w.path != docs || w.prefs.RepositoryVisits[repositoryKey(docs)].Score != 1 {
		t.Fatal("Alt+Enter should initialize, register and open the listed folder")
	}

	w.showRepositoryPicker()
	picker = findPicker()
	waitRepositoryUI(t, w, func() bool { return len(picker.rows) == 3 })
	paintOverlays()
	last := picker.rows[2]
	move(last.ScreenBounds().Center())
	paintOverlays()
	testClick(w, last.add.ScreenBounds().Center())
	waitForOpen()
	if picker.dialog.IsOpen() || w.path != zeta {
		t.Fatal("clicking Add on a repository row should register and open it")
	}
}
