package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/checkbox"
	"github.com/gogpu/ui/core/treeview"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func TestWorktreeRowAndSectionActions(t *testing.T) {
	dir := t.TempDir()
	testGit(t, dir, "init", "-b", "main")
	write := func(path, content string) {
		t.Helper()
		name := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("tracked.txt", "original")
	testGit(t, dir, "add", "--", ".")
	testGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@test", "commit", "-qm", "first")
	write("tracked.txt", "changed")
	write("sub [1]/new.txt", "new")
	w := testWorkbench(t, dir)
	w.prefs.Settings.UnifiedDetails = true
	testActionFrame(w)
	win := w.ui.Window()
	rowAction := func(stage bool, row int, discard bool) {
		t.Helper()
		var section *fileTreeActions
		walkWidgets(w.inlineContent, func(node widget.Widget) {
			if tree, ok := node.(*fileTreeActions); ok && tree.stage == stage {
				section = tree
			}
		})
		if section == nil {
			t.Fatal("missing worktree tree")
		}
		bounds := section.tree.ScreenBounds()
		_, rowBounds, _ := section.rowAt(bounds.Min.Add(geometry.Pt(20, float32(row)*fileRowHeight+fileRowHeight/2)))
		remove, index := fileRowButtons(rowBounds)
		point := index.Center()
		if discard {
			point = remove.Center()
		}
		win.HandleEvent(event.NewMouseEvent(event.MouseMove, event.ButtonNone, 0, point, point, event.ModNone))
		if w.toolbarHover.bar != section {
			t.Fatal("row action did not offer a tooltip on hover")
		}
		testClick(w, point)
	}
	bulk := func(label string) {
		t.Helper()
		var control *button.Widget
		walkWidgets(w.inlineContent, func(node widget.Widget) {
			if heading, ok := node.(*buttonHints); ok {
				for btn, hint := range heading.hints {
					if hint == label {
						control = btn
					}
				}
			}
		})
		if control == nil {
			t.Fatalf("missing section button %q", label)
		}
		testClick(w, control.ScreenBounds().Center())
		testWaitForGit(t, w)
	}

	rowAction(true, 0, false) // Folder.
	testWaitForGit(t, w)
	if got := testGit(t, dir, "diff", "--cached", "--name-only"); got != "sub [1]/new.txt" {
		t.Fatalf("stage folder staged %q", got)
	}
	rowAction(false, 1, false) // File.
	testWaitForGit(t, w)
	if got := testGit(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("unstage file left %q staged", got)
	}
	bulk("Stage all")
	if got := testGit(t, dir, "diff", "--cached", "--name-only"); got != "sub [1]/new.txt\ntracked.txt" {
		t.Fatalf("stage all staged %q", got)
	}
	bulk("Unstage all")
	if got := testGit(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("unstage all left %q staged", got)
	}

	key := func(k event.Key) { win.HandleEvent(event.NewKeyEvent(event.KeyPress, k, 0, event.ModNone)) }
	walkWidgets(w.inlineContent, func(node widget.Widget) {
		if tree, ok := node.(*fileTreeActions); ok && tree.stage {
			testClick(w, tree.tree.ScreenBounds().Min.Add(geometry.Pt(90, 2.5*fileRowHeight)))
		}
	})
	key(event.KeySpace)
	testWaitForGit(t, w)
	if got := testGit(t, dir, "diff", "--cached", "--name-only"); got != "tracked.txt" {
		t.Fatalf("Space staged %q", got)
	}

	rowAction(true, 0, true)
	if w.busy || !win.HasOverlays() {
		t.Fatal("discard folder should wait for confirmation")
	}
	key(event.KeyEnter)
	if w.busy || win.HasOverlays() {
		t.Fatal("the default discard action should cancel")
	}
	rowAction(true, 0, true)
	key(event.KeyTab)
	key(event.KeyEnter)
	testWaitForGit(t, w)
	if got := testGit(t, dir, "status", "--porcelain", "-uall"); got != "M  tracked.txt" {
		t.Fatalf("discard folder touched the wrong files: %q", got)
	}

	write("keep.txt", "untracked")
	w.refresh(w.filter)
	testWaitForGit(t, w)
	discardAll := func(includeUntracked bool) {
		t.Helper()
		var trigger, submit *button.Widget
		findButton := func(root widget.Widget, label string) *button.Widget {
			var found *button.Widget
			walkWidgets(root, func(node widget.Widget) {
				if btn, ok := node.(*button.Widget); ok {
					canvas := &uitest.MockCanvas{}
					btn.Draw(win.Context(), canvas)
					for _, call := range canvas.Texts {
						if call.Text == label {
							found = btn
						}
					}
				}
			})
			if found == nil {
				t.Fatalf("missing button %q", label)
			}
			return found
		}
		trigger = findButton(w.inlineContent, "Discard All…")
		testClick(w, trigger.ScreenBounds().Center())
		w.ui.Frame()
		win.DrawOverlays(&uitest.MockCanvas{})
		overlay := win.OverlayContentWidgets()[0]
		if includeUntracked {
			walkWidgets(overlay, func(node widget.Widget) {
				if check, ok := node.(*checkbox.Widget); ok {
					testClick(w, check.ScreenBounds().Center())
				}
			})
		}
		submit = findButton(overlay, "Discard all")
		testClick(w, submit.ScreenBounds().Center())
		testWaitForGit(t, w)
	}
	discardAll(false)
	if got := testGit(t, dir, "status", "--porcelain"); got != "?? keep.txt" {
		t.Fatalf("default discard all status = %q", got)
	}
	discardAll(true)
	if got := testGit(t, dir, "status", "--porcelain"); got != "" || len(w.data.Stashes) != 3 {
		t.Fatalf("discard all including untracked left %q, stashes %v", got, w.data.Stashes)
	}
}

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
