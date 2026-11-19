package main

import (
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

const (
	graphPadding  = 6
	graphSpacing  = 16
	graphMaxWidth = 188
)

func (g historyGraph) width() float32 {
	return min(graphPadding*2+float32(max(1, g.maxLanes))*graphSpacing, graphMaxWidth)
}

func (g historyGraph) laneX(lane int) float32 {
	spacing := (g.width() - graphPadding*2) / float32(max(1, g.maxLanes))
	return graphPadding + (float32(lane)+0.5)*spacing
}

var graphColors = [...]struct {
	key         string
	light, dark uint32
}{
	{"terminal.ansiBlue", 0x0451a5, 0x2472c8},
	{"terminal.ansiMagenta", 0xbc05bc, 0xbc3fbc},
	{"terminal.ansiGreen", 0x107c10, 0x0dbc79},
	{"terminal.ansiYellow", 0x949800, 0xe5e510},
	{"terminal.ansiCyan", 0x0598bc, 0x11a8cd},
	{"terminal.ansiRed", 0xcd3131, 0xcd3131},
	{"terminal.ansiBrightBlue", 0x0451a5, 0x3b8eea},
	{"terminal.ansiBrightMagenta", 0xbc05bc, 0xd670d6},
	{"terminal.ansiBrightGreen", 0x14ce14, 0x23d18b},
	{"terminal.ansiBrightYellow", 0xb5ba00, 0xf5f543},
	{"terminal.ansiBrightCyan", 0x0598bc, 0x29b8db},
	{"terminal.ansiBrightRed", 0xcd3131, 0xf14c4c},
}

func (w *workbench) setGraphPalette() {
	settings := w.prefs.Settings
	for i, entry := range graphColors {
		color := entry.light
		if w.theme.IsDark() {
			color = entry.dark
		}
		w.graphPalette[i] = widget.Hex(color)
		if settings.UseImportedTheme && settings.ImportedTheme != nil {
			if imported, ok := settings.ImportedTheme.color(entry.key); ok {
				w.graphPalette[i] = imported
			}
		}
	}
}

func (w *workbench) paintGraphRow(canvas widget.Canvas, bounds geometry.Rect, index int, background widget.Color) {
	if index < 0 || index >= len(w.graph.rows) || bounds.IsEmpty() {
		return
	}
	row := w.graph.rows[index]
	middle := bounds.Center().Y
	canvas.PushClip(bounds)
	defer canvas.PopClip()
	for _, segment := range row.segments {
		top, bottom := bounds.Min.Y, bounds.Max.Y
		switch segment.part {
		case graphTop:
			bottom = middle
		case graphBottom:
			top = middle
		}
		from := geometry.Pt(bounds.Min.X+w.graph.laneX(segment.from), top)
		to := geometry.Pt(bounds.Min.X+w.graph.laneX(segment.to), bottom)
		drawGraphSegment(canvas, from, to, w.graphPalette[segment.color])
	}
	center := geometry.Pt(bounds.Min.X+w.graph.laneX(row.lane), middle)
	color := w.graphPalette[row.color]
	switch row.kind {
	case graphWorkingTreeNode:
		canvas.DrawCircle(center, 4, background)
		canvas.StrokeCircle(center, 4, color, 2)
	case graphStashNode:
		box := geometry.NewRect(center.X-4, center.Y-4, 8, 8)
		canvas.DrawRoundRect(box, color, 1.5)
		canvas.StrokeRoundRect(box, background, 1.5, 1)
	case graphHeadNode:
		canvas.DrawCircle(center, 5, color)
		canvas.StrokeCircle(center, 5, w.theme.Colors.OnSurface, 1.5)
	default:
		canvas.DrawCircle(center, 4, color)
	}
}

func drawGraphSegment(canvas widget.Canvas, from, to geometry.Point, color widget.Color) {
	if from.X == to.X {
		canvas.DrawLine(from, to, color, 2)
		return
	}
	dy := to.Y - from.Y
	first := from.Add(geometry.Pt(0, dy*0.9))
	second := from.Add(geometry.Pt(to.X-from.X, dy*0.1))
	previous := from
	for i := 1; i <= 12; i++ {
		t := float32(i) / 12
		a, b, c := from.Lerp(first, t), first.Lerp(second, t), second.Lerp(to, t)
		point := a.Lerp(b, t).Lerp(b.Lerp(c, t), t)
		canvas.DrawLine(previous, point, color, 2)
		previous = point
	}
}

type historyGraphGutter struct {
	widget.WidgetBase
	bench *workbench
	width float32
	row   int
}

func (g *historyGraphGutter) Layout(_ widget.Context, c geometry.Constraints) geometry.Size {
	return c.Constrain(geometry.Sz(g.width, c.MinHeight))
}

func (g *historyGraphGutter) Draw(_ widget.Context, canvas widget.Canvas) {
	if g.row < 0 || g.row >= len(g.bench.graph.rows) || g.Bounds().IsEmpty() {
		return
	}
	bounds := g.Bounds()
	canvas.PushClip(bounds)
	defer canvas.PopClip()
	// Through and bottom segments leave an active lane below the row.
	for _, segment := range g.bench.graph.rows[g.row].segments {
		if segment.part != graphTop {
			x := bounds.Min.X + g.bench.graph.laneX(segment.to)
			canvas.DrawLine(geometry.Pt(x, bounds.Min.Y), geometry.Pt(x, bounds.Max.Y), g.bench.graphPalette[segment.color], 2)
		}
	}
}

func (*historyGraphGutter) Event(widget.Context, event.Event) bool { return false }
