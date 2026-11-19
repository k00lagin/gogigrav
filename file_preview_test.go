package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gogpu/ui/core/menu"
	"github.com/gogpu/ui/core/treeview"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/overlay"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func lineText(l codeLine) string {
	var s strings.Builder
	for _, span := range l.spans {
		s.WriteString(span.text)
	}
	return s.String()
}

func TestPreviewUsesIndexAndWorkingSnapshots(t *testing.T) {
	dir := t.TempDir()
	testGit(t, dir, "init", "-b", "main")
	path := "name [1].go"
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, path), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("package before\n")
	testGit(t, dir, "add", "--", path)
	testGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@test", "commit", "-qm", "first")
	write("package staged\n")
	testGit(t, dir, "add", "--", path)
	write("package working\n")
	w := testWorkbench(t, dir)
	for _, tc := range []struct {
		unstaged    bool
		left, right string
	}{{false, "package before", "package staged"}, {true, "package staged", "package working"}} {
		target := w.target(w.data.Files[0], tc.unstaged)
		result, err := previewLines(t.Context(), dir, target, diffMode, true, false)
		if err != nil {
			t.Fatal(err)
		}
		rows := layoutDiffRows(result, true)
		if len(rows) != 2 || lineText(rows[1].left) != tc.left || lineText(rows[1].right) != tc.right || len(rows[1].right.spans) < 2 {
			t.Fatalf("unexpected highlighted diff %+v", rows)
		}
	}
}
func TestTreeOpensNativeMenuAndPreview(t *testing.T) {
	dir := t.TempDir()
	testGit(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := testWorkbench(t, dir)
	w.prefs.Settings.UnifiedDetails = true
	testActionFrame(w)
	var tree *treeview.Widget
	walkWidgets(w.inlineContent, func(node widget.Widget) {
		if n, ok := node.(*treeview.Widget); ok {
			tree = n
		}
	})
	if tree == nil {
		t.Fatal("missing file tree")
	}
	p := tree.ScreenBounds().Min.Add(geometry.Pt(66, fileRowHeight/2))
	win := w.ui.Window()
	win.HandleEvent(event.NewMouseEvent(event.MousePress, event.ButtonRight, event.ButtonStateRight, p, p, event.ModNone))
	container, ok := win.Overlays().TopInteractive().(*overlay.Container)
	if !ok {
		t.Fatalf("right click failed: %T", win.Overlays().TopInteractive())
	}
	if _, ok := container.Content().(menu.PanelState); !ok {
		t.Fatal("missing menu")
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))
	testClick(w, p)
	testClick(w, p)
	if w.viewer == nil {
		t.Fatal("double click did not open viewer")
	}
	select {
	case apply := <-w.done:
		apply()
	case <-time.After(10 * time.Second):
		t.Fatal("preview timed out")
	}
	if w.viewer.err != nil || len(w.viewer.rows) != 2 {
		t.Fatalf("untracked preview: %+v", w.viewer)
	}
	w.prefs.Settings.SplitDiff = true
	w.previewWrap = true
	w.previewSidebar = true
	testActionFrame(w)
	var panes int
	walkWidgets(win.Root(), func(n widget.Widget) {
		if code, ok := n.(*codePane); ok {
			panes++
			size := code.scroll.ViewportSize()
			if size.Width < 100 || size.Width > 1080 || size.Height < 100 || size.Height > 520 {
				t.Errorf("split viewport has invalid dimensions: %+v", size)
			}
		}
	})
	if panes != 2 {
		t.Fatalf("expected two file panes, got %d", panes)
	}
	w.closePreview()
	testActionFrame(w)
	if w.viewer != nil || w.inlineContent == nil {
		t.Fatal("history was not restored")
	}
}
func TestDeletedAndRenamedPreviewTargets(t *testing.T) {
	w := &workbench{page: "commit", commit: repo.Commit{Hash: "next", Parents: "first second"}}
	target := w.target(repo.File{Path: "new.go", From: "old.go", Index: 'R'}, false)
	if target.before.Path != "old.go" || target.before.Revision != "first" || target.after.Revision != "next" {
		t.Fatal(target)
	}
	v := filePreview{files: []repo.File{{Path: "removed.go", Index: 'D'}}, compare: true}
	target = v.target(w)
	if target.before.Revision != "first" || target.after.Revision != repo.WorkingFile {
		t.Fatal(target)
	}
}
