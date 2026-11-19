package main

import (
	"fmt"
	"strings"

	"github.com/gogpu/ui/core/datatable"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
)

const historyDescription = "Description"

// A ref shares the description cell with the subject. Reserve space for the
// message even when many branches point to the same commit.
type historyPainter struct {
	devtools.DataTablePainter
	bench *workbench
}

func (p historyPainter) PaintHeaderCell(canvas widget.Canvas, bounds geometry.Rect, state datatable.HeaderCellPaintState) {
	if state.Title == historyDescription {
		gutter := min(bounds.Width(), p.bench.graph.width())
		if gutter >= 28 {
			icon.Draw(canvas, branchIcon, geometry.NewRect(bounds.Min.X+6, bounds.Center().Y-8, 16, 16), p.bench.theme.Colors.OnSurface)
		}
		bounds.Min.X += gutter
	}
	p.DataTablePainter.PaintHeaderCell(canvas, bounds, state)
}

func (p historyPainter) PaintCell(canvas widget.Canvas, cell datatable.CellPaintState) {
	if cell.ColIndex == 0 {
		width := min(cell.Bounds.Width(), p.bench.graph.width())
		bounds := geometry.NewRect(cell.Bounds.Min.X, cell.Bounds.Min.Y, width, cell.Bounds.Height())
		background := p.bench.theme.Colors.Background
		if cell.Selected {
			background = p.bench.theme.Colors.Selection
		}
		p.bench.paintGraphRow(canvas, bounds, cell.RowIndex, background)
		cell.Bounds.Min.X += width
		if cell.Bounds.IsEmpty() {
			return
		}
	}
	row := cell.RowIndex - p.bench.uncommittedRowCount()
	if cell.ColIndex == 0 && row >= 0 && row < len(p.bench.rows) {
		refs := p.bench.rows[row].Refs
		if refs != "" {
			first, rest, _ := strings.Cut(refs, ", ")
			cs := p.bench.theme.Colors
			fg := cs.Success
			switch {
			case strings.HasPrefix(first, "HEAD -> "):
				first = strings.TrimPrefix(first, "HEAD -> ")
				fg = cs.Primary
			case strings.HasPrefix(first, "tag: "):
				first = strings.TrimPrefix(first, "tag: ")
				fg = cs.Warning
			}
			if rest != "" {
				first += fmt.Sprintf("  +%d", strings.Count(rest, ", ")+1)
			}
			width := min(canvas.MeasureText(first, 12, false)+12, cell.Bounds.Width()*0.45)
			badge := geometry.NewRect(cell.Bounds.Min.X+8, cell.Bounds.Center().Y-9, width, 18)
			canvas.DrawRoundRect(badge, fg.WithAlpha(0.12), 3)
			canvas.StrokeRoundRect(badge, fg.WithAlpha(0.65), 3, 1)
			textBounds := geometry.NewRect(badge.Min.X+5, badge.Min.Y, max(0, width-10), 18)
			canvas.PushClip(textBounds)
			canvas.DrawText(first, textBounds, 12, cs.OnSurface, false, widget.TextAlignLeft)
			canvas.PopClip()
			cell.Bounds.Min.X = badge.Max.X
		}
	}
	p.DataTablePainter.PaintCell(canvas, cell)
}
