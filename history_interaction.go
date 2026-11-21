package main

import (
	"strings"

	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

type historyHit struct {
	row    int
	commit repo.Commit
	label  historyRef
}

type historyBranchDrag struct {
	from, target historyHit
	press        geometry.Point
	active       bool
}

func historyRowAt(y float32, detail int) int {
	if y < 0 {
		return -1
	}
	if detail >= 0 {
		top := float32(detail+1) * historyRowHeight
		if y >= top && y < top+inlineDetailHeight {
			return -1
		}
		if y >= top+inlineDetailHeight {
			y -= inlineDetailHeight
		}
	}
	return int(y / historyRowHeight)
}

func (h *historyHeader) historyHitAt(at geometry.Point) historyHit {
	miss := historyHit{row: -1}
	bounds := h.table.ScreenBounds()
	scroll := h.table.Children()[0].(*scrollview.Widget)
	p := at.Sub(bounds.Min)
	if !bounds.Contains(at) || p.Y < historyHeaderHeight || p.X >= bounds.Width()-scroll.ScrollbarInset() {
		return miss
	}
	_, offset := scroll.ScrollOffset()
	p.Y += offset - historyHeaderHeight
	row := historyRowAt(p.Y, h.bench.inlineHistoryRow())
	if row < 0 || row >= h.table.GetRowCount() {
		return miss
	}
	hit := historyHit{row: row}
	if i := row - h.bench.uncommittedRowCount(); i >= 0 {
		hit.commit = h.bench.rows[i]
	}
	for _, badge := range h.badges[row] {
		if badge.bounds.Contains(p) {
			hit.label = badge.label
			break
		}
	}
	return hit
}

func (h *historyHeader) historyInteraction(ctx widget.Context, e event.Event) bool {
	w := h.bench
	drag := &h.branchDrag
	if key, ok := e.(*event.KeyEvent); ok && ctx.IsFocused(h.table) {
		if drag.from.label.ref != "" {
			if key.KeyType == event.KeyPress && key.Key == event.KeyEscape {
				h.finishBranchDrag(ctx)
			}
			return true
		}
		if !w.busy && isContextMenuKey(key) {
			bounds := h.table.ScreenBounds()
			row := w.selected.Get()
			hit := historyHit{row: row}
			if i := row - w.uncommittedRowCount(); i >= 0 && i < len(w.rows) {
				hit.commit = w.rows[i]
			}
			at := geometry.Pt(bounds.Min.X, bounds.Min.Y+historyHeaderHeight)
			if row >= 0 && row < h.table.GetRowCount() {
				scroll := h.table.Children()[0].(*scrollview.Widget)
				_, offset := scroll.ScrollOffset()
				y := float32(row+1) * historyRowHeight
				if detail := w.inlineHistoryRow(); detail >= 0 && row > detail {
					y += inlineDetailHeight
				}
				at.Y = min(bounds.Max.Y, at.Y+max(0, y-offset))
				w.openHistoryMenu(hit, at)
			} else {
				w.historyColumnMenu(at)
			}
			return true
		}
	}
	mouse, ok := e.(*event.MouseEvent)
	if !ok || h.drag != "" {
		return false
	}
	if w.busy {
		if drag.from.label.ref != "" {
			h.finishBranchDrag(ctx)
		}
		return false
	}

	at := mouse.GlobalPosition
	if drag.from.label.ref != "" {
		switch mouse.MouseType {
		case event.MouseMove:
			if !mouse.Buttons.IsLeftPressed() {
				h.finishBranchDrag(ctx)
				return false
			}
			delta := at.Sub(drag.press)
			if !drag.active && delta.X*delta.X+delta.Y*delta.Y < 25 {
				return true
			}
			drag.active = true
			target := w.branchDropTarget(drag.from, h.historyHitAt(at))
			cursor := widget.CursorMove
			if target.commit.Hash == "" {
				cursor = widget.CursorNotAllowed
			}
			ctx.SetCursor(cursor)
			if target.commit.Hash != drag.target.commit.Hash || target.row != drag.target.row || target.label.ref != drag.target.label.ref {
				drag.target = target
				h.invalidateHistory(ctx)
			}
			return true
		case event.MouseRelease:
			if mouse.Button != event.ButtonLeft {
				return true
			}
			from, active := drag.from, drag.active
			to := h.historyHitAt(at)
			h.finishBranchDrag(ctx)
			if active {
				if target := w.branchDropTarget(from, to); target.commit.Hash != "" {
					w.openBranchDropMenu(from, target, at)
				}
			} else if from.commit.Hash == to.commit.Hash && from.label.ref == to.label.ref {
				w.openHistoryMenu(from, at)
			}
			return true
		case event.MousePress:
			return true
		case event.MouseLeave:
			if drag.active && drag.target.commit.Hash != "" {
				drag.target = historyHit{}
				h.invalidateHistory(ctx)
			}
		}
		return false
	}

	if mouse.MouseType != event.MousePress {
		return false
	}
	hit := h.historyHitAt(at)
	if hit.row < 0 {
		return false
	}
	if mouse.Button == event.ButtonRight || (mouse.Button == event.ButtonLeft && hit.label.text != "") {
		ctx.RequestFocus(h.table)
		if mouse.Button == event.ButtonLeft && strings.HasPrefix(hit.label.ref, repo.LocalRefPrefix) {
			if capture, ok := ctx.(widget.PointerCapturer); ok {
				*drag = historyBranchDrag{from: hit, press: at}
				capture.CapturePointer(h)
				return true
			}
		}
		w.openHistoryMenu(hit, at)
		return true
	}
	return false
}

func (h *historyHeader) invalidateHistory(ctx widget.Context) {
	h.table.SetNeedsRedraw(true)
	ctx.InvalidateRect(h.table.ScreenBounds())
}

func (h *historyHeader) finishBranchDrag(ctx widget.Context) {
	h.branchDrag = historyBranchDrag{}
	ctx.SetCursor(widget.CursorDefault)
	if capture, ok := ctx.(widget.PointerCapturer); ok {
		capture.ReleasePointer(h)
	}
	h.invalidateHistory(ctx)
}

func (w *workbench) branchDropTarget(from, to historyHit) historyHit {
	if !strings.HasPrefix(to.label.ref, repo.LocalRefPrefix) {
		to.label = historyRef{}
	}
	if to.commit.Hash == "" || w.historyStash(to.commit.Hash).Hash != "" ||
		to.label.ref == from.label.ref || (to.label.ref == "" && to.commit.Hash == from.commit.Hash) {
		return historyHit{}
	}
	return to
}
