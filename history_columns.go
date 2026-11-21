package main

import (
	"math"
	"time"

	"github.com/gogpu/ui/core/datatable"
	"github.com/gogpu/ui/core/menu"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
)

const historyHeaderHeight = 36
const historyRowHeight = 27
const historyColumnMin = 44

var historyColumns = []datatable.Column{
	{Key: "diff", Title: "Diff", Width: 116}, {Key: "date", Title: "Date", Width: 126},
	{Key: "author", Title: "Author", Width: 106}, {Key: "hash", Title: "Commit", Width: 82},
}

type columnLayout struct {
	Hidden map[string]bool
	Widths map[string]float32
}

func (w *workbench) columnWidth(col datatable.Column) float32 {
	if width := w.prefs.Columns.Widths[col.Key]; width >= historyColumnMin {
		return width
	}
	if col.Key == "author" && w.prefs.Settings.ShowCommitter {
		return 190
	}
	return col.Width
}

type historyHeader struct {
	*primitives.BoxWidget
	bench                        *workbench
	columns                      []datatable.Column
	table                        *datatable.Widget
	badges                       map[int][]historyBadge
	branchDrag                   historyBranchDrag
	drag, last                   string
	pressX, startWidth, maxWidth float32
	pressed                      time.Time
	resizeCursor                 bool
}

func (h *historyHeader) Layout(ctx widget.Context, c geometry.Constraints) geometry.Size {
	// Keep the graph and subject visible when the history panel gets narrower.
	maxFixed := max(historyColumnMin*float32(len(h.columns)-1), c.BiggestFinite(900, 500).Width-h.columns[0].MinWidth)
	remaining := maxFixed
	for i := len(h.columns) - 1; i > 0; i-- {
		col := &h.columns[i]
		col.Width = min(h.bench.columnWidth(defaultHistoryColumn(col.Key)), remaining-historyColumnMin*float32(i-1))
		remaining -= col.Width
	}
	return h.BoxWidget.Layout(ctx, c)
}

func defaultHistoryColumn(key string) datatable.Column {
	for _, col := range historyColumns {
		if col.Key == key {
			return col
		}
	}
	return datatable.Column{}
}

func (h *historyHeader) Event(ctx widget.Context, e event.Event) bool {
	if h.historyInteraction(ctx, e) {
		return true
	}
	mouse, ok := e.(*event.MouseEvent)
	if !ok {
		return h.BoxWidget.Event(ctx, e)
	}
	w := h.bench
	if h.drag != "" {
		switch mouse.MouseType {
		case event.MouseMove:
			if math.Abs(float64(mouse.GlobalPosition.X-h.pressX)) < 1 && h.last != "" {
				return true
			}
			h.last = ""
			saved := w.prefs.Columns.Widths
			if saved == nil {
				saved = make(map[string]float32)
				w.prefs.Columns.Widths = saved
			}
			saved[h.drag] = max(historyColumnMin, min(float32(math.Round(float64(h.startWidth+h.pressX-mouse.GlobalPosition.X))), h.maxWidth))
			h.relayout()
			return true
		case event.MouseRelease:
			h.drag = ""
			if capture, ok := ctx.(widget.PointerCapturer); ok {
				capture.ReleasePointer(h)
			}
			w.prefs.save(w.settingsPath)
			return true
		}
	}
	bounds := h.ScreenBounds()
	onHandle := false
	if bounds.Contains(mouse.GlobalPosition) && mouse.GlobalPosition.Y < bounds.Min.Y+historyHeaderHeight {
		if mouse.MouseType == event.MousePress && mouse.Button == event.ButtonRight {
			w.historyColumnMenu(mouse.GlobalPosition)
			return true
		}
		boundary := bounds.Min.X + h.descriptionWidth()
		for _, col := range h.columns[1:] {
			if math.Abs(float64(mouse.GlobalPosition.X-boundary)) <= 5 {
				switch mouse.MouseType {
				case event.MouseMove:
					ctx.SetCursor(widget.CursorResizeEW)
					onHandle = true
				case event.MousePress:
					if mouse.Button != event.ButtonLeft {
						break
					}
					now := time.Now()
					if h.last == col.Key && now.Sub(h.pressed) < 500*time.Millisecond {
						delete(w.prefs.Columns.Widths, col.Key)
						h.last = ""
						h.relayout()
						w.prefs.save(w.settingsPath)
						return true
					}
					h.last, h.pressed = col.Key, now
					h.drag, h.pressX, h.startWidth = col.Key, mouse.GlobalPosition.X, col.Width
					h.maxWidth = col.Width + max(0, h.descriptionWidth()-h.columns[0].MinWidth)
					if capture, ok := ctx.(widget.PointerCapturer); ok {
						capture.CapturePointer(h)
					}
					return true
				}
				break
			}
			boundary += col.Width
		}
	}
	if mouse.MouseType == event.MouseMove || mouse.MouseType == event.MouseLeave {
		if h.resizeCursor && !onHandle {
			ctx.SetCursor(widget.CursorDefault)
		}
		h.resizeCursor = onHandle
	}
	return h.BoxWidget.Event(ctx, e)
}

func (h *historyHeader) descriptionWidth() float32 {
	width := h.Bounds().Width()
	for _, col := range h.columns[1:] {
		width -= col.Width
	}
	return max(width, h.columns[0].MinWidth)
}

func (h *historyHeader) relayout() {
	h.bench.table.MarkNeedsLayout()
	h.MarkNeedsLayout()
}

func (w *workbench) historyColumnMenu(at geometry.Point) {
	var items []menu.MenuItem
	apply := func() {
		w.prefs.save(w.settingsPath)
		w.table = nil
		w.redraw()
	}
	for _, col := range historyColumns {
		label := "✓  " + col.Title
		if w.prefs.Columns.Hidden[col.Key] {
			label = "    " + col.Title
		}
		items = append(items, menu.Item(label, "", func() {
			if w.prefs.Columns.Hidden == nil {
				w.prefs.Columns.Hidden = make(map[string]bool)
			}
			w.prefs.Columns.Hidden[col.Key] = !w.prefs.Columns.Hidden[col.Key]
			apply()
		}))
	}
	items = append(items, menu.Sep(), menu.Item("Reset Columns", "", func() { w.prefs.Columns = columnLayout{}; apply() }))
	menu.NewContextMenu(items, menu.ContextPainterOpt(w.paint.Menu)).Show(w.ui.Window().Context(), at)
}
