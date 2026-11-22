package main

import (
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/listview"
	"github.com/gogpu/ui/core/popover"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
)

const (
	dropdownCornerRadius    float32 = 6
	dropdownItemRadius      float32 = 5
	dropdownFilterHeight    float32 = 36
	dropdownListPadding     float32 = 4
	dropdownFooterHeight    float32 = 30
	dropdownHeadingHeight   float32 = 26
	dropdownSeparatorHeight float32 = 9
)

type dropdownPopoverPainter struct {
	devtools.PopoverPainter
}

func (p dropdownPopoverPainter) PaintPopover(canvas widget.Canvas, st *popover.PopoverPaintState) {
	if st.Bounds.IsEmpty() {
		return
	}
	cs := p.Theme.Colors
	spread := st.Bounds.Expand(1)
	spread.Min.Y += 4
	spread.Max.Y += 5
	canvas.DrawRoundRect(spread, cs.Shadow.WithAlpha(min(cs.Shadow.A, 0.28)), dropdownCornerRadius+1)
	closeShadow := geometry.Rect{
		Min: geometry.Pt(st.Bounds.Min.X, st.Bounds.Min.Y+2),
		Max: geometry.Pt(st.Bounds.Max.X, st.Bounds.Max.Y+3),
	}
	canvas.DrawRoundRect(closeShadow, cs.Shadow.WithAlpha(min(cs.Shadow.A, 0.22)), dropdownCornerRadius)
	canvas.DrawRoundRect(st.Bounds, cs.SurfaceElevated, dropdownCornerRadius)
	canvas.StrokeRoundRect(st.Bounds, cs.BorderStrong, dropdownCornerRadius, 1)
}

func dropdownDivider(w *workbench) widget.Widget {
	return primitives.VBox(primitives.Box().Height(1).Background(w.theme.Colors.BorderStrong)).
		PaddingXY(1, 0).CrossAlign(primitives.CrossAxisStretch)
}

func dropdownSeparator(w *workbench) widget.Widget {
	return primitives.VBox(primitives.Box().Height(1).Background(w.theme.Colors.BorderStrong)).
		Height(dropdownSeparatorHeight).PaddingXY(8, 4).CrossAlign(primitives.CrossAxisStretch)
}

func (w *workbench) newFilterInput(placeholder string, invalid func() bool, onChange, onSubmit func(string)) (*primitives.BoxWidget, *textfield.Widget) {
	cs := w.theme.Colors
	var header *primitives.BoxWidget
	var field *textfield.Widget
	var clearBtn *button.Widget
	var clearVisible bool

	syncClear := func(show bool) {
		if show == clearVisible {
			return
		}
		clearVisible = show
		if show {
			header.AddChild(clearBtn)
		} else {
			header.RemoveChild(clearBtn)
		}
		header.MarkNeedsLayout()
	}

	opts := []textfield.Option{
		textfield.Placeholder(placeholder),
		textfield.A11yLabel(placeholder),
		textfield.Disabled(w.busy),
		textfield.PainterOpt(filterFieldPainter{TextFieldPainter: w.paint.TextField, invalid: invalid}),
		textfield.ContextMenuPainterOpt(w.paint.Menu),
		textfield.OnChange(func(text string) {
			syncClear(text != "")
			if onChange != nil {
				onChange(text)
			}
			field.SetNeedsRedraw(true)
		}),
	}
	if onSubmit != nil {
		opts = append(opts, textfield.OnSubmit(onSubmit))
	}
	field = textfield.New(opts...)
	clearBtn = w.iconButton(icon.ExpClose, "Clear search", 22, 14, func() bool { return w.busy }, func() {
		field.SetText("")
		syncClear(false)
		if onChange != nil {
			onChange("")
		}
		field.SetNeedsRedraw(true)
		ctx := w.ui.Window().Context()
		ctx.RequestFocus(field)
		ctx.Invalidate()
	})

	searchGlyph := primitives.VBox(icon.NewIcon(icon.ExpSearch, icon.Size(14), icon.Color(cs.OnSurfaceSecondary))).
		Width(14).Height(14)
	header = primitives.HBox(
		searchGlyph,
		primitives.Expanded(primitives.VBox(field).Height(dropdownFilterHeight).CrossAlign(primitives.CrossAxisStretch)),
	).Height(dropdownFilterHeight).PaddingLeft(12).PaddingRight(8).Gap(8).CrossAlign(primitives.CrossAxisCenter)

	return primitives.VBox(header, dropdownDivider(w)).CrossAlign(primitives.CrossAxisStretch), field
}

type filterFieldPainter struct {
	devtools.TextFieldPainter
	invalid func() bool
}

func (filterFieldPainter) ContentPadding() (float32, float32) { return 0, 8 }

func (p filterFieldPainter) PaintTextField(canvas widget.Canvas, field *textfield.PaintState) {
	cs := p.Theme.Colors
	canvas.PushClip(field.ContentRect)
	defer canvas.PopClip()
	if field.ShowSelection {
		canvas.DrawRect(field.SelectionRect, cs.Selection)
	}
	text, bounds, color := field.DisplayText, field.TextRect, cs.OnSurface
	invalid := text != "" && !field.Disabled && p.invalid != nil && p.invalid()
	if text == "" {
		text, bounds, color = field.Placeholder, field.ContentRect, cs.OnSurfaceSecondary
	} else if field.Disabled {
		color = cs.OnSurfaceDisabled
	} else if invalid {
		color = cs.Error
	}
	canvas.DrawText(text, bounds, field.FontSize, color, false, widget.TextAlignLeft)
	caretColor := cs.OnSurface
	if invalid {
		caretColor = cs.Error
	}
	textfield.PaintCursor(canvas, field, caretColor, 0)
}

func (w *workbench) dropdownActionButton(glyph icon.IconData, label string, enabled bool, fn func()) *button.Widget {
	return button.New(
		button.TextOpt(label),
		button.VariantOpt(button.TextOnly),
		button.SizeOpt(button.Small),
		button.Disabled(!enabled || w.busy),
		button.OnClick(fn),
		button.PainterOpt(dropdownActionPainter{ButtonPainter: w.paint.Button, glyph: glyph}),
	)
}

type dropdownActionPainter struct {
	devtools.ButtonPainter
	glyph icon.IconData
}

func (dropdownActionPainter) ButtonHeight(button.Size) float32             { return dropdownFooterHeight }
func (dropdownActionPainter) ButtonPadding(button.Size) (float32, float32) { return 8, 0 }
func (dropdownActionPainter) ButtonRadius() float32                        { return dropdownItemRadius }

func (p dropdownActionPainter) PaintButton(canvas widget.Canvas, s button.PaintState) {
	if s.Bounds.IsEmpty() {
		return
	}
	cs := p.Theme.Colors
	if !s.Disabled && (s.Hovered || s.Pressed) {
		bg := cs.ControlHover
		if s.Pressed {
			bg = bg.Lerp(widget.ColorBlack, 0.10)
		}
		canvas.DrawRoundRect(s.Bounds, bg, dropdownItemRadius)
	}
	iconColor := cs.OnSurfaceSecondary
	textColor := cs.OnSurface
	if s.Disabled {
		iconColor = cs.OnSurfaceDisabled
		textColor = cs.OnSurfaceDisabled
	}
	x := s.Bounds.Min.X + 8
	if p.glyph.Name != "" {
		centerY := s.Bounds.Center().Y
		icon.Draw(canvas, p.glyph, geometry.NewRect(x, centerY-7, 14, 14), iconColor)
		x += 22
	}
	labelRect := geometry.NewRect(x, s.Bounds.Min.Y, max(0, s.Bounds.Max.X-x-8), s.Bounds.Height())
	if !labelRect.IsEmpty() {
		canvas.PushClip(labelRect)
		canvas.DrawText(s.Text, labelRect, 13, textColor, false, widget.TextAlignLeft)
		canvas.PopClip()
	}
}

func (w *workbench) dropdownFooter(items ...widget.Widget) widget.Widget {
	return primitives.VBox(
		dropdownDivider(w),
		primitives.VBox(items...).Padding(dropdownListPadding).CrossAlign(primitives.CrossAxisStretch),
	).CrossAlign(primitives.CrossAxisStretch)
}

type dropdownListPainter struct {
	devtools.ListViewPainter
	selectable func(int) bool
}

func (p dropdownListPainter) PaintItemBackground(canvas widget.Canvas, ips listview.ItemPaintState) {
	if ips.Bounds.IsEmpty() || ips.Disabled || !ips.Hovered {
		return
	}
	if p.selectable != nil && !p.selectable(ips.Index) {
		return
	}
	canvas.DrawRoundRect(ips.Bounds, p.Theme.Colors.ControlHover, dropdownItemRadius)
}

func (p dropdownListPainter) PaintSelection(canvas widget.Canvas, ips listview.ItemPaintState) {
	if ips.Bounds.IsEmpty() || !ips.Selected {
		return
	}
	if p.selectable != nil && !p.selectable(ips.Index) {
		return
	}
	canvas.DrawRoundRect(ips.Bounds, p.Theme.Colors.Selection, dropdownItemRadius)
}
