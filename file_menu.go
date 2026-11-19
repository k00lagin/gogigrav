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
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/core/treeview"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

// Keep a single click available for the existing Stage / Unstage toolbar.
type fileTreeActions struct {
	*primitives.BoxWidget
	tree      *treeview.Widget
	root      *treeview.TreeNode
	activate  func(*treeview.TreeNode, bool, geometry.Point)
	last      *treeview.TreeNode
	lastPress time.Time
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
func (a *fileTreeActions) Event(ctx widget.Context, e event.Event) bool {
	mouse, ok := e.(*event.MouseEvent)
	if ok && mouse.MouseType == event.MousePress && a.tree.ScreenBounds().Contains(mouse.GlobalPosition) {
		scroll := a.tree.Children()[0].(*scrollview.Widget)
		_, offset := scroll.ScrollOffset()
		p := mouse.GlobalPosition.Sub(a.tree.ScreenOrigin())
		if p.X < a.tree.Bounds().Width()-scroll.ScrollbarInset() {
			index := int((p.Y + offset) / fileRowHeight)
			if hit := visibleFileAt(a.root.Children, &index); hit != nil {
				if mouse.Button == event.ButtonRight {
					a.activate(hit, true, mouse.GlobalPosition)
					return true
				}
				if mouse.Button == event.ButtonLeft {
					now := time.Now()
					if hit.IsLeaf() && a.last == hit && now.Sub(a.lastPress) < 500*time.Millisecond {
						a.last = nil
						a.activate(hit, false, mouse.GlobalPosition)
						return true
					}
					a.last, a.lastPress = hit, now
				}
			}
		}
	}
	return a.BoxWidget.Event(ctx, e)
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
		paths := []string{path}
		for _, f := range files {
			if (f.Path == path || strings.HasPrefix(f.Path, path+"/")) && f.From != "" && ((!stage && f.Index == 'R') || (stage && f.Work == 'R')) {
				paths = append(paths, f.From)
			}
		}
		add(label, func() {
			args := []string{"add", "-A"}
			if !stage {
				args = []string{"restore", "--staged"}
				if w.data.Head == "" {
					args = []string{"rm", "-r", "--cached", "-f"}
				}
			}
			w.command(verb, "git", append(append(args, "--"), paths...)...)
		})
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
		add(label, func() { w.confirmDiscard(path, paths) })
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
	dir := w.path
	unborn := w.data.Head == ""
	canStash := !unborn && !slices.ContainsFunc(w.data.Files, func(f repo.File) bool { return f.Conflicted })
	message := "Staged and unstaged changes under this path will be saved in a Git stash and removed from the working tree."
	if !canStash {
		message = "Git cannot create a stash in this repository state. Discarding will permanently remove staged and working changes for the selected path."
	}
	dialog.New(dialog.Title("Discard changes to "+path+"?"), dialog.Content(w.text(message)), dialog.Actions(
		dialog.Action{Label: "Cancel", Default: true, Variant: dialog.VariantTextOnly},
		dialog.Action{Label: "Discard", Variant: dialog.VariantFilled, OnClick: func() {
			w.operation("Discard changes", func(ctx context.Context) error {
				if !canStash {
					return repo.DiscardWithoutStash(ctx, dir, paths, unborn)
				}
				args := []string{"stash", "push", "--include-untracked", "-m", "Gogigrav: discard " + path, "--"}
				_, err := repo.Run(ctx, dir, "git", append(args, paths...)...)
				if err != nil {
					return fmt.Errorf("save changes before discard: %w", err)
				}
				return nil
			}, nil)
		}},
	), dialog.PainterOpt(w.paint.Dialog)).Show(w.ui.Window().Context())
}
