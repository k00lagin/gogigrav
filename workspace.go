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
	w.clearToolbarTooltip()
	w.closeToolbarPopup()
	w.dirty = false
	cs := w.theme.Colors
	hint := state.NewSignal("")
	active := w.path != ""
	historyFocused := w.table != nil && w.ui.Window().Context().FocusedWidget() == w.table
	w.titleBar = w.newTitleBar(row(w.repositoryToolbar(hint)).PaddingLeft(4), w.actionToolbar(hint))
	w.searchField = nil
	w.inlineContent = nil
	w.inlineGraph = nil
	var body widget.Widget
	if w.viewer != nil && active {
		body = w.previewPanel()
	} else if active {
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
		detailRow := w.syncInlineDetails()
		if w.revealInline && detailRow >= 0 {
			w.table.ScrollToRow(detailRow)
		}
		w.revealInline = false
		if w.usesInlineHistory() {
			body = history
		} else {
			body = splitview.New(splitview.First(history), splitview.Second(w.panel()),
				splitview.InitialRatio(w.ratio), splitview.OnRatioChange(func(r float32) { w.ratio = r }),
				splitview.MinFirst(380), splitview.MinSecond(320), splitview.DividerWidth(3), splitview.PainterOpt(w.paint.SplitView))
		}
	} else {
		body = primitives.VBox(w.panel()).Padding(26).Background(cs.Background)
	}
	w.ui.SetRoot(primitives.VBox(w.titleBar, primitives.Expanded(body), line(w), w.statusBar(hint)).Background(cs.Background))
	if (w.focusHistory || historyFocused) && active && w.viewer == nil {
		w.focusHistory = false
		w.ui.Window().Context().RequestFocus(w.table)
	}
	if w.focusSearch && w.searchField != nil {
		w.focusSearch = false
		w.ui.Window().Context().RequestFocus(w.searchField)
	}
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
	return strings.TrimPrefix(strings.TrimPrefix(ref, repo.LocalRefPrefix), repo.RemoteRefPrefix)
}
func (w *workbench) commitDate(c repo.Commit) string {
	raw := c.Date
	if w.prefs.Settings.CommitDate && c.CommitDate != "" {
		raw = c.CommitDate
	}
	date, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	switch w.prefs.Settings.DateFormat {
	case "date":
		return date.Local().Format("2006-01-02")
	case "relative":
		return relativeDate(date, time.Now())
	default:
		return date.Local().Format("2006-01-02 15:04")
	}
}

func relativeDate(date, now time.Time) string {
	age := now.Sub(date)
	future := age < 0
	for _, unit := range []struct {
		duration time.Duration
		name     string
	}{{365 * 24 * time.Hour, "year"}, {30 * 24 * time.Hour, "month"}, {24 * time.Hour, "day"}, {time.Hour, "hour"}, {time.Minute, "minute"}} {
		if count := int(age.Abs() / unit.duration); count > 0 {
			name := unit.name
			if count != 1 {
				name += "s"
			}
			if future {
				return fmt.Sprintf("in %d %s", count, name)
			}
			return fmt.Sprintf("%d %s ago", count, name)
		}
	}
	return "just now"
}

func (w *workbench) uncommittedRowCount() int {
	if w.prefs.Settings.HideUncommitted {
		return 0
	}
	return 1
}

func (w *workbench) showCommitter(c repo.Commit) bool {
	return w.prefs.Settings.ShowCommitter && c.Committer != "" &&
		(c.Committer != c.Author || c.CommitterEmail != c.Email)
}

func (w *workbench) commitAuthor(c repo.Commit) string {
	if w.showCommitter(c) {
		return c.Author + " / " + c.Committer
	}
	return c.Author
}

func (w *workbench) historyTable() *datatable.Widget {
	authorWidth := float32(106)
	if w.prefs.Settings.ShowCommitter {
		authorWidth = 190
	}
	cols := []datatable.Column{{Key: "description", Title: historyDescription, MinWidth: 120}, {Key: "date", Title: "Date", Width: 126}, {Key: "author", Title: "Author", Width: authorWidth}, {Key: "hash", Title: "Commit", Width: 82}}
	return datatable.New(datatable.Columns(cols), datatable.RowCountFn(func() int { return len(w.rows) + w.uncommittedRowCount() }), datatable.RowHeight(27), datatable.PainterOpt(historyPainter{w.paint.DataTable, w}), datatable.SelectedRowSignal(w.selected), datatable.SelectionModeOpt(datatable.SelectionSingle), datatable.A11yLabel("Commit history"), datatable.CellValue(func(i int, k string) string {
		i -= w.uncommittedRowCount()
		if i == -1 {
			if k == "description" {
				return fmt.Sprintf("Uncommitted Changes (%d)", len(w.data.Files))
			}
			return ""
		}
		if i < 0 || i >= len(w.rows) {
			return ""
		}
		c := w.rows[i]
		switch k {
		case "date":
			return w.commitDate(c)
		case "author":
			return w.commitAuthor(c)
		case "hash":
			return c.Short()
		case "description":
			return c.Subject
		}
		return ""
	}), datatable.DisabledFn(func() bool { return w.busy }), datatable.OnRowSelect(w.selectHistoryRow), datatable.OnRowReselect(func(i int) {
		if w.prefs.Settings.UnifiedDetails {
			w.selectHistoryRow(i)
		}
	}))
}

func (w *workbench) selectHistoryRow(row int) {
	if w.busy {
		return
	}
	if row == w.inlineHistoryRow() {
		w.closeInlineDetails()
		return
	}
	i := row - w.uncommittedRowCount()
	if i < -1 || i >= len(w.rows) {
		return
	}
	w.form = nil
	w.file = nil
	w.inlineClosed = false
	w.focusHistory = w.prefs.Settings.UnifiedDetails
	w.revealInline = w.focusHistory
	if i == -1 {
		w.commit = repo.Commit{}
		w.show("changes")
		return
	}
	c, path := w.rows[i], w.path
	w.commit = c
	w.page = "commit"
	w.changed = nil
	w.job("Read changed paths", func(ctx context.Context) (func(), error) {
		files, err := repo.ChangedPaths(ctx, path, c)
		return func() { w.changed = files }, err
	})
}
func (w *workbench) panel() widget.Widget {
	cs := w.theme.Colors
	var title string
	var content widget.Widget
	switch w.page {
	case "open":
		title = "Open repository"
		content = w.openPanel()
	case "form":
		title = w.form.title
		content = w.formPanel()
	case "commit":
		title = "Commit details"
		content = w.commitPanel(false)
	default:
		title = "Uncommitted Changes"
		content = w.changesPanel(false)
	}
	head := []widget.Widget{primitives.Expanded(w.text(title).Bold())}
	if w.path != "" && w.page != "changes" {
		head = append(head, w.btn("Changes", button.TextOnly, true, func() { w.commit = repo.Commit{}; w.selected.Set(w.uncommittedRowCount() - 1); w.show("changes") }))
	}
	if w.path == "" && w.page != "open" {
		head = append(head, w.btn("Open repository", button.TextOnly, true, func() { w.show("open") }))
	}
	return primitives.VBox(row(head...).PaddingXY(12, 6).Height(40), line(w), primitives.Expanded(content)).Background(cs.Surface)
}

func (w *workbench) commitPanel(inline bool) widget.Widget {
	c := w.commit
	meta := []widget.Widget{w.text(c.Subject).Bold().MaxLines(3), w.muted(c.Author + "  <" + c.Email + ">")}
	if w.showCommitter(c) {
		meta = append(meta, w.muted("Committer  "+c.Committer+"  <"+c.CommitterEmail+">"))
	}
	meta = append(meta, w.muted(w.commitDate(c)), w.muted(c.Hash).FontSize(11))
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
	metadata := primitives.VBox(meta...).Padding(12).Gap(5)
	files := w.fileSection("Changed files", "No changed files", w.changed, false, 0, state.NewSignal(""))
	if inline {
		return w.inlineColumns(scrollview.New(metadata, scrollview.PainterOpt(w.paint.Scrollbar)), files)
	}
	return primitives.VBox(metadata, line(w), primitives.Expanded(files))
}

func (w *workbench) changesPanel(inline bool) widget.Widget {
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
	files := w.worktreeFiles(d.Files)
	if inline {
		return w.inlineColumns(controls, primitives.VBox(actions, line(w), primitives.Expanded(files)))
	}
	return primitives.VBox(controls, line(w), actions, primitives.Expanded(files))
}
