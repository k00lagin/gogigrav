package main

import (
	"testing"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/widget"
)

func TestHistorySearchExpandsInToolbar(t *testing.T) {
	dir := t.TempDir()
	testGit(t, dir, "init", "-b", "main")
	for _, title := range []string{"First alpha", "Other commit", "Last alpha"} {
		testGit(t, dir, "-c", "user.name=Tester", "-c", "user.email=tester@example.test",
			"-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", title)
	}
	testGit(t, dir, "remote", "add", "origin", t.TempDir())
	w := testWorkbench(t, dir)
	win := w.ui.Window()
	testToolbarAction(t, w, icon.ExpSearch.Name)
	testActionFrame(w)
	if w.searchField == nil || win.Context().FocusedWidget() != w.searchField ||
		!w.titleBar.ScreenBounds().Contains(w.searchField.ScreenBounds().Center()) {
		t.Fatal("search should replace the search button inside the title bar and take focus")
	}
	walkWidgets(w.titleBar, func(node widget.Widget) {
		if bar, ok := node.(*toolbarHints); ok {
			for i := range bar.ItemCount() {
				if bar.ItemBounds(i).Max.X > bar.Bounds().Width()+0.1 {
					t.Errorf("toolbar item %q overflows at minimum window width", bar.ItemAt(i).Label)
				}
			}
		}
	})
	key := func(k event.Key, char rune, modifiers event.Modifiers) {
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, k, char, modifiers))
	}
	for _, char := range "alpha" {
		key(event.KeyUnknown, char, event.ModNone)
	}
	if w.table.GetRowCount() != 3 || len(w.searchMatches) != 2 || w.searchIndex != 0 {
		t.Fatal("search should highlight two matches while leaving all history rows visible")
	}
	key(event.KeyEnter, 0, event.ModNone)
	if w.searchIndex != 1 {
		t.Fatal("Enter should navigate to the next match")
	}
	key(event.KeyEnter, 0, event.ModShift)
	if w.searchIndex != 0 {
		t.Fatal("Shift+Enter should navigate to the previous match")
	}
	key(event.KeyEscape, 0, event.ModNone)
	testActionFrame(w)
	if w.searchOpen || w.searchField != nil || len(w.searchMatches) != 0 {
		t.Fatal("Escape should close toolbar search and clear its highlights")
	}
}
