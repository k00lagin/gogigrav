package main

import (
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
)

const (
	codeRowHeight = 19
	codeGutter    = 61
	codeCell      = 8
)

type codePane struct {
	widget.WidgetBase
	rows      []previewRow
	side      int
	wrap      bool
	colors    devtools.ColorScheme
	scroll    *scrollview.Widget
	starts    []int
	columns   int
	hunkLabel string
	applyHunk func(int)
}

func (p *codePane) Layout(_ widget.Context, c geometry.Constraints) geometry.Size {
	p.columns = 0
	if p.wrap {
		p.columns = max(1, int((c.MaxWidth-codeGutter-16)/codeCell))
	}
	p.starts = make([]int, len(p.rows)+1)
	widest := 0
	for i, r := range p.rows {
		width := max(r.left.width, r.right.width)
		widest = max(widest, width)
		n := 1
		if p.wrap {
			n = max(1, (width+p.columns-1)/p.columns)
		}
		p.starts[i+1] = p.starts[i] + n
	}
	return c.Constrain(geometry.Sz(float32(widest)*codeCell+codeGutter+16, float32(p.starts[len(p.rows)])*codeRowHeight))
}
func (p *codePane) hunkButtonBounds(y float32) geometry.Rect {
	const width = 105
	x, _ := p.scroll.ScrollOffset()
	right := x + p.scroll.ViewportSize().Width - p.scroll.ScrollbarInset() - 6
	return geometry.NewRect(right-width, y+1, width, codeRowHeight-2)
}

func (p *codePane) Event(ctx widget.Context, e event.Event) bool {
	mouse, ok := e.(*event.MouseEvent)
	if !ok || p.applyHunk == nil || (mouse.MouseType != event.MouseMove && mouse.MouseType != event.MousePress) || len(p.starts) == 0 {
		return false
	}
	row := sort.Search(len(p.rows), func(i int) bool { return float32(p.starts[i+1])*codeRowHeight > mouse.Position.Y })
	if row == len(p.rows) || p.rows[row].hunk == 0 || !p.hunkButtonBounds(float32(p.starts[row])*codeRowHeight).Contains(mouse.Position) {
		return false
	}
	if mouse.MouseType == event.MouseMove {
		ctx.SetCursor(widget.CursorPointer)
	} else if mouse.Button == event.ButtonLeft {
		p.applyHunk(p.rows[row].hunk - 1)
		return true
	}
	return false
}
func (p *codePane) Draw(_ widget.Context, c widget.Canvas) {
	if len(p.starts) == 0 {
		return
	}
	_, offset := p.scroll.ScrollOffset()
	bottom := offset + p.scroll.Bounds().Height()
	first := sort.Search(len(p.rows), func(i int) bool { return float32(p.starts[i+1])*codeRowHeight > offset })
	for i := first; i < len(p.rows); i++ {
		y := float32(p.starts[i]) * codeRowHeight
		headerY := y
		if y >= bottom {
			break
		}
		line := p.rows[i].left
		if p.side == 1 {
			line = p.rows[i].right
		}
		width := max(p.Bounds().Width(), p.scroll.Bounds().Width())
		bounds := geometry.NewRect(0, y, width, float32(p.starts[i+1]-p.starts[i])*codeRowHeight)
		switch line.kind {
		case '+':
			c.DrawRect(bounds, widget.RGBA8(47, 177, 86, 42))
		case '-':
			c.DrawRect(bounds, widget.RGBA8(215, 66, 68, 44))
		case '@':
			c.DrawRect(bounds, p.colors.Surface)
		}
		if line.number > 0 {
			label := strconv.Itoa(line.number)
			if line.kind == '+' || line.kind == '-' {
				label += " " + string(line.kind)
			}
			c.DrawText(label, geometry.NewRect(0, y, codeGutter-8, codeRowHeight), 12, p.colors.OnSurfaceSecondary, false, widget.TextAlignRight)
		}
		column := 0
		x := float32(codeGutter)
		for _, span := range line.spans {
			text := span.text
			for text != "" {
				n := len(text)
				count := 0
				if p.wrap {
					n = 0
					for n < len(text) && count < p.columns-column {
						_, size := utf8.DecodeRuneInString(text[n:])
						n += size
						count++
					}
				} else {
					count = utf8.RuneCountInString(text)
				}
				part := text[:n]
				text = text[n:]
				color := span.color
				if color.A == 0 {
					color = p.colors.OnSurface
				}
				rect := geometry.NewRect(x, y, max(width, float32(count)*codeCell+4), codeRowHeight)
				style := widget.TextStyle{FontFamily: "Consolas", FontSize: 13, Color: color}
				if styled, ok := c.(widget.StyledTextDrawer); ok {
					styled.DrawStyledText(part, rect, style)
					x += styled.MeasureStyledText(part, style)
				} else {
					c.DrawText(part, rect, 13, color, false, widget.TextAlignLeft)
					x += c.MeasureText(part, 13, false)
				}
				column += count
				if p.wrap && column == p.columns {
					x = codeGutter
					y += codeRowHeight
					column = 0
				}
			}
		}
		if p.applyHunk != nil && p.rows[i].hunk != 0 {
			button := p.hunkButtonBounds(headerY)
			c.DrawRoundRect(button, p.colors.ControlFill, 4)
			c.DrawText(p.hunkLabel, button, 12, p.colors.OnSurface, false, widget.TextAlignCenter)
		}
	}
}
