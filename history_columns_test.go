package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"gogigrav/internal/repo"
)

func TestHistoryColumnsAndRefs(t *testing.T) {
	dir := t.TempDir()
	testGit(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("one\ntwo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "add", "sample.txt")
	testGit(t, dir, "-c", "user.name=Tester", "-c", "user.email=tester@example.test", "-c", "commit.gpgsign=false", "commit", "-m", "Add sample")
	testGit(t, dir, "remote", "add", "origin", "https://example.invalid/repo.git")
	testGit(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	testGit(t, dir, "branch", "--set-upstream-to", "origin/main")
	testGit(t, dir, "tag", "v1")
	w := testWorkbench(t, dir)
	if w.table.GetRowCount() != 1 || w.rows[0].Diff != (repo.ChangeSummary{Files: 1, Added: 2}) {
		t.Fatalf("clean history rows %d, stats %+v", w.table.GetRowCount(), w.rows[0].Diff)
	}
	labels := w.historyRefs(w.rows[0])
	if len(labels) != 2 || labels[0].text != "main" || !labels[0].current || labels[0].remote != "origin" || labels[1].text != "v1" {
		t.Fatalf("ref labels %+v", labels)
	}
	h := w.historyHeader
	x := h.ScreenBounds().Min.X + h.descriptionWidth()
	y := h.ScreenBounds().Min.Y + 15
	win := w.ui.Window()
	press := geometry.Pt(x, y)
	move := geometry.Pt(x+18, y-45)
	win.HandleEvent(event.NewMouseEvent(event.MousePress, event.ButtonLeft, event.ButtonStateLeft, press, press, event.ModNone))
	win.HandleEvent(event.NewMouseEvent(event.MouseMove, event.ButtonNone, event.ButtonStateLeft, move, move, event.ModNone))
	win.HandleEvent(event.NewMouseEvent(event.MouseRelease, event.ButtonLeft, 0, move, move, event.ModNone))
	if width := w.prefs.Columns.Widths["diff"]; width != 98 {
		t.Fatalf("Diff width after drag = %v", width)
	}
	win.HandleEvent(event.NewMouseEvent(event.MousePress, event.ButtonRight, event.ButtonStateRight, press, press, event.ModNone))
	if win.OverlayCount() != 1 {
		t.Fatal("history header context menu did not open")
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyDown, 0, event.ModNone))
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	if !w.prefs.Columns.Hidden["diff"] {
		t.Fatal("Diff column menu did not toggle visibility")
	}
	testActionFrame(w)
	if len(w.historyHeader.columns) != 4 {
		t.Fatal("history did not rerender hidden columns")
	}
}
