package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gogpu/ui/core/datatable"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

const historyDescription = "Description"

var historyTag = icon.FromSVGXML("history-tag", []byte(`<svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
<path d="M2.5 3.5C2.5 2.94772 2.94772 2.5 3.5 2.5H7.29289C7.55811 2.5 7.81246 2.60536 8 2.79289L13.2929 8.08579C13.6834 8.47631 13.6834 9.10948 13.2929 9.5L9.5 13.2929C9.10948 13.6834 8.47631 13.6834 8.08579 13.2929L2.79289 8C2.60536 7.81246 2.5 7.55811 2.5 7.29289V3.5Z" stroke="#6C707E" stroke-linejoin="round"/>
<circle cx="5.5" cy="5.5" r="1" fill="#6C707E"/>
</svg>`))

type historyPainter struct {
	devtools.DataTablePainter
	bench   *workbench
	columns []datatable.Column
	history *historyHeader
}

func (p historyPainter) PaintRow(canvas widget.Canvas, row datatable.RowPaintState) {
	p.DataTablePainter.PaintRow(canvas, row)
	if drag := p.history.branchDrag; drag.active && drag.target.row == row.RowIndex && drag.target.commit.Hash != "" {
		canvas.DrawRect(row.Bounds, p.bench.theme.Colors.Primary.WithAlpha(.20))
	}
	matches := p.bench.searchMatches
	if _, found := slices.BinarySearch(matches, row.RowIndex); found {
		alpha := float32(0.10)
		if row.RowIndex == matches[p.bench.searchIndex] {
			alpha = 0.25
		}
		canvas.DrawRect(row.Bounds, p.bench.theme.Colors.Primary.WithAlpha(alpha))
	}
}

func (p historyPainter) PaintHeader(canvas widget.Canvas, bounds geometry.Rect, state datatable.HeaderPaintState) {
	clear(p.history.badges)
	p.DataTablePainter.PaintHeader(canvas, bounds, state)
}

func (p historyPainter) PaintHeaderCell(canvas widget.Canvas, bounds geometry.Rect, state datatable.HeaderCellPaintState) {
	if state.Title == historyDescription {
		gutter := min(bounds.Width(), p.bench.graph.width())
		if gutter >= 28 {
			icon.Draw(canvas, branchIcon, geometry.NewRect(bounds.Min.X+6, bounds.Center().Y-8, 16, 16), p.bench.theme.Colors.OnSurface)
		}
		bounds.Min.X += gutter
	} else {
		x := bounds.Min.X
		canvas.DrawLine(geometry.Pt(x, bounds.Min.Y+8), geometry.Pt(x, bounds.Max.Y-8), p.bench.theme.Colors.Border, 1)
	}
	p.DataTablePainter.PaintHeaderCell(canvas, bounds, state)
}

func summaryText(s repo.ChangeSummary) string {
	return fmt.Sprintf("%d   +%d   −%d", s.Files, s.Added, s.Deleted)
}

func (p historyPainter) PaintCell(canvas widget.Canvas, cell datatable.CellPaintState) {
	w := p.bench
	row := cell.RowIndex - w.uncommittedRowCount()
	if p.columns[cell.ColIndex].Key == "diff" {
		summary := w.data.WorkDiff
		if row >= 0 && row < len(w.rows) {
			summary = w.rows[row].Diff
		}
		paintChangeSummary(canvas, cell.Bounds, summary, w.theme.Colors)
		return
	}
	if cell.ColIndex == 0 {
		width := min(cell.Bounds.Width(), w.graph.width())
		bounds := geometry.NewRect(cell.Bounds.Min.X, cell.Bounds.Min.Y, width, cell.Bounds.Height())
		background := w.theme.Colors.Background
		if cell.Selected {
			background = w.theme.Colors.Selection
		}
		w.paintGraphRow(canvas, bounds, cell.RowIndex, background)
		cell.Bounds.Min.X += width
		if cell.Bounds.IsEmpty() {
			return
		}
		if row >= 0 && row < len(w.rows) {
			color := w.theme.Colors.Primary
			if cell.RowIndex < len(w.graph.rows) {
				color = w.graphPalette[w.graph.rows[cell.RowIndex].color]
			}
			x, badges := paintHistoryRefs(canvas, cell.Bounds, w.historyRefs(w.rows[row]), color, w.theme.Colors.OnSurface)
			p.history.badges[cell.RowIndex] = badges
			cell.Bounds.Min.X = x
		}
	}
	p.DataTablePainter.PaintCell(canvas, cell)
}

func paintChangeSummary(canvas widget.Canvas, bounds geometry.Rect, summary repo.ChangeSummary, cs devtools.ColorScheme) {
	canvas.PushClip(bounds)
	defer canvas.PopClip()
	x := bounds.Min.X + 8
	for i, part := range []string{fmt.Sprint(summary.Files), fmt.Sprintf("+%d", summary.Added), fmt.Sprintf("−%d", summary.Deleted)} {
		width := canvas.MeasureText(part, 11, false)
		color := cs.OnSurface
		switch i {
		case 0:
			pill := geometry.NewRect(x-3, bounds.Center().Y-8, width+8, 16)
			canvas.DrawRoundRect(pill, cs.Border, 3)
			canvas.DrawText(part, pill, 11, color, false, widget.TextAlignCenter)
		case 1:
			text := geometry.NewRect(x, bounds.Center().Y-8, width+2, 16)
			canvas.DrawText(part, text, 11, cs.Success, false, widget.TextAlignLeft)
		case 2:
			text := geometry.NewRect(x, bounds.Center().Y-8, width+2, 16)
			canvas.DrawText(part, text, 11, cs.Error, false, widget.TextAlignLeft)
		}
		x += width + 10
	}
}

type historyRef struct {
	text, remote string
	ref, synced  string
	glyph        icon.IconData
	current      bool
}

func (w *workbench) historyRefs(c repo.Commit) []historyRef {
	refs := w.data.RefsByCommit[c.Hash]
	var labels []historyRef
	current := repo.LocalRefPrefix + w.data.CurrentBranch()
	if c.Hash == w.data.Head && w.data.CurrentBranch() == "" {
		labels = append(labels, historyRef{text: "HEAD", glyph: icon.ExpCheck, current: true})
	}
	absorbed := make(map[string]bool)
	for _, ref := range refs {
		if !strings.HasPrefix(ref.Name, repo.LocalRefPrefix) {
			continue
		}
		name := shortRef(ref.Name)
		label := historyRef{text: name, ref: ref.Name, glyph: branchIcon, current: ref.Name == current}
		if label.current {
			label.glyph = icon.ExpCheck
		}
		if w.filter.Remote {
			for _, remote := range refs {
				remoteName, ok := strings.CutPrefix(remote.Name, repo.RemoteRefPrefix)
				if !ok || absorbed[remote.Name] {
					continue
				}
				server := branchRemote(remote.Name, w.data.Remotes)
				branch := strings.TrimPrefix(remoteName, server+"/")
				if remote.Name == ref.Upstream || (ref.Upstream == "" && branch == name) {
					absorbed[remote.Name] = true
					label.remote = server
					label.synced = remote.Name
					break
				}
			}
		}
		if label.current {
			labels = append([]historyRef{label}, labels...)
		} else {
			labels = append(labels, label)
		}
	}
	for _, ref := range refs {
		if w.filter.Remote && strings.HasPrefix(ref.Name, repo.RemoteRefPrefix) && !absorbed[ref.Name] {
			labels = append(labels, historyRef{text: shortRef(ref.Name), ref: ref.Name, glyph: remotesIcon})
		}
	}
	if !w.filter.HideTags {
		for _, ref := range refs {
			if name, ok := strings.CutPrefix(ref.Name, "refs/tags/"); ok {
				labels = append(labels, historyRef{text: name, ref: ref.Name, glyph: historyTag})
			}
		}
	}
	if !w.filter.HideStashes && len(w.filter.VisibleRefs()) == 0 {
		if i := slices.IndexFunc(w.data.Stashes, func(s repo.Stash) bool { return s.Hash == c.Hash }); i >= 0 {
			labels = append(labels, historyRef{text: w.data.Stashes[i].Selector, glyph: stashIcon})
		}
	}
	return labels
}

type historyBadge struct {
	bounds geometry.Rect
	label  historyRef
}

func paintHistoryRefs(canvas widget.Canvas, bounds geometry.Rect, labels []historyRef, color, foreground widget.Color) (float32, []historyBadge) {
	x := bounds.Min.X
	right := bounds.Max.X - 62
	var badges []historyBadge
	for _, label := range labels {
		left := x + 6
		if right-left < 26 {
			break
		}
		textWidth := canvas.MeasureText(label.text, 11, label.current)
		baseWidth := 24 + textWidth
		remoteWidth := float32(0)
		if label.remote != "" {
			remoteWidth = 30 + canvas.MeasureText(label.remote, 11, false)
		}
		badge := geometry.NewRect(left, bounds.Center().Y-9, min(right-left, baseWidth+remoteWidth), 18)
		badges = append(badges, historyBadge{bounds: badge, label: label})
		alpha := float32(.12)
		if label.current {
			alpha = .27
		}
		canvas.DrawRoundRect(badge, color.WithAlpha(alpha), 4)
		canvas.StrokeRoundRect(badge, color.WithAlpha(.72), 4, 1)
		canvas.PushClip(badge)
		icon.Draw(canvas, label.glyph, geometry.NewRect(left+4, badge.Min.Y+3, 12, 12), foreground)
		canvas.DrawText(label.text, geometry.NewRect(left+20, badge.Min.Y, max(0, badge.Width()-22), 18), 11, foreground, label.current, widget.TextAlignLeft)
		if remoteWidth > 0 && left+baseWidth+24 < badge.Max.X {
			segment := geometry.NewRect(left+baseWidth, badge.Min.Y, badge.Width()-baseWidth, 18)
			canvas.DrawRect(segment, color.WithAlpha(.19))
			canvas.DrawText("⇄", geometry.NewRect(segment.Min.X+2, badge.Min.Y, 12, 18), 11, foreground, false, widget.TextAlignLeft)
			icon.Draw(canvas, remotesIcon, geometry.NewRect(segment.Min.X+14, badge.Min.Y+3, 12, 12), foreground)
			canvas.DrawText(label.remote, geometry.NewRect(segment.Min.X+28, badge.Min.Y, max(0, segment.Width()-30), 18), 11, foreground, false, widget.TextAlignLeft)
		}
		canvas.PopClip()
		x = badge.Max.X
	}
	return x, badges
}
