package main

import (
	"testing"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/gesture"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/uitest"
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
	actionBounds := func() map[string]geometry.Rect {
		bounds := make(map[string]geometry.Rect)
		walkWidgets(w.titleBar, func(node widget.Widget) {
			if bar, ok := node.(*toolbarHints); ok {
				for i := range bar.ItemCount() {
					if label := bar.ItemAt(i).Label; label != "" {
						bounds[label] = bar.ItemBounds(i).Translate(bar.ScreenOrigin())
					}
				}
			}
		})
		return bounds
	}
	closed := actionBounds()
	testToolbarAction(t, w, icon.ExpSearch.Name)
	testActionFrame(w)
	if w.searchField == nil || win.Context().FocusedWidget() != w.searchField ||
		!w.titleBar.ScreenBounds().Contains(w.searchField.ScreenBounds().Center()) {
		t.Fatal("search should replace the search button inside the title bar and take focus")
	}
	opened := actionBounds()
	for _, label := range []string{"Create branch from HEAD", "Stash changes", "Remotes", "Settings"} {
		if closed[label].IsEmpty() || closed[label] != opened[label] {
			t.Errorf("%s moved when search opened: %v → %v", label, closed[label], opened[label])
		}
	}
	if w.searchField.ScreenBounds().Max.X > opened["Create branch from HEAD"].Min.X {
		t.Fatal("search overlaps the actions on its right")
	}
	canvas := &uitest.MockCanvas{}
	if width := w.searchField.Bounds().Width(); width < canvas.MeasureText(compactSearchPlaceholder, 13, false) {
		t.Fatalf("search field is only %v pixels wide at minimum window width", width)
	}
	foundLens := false
	walkWidgets(w.titleBar, func(node widget.Widget) {
		if lens, ok := node.(*icon.IconWidget); ok && lens.Data().Name == icon.ExpSearch.Name {
			foundLens = true
			if size := lens.Bounds().Size(); size != (geometry.Size{Width: 14, Height: 14}) {
				t.Errorf("search icon must be square, got %v", size)
			}
		}
		if bar, ok := node.(*toolbarHints); ok {
			for i := range bar.ItemCount() {
				if bar.ItemBounds(i).Max.X > bar.Bounds().Width()+0.1 {
					t.Errorf("toolbar item %q overflows at minimum window width", bar.ItemAt(i).Label)
				}
			}
		}
	})
	if !foundLens {
		t.Fatal("search icon is missing")
	}
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
	var closeButton *button.Widget
	walkWidgets(w.titleBar, func(node widget.Widget) {
		if control, ok := node.(*button.Widget); ok {
			closeButton = control
		}
	})
	if closeButton == nil {
		t.Fatal("search close button is missing")
	}
	at := closeButton.ScreenBounds().Center()
	for _, kind := range []gesture.PointerEventType{gesture.PointerDown, gesture.PointerUp} {
		buttons := event.ButtonState(0)
		if kind == gesture.PointerDown {
			buttons = event.ButtonStateLeft
		}
		win.HandlePointerEvent(&gesture.PointerEvent{
			Base: event.NewBase(event.TypeMouse, 0), EventType: kind, PointerID: 1,
			PointerType: gesture.PointerTypeMouse, Position: at, GlobalPosition: at,
			Button: event.ButtonLeft, Buttons: buttons,
		})
	}
	testActionFrame(w)
	if w.searchOpen || w.searchField != nil || len(w.searchMatches) != 0 {
		t.Fatalf("clicking the close button at %v did not close search", closeButton.ScreenBounds())
	}
	if restored := actionBounds()["Find in history (Ctrl+F)"]; restored.IsEmpty() || restored != closed["Find in history (Ctrl+F)"] {
		t.Fatal("closing search did not restore the original toolbar button")
	}
}
