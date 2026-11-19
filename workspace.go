package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/checkbox"
	"github.com/gogpu/ui/core/datatable"
	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/core/dropdown"
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/core/splitview"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/core/treeview"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func (w *workbench) text(s string) *primitives.TextWidget {
	return primitives.Text(s).FontSize(13).Color(w.theme.Colors.OnSurface)
}
func (w *workbench) muted(s string) *primitives.TextWidget {
	return w.text(s).Color(w.theme.Colors.OnSurfaceSecondary)
}
func (w *workbench) btn(s string, v button.Variant, enabled bool, fn func()) widget.Widget {
	return button.New(button.TextOpt(s), button.VariantOpt(v), button.SizeOpt(button.Small), button.Disabled(!enabled || w.busy), button.OnClick(fn), button.PainterOpt(w.paint.Button))
}
func (w *workbench) input(label, val string, change func(string), submit func(string)) *textfield.Widget {
	opts := []textfield.Option{textfield.Placeholder(label), textfield.A11yLabel(label), textfield.InitialValue(val), textfield.Disabled(w.busy), textfield.PainterOpt(w.paint.TextField)}
	if change != nil {
		opts = append(opts, textfield.OnChange(change))
	}
	if submit != nil {
		opts = append(opts, textfield.OnSubmit(submit))
	}
	return textfield.New(opts...)
}

func compactInput(field *textfield.Widget) widget.Widget {
	return primitives.VBox(field).Height(32)
}
func (w *workbench) pick(label string, items []string, selected int, fn func(int)) widget.Widget {
	return dropdown.New(dropdown.Items(items...), dropdown.Placeholder(label), dropdown.Selected(selected), dropdown.MaxVisibleItems(12), dropdown.Disabled(w.busy || len(items) == 0), dropdown.PainterOpt(w.paint.Dropdown), dropdown.OnChange(func(i int, _ string) {
		if !w.busy && i >= 0 && i < len(items) {
			fn(i)
		}
	}))
}
func (w *workbench) check(label string, value, enabled bool, change func(bool)) widget.Widget {
	return checkbox.New(checkbox.Label(label), checkbox.Checked(value), checkbox.Disabled(w.busy || !enabled), checkbox.OnToggle(change), checkbox.PainterOpt(w.paint.Checkbox))
}
func row(items ...widget.Widget) *primitives.BoxWidget {
	return primitives.HBox(items...).Gap(6).CrossAlign(primitives.CrossAxisCenter)
}
func line(w *workbench) widget.Widget {
	return primitives.Box().Height(1).Background(w.theme.Colors.Border)
}
func (w *workbench) showError(err error) {
	d := dialog.New(dialog.Title("Command failed"), dialog.Content(scrollview.New(w.text(err.Error()), scrollview.PainterOpt(w.paint.Scrollbar))), dialog.MaxWidth(620), dialog.MaxHeight(330), dialog.Actions(dialog.Action{Label: "Close", Default: true, Variant: dialog.VariantFilled}), dialog.PainterOpt(w.paint.Dialog))
	d.Show(w.ui.Window().Context())
}

func (w *workbench) render() {
	w.closeToolbarPopup()
	w.dirty = false
	cs := w.theme.Colors
	hint := state.NewSignal("")
	active := w.path != ""
	w.titleBar = w.newTitleBar(row(w.repositoryToolbar(hint)).PaddingLeft(4), w.actionToolbar(hint))
	w.searchField = nil
	var body widget.Widget
	if active {
		var find widget.Widget = primitives.Box()
		if w.searchOpen {
			w.searchField = w.input("Find by message, author, hash or ref", w.search, func(s string) {
				w.search = s
				w.filterRows()
			}, nil)
			find = compactInput(w.searchField)
		}
		w.loadMoreDisabled = state.NewSignal(!w.data.More || w.busy)
		loadMore := button.New(button.TextOpt("Load more"), button.VariantOpt(button.TextOnly),
			button.SizeOpt(button.Small), button.DisabledSignal(w.loadMoreDisabled), button.PainterOpt(w.paint.Button),
			button.OnClick(func() { f := w.filter; f.Limit += 250; w.refresh(f) }))
		historyBar := row(w.text("History").Bold(), primitives.Expanded(find), loadMore).PaddingXY(10, 5).Background(cs.Surface)
		if w.table == nil {
			w.table = w.historyTable()
		}
		history := primitives.VBox(historyBar, line(w), primitives.Expanded(w.table)).Background(cs.Background)
		body = splitview.New(splitview.First(history), splitview.Second(w.panel()), splitview.InitialRatio(w.ratio), splitview.OnRatioChange(func(r float32) { w.ratio = r }), splitview.MinFirst(380), splitview.MinSecond(320), splitview.DividerWidth(3), splitview.PainterOpt(w.paint.SplitView))
	} else {
		body = primitives.VBox(w.panel()).Padding(26).Background(cs.Background)
	}
	w.statusText = state.NewSignal(w.historyStatus())
	footer := row(primitives.Expanded(w.muted("").ContentSignal(w.statusText).MaxLines(1).Ellipsis()),
		w.muted("").ContentSignal(hint).MaxLines(1).Ellipsis(), w.muted(w.path).MaxLines(1).Ellipsis()).PaddingXY(10, 4).Background(cs.Surface)
	w.ui.SetRoot(primitives.VBox(w.titleBar, primitives.Expanded(body), line(w), footer).Background(cs.Background))
	if w.focusSearch && w.searchField != nil {
		w.focusSearch = false
		w.ui.Window().Context().RequestFocus(w.searchField)
	}
}

func (w *workbench) historyStatus() string {
	if w.path == "" || w.status != "" {
		return w.status
	}
	tracking := ""
	if w.data.Upstream != "" {
		tracking = fmt.Sprintf("   %s ↑%d ↓%d", w.data.Upstream, w.data.Ahead, w.data.Behind)
	}
	return fmt.Sprintf("%s%s     %d commits     %d changes     %d stashes", w.data.Branch, tracking, len(w.data.Commits), len(w.data.Files), len(w.data.Stashes))
}

func (w *workbench) showSearch() {
	if w.path == "" || w.busy {
		return
	}
	if w.searchField != nil {
		w.ui.Window().FocusManager().Focus(w.searchField)
		w.ui.Window().Context().RequestFocus(w.searchField)
		return
	}
	w.searchOpen, w.focusSearch = true, true
	w.redraw()
}

func (w *workbench) toggleSearch() {
	if !w.searchOpen {
		w.showSearch()
		return
	}
	w.searchOpen = false
	w.search = ""
	w.filterRows()
	w.redraw()
}

func shortRef(ref string) string {
	return strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/remotes/")
}
func displayDate(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return s
}
func (w *workbench) historyTable() *datatable.Widget {
	cols := []datatable.Column{{Key: "description", Title: "Description", MinWidth: 120}, {Key: "date", Title: "Date", Width: 126}, {Key: "author", Title: "Author", Width: 106}, {Key: "hash", Title: "Commit", Width: 82}}
	return datatable.New(datatable.Columns(cols), datatable.RowCountFn(func() int { return len(w.rows) + 1 }), datatable.RowHeight(27), datatable.PainterOpt(historyPainter{w.paint.DataTable, w}), datatable.SelectedRowSignal(w.selected), datatable.SelectionModeOpt(datatable.SelectionSingle), datatable.A11yLabel("Commit history"), datatable.CellValue(func(i int, k string) string {
		if i == 0 {
			if k == "description" {
				return fmt.Sprintf("Uncommitted Changes (%d)", len(w.data.Files))
			}
			return ""
		}
		if i < 1 || i > len(w.rows) {
			return ""
		}
		c := w.rows[i-1]
		switch k {
		case "date":
			return displayDate(c.Date)
		case "author":
			return c.Author
		case "hash":
			return c.Short()
		case "description":
			return c.Subject
		}
		return ""
	}), datatable.OnRowSelect(func(i int) {
		if w.busy {
			return
		}
		w.form = nil
		w.file = nil
		if i == 0 {
			w.commit = repo.Commit{}
			w.show("changes")
			return
		}
		if i < 1 || i > len(w.rows) {
			return
		}
		c, path := w.rows[i-1], w.path
		w.commit = c
		w.page = "commit"
		w.changed = nil
		w.job("Read changed paths", func(ctx context.Context) (func(), error) {
			files, err := repo.ChangedPaths(ctx, path, c)
			return func() { w.changed = files }, err
		})
	}))
}
func (w *workbench) panel() widget.Widget {
	cs := w.theme.Colors
	var title string
	var content widget.Widget
	switch w.page {
	case "settings":
		title = "Settings"
		idx := 1
		if w.prefs.Light {
			idx = 0
		}
		content = primitives.VBox(w.muted("Color theme"), w.pick("Theme", []string{"DevTools", "DevTools Dark"}, idx, func(i int) {
			w.prefs.Light = i == 0
			w.prefs.save()
			w.setTheme()
			w.ui.SetTheme(w.theme.AsTheme())
			w.table = nil
			w.redraw()
		}), w.btn("Check GitHub sign-in", button.Outlined, true, w.checkGitHub)).Gap(8).Padding(12)
	case "open":
		title = "Open repository"
		content = w.openPanel()
	case "form":
		title = w.form.title
		content = w.formPanel()
	case "commit":
		title = "Commit details"
		content = w.commitPanel()
	default:
		title = "Uncommitted Changes"
		content = w.changesPanel()
	}
	head := []widget.Widget{primitives.Expanded(w.text(title).Bold())}
	if w.path != "" && w.page != "changes" {
		head = append(head, w.btn("Changes", button.TextOnly, true, func() { w.commit = repo.Commit{}; w.selected.Set(0); w.show("changes") }))
	}
	if w.path == "" && w.page != "open" {
		head = append(head, w.btn("Open repository", button.TextOnly, true, func() { w.show("open") }))
	}
	return primitives.VBox(row(head...).PaddingXY(12, 6).Height(40), line(w), primitives.Expanded(content)).Background(cs.Surface)
}

type fileItem struct {
	file  repo.File
	stage bool
}

func (w *workbench) fileTree(groups ...*treeview.TreeNode) widget.Widget {
	root := &treeview.TreeNode{ID: "root", Label: "Files", Expanded: true, Children: groups}
	return treeview.New(treeview.Root(root), treeview.SelectionModeOpt(treeview.SelectionSingle), treeview.ItemHeight(23), treeview.IndentWidth(12), treeview.ShowLines(true), treeview.PainterOpt(w.paint.TreeView), treeview.OnSelect(func(n *treeview.TreeNode) {
		item, _ := n.Data.(*fileItem)
		w.file = item
		w.ui.Window().Context().Invalidate()
	}))
}
func fileGroup(name string, files []repo.File, stage bool, filter func(repo.File) bool) *treeview.TreeNode {
	root := &treeview.TreeNode{ID: name, Expanded: true}
	dirs := map[string]*treeview.TreeNode{"": root}
	count := 0
	for _, f := range files {
		if filter != nil && !filter(f) {
			continue
		}
		count++
		parts := strings.Split(f.Path, "/")
		parent := root
		dir := ""
		for _, part := range parts[:len(parts)-1] {
			dir += part + "/"
			if dirs[dir] == nil {
				n := &treeview.TreeNode{ID: name + "/" + dir, Label: part, Expanded: true}
				parent.Children = append(parent.Children, n)
				dirs[dir] = n
			}
			parent = dirs[dir]
		}
		status := f.Index
		if stage {
			status = f.Work
		}
		label := parts[len(parts)-1]
		if f.From != "" {
			label += "  ← " + f.From
		}
		parent.Children = append(parent.Children, &treeview.TreeNode{ID: name + "/" + f.Path, Label: fmt.Sprintf("%c   %s", status, label), Data: &fileItem{f, stage}})
	}
	root.Label = fmt.Sprintf("%s  (%d)", name, count)
	return root
}

func (w *workbench) commitPanel() widget.Widget {
	c := w.commit
	meta := []widget.Widget{w.text(c.Subject).Bold().MaxLines(3), w.muted(c.Author + "  <" + c.Email + ">"), w.muted(displayDate(c.Date)), w.muted(c.Hash).FontSize(11)}
	if c.Body != "" {
		body := scrollview.New(w.text(c.Body), scrollview.PainterOpt(w.paint.Scrollbar))
		meta = append(meta, primitives.VBox(body).Height(82))
	}
	if c.Refs != "" {
		meta = append(meta, w.text(c.Refs).Color(w.theme.Colors.Primary))
	}
	if c.Parents != "" {
		var parents []string
		for _, p := range strings.Fields(c.Parents) {
			parents = append(parents, p[:min(8, len(p))])
		}
		meta = append(meta, w.muted("Parents   "+strings.Join(parents, "  ")))
	}
	meta = append(meta, row(w.btn("Create branch", button.Outlined, true, func() { w.branchAt(c.Hash) }), w.btn("Create tag", button.Outlined, true, func() { w.tagAt(c.Hash) })))
	return primitives.VBox(primitives.VBox(meta...).Padding(12).Gap(5), line(w), primitives.Expanded(w.fileTree(fileGroup("Changed files", w.changed, false, nil))))
}

func (w *workbench) changesPanel() widget.Widget {
	d := w.data
	staged := slices.ContainsFunc(d.Files, repo.File.Staged)
	msg := w.input("Commit message", w.drafts[w.path], func(s string) { w.drafts[w.path] = s }, nil)
	commit := func() {
		message := w.drafts[w.path]
		if strings.TrimSpace(message) == "" {
			w.showError(fmt.Errorf("enter a commit message"))
			return
		}
		path := w.path
		args := []string{"commit", "-m", message}
		if w.amend {
			args = append(args, "--amend")
		}
		w.operation("Commit", func(ctx context.Context) error { _, err := repo.Run(ctx, path, "git", args...); return err }, func() { delete(w.drafts, path); w.amend = false })
	}
	amend := w.check("Amend", w.amend, d.Head != "", func(v bool) {
		w.amend = v
		if v && w.drafts[w.path] == "" {
			for _, c := range d.Commits {
				if c.Hash == d.Head {
					w.drafts[w.path] = strings.TrimSpace(c.Subject + "\n\n" + c.Body)
					break
				}
			}
		}
		w.redraw()
	})
	controls := primitives.VBox(w.muted("Commit to "+d.Branch), compactInput(msg), row(w.btn("Commit", button.Filled, staged || w.amend, commit), amend)).Gap(7).Padding(12)
	selectAction := func(stage bool) widget.Widget {
		title := "Unstage"
		if stage {
			title = "Stage"
		}
		return button.New(button.TextOpt(title), button.SizeOpt(button.Small), button.VariantOpt(button.Outlined), button.PainterOpt(w.paint.Button), button.DisabledFn(func() bool { return w.busy || w.file == nil || w.file.stage != stage }), button.OnClick(func() {
			if w.file == nil {
				return
			}
			path, f, unborn := w.path, w.file.file, d.Head == ""
			w.operation(title, func(ctx context.Context) error { return repo.ChangeIndex(ctx, path, f, stage, unborn) }, nil)
		}))
	}
	actions := row(selectAction(true), selectAction(false), w.btn("Stage all", button.TextOnly, slices.ContainsFunc(d.Files, repo.File.Unstaged), func() { w.command("Stage all", "git", "add", "-A", "--", ".") })).PaddingXY(10, 6)
	return primitives.VBox(controls, line(w), actions, primitives.Expanded(w.fileTree(fileGroup("Staged changes", d.Files, false, repo.File.Staged), fileGroup("Changes", d.Files, true, repo.File.Unstaged))))
}
