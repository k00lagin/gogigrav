package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/popover"
	"github.com/gogpu/ui/core/treeview"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

const fileRowHeight = 23

type fileItem struct {
	file  repo.File
	stage bool
}

func (item fileItem) change() (byte, repo.DiffStat) {
	switch {
	case item.file.Conflicted:
		return 'U', repo.DiffStat{}
	case item.stage:
		return item.file.Work, item.file.WorkStat
	default:
		return item.file.Index, item.file.IndexStat
	}
}

func fileTreeRoot(id string, files []repo.File, stage bool) *treeview.TreeNode {
	root := &treeview.TreeNode{ID: id, Expanded: true}
	dirs := map[string]*treeview.TreeNode{"": root}
	for _, file := range files {
		parts := strings.Split(file.Path, "/")
		dir := ""
		parent := root
		for _, part := range parts[:len(parts)-1] {
			dir += part + "/"
			if dirs[dir] == nil {
				folder := &treeview.TreeNode{ID: id + "/" + dir, Label: part, Expanded: true}
				parent.Children = append(parent.Children, folder)
				dirs[dir] = folder
			}
			parent = dirs[dir]
		}
		item := &fileItem{file: file, stage: stage}
		label := parts[len(parts)-1]
		status, _ := item.change()
		if file.From != "" && (status == 'R' || status == 'C') {
			label += "  ← " + file.From
		}
		parent.Children = append(parent.Children, &treeview.TreeNode{ID: id + "/" + file.Path, Label: label, Data: item})
	}
	compactFileFolders(root)
	return root
}

func compactFileFolders(parent *treeview.TreeNode) {
	for _, folder := range parent.Children {
		for len(folder.Children) == 1 && !folder.Children[0].IsLeaf() {
			child := folder.Children[0]
			folder.Label += " / " + child.Label
			folder.ID = child.ID
			folder.Children = child.Children
		}
		if !folder.IsLeaf() {
			compactFileFolders(folder)
		}
	}
	slices.SortStableFunc(parent.Children, func(a, b *treeview.TreeNode) int {
		if a.IsLeaf() != b.IsLeaf() {
			if a.IsLeaf() {
				return 1
			}
			return -1
		}
		return strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
	})
}

func (w *workbench) fileSection(title, empty string, files []repo.File, stage, worktree bool, maxRows int, selection state.Signal[string]) widget.Widget {
	colors := w.theme.Colors
	header := &fileSectionHeader{title: strings.ToUpper(title), count: strconv.Itoa(len(files)),
		text: colors.OnSurface, badgeText: colors.OnSurfaceSecondary, badge: colors.Border}
	header.SetVisible(true)
	var heading widget.Widget = header
	if worktree {
		glyph := icon.Minus
		if stage {
			glyph = icon.ExpAdd
		}
		label := fileIndexLabel(stage, false) + " all"
		all := w.iconButton(glyph, label, 24, 14, func() bool { return w.busy || len(files) == 0 },
			func() { w.changeAllIndex(stage) })
		heading = &buttonHints{BoxWidget: row(primitives.Expanded(header), all).Height(30).PaddingRight(6),
			bench: w, hints: map[*button.Widget]string{all: label}, placement: popover.BottomEnd}
	}
	if len(files) == 0 {
		message := row(w.muted(empty)).Height(fileRowHeight+8).PaddingXY(12, 0)
		return primitives.VBox(heading, line(w), message)
	}

	root := fileTreeRoot(title, files, stage)
	painter := &fileTreePainter{
		TreeViewPainter: w.paint.TreeView,
		dark:            w.theme.IsDark(),
		colors:          w.fileChangeColors(),
		actions:         worktree && !w.busy,
		stage:           stage,
	}
	var selected *treeview.TreeNode
	tree := treeview.New(treeview.Root(root), treeview.HideRoot(true), treeview.FitContent(maxRows > 0),
		treeview.A11yLabel(title), treeview.SelectionModeOpt(treeview.SelectionSingle),
		treeview.SelectedNodeSignal(selection), treeview.ItemHeight(fileRowHeight), treeview.IndentWidth(16),
		treeview.ShowLines(true), treeview.PainterOpt(painter), treeview.ScrollbarPainterOpt(w.paint.Scrollbar),
		treeview.OnSelect(func(n *treeview.TreeNode) {
			selected = n
			w.file, _ = n.Data.(*fileItem)
			if v := w.viewer; v != nil && w.file != nil {
				for i, f := range files {
					if f.Path == w.file.file.Path && i != v.index {
						w.openPreview(files, i, stage, v.compare, v.mode)
						break
					}
				}
			}
			w.ui.Window().Context().Invalidate()
		}))
	wrap := func(box *primitives.BoxWidget) widget.Widget {
		actions := &fileTreeActions{BoxWidget: box, tree: tree, root: root, bench: w, stage: stage,
			activate: func(n *treeview.TreeNode, menu bool, at geometry.Point) {
				w.fileTreeAction(title, n, files, stage, menu, at)
			}}
		if worktree {
			actions.selected = func() *treeview.TreeNode {
				if selected != nil && selection.Get() == selected.ID {
					return selected
				}
				return nil
			}
			actions.change = func(n *treeview.TreeNode, discard bool) {
				if w.busy {
					return
				}
				paths := fileTreePaths(title, n, files, stage, discard)
				if discard {
					w.confirmDiscard(paths[0], paths)
				} else {
					w.changeFileIndex(fileIndexLabel(stage, !n.IsLeaf()), paths, stage)
				}
			}
		}
		return actions
	}
	if maxRows > 0 {
		return wrap(primitives.VBox(heading, line(w), primitives.VBox(tree).MaxHeightValue(float32(maxRows)*fileRowHeight)))
	}
	return wrap(primitives.VBox(heading, line(w), primitives.Expanded(tree)))
}

type fileSectionHeader struct {
	widget.WidgetBase
	title, count           string
	text, badgeText, badge widget.Color
}

func (h *fileSectionHeader) Layout(_ widget.Context, c geometry.Constraints) geometry.Size {
	return c.Constrain(geometry.Sz(c.BiggestFinite(220, 30).Width, 30))
}

func (h *fileSectionHeader) Draw(_ widget.Context, canvas widget.Canvas) {
	const font, padding, gap = 11, 10, 6
	bounds := h.Bounds()
	titleWidth := canvas.MeasureText(h.title, font, true)
	badgeWidth := max(18, canvas.MeasureText(h.count, font, false)+10)
	title := geometry.NewRect(bounds.Min.X+padding, bounds.Min.Y, titleWidth, bounds.Height())
	badge := geometry.NewRect(title.Max.X+gap, bounds.Center().Y-9, badgeWidth, 18)
	canvas.PushClip(bounds)
	canvas.DrawText(h.title, title, font, h.text, true, widget.TextAlignLeft)
	canvas.DrawRoundRect(badge, h.badge, 9)
	canvas.DrawText(h.count, badge, font, h.badgeText, false, widget.TextAlignCenter)
	canvas.PopClip()
}

func (*fileSectionHeader) Event(widget.Context, event.Event) bool { return false }

func (w *workbench) worktreeFiles(files []repo.File) widget.Widget {
	var staged, unstaged []repo.File
	for _, file := range files {
		if file.Staged() {
			staged = append(staged, file)
		}
		if file.Unstaged() {
			unstaged = append(unstaged, file)
		}
	}
	selection := state.NewSignal("")
	stagedRows := 0
	if len(unstaged) != 0 {
		stagedRows = 5
	}
	stagedSection := w.fileSection("Staged changes", "No staged changes", staged, false, true, stagedRows, selection)
	unstagedSection := w.fileSection("Changes", "No unstaged changes", unstaged, true, true, 0, selection)
	if len(unstaged) == 0 && len(staged) != 0 {
		return primitives.VBox(primitives.Expanded(stagedSection), line(w), unstagedSection)
	}
	if len(unstaged) != 0 {
		unstagedSection = primitives.Expanded(unstagedSection)
	}
	return primitives.VBox(stagedSection, line(w), unstagedSection)
}
