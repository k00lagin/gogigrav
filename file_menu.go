package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/core/menu"
	"github.com/gogpu/ui/core/popover"
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/core/treeview"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

type fileTreeActions struct {
	*primitives.BoxWidget
	tree      *treeview.Widget
	root      *treeview.TreeNode
	bench     *workbench
	stage     bool
	selected  func() *treeview.TreeNode
	change    func(*treeview.TreeNode, bool)
	activate  func(*treeview.TreeNode, bool, geometry.Point)
	last      *treeview.TreeNode
	lastPress time.Time
}

var discardFileIcon = icon.FromSVGXML("discard-file", []byte(`<svg viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M6.5 3 3 6.5 6.5 10M3 6.5h6a3 3 0 0 1 0 6H7.5" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg>`))

func fileIndexLabel(stage, folder bool) string {
	label := "Unstage"
	if stage {
		label = "Stage"
	}
	if folder {
		label += " folder"
	}
	return label
}

func fileDiscardLabel(node *treeview.TreeNode) string {
	if item, ok := node.Data.(*fileItem); ok {
		if item.stage && item.file.Work == '?' {
			return "Delete file…"
		}
		return "Discard changes…"
	}
	return "Discard folder changes…"
}

func fileRowButtons(row geometry.Rect) (discard, index geometry.Rect) {
	const rightPadding, width = 29, 21
	x := row.Max.X - rightPadding - 2*width
	return geometry.NewRect(x, row.Min.Y, width, row.Height()),
		geometry.NewRect(x+width, row.Min.Y, width, row.Height())
}

func visibleFileAt(nodes []*treeview.TreeNode, index *int) *treeview.TreeNode {
	for _, n := range nodes {
		if *index == 0 {
			return n
		}
		*index--
		if n.Expanded {
			if hit := visibleFileAt(n.Children, index); hit != nil {
				return hit
			}
		}
	}
	return nil
}

func (a *fileTreeActions) rowAt(point geometry.Point) (*treeview.TreeNode, geometry.Rect, int) {
	bounds := a.tree.ScreenBounds()
	scroll := a.tree.Children()[0].(*scrollview.Widget)
	bounds.Max.X -= scroll.ScrollbarInset()
	if !bounds.Contains(point) {
		return nil, geometry.Rect{}, 0
	}
	_, offset := scroll.ScrollOffset()
	index := int((point.Y - bounds.Min.Y + offset) / fileRowHeight)
	row := geometry.NewRect(bounds.Min.X, bounds.Min.Y+float32(index)*fileRowHeight-offset, bounds.Width(), fileRowHeight)
	remaining := index
	return visibleFileAt(a.root.Children, &remaining), row, index
}

func (a *fileTreeActions) Event(ctx widget.Context, e event.Event) bool {
	if key, ok := e.(*event.KeyEvent); ok && a.change != nil && a.tree.IsFocused() &&
		key.KeyType == event.KeyPress && key.Key == event.KeySpace && key.Modifiers() == event.ModNone {
		if hit := a.selected(); hit != nil {
			a.change(hit, false)
			return true
		}
	}
	if mouse, ok := e.(*event.MouseEvent); ok && (mouse.MouseType == event.MousePress || mouse.MouseType == event.MouseMove && a.change != nil) {
		if a.handleMouse(ctx, mouse) {
			return true
		}
	}
	if _, ok := e.(*event.WheelEvent); ok && a.bench.toolbarHover.bar == a {
		a.bench.clearToolbarTooltip()
	}
	return a.BoxWidget.Event(ctx, e)
}

func (a *fileTreeActions) handleMouse(ctx widget.Context, mouse *event.MouseEvent) bool {
	point := mouse.GlobalPosition
	node, row, index := a.rowAt(point)
	if mouse.MouseType == event.MouseMove {
		var label string
		var anchor geometry.Rect
		if node != nil && !a.bench.busy {
			discard, change := fileRowButtons(row)
			switch {
			case discard.Contains(point):
				label, anchor = fileDiscardLabel(node), discard
			case change.Contains(point):
				label, anchor = fileIndexLabel(a.stage, !node.IsLeaf()), change
			}
		}
		if label != "" {
			ctx.SetCursor(widget.CursorPointer)
			a.bench.hoverToolbarItem(a, index, label, func() geometry.Rect { return anchor }, popover.BottomEnd)
		} else if a.bench.toolbarHover.bar == a {
			a.bench.clearToolbarTooltip()
		}
		return false
	}
	if node == nil {
		return false
	}
	switch mouse.Button {
	case event.ButtonRight:
		a.activate(node, true, point)
		return true
	case event.ButtonLeft:
		if a.change != nil && !a.bench.busy {
			discard, change := fileRowButtons(row)
			shouldDiscard := discard.Contains(point)
			if shouldDiscard || change.Contains(point) {
				a.last = nil
				a.bench.clearToolbarTooltip()
				a.change(node, shouldDiscard)
				return true
			}
		}
		now := time.Now()
		if node.IsLeaf() && a.last == node && now.Sub(a.lastPress) < 500*time.Millisecond {
			a.last = nil
			a.activate(node, false, point)
			return true
		}
		a.last, a.lastPress = node, now
	}
	return false
}

func fileTreePaths(title string, node *treeview.TreeNode, files []repo.File, stage, discard bool) []string {
	path := strings.TrimSuffix(strings.TrimPrefix(node.ID, title+"/"), "/")
	paths := []string{path}
	for _, f := range files {
		renamed := (discard || !stage) && f.Index == 'R' || (discard || stage) && f.Work == 'R'
		if renamed && f.From != "" && (f.Path == path || strings.HasPrefix(f.Path, path+"/")) {
			paths = append(paths, f.From)
		}
	}
	return paths
}

func (w *workbench) changeFileIndex(label string, paths []string, stage bool) {
	dir, unborn := w.path, w.data.Head == ""
	w.operation(label, func(ctx context.Context) error {
		return repo.ChangeIndexPaths(ctx, dir, paths, stage, unborn)
	}, nil)
}

func (w *workbench) changeAllIndex(stage bool) {
	w.changeFileIndex(fileIndexLabel(stage, false)+" all", []string{"."}, stage)
}
func (w *workbench) fileTreeAction(title string, n *treeview.TreeNode, files []repo.File, stage, contextMenu bool, position geometry.Point) {
	if w.busy {
		return
	}
	path := strings.TrimSuffix(strings.TrimPrefix(n.ID, title+"/"), "/")
	item, _ := n.Data.(*fileItem)
	index := slices.IndexFunc(files, func(f repo.File) bool { return f.Path == path })
	open := func(mode string, compare bool) {
		if index >= 0 {
			w.openPreview(files, index, stage, compare, mode)
		}
	}
	if !contextMenu {
		open(diffMode, false)
		return
	}
	var entries []menu.MenuItem
	add := func(label string, fn func()) { entries = append(entries, menu.Item(label, "", fn)) }
	if item != nil && index >= 0 {
		t := w.target(item.file, stage)
		add("View Diff", func() { open(diffMode, false) })
		revision := t.after.Revision
		if revision == "" {
			revision = t.before.Revision
			add("View File before Deletion", func() { open(beforeMode, false) })
		} else {
			add("View File at this Revision", func() { open(afterMode, false) })
		}
		entries = append(entries, menu.MenuItem{Label: "View Diff with Working File", Disabled: revision == repo.WorkingFile, OnAction: func() { open(diffMode, true) }})
		add("Open Working File", func() { open(workingMode, false) })
		entries = append(entries, menu.Sep())
	}
	if w.page != "commit" && w.viewer == nil {
		verb := "Unstage"
		if stage {
			verb = "Stage"
		}
		label := verb
		if item == nil {
			label += " Folder"
		}
		paths := fileTreePaths(title, n, files, stage, false)
		add(label, func() { w.changeFileIndex(verb, paths, stage) })
		if item != nil && item.file.Conflicted {
			for _, side := range []string{"ours", "theirs"} {
				add("Resolve using "+strings.ToUpper(side[:1])+side[1:], func() {
					dir := w.path
					w.operation("Resolve conflict", func(ctx context.Context) error {
						if _, err := repo.Run(ctx, dir, "git", "checkout", "--"+side, "--", path); err != nil {
							return err
						}
						_, err := repo.Run(ctx, dir, "git", "add", "--", path)
						return err
					}, nil)
				})
			}
		}
		label = "Discard Changes…"
		if item == nil {
			label = "Discard Folder Changes…"
		} else if item.file.Work == '?' {
			label = "Delete File…"
		}
		add(label, func() { w.confirmDiscard(path, fileTreePaths(title, n, files, stage, true)) })
		entries = append(entries, menu.Sep())
	}
	abs := filepath.Join(w.path, filepath.FromSlash(path))
	add("Reveal in File Explorer", func() {
		if err := revealFile(abs); err != nil {
			w.showError(err)
		}
	})
	add("Copy Absolute File Path", func() { widget.ClipboardWrite(abs) })
	add("Copy Relative File Path", func() { widget.ClipboardWrite(path) })
	menu.NewContextMenu(entries, menu.ContextPainterOpt(w.paint.Menu)).Show(w.ui.Window().Context(), position)
}
func revealFile(path string) error {
	parent := filepath.Dir(path)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		if info, err := os.Lstat(path); err == nil && info.IsDir() {
			cmd = exec.Command("explorer.exe", path)
		} else if err == nil {
			cmd = exec.Command("explorer.exe", "/select,", path)
		} else {
			cmd = exec.Command("explorer.exe", parent)
		}
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	default:
		cmd = exec.Command("xdg-open", parent)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
func (w *workbench) confirmDiscard(path string, paths []string) {
	w.clearToolbarTooltip()
	dir := w.path
	unborn := w.data.Head == ""
	canStash := w.canStashDiscard()
	message := "Staged and unstaged changes under this path will be saved in a Git stash and removed from the working tree."
	if !canStash {
		message = "Git cannot create a stash in this repository state. Discarding will permanently remove staged and working changes for the selected path."
	}
	dialog.New(dialog.Title("Discard changes to "+path+"?"), dialog.Content(w.text(message)), dialog.Actions(
		dialog.Action{Label: "Cancel", Default: true, Variant: dialog.VariantTextOnly},
		dialog.Action{Label: "Discard", Variant: dialog.VariantFilled, OnClick: func() {
			w.discardChanges(dir, "Discard changes", path, paths, unborn, canStash, true)
		}},
	), dialog.PainterOpt(w.paint.Dialog)).Show(w.ui.Window().Context())
}

func (w *workbench) canStashDiscard() bool {
	return w.data.Head != "" && !slices.ContainsFunc(w.data.Files, func(f repo.File) bool { return f.Conflicted })
}

func (w *workbench) confirmDiscardAll() {
	dir, unborn := w.path, w.data.Head == ""
	canStash := w.canStashDiscard()
	message := "Staged and unstaged changes will be saved in a Git stash and removed from the working tree."
	if !canStash {
		message = "Git cannot create a stash in this repository state. Discarding will permanently remove staged and unstaged changes to tracked files."
	}
	untracked := flagField("Also delete untracked files and directories", false)
	w.formDialog(formDialogOptions{
		title: "Discard all changes?", description: message, submit: "Discard all",
		fields: []*formField{untracked},
		onSubmit: func() (func(), error) {
			return func() {
				w.discardChanges(dir, "Discard all changes", "all changes", []string{"."}, unborn, canStash, untracked.checked)
			}, nil
		},
	})
}

func (w *workbench) discardChanges(dir, label, description string, paths []string, unborn, canStash, untracked bool) {
	if w.path != dir {
		return
	}
	w.operation(label, func(ctx context.Context) error {
		if !canStash {
			return repo.DiscardWithoutStash(ctx, dir, paths, unborn, untracked)
		}
		args := []string{"stash", "push", "-m", "Gogigrav: discard " + description}
		if untracked {
			args = append(args, "--include-untracked")
		}
		if _, err := repo.Run(ctx, dir, "git", append(append(args, "--"), paths...)...); err != nil {
			return fmt.Errorf("save changes before discard: %w", err)
		}
		return nil
	}, nil)
}
