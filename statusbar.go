package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/popover"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func (w *workbench) statusBar(hint state.Signal[string]) widget.Widget {
	w.statusText = state.NewSignal(w.historyStatus())
	var items []widget.Widget
	if w.path != "" {
		refs := slices.DeleteFunc(slices.Clone(w.data.Branches), func(ref string) bool {
			return !strings.HasPrefix(ref, repo.LocalRefPrefix)
		})
		const checkoutMenu = "checkout-branch"
		var checkout *button.Widget
		checkout = w.statusButton(w.currentBranchLabel(), branchIcon, len(refs) != 0, checkoutMenu, func() {
			choices := make([]popupChoice, 0, len(refs))
			for _, ref := range refs {
				branch := shortRef(ref)
				choice := popupChoice{label: branch, glyph: branchIcon}
				if branch == w.data.CurrentBranch() {
					choice.glyph, choice.current = icon.Check, true
				} else {
					choice.action = func() { w.switchBranch(ref) }
				}
				choices = append(choices, choice)
			}
			content, focus := w.choicePanel(400, 28, "Search branches", choices)
			w.openAnchoredPopup(checkout, checkoutMenu, checkout.ScreenBounds, popover.TopStart, content, focus)
		}).MaxWidth(230)
		items = append(items, checkout)
		if w.data.Upstream != "" {
			hasRemote := len(w.data.Remotes) != 0
			items = append(items, w.muted(w.data.Upstream).FontSize(12).MaxLines(1).Ellipsis(),
				w.statusButton(strconv.Itoa(w.data.Ahead), pushIcon, hasRemote && w.data.CurrentBranch() != "", "", func() { w.syncBranch(true) }),
				w.statusButton(strconv.Itoa(w.data.Behind), pullIcon, hasRemote, "", func() { w.syncBranch(false) }),
			)
		}
		if count := len(w.data.Stashes); count > 0 {
			label := fmt.Sprintf("%d stashes", count)
			if count == 1 {
				label = "1 stash"
			}
			const stashMenu = "status-stashes"
			var saved *button.Widget
			saved = w.statusButton(label, stashIcon, true, stashMenu, func() {
				content, focus := w.stashesPanel()
				w.openAnchoredPopup(saved, stashMenu, saved.ScreenBounds, popover.TopStart, content, focus)
			})
			items = append(items, saved)
		}
	}
	items = append(items, primitives.Expanded(w.muted("").FontSize(12).ContentSignal(w.statusText).MaxLines(1).Ellipsis()),
		w.muted("").FontSize(12).ContentSignal(hint).MaxLines(1).Ellipsis(), w.muted(w.path).FontSize(12).MaxLines(1).Ellipsis())
	return row(items...).PaddingXY(8, 2).Background(w.theme.Colors.Surface)
}

func (w *workbench) historyStatus() string {
	if w.path == "" || w.status != "" {
		return w.status
	}
	return fmt.Sprintf("%d commits     %d changes", len(w.data.Commits), len(w.data.Files))
}

func (w *workbench) currentBranchLabel() string {
	head := w.data.Head
	if head == "" {
		return "No commits yet"
	}
	if branch := w.data.CurrentBranch(); branch != "" {
		return branch
	}
	return "HEAD detached at " + head[:min(8, len(head))]
}

func (w *workbench) statusButton(label string, glyph icon.IconData, enabled bool, popupID string, click func()) *button.Widget {
	painter := statusButtonPainter{ButtonPainter: w.paint.Button, glyph: glyph,
		active: func() bool { return popupID != "" && w.popupIcon == popupID }}
	return button.New(button.TextOpt(label), button.VariantOpt(button.TextOnly), button.SizeOpt(button.Small),
		button.Disabled(!enabled || w.busy), button.OnClick(click), button.PainterOpt(painter))
}

type statusButtonPainter struct {
	devtools.ButtonPainter
	glyph  icon.IconData
	active func() bool
}

func (p statusButtonPainter) ButtonHeight(button.Size) float32             { return 24 }
func (p statusButtonPainter) ButtonPadding(button.Size) (float32, float32) { return 15, 0 }
func (p statusButtonPainter) ButtonFontSize(button.Size) float32           { return 12 }

func (p statusButtonPainter) PaintButton(canvas widget.Canvas, s button.PaintState) {
	label := s.Text
	s.Text = ""
	s.Hovered = s.Hovered || p.active()
	p.ButtonPainter.PaintButton(canvas, s)
	color := p.Theme.Colors.OnSurface
	if s.Disabled {
		color = p.Theme.Colors.OnSurfaceDisabled
	}
	x := s.Bounds.Min.X + 5
	icon.Draw(canvas, p.glyph, geometry.NewRect(x, s.Bounds.Center().Y-7, 14, 14), color)
	text := geometry.NewRect(x+18, s.Bounds.Min.Y, s.Bounds.Width()-28, s.Bounds.Height())
	if !text.IsEmpty() {
		canvas.PushClip(text)
		canvas.DrawText(label, text, 12, color, false, widget.TextAlignLeft)
		canvas.PopClip()
	}
}
