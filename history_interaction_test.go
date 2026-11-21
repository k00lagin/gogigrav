package main

import (
	"testing"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
)

func testHistoryRepository(t *testing.T) (path, main, feature string) {
	t.Helper()
	path = t.TempDir()
	testGit(t, path, "init", "-b", "main")
	testGit(t, path, "config", "user.name", "Tester")
	testGit(t, path, "config", "user.email", "tester@example.test")
	testGit(t, path, "config", "commit.gpgsign", "false")
	testGit(t, path, "commit", "--allow-empty", "-m", "Main commit")
	main = testGit(t, path, "rev-parse", "HEAD")
	testGit(t, path, "switch", "-c", "feature")
	testGit(t, path, "commit", "--allow-empty", "-m", "Feature commit")
	feature = testGit(t, path, "rev-parse", "HEAD")
	testGit(t, path, "switch", "main")
	return path, main, feature
}

func TestHistoryCommitContextMenuBelowInlineDetails(t *testing.T) {
	path, _, _ := testHistoryRepository(t)
	w := testWorkbench(t, path)
	w.prefs.Settings.UnifiedDetails = true
	w.selected.Set(0)
	w.selectHistoryRow(0)
	testWaitForGit(t, w)

	win := w.ui.Window()
	clipboard := &repositoryClipboard{}
	widget.RegisterClipboardProvider(clipboard)
	defer widget.RegisterClipboardProvider(nil)

	origin := w.table.ScreenOrigin()
	pos := origin.Add(geometry.Pt(w.table.Bounds().Width()-35, historyHeaderHeight+historyRowHeight*1.5+inlineDetailHeight))
	win.HandleEvent(event.NewMouseEvent(event.MousePress, event.ButtonRight, event.ButtonStateRight, pos, pos, event.ModNone))
	win.HandleEvent(event.NewMouseEvent(event.MouseRelease, event.ButtonRight, 0, pos, pos, event.ModNone))
	if win.OverlayCount() != 1 || w.selected.Get() != 0 {
		t.Fatal("right-click should open the later row's menu without changing the selection")
	}
	for range 16 {
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyDown, 0, event.ModNone))
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	if got, want := clipboard.value, w.rows[1].Subject; got != want {
		t.Fatalf("copied subject = %q, want %q", got, want)
	}

	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyF10, 0, event.ModShift))
	for range 16 {
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyDown, 0, event.ModNone))
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	if got, want := clipboard.value, w.rows[0].Subject; got != want {
		t.Fatalf("keyboard menu copied %q, want %q", got, want)
	}

	win.HandleEvent(event.NewMouseEvent(event.MousePress, event.ButtonRight, event.ButtonStateRight, pos, pos, event.ModNone))
	win.HandleEvent(event.NewMouseEvent(event.MouseRelease, event.ButtonRight, 0, pos, pos, event.ModNone))
	for range 16 {
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyDown, 0, event.ModNone))
	}
	for range 2 {
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyUp, 0, event.ModNone))
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	testWaitForGit(t, w)
	if w.selected.Get() != 0 || w.compareBase != w.rows[1].Hash || w.compareTip != w.rows[0].Hash {
		t.Fatal("comparison should keep the selected row and compare the older commit to the newer one")
	}
}

func TestHistoryBranchDropOpensMergeDialog(t *testing.T) {
	path, main, feature := testHistoryRepository(t)
	w := testWorkbench(t, path)
	win := w.ui.Window()
	h := w.historyHeader
	badgePoint := func(ref string) geometry.Point {
		t.Helper()
		for _, badges := range h.badges {
			for _, badge := range badges {
				if badge.label.ref == ref {
					return h.table.ScreenOrigin().Add(badge.bounds.Center().Add(geometry.Pt(0, historyHeaderHeight)))
				}
			}
		}
		t.Fatalf("missing badge for %q", ref)
		return geometry.Point{}
	}
	from, to := badgePoint("refs/heads/feature"), badgePoint("refs/heads/main")
	mouse := func(kind event.MouseEventType, button event.Button, held event.ButtonState, at geometry.Point) {
		win.HandleEvent(event.NewMouseEvent(kind, button, held, at, at, event.ModNone))
	}
	mouse(event.MousePress, event.ButtonLeft, event.ButtonStateLeft, from)
	mouse(event.MouseMove, event.ButtonNone, event.ButtonStateLeft, to)
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))
	mouse(event.MouseRelease, event.ButtonLeft, 0, to)
	if h.branchDrag.active || win.OverlayCount() != 0 || w.busy {
		t.Fatal("Escape should cancel the drag and release pointer capture")
	}

	mouse(event.MousePress, event.ButtonLeft, event.ButtonStateLeft, from)
	mouse(event.MouseMove, event.ButtonNone, event.ButtonStateLeft, to)
	if h.branchDrag.target.label.ref != "refs/heads/main" {
		t.Fatal("drag did not highlight the destination")
	}
	mouse(event.MouseRelease, event.ButtonLeft, 0, to)
	if h.branchDrag.active || win.OverlayCount() != 1 || w.busy {
		t.Fatal("drop should clear the drag and open a menu without running Git")
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyDown, 0, event.ModNone))
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	if win.OverlayCount() != 1 || w.busy || testGit(t, path, "rev-parse", "HEAD") != main {
		t.Fatal("the merge should wait for confirmation")
	}

	w.ui.Frame()
	win.DrawTo(&uitest.MockCanvas{})
	var submit *button.Widget
	walkWidgets(win.Overlays().TopInteractive(), func(node widget.Widget) {
		if btn, ok := node.(*button.Widget); ok {
			submit = btn
		}
	})
	if submit == nil {
		t.Fatal("merge dialog has no submit button")
	}
	testClick(w, submit.ScreenBounds().Center())
	testWaitForGit(t, w)
	if got := testGit(t, path, "rev-parse", "HEAD"); got != feature {
		t.Fatalf("HEAD after merge = %q, want %q", got, feature)
	}
}
