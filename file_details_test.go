package main

import (
	"testing"
	"time"

	"github.com/gogpu/ui/core/treeview"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func TestFileTreeCompactsOnlySingleFolderChains(t *testing.T) {
	root := fileTreeRoot("Changes", []repo.File{
		{Path: "internal/repo/repo.go", Work: 'M'},
		{Path: "internal/repo/stats.go", Work: 'M'},
		{Path: "src/api/request.go", Work: 'A'},
		{Path: "src/main.go", Work: 'M'},
	}, true)
	if len(root.Children) != 2 || root.Children[0].Label != "internal / repo" ||
		root.Children[1].Label != "src" || root.Children[1].Children[0].Label != "api" {
		t.Fatalf("folder labels did not compact as expected: %+v", root.Children)
	}
	file := root.Children[0].Children[0]
	if file.Data.(*fileItem).file.Path != "internal/repo/repo.go" || file.Label != "repo.go" {
		t.Fatalf("compaction altered the file path or label: %+v", file)
	}
}

func TestWorktreeFilesShareOneSelectionAcrossTwoTrees(t *testing.T) {
	path := t.TempDir()
	testGit(t, path, "init", "-b", "main")
	w := testWorkbench(t, path)
	w.drafts = make(map[string]string)
	w.prefs.Settings.UnifiedDetails = true
	w.data.Files = []repo.File{{Path: "partially-staged.txt", Index: 'M', Work: 'M'}}
	testActionFrame(w)
	var trees []*treeview.Widget
	walkWidgets(w.inlineContent, func(node widget.Widget) {
		if tree, ok := node.(*treeview.Widget); ok {
			trees = append(trees, tree)
		}
	})
	if len(trees) != 2 || trees[0].RowCount() != 1 || trees[1].RowCount() != 1 {
		t.Fatalf("expected two trees with one file row each, got %d trees", len(trees))
	}
	testClick(w, trees[0].ScreenBounds().Center())
	if w.file == nil || w.file.stage {
		t.Fatal("staged row did not become the selection")
	}
	testClick(w, trees[1].ScreenBounds().Min.Add(geometry.Pt(100, fileRowHeight/2)))
	if w.file == nil || !w.file.stage || trees[0].AccessibilityValue() != "1 visible rows" {
		t.Fatal("unstaged row did not replace the staged selection")
	}
}

func TestToolbarTooltipAppearsNextToTheRepositoryButton(t *testing.T) {
	path := t.TempDir()
	testGit(t, path, "init", "-b", "main")
	w := testWorkbench(t, path)
	win := w.ui.Window()
	anchor := w.branchBar.ItemBounds(0).Translate(w.branchBar.ScreenOrigin())
	position := anchor.Center()
	win.HandleEvent(event.NewMouseEvent(event.MouseMove, event.ButtonNone, 0, position, position, event.ModNone))
	if w.toolbarHover.label != w.branchBar.repoPath || w.toolbarHover.due.IsZero() {
		t.Fatalf("repository tooltip was not scheduled: %+v", w.toolbarHover)
	}
	w.updateToolbarTooltip(w.toolbarHover.due.Add(-time.Millisecond))
	if win.HasOverlays() {
		t.Fatal("tooltip opened before its hover delay")
	}
	w.updateToolbarTooltip(w.toolbarHover.due)
	w.ui.Frame()
	win.DrawTo(&uitest.MockCanvas{})
	tip := w.toolbarHover.visible
	if tip == nil || tip.Bounds().Min.Y <= anchor.Max.Y || tip.Bounds().Min.X != anchor.Min.X {
		t.Fatalf("expected a tooltip anchored beneath the repository button, got %v", tip)
	}
	history := geometry.Pt(200, 90)
	win.HandleEvent(event.NewMouseEvent(event.MouseMove, event.ButtonNone, 0, history, history, event.ModNone))
	if win.HasOverlays() || w.toolbarHover.bar != nil {
		t.Fatal("moving away from the toolbar did not hide the tooltip")
	}
	win.HandleEvent(event.NewMouseEvent(event.MouseMove, event.ButtonNone, 0, position, position, event.ModNone))
	w.updateToolbarTooltip(w.toolbarHover.due)
	testClick(w, position)
	if w.toolbarHover.visible != nil || w.openPopup == nil {
		t.Fatal("tooltip did not give way to the repository popup")
	}
}
