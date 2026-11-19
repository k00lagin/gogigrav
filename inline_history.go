package main

import (
	"slices"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

const inlineDetailHeight = 330

func (w *workbench) usesInlineHistory() bool {
	return w.prefs.Settings.UnifiedDetails &&
		(w.page == "commit" || (w.page == "changes" && w.uncommittedRowCount() != 0))
}

func (w *workbench) inlineHistoryRow() int {
	if !w.usesInlineHistory() || w.inlineClosed {
		return -1
	}
	if w.page == "changes" {
		return 0
	}
	i := slices.IndexFunc(w.rows, func(c repo.Commit) bool { return c.Hash == w.commit.Hash })
	if i < 0 {
		return -1
	}
	return i + w.uncommittedRowCount()
}

func (w *workbench) syncInlineDetails() int {
	row := w.inlineHistoryRow()
	if row >= 0 && w.inlineContent == nil {
		w.inlineContent = w.inlinePanel()
	}
	if w.inlineGraph != nil {
		w.inlineGraph.row = row
		if width := w.graph.width(); width != w.inlineGraph.width {
			w.inlineGraph.width = width
			w.inlineGraph.MarkNeedsLayout()
		}
		w.inlineGraph.SetNeedsRedraw(true)
	}
	w.table.SetRowDetails(row, w.inlineContent, inlineDetailHeight)
	return row
}

func (w *workbench) closeInlineDetails() {
	w.inlineClosed = true
	w.focusHistory = true
	w.selected.Set(-1)
	w.redraw()
}

func (w *workbench) inlinePanel() widget.Widget {
	var content widget.Widget
	title := "Working tree"
	if w.page == "commit" {
		title = "Commit details"
		content = w.commitPanel(true)
	} else {
		content = w.changesPanel(true)
	}
	closeButton := button.New(button.TextOpt("×"), button.A11yHint("Close details"), button.SizeOpt(button.Small),
		button.VariantOpt(button.TextOnly), button.PainterOpt(w.paint.Button), button.OnClick(w.closeInlineDetails))
	header := row(primitives.Expanded(w.muted(title)), closeButton).Height(29).PaddingXY(10, 0)
	details := primitives.VBox(line(w), header, line(w), primitives.Expanded(content), line(w)).
		CrossAlign(primitives.CrossAxisStretch)
	w.inlineGraph = &historyGraphGutter{bench: w, width: w.graph.width(), row: -1}
	return primitives.HBox(w.inlineGraph, primitives.Expanded(details)).
		CrossAlign(primitives.CrossAxisStretch).Background(w.theme.Colors.Surface)
}

func (w *workbench) inlineColumns(left, right widget.Widget) widget.Widget {
	divider := primitives.Box().Width(1).Background(w.theme.Colors.Border)
	return primitives.HBox(primitives.VBox(left).Width(400), divider, primitives.Expanded(right)).
		CrossAlign(primitives.CrossAxisStretch)
}
