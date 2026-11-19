package main

import (
	"time"
	"unicode/utf8"

	"github.com/gogpu/ui/core/popover"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

const toolbarTooltipDelay = 450 * time.Millisecond

type toolbarHover struct {
	bar     *toolbarHints
	index   int
	label   string
	due     time.Time
	timer   *time.Timer
	visible *toolbarTooltip
}

func (w *workbench) hoverToolbarItem(bar *toolbarHints, index int, label string) {
	h := &w.toolbarHover
	if h.bar == bar && h.index == index && h.label == label {
		return
	}
	w.clearToolbarTooltip()
	if label == "" {
		return
	}
	w.toolbarHover = toolbarHover{
		bar: bar, index: index, label: label, due: time.Now().Add(toolbarTooltipDelay),
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
	if win.Overlays().TopInteractive() != nil {
		w.clearToolbarTooltip()
		return
	}
	if h.visible != nil || now.Before(h.due) {
		return
	}
	if manager, ok := win.Context().OverlayManager().(widget.PassiveOverlayManager); ok {
		tip := &toolbarTooltip{bar: h.bar, index: h.index, text: h.label, painter: w.paint.Popover}
		tip.SetVisible(true)
		manager.PushPassiveOverlay(tip)
		h.visible = tip
	}
}

type toolbarTooltip struct {
	widget.WidgetBase
	bar     *toolbarHints
	index   int
	text    string
	painter popover.Painter
}

func (t *toolbarTooltip) Layout(ctx widget.Context, c geometry.Constraints) geometry.Size {
	size := c.Constrain(geometry.Sz(float32(utf8.RuneCountInString(t.text))*9+18, 30))
	anchor := t.bar.ItemBounds(t.index).Translate(t.bar.ScreenOrigin())
	pos := popover.CalculatePosition(popover.BottomStart, anchor, size, ctx.WindowSize(), 6)
	t.SetBounds(geometry.FromPointSize(pos, size))
	return size
}

func (t *toolbarTooltip) Draw(_ widget.Context, canvas widget.Canvas) {
	bounds := t.Bounds()
	width := min(bounds.Width(), canvas.MeasureText(t.text, 12, false)+18)
	bounds.Max.X = bounds.Min.X + width
	canvas.PushClip(bounds)
	t.painter.PaintTooltip(canvas, &popover.TooltipPaintState{Bounds: bounds, Text: t.text, Placement: popover.BottomStart})
	canvas.PopClip()
}

func (*toolbarTooltip) Event(widget.Context, event.Event) bool { return false }
