package main

import (
	"testing"
	"time"

	"github.com/gogpu/ui/core/listview"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
)

type repositoryClipboard struct{ value string }

func (c *repositoryClipboard) ClipboardRead() (string, error)    { return c.value, nil }
func (c *repositoryClipboard) ClipboardWrite(value string) error { c.value = value; return nil }

func TestRepositoryMenusAndPastedPath(t *testing.T) {
	active, other := t.TempDir(), t.TempDir()
	testGit(t, active, "init", "-b", "main")
	testGit(t, other, "init", "-b", "main")
	w := testWorkbench(t, active)
	w.prefs.Recent = []string{active}
	win := w.ui.Window()
	right := func(p geometry.Point) {
		win.HandleEvent(event.NewMouseEvent(event.MousePress, event.ButtonRight, event.ButtonStateRight, p, p, event.ModNone))
		win.HandleEvent(event.NewMouseEvent(event.MouseRelease, event.ButtonRight, 0, p, p, event.ModNone))
	}
	key := func(k event.Key, mod event.Modifiers) { win.HandleEvent(event.NewKeyEvent(event.KeyPress, k, 0, mod)) }
	trigger := w.branchBar.ItemBounds(0).Translate(w.branchBar.ScreenOrigin()).Center()
	right(trigger)
	if win.OverlayCount() != 1 || w.openPopup != nil {
		t.Fatal("trigger did not open repository context menu")
	}
	key(event.KeyEscape, event.ModNone)
	testClick(w, trigger)
	w.ui.Frame()
	win.DrawOverlays(&uitest.MockCanvas{})
	var list *listview.Widget
	walkWidgets(win.OverlayContentWidgets()[0], func(node widget.Widget) {
		if l, ok := node.(*listview.Widget); ok {
			list = l
		}
	})
	if list == nil {
		t.Fatal("repository popup has no list")
	}
	right(list.ScreenBounds().Center())
	if win.OverlayCount() != 2 {
		t.Fatal("right click did not open a context menu over the repository list")
	}
	key(event.KeyEscape, event.ModNone)
	if win.OverlayCount() != 1 {
		t.Fatal("dismissing context menu also closed the repository list")
	}
	clipboard := &repositoryClipboard{value: `"` + other + `"`}
	widget.RegisterClipboardProvider(clipboard)
	defer widget.RegisterClipboardProvider(nil)
	field, ok := win.Context().FocusedWidget().(*textfield.Widget)
	if !ok {
		t.Fatal("repository search field lost focus")
	}
	right(field.ScreenBounds().Center())
	if win.OverlayCount() != 2 {
		t.Fatal("right click did not open the field's context menu")
	}
	key(event.KeyEscape, event.ModNone)
	key(event.KeyF10, event.ModShift)
	if win.OverlayCount() != 2 {
		t.Fatal("Shift+F10 did not open the field's context menu")
	}
	// Paste is the only enabled entry in an empty search field.
	key(event.KeyDown, event.ModNone)
	key(event.KeyEnter, event.ModNone)
	if win.OverlayCount() != 1 || !win.Context().IsFocused(field) {
		t.Fatal("paste closed the repository popup or moved focus away from the field")
	}
	waitRepositoryUI(t, w, func() bool {
		key(event.KeyEnter, event.ModNone)
		return w.popupAction != nil
	})
	if list.GetItemCount() != 1 {
		t.Fatalf("expected one resolved repository, got %d", list.GetItemCount())
	}
	w.popupAction()
	waitRepositoryUI(t, w, func() bool { return !w.busy })
	if !sameRepositoryPath(w.path, other) {
		t.Fatalf("opened %q, want %q", w.path, other)
	}
}

func waitRepositoryUI(t *testing.T, w *workbench, ready func() bool) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for !ready() {
		select {
		case apply := <-w.done:
			apply()
			w.ui.Frame()
		case <-timeout:
			t.Fatal("timed out waiting for repository UI")
		}
	}
}

func TestStatusBranchTooltipFollowsHover(t *testing.T) {
	dir := t.TempDir()
	testGit(t, dir, "init", "-b", "main")
	w := testWorkbench(t, dir)
	win := w.ui.Window()
	root := win.Root().Children()
	status := root[len(root)-1].(*statusHints)
	var position geometry.Point
	for control := range status.hints {
		position = control.ScreenBounds().Center()
		break
	}
	move := func(at geometry.Point) {
		win.HandleEvent(event.NewMouseEvent(event.MouseMove, event.ButtonNone, 0, at, at, event.ModNone))
	}
	move(position)
	if w.toolbarHover.due.IsZero() {
		t.Fatal("status bar tooltip was not scheduled")
	}
	w.updateToolbarTooltip(w.toolbarHover.due)
	w.ui.Frame()
	win.DrawOverlays(&uitest.MockCanvas{})
	if tip := w.toolbarHover.visible; tip == nil || tip.Bounds().Max.Y >= position.Y {
		t.Fatal("status tooltip should appear above the button")
	}
	move(geometry.Pt(300, 100))
	w.updateToolbarTooltip(time.Now())
	if w.toolbarHover.visible != nil {
		t.Fatal("status tooltip survived mouse leave")
	}
}
