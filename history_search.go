package main

import (
	"fmt"
	"strings"

	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
)

const historySearchPlaceholder = "Find (message, author, hash, ref)"
const compactSearchPlaceholder = "Find…"

func (w *workbench) showSearch() {
	if w.path == "" || w.busy || w.viewer != nil {
		return
	}
	if w.searchField != nil {
		w.ui.Window().Context().RequestFocus(w.searchField)
		return
	}
	w.searchOpen, w.focusSearch = true, true
	w.redraw()
}

func (w *workbench) closeSearch() {
	w.searchOpen, w.focusSearch = false, false
	w.search = ""
	w.updateHistorySearch()
	w.focusHistory = true
	w.redraw()
}

func (w *workbench) historySearchPanel() widget.Widget {
	w.searchSummary = state.NewSignal("")
	w.searchDisabled = state.NewSignal(true)
	w.updateHistorySearchSummary()
	cs := w.theme.Colors
	w.searchField = textfield.New(
		textfield.Placeholder(historySearchPlaceholder),
		textfield.A11yLabel("Find in history"),
		textfield.InitialValue(w.search), textfield.Disabled(w.busy),
		textfield.PainterOpt(searchFieldPainter{w.paint.TextField}),
		textfield.ContextMenuPainterOpt(w.paint.Menu),
		textfield.OnChange(func(query string) {
			w.search, w.searchIndex = query, 0
			w.updateHistorySearch()
			w.revealSearchMatch()
		}), textfield.OnSubmit(func(string) { w.stepSearch(1) }),
	)
	control := func(glyph icon.IconData, hint string, disabled state.Signal[bool], action func()) widget.Widget {
		return w.iconButtonSignal(glyph, hint, 22, 14, disabled, action)
	}
	return primitives.HBox(
		primitives.VBox(icon.NewIcon(icon.ExpSearch, icon.Size(14), icon.Color(cs.OnSurfaceSecondary))).Width(14).Height(14),
		primitives.Expanded(primitives.VBox(w.searchField).Height(26)),
		w.muted("").FontSize(10).ContentSignal(w.searchSummary).MaxLines(1),
		control(icon.ExpChevronUp, "Previous match (Shift+Enter)", w.searchDisabled, func() { w.stepSearch(-1) }),
		control(icon.ExpChevronDown, "Next match (Enter)", w.searchDisabled, func() { w.stepSearch(1) }),
		control(icon.ExpClose, "Close search (Esc)", nil, w.closeSearch),
	).MaxWidthValue(320).Height(28).PaddingXY(5, 0).Gap(2).Rounded(4).
		Background(cs.ControlFill).BorderStyle(1, cs.Border).CrossAlign(primitives.CrossAxisCenter)
}

type searchFieldPainter struct{ devtools.TextFieldPainter }

func (searchFieldPainter) ContentPadding() (float32, float32) { return 0, 2 }

func (p searchFieldPainter) PaintTextField(canvas widget.Canvas, field *textfield.PaintState) {
	cs := p.Theme.Colors
	canvas.PushClip(field.ContentRect)
	defer canvas.PopClip()
	if field.ShowSelection {
		canvas.DrawRect(field.SelectionRect, cs.Selection)
	}
	text, bounds, color := field.DisplayText, field.TextRect, cs.OnSurface
	if text == "" {
		text, bounds, color = field.Placeholder, field.ContentRect, cs.OnSurfaceSecondary
		if canvas.MeasureText(text, field.FontSize, false) > bounds.Width() {
			text = compactSearchPlaceholder
		}
	}
	canvas.DrawText(text, bounds, field.FontSize, color, false, widget.TextAlignLeft)
	textfield.PaintCursor(canvas, field, cs.OnSurface, 0)
}

func (w *workbench) updateHistorySearch() {
	query := strings.ToLower(strings.TrimSpace(w.search))
	w.searchMatches = w.searchMatches[:0]
	if query != "" {
		offset := w.uncommittedRowCount()
		for i, c := range w.rows {
			haystack := strings.Join([]string{c.Subject, w.commitAuthor(c), c.Email, c.Hash, c.Refs, c.Body}, " ")
			if strings.Contains(strings.ToLower(haystack), query) {
				w.searchMatches = append(w.searchMatches, i+offset)
			}
		}
	}
	w.searchIndex = min(w.searchIndex, max(0, len(w.searchMatches)-1))
	w.updateHistorySearchSummary()
}

func (w *workbench) updateHistorySearchSummary() {
	if w.searchSummary == nil {
		return
	}
	n := len(w.searchMatches)
	w.searchDisabled.Set(w.busy || n == 0)
	summary := ""
	switch {
	case n > 0:
		summary = fmt.Sprintf("%d/%d", w.searchIndex+1, n)
	case strings.TrimSpace(w.search) != "":
		summary = "No results"
	}
	w.searchSummary.Set(summary)
}

func (w *workbench) stepSearch(delta int) {
	n := len(w.searchMatches)
	if w.busy || n == 0 {
		return
	}
	w.searchIndex = (w.searchIndex + delta + n) % n
	w.updateHistorySearchSummary()
	w.revealSearchMatch()
}

func (w *workbench) revealSearchMatch() {
	if w.table == nil {
		return
	}
	if len(w.searchMatches) > 0 {
		w.table.ScrollToRow(w.searchMatches[w.searchIndex])
	}
	w.table.SetNeedsRedraw(true)
	w.ui.Window().Context().InvalidateRect(w.table.ScreenBounds())
}
