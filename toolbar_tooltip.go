package main

import (
	"time"
	"unicode/utf8"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/popover"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

const toolbarTooltipDelay = 450 * time.Millisecond

type toolbarHover struct {
	bar       widget.Widget
	index     int
	label     string
	due       time.Time
	timer     *time.Timer
	visible   *toolbarTooltip
	anchor    func() geometry.Rect
	placement popover.Placement
}

func (w *workbench) hoverToolbarItem(bar widget.Widget, index int, label string, anchor func() geometry.Rect, placement popover.Placement) {
	h := &w.toolbarHover
	if h.bar == bar && h.index == index && h.label == label {
		return
	}
	w.clearToolbarTooltip()
	if label == "" {
		return
	}
	w.toolbarHover = toolbarHover{
		bar: bar, index: index, label: label, anchor: anchor, placement: placement, due: time.Now().Add(toolbarTooltipDelay),
		timer: time.AfterFunc(toolbarTooltipDelay, w.host.RequestRedraw),
	}
}

func (w *workbench) clearToolbarTooltip() {
	h := w.toolbarHover
	w.toolbarHover = toolbarHover{}
	if h.timer != nil {
		h.timer.Stop()
	}
	if h.visible != nil {
		w.ui.Window().Context().OverlayManager().RemoveOverlay(h.visible)
	}
}

func (w *workbench) updateToolbarTooltip(now time.Time) {
	h := &w.toolbarHover
	if h.bar == nil {
		return
	}
	win := w.ui.Window()
	control, statusButton := h.bar.(*button.Widget)
	if win.Overlays().TopInteractive() != nil || statusButton && win.HoveredWidget() != control {
		w.clearToolbarTooltip()
		return
	}
	if h.visible != nil || now.Before(h.due) {
		return
	}
	if manager, ok := win.Context().OverlayManager().(widget.PassiveOverlayManager); ok {
		tip := &toolbarTooltip{anchor: h.anchor, placement: h.placement, text: h.label, painter: w.paint.Popover}
		tip.SetVisible(true)
		manager.PushPassiveOverlay(tip)
		h.visible = tip
	}
}

type toolbarTooltip struct {
	widget.WidgetBase
	anchor    func() geometry.Rect
	placement popover.Placement
	text      string
	painter   popover.Painter
}

func (t *toolbarTooltip) Layout(ctx widget.Context, c geometry.Constraints) geometry.Size {
	width := float32(utf8.RuneCountInString(t.text))*6.5 + 18
	if provider, ok := ctx.(interface{ TextRasterizer() widget.TextRasterizer }); ok {
		if r := provider.TextRasterizer(); r != nil {
			if m, ok := r.Measure(t.text, widget.TextStyle{FontSize: 12}, 1); ok && m.Advance > 0 {
				width = float32(m.Advance) + 18
			}
		}
	}
	size := c.Constrain(geometry.Sz(width, 26))
	pos := popover.CalculatePosition(t.placement, t.anchor(), size, ctx.WindowSize(), 6)
	t.SetBounds(geometry.FromPointSize(pos, size))
	return size
}

func (t *toolbarTooltip) Draw(_ widget.Context, canvas widget.Canvas) {
	bounds := t.Bounds()
	width := min(bounds.Width(), canvas.MeasureText(t.text, 12, false)+18)
	if t.placement == popover.BottomEnd || t.placement == popover.TopEnd {
		bounds.Min.X = bounds.Max.X - width
	} else {
		bounds.Max.X = bounds.Min.X + width
	}
	canvas.PushClip(bounds)
	t.painter.PaintTooltip(canvas, &popover.TooltipPaintState{Bounds: bounds, Text: t.text, Placement: t.placement})
	canvas.PopClip()
}

func (*toolbarTooltip) Event(widget.Context, event.Event) bool { return false }
