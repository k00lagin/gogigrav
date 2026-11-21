package main

import (
	"fmt"
	"strings"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
)

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
	w.searchField = w.input("Find message, author, hash, ref", w.search, func(query string) {
		w.search, w.searchIndex = query, 0
		w.updateHistorySearch()
		w.revealSearchMatch()
	}, func(string) { w.stepSearch(1) })
	control := func(label, hint string, disabled state.Signal[bool], action func()) widget.Widget {
		return button.New(button.TextOpt(label), button.A11yHint(hint),
			button.SizeOpt(button.Small), button.VariantOpt(button.TextOnly),
			button.DisabledSignal(disabled), button.PainterOpt(w.paint.Button), button.OnClick(action))
	}
	return primitives.HBox(
		icon.NewIcon(icon.ExpSearch, icon.Size(13), icon.Color(w.theme.Colors.OnSurfaceSecondary)),
		primitives.Expanded(compactInput(w.searchField)),
		w.muted("").FontSize(11).ContentSignal(w.searchSummary).MaxLines(1),
		control("↑", "Previous match (Shift+Enter)", w.searchDisabled, func() { w.stepSearch(-1) }),
		control("↓", "Next match (Enter)", w.searchDisabled, func() { w.stepSearch(1) }),
		control("×", "Close search (Esc)", nil, w.closeSearch),
	).Width(254).Gap(2).CrossAlign(primitives.CrossAxisCenter)
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
