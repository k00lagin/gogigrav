package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/core/treeview"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

func TestUnifiedCommitFileTreeScroll(t *testing.T) {
	path := t.TempDir()
	testGit(t, path, "init", "-b", "main")
	testGit(t, path, "config", "user.name", "Tester")
	testGit(t, path, "config", "user.email", "tester@example.test")
	for i := range 48 {
		name := filepath.Join(path, fmt.Sprintf("file%02d.txt", i))
		if err := os.WriteFile(name, []byte("example"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	testGit(t, path, "add", ".")
	testGit(t, path, "-c", "commit.gpgsign=false", "commit", "-m", "Add files")
	w := testWorkbench(t, path)
	w.prefs.Settings.UnifiedDetails = true
	w.selectHistoryRow(w.uncommittedRowCount())
	testWaitForGit(t, w)

	var tree *treeview.Widget
	walkWidgets(w.inlineContent, func(node widget.Widget) {
		if files, ok := node.(*treeview.Widget); ok {
			tree = files
		}
	})
	if tree == nil {
		t.Fatal("file tree is missing from the expanded commit")
	}
	win := w.ui.Window()
	treeScroll := tree.Children()[0].(*scrollview.Widget)
	bounds := tree.ScreenBounds()
	if treeScroll.ScrollbarInset() == 0 {
		t.Fatalf("file tree has no scrollbar for content %v in viewport %v", treeScroll.ContentSize(), bounds)
	}
	historyScroll := w.table.Children()[0].(*scrollview.Widget)
	if historyScroll.ScrollbarInset() == 0 {
		t.Fatal("test needs both the file tree's and the history's scrollbars")
	}
	_, start := historyScroll.ScrollOffset()
	position := bounds.Center()
	wheel := func(delta float32) {
		win.HandleEvent(event.NewWheelEvent(geometry.Pt(0, delta), position, position, event.ModNone))
	}
	wheel(1)
	if _, y := treeScroll.ScrollOffset(); y != 40 {
		t.Fatalf("wheel down scrolled the file tree to %v, want 40", y)
	}
	if _, y := historyScroll.ScrollOffset(); y != start {
		t.Fatalf("scroll leaked to history (%v -> %v)", start, y)
	}
	testClick(w, bounds.Min.Add(geometry.Pt(100, 85)))
	if w.file == nil || w.file.file.Path != "file05.txt" {
		t.Fatalf("click did not select the file at the scrolled position: %+v", w.file)
	}
	wheel(-1)
	if _, y := treeScroll.ScrollOffset(); y != 0 {
		t.Fatalf("wheel up scrolled the file tree to %v, want 0", y)
	}

	// Start near the top of the thumb and continue the drag outside the tree.
	press := geometry.Pt(bounds.Max.X-6, bounds.Min.Y+12)
	move := geometry.Pt(bounds.Min.X-10, press.Y+100)
	win.HandleEvent(event.NewMouseEvent(event.MousePress, event.ButtonLeft, event.ButtonStateLeft, press, press, event.ModNone))
	win.HandleEvent(event.NewMouseEvent(event.MouseMove, event.ButtonNone, event.ButtonStateLeft, move, move, event.ModNone))
	win.HandleEvent(event.NewMouseEvent(event.MouseRelease, event.ButtonLeft, 0, move, move, event.ModNone))
	if _, y := treeScroll.ScrollOffset(); y <= 40 {
		t.Fatalf("thumb drag outside the tree only scrolled to %v", y)
	}
}

func TestUnifiedHistoryExpandsTheSelectedRow(t *testing.T) {
	path := t.TempDir()
	testGit(t, path, "init", "-b", "main")
	testGit(t, path, "config", "user.name", "Tester")
	testGit(t, path, "config", "user.email", "tester@example.test")
	for _, subject := range []string{"First commit", "Second commit"} {
		testGit(t, path, "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", subject)
	}
	if err := os.WriteFile(filepath.Join(path, "pending.txt"), []byte("working changes"), 0600); err != nil {
		t.Fatal(err)
	}
	w := testWorkbench(t, path)
	w.drafts = make(map[string]string)
	w.prefs.Settings.UnifiedDetails = true
	testActionFrame(w)

	detailBounds := func() geometry.Rect {
		t.Helper()
		detail, ok := w.inlineContent.(interface{ Bounds() geometry.Rect })
		if !ok {
			t.Fatal("expected an inline detail panel")
		}
		return detail.Bounds()
	}
	bodyPoint := func(y float32) geometry.Point {
		view := w.table.Children()[0].(*scrollview.Widget)
		_, scroll := view.ScrollOffset()
		return view.ScreenBounds().Min.Add(geometry.Pt(120, y-scroll))
	}
	baseHeight := detailBounds().Min.Y
	historyViewport := w.table.Children()[0].(*scrollview.Widget)
	if got := detailBounds(); baseHeight != 27 || got.Width() != w.table.Bounds().Width()-historyViewport.ScrollbarInset() {
		t.Fatalf("working tree should be under the first row and span the history: %v", got)
	}
	if g := w.inlineGraph.Bounds(); g.Width() != w.graph.width() || g.Height() != detailBounds().Height() {
		t.Fatalf("graph gutter %v does not span the inline panel %v", g, detailBounds())
	}

	var message *textfield.Widget
	var files *treeview.Widget
	var buttons []*button.Widget
	walkWidgets(w.inlineContent, func(node widget.Widget) {
		switch v := node.(type) {
		case *textfield.Widget:
			message = v
		case *treeview.Widget:
			files = v
		case *button.Widget:
			buttons = append(buttons, v)
		}
	})
	if message == nil || files == nil || len(buttons) < 3 {
		t.Fatal("inline working tree is missing its controls")
	}
	testClick(w, message.ScreenBounds().Center())
	w.ui.Window().HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyUnknown, 'x', event.ModNone))
	if got := w.drafts[path]; got != "x" {
		t.Fatalf("commit input did not receive typing: %q", got)
	}
	tree := files.ScreenBounds()
	testClick(w, tree.Min.Add(geometry.Pt(100, fileRowHeight/2)))
	if w.file == nil || w.file.file.Path != "pending.txt" {
		t.Fatal("inline file tree did not receive the click")
	}
	stage := buttons[2]
	testClick(w, stage.ScreenBounds().Center())
	testWaitForGit(t, w)
	if got := testGit(t, path, "diff", "--cached", "--name-only"); got != "pending.txt" {
		t.Fatalf("file staged from inline panel = %q", got)
	}

	// The first commit can be selected at its shifted position after the panel.
	testClick(w, bodyPoint(detailBounds().Max.Y+baseHeight/2))
	testWaitForGit(t, w)
	if w.commit.Subject != "Second commit" || detailBounds().Min.Y != 2*baseHeight {
		t.Fatalf("selected commit = %q, detail y = %v", w.commit.Subject, detailBounds().Min.Y)
	}
	testClick(w, bodyPoint(baseHeight+baseHeight/2))
	testActionFrame(w)
	if w.inlineContent != nil || w.selected.Get() != -1 {
		t.Fatal("clicking the expanded commit again should close its details")
	}

	testClick(w, bodyPoint(baseHeight/2))
	testActionFrame(w)
	if w.page != "changes" || detailBounds().Min.Y != baseHeight {
		t.Fatal("could not reopen the working tree below its row")
	}
	w.prefs.Settings.UnifiedDetails = false
	testActionFrame(w)
	if w.inlineContent != nil || w.table.Bounds().Width() >= w.ui.Window().Root().(interface{ Bounds() geometry.Rect }).Bounds().Width() {
		t.Fatal("split view should place details beside the history")
	}
}
