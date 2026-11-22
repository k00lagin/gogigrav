package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/checkbox"
	"github.com/gogpu/ui/core/popover"
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/core/toggleswitch"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

const (
	branchRowHeight         = 28
	branchViewportMaxHeight = 12 * branchRowHeight
)

func (w *workbench) branchesPopup(bar *toolbarHints, index int) {
	content, focus := w.branchPanel()
	w.openToolbarPopup(bar, index, popover.BottomStart, content, focus)
}

func (w *workbench) branchPanel() (widget.Widget, widget.Widget) {
	filter := w.filter
	ctx := w.ui.Window().Context()
	scroll := state.NewSignal(float32(0))
	currentBranch := w.data.CurrentBranch()
	var query, firstLocal, firstRemote string
	toggle := func(ref string, checked bool) {
		if checked {
			filter.Refs = append(slices.Clone(filter.Refs), ref)
		} else {
			filter.Refs = slices.DeleteFunc(slices.Clone(filter.Refs), func(selected string) bool { return selected == ref })
		}
		w.refreshBranchHistory(filter)
	}
	check := func(label string, bold bool, checked func() bool, onToggle func(bool)) *checkbox.Widget {
		return checkbox.New(checkbox.Label(label), checkbox.CheckedFn(checked), checkbox.OnToggle(onToggle),
			checkbox.PainterOpt(branchCheckboxPainter{CheckboxPainter: w.paint.Checkbox, bold: bold}))
	}
	all := check("All branches", false, func() bool { return len(filter.VisibleRefs()) == 0 }, func(checked bool) {
		if checked {
			filter.Refs = nil
			w.refreshBranchHistory(filter)
			ctx.Invalidate()
		}
	})
	leaf := func(label, ref string, bold bool) widget.Widget {
		cb := check(label, bold, func() bool { return slices.Contains(filter.Refs, ref) },
			func(checked bool) { toggle(ref, checked); all.SetNeedsRedraw(true) })
		return primitives.VBox(primitives.Expanded(cb)).Height(branchRowHeight).CrossAlign(primitives.CrossAxisStretch)
	}
	branchRows := func(refs []string, prefix string, isLocal bool) ([]widget.Widget, string) {
		var rows []widget.Widget
		var first string
		for _, ref := range refs {
			name := shortRef(ref)
			if strings.Contains(strings.ToLower(name), query) {
				if first == "" {
					first = ref
				}
				rows = append(rows, leaf(strings.TrimPrefix(name, prefix), ref, isLocal && name == currentBranch))
			}
		}
		return rows, first
	}

	localSlot := primitives.VBox().CrossAlign(primitives.CrossAxisStretch)
	allSlot := primitives.VBox().CrossAlign(primitives.CrossAxisStretch)
	emptySlot := primitives.VBox().CrossAlign(primitives.CrossAxisStretch)
	local := w.newBranchGroup("Local", "refs/heads", false)
	remote := w.newBranchGroup("Remote", "refs/remotes", false)
	remoteGroups := make(map[string]*branchGroup)
	var remoteNames []string
	remoteRefs := make(map[string][]string)
	var localRefs []string
	for _, ref := range w.data.Branches {
		if !strings.HasPrefix(ref, repo.RemoteRefPrefix) {
			localRefs = append(localRefs, ref)
			continue
		}
		name := branchRemote(ref, w.data.Remotes)
		if _, ok := remoteRefs[name]; !ok {
			remoteNames = append(remoteNames, name)
			remoteGroups[name] = w.newBranchGroup(name, repo.RemoteRefPrefix+name, true)
		}
		remoteRefs[name] = append(remoteRefs[name], ref)
	}
	rebuildLocal := func() {
		allSlot.ClearChildren()
		if query == "" {
			allSlot.AddChild(primitives.VBox(primitives.Expanded(all)).Height(branchRowHeight).CrossAlign(primitives.CrossAxisStretch))
			allSlot.AddChild(dropdownSeparator(w))
		}
		children, first := branchRows(localRefs, "", true)
		firstLocal = first
		localSlot.ClearChildren()
		local.setChildren(children, len(children), query != "")
		if len(children) != 0 || query == "" {
			localSlot.AddChild(local.box)
		}
	}
	rebuildRemote := func() {
		firstRemote = ""
		var groups []widget.Widget
		remoteCount := 0
		if filter.Remote {
			for _, name := range remoteNames {
				children, first := branchRows(remoteRefs[name], name+"/", false)
				if first != "" {
					if firstRemote == "" {
						firstRemote = first
					}
					remoteCount += len(children)
					group := remoteGroups[name]
					group.setChildren(children, len(children), query != "")
					groups = append(groups, group.box)
				}
			}
		}
		remote.setChildren(groups, remoteCount, query != "")
	}
	updateEmpty := func() {
		emptySlot.ClearChildren()
		if firstLocal == "" && firstRemote == "" {
			label := "No branches"
			if query != "" {
				label = "No matching branches"
			}
			emptySlot.AddChild(primitives.VBox(w.muted(label)).Height(branchRowHeight).PaddingXY(8, 5))
		}
	}
	rebuild := func() {
		rebuildLocal()
		rebuildRemote()
		updateEmpty()
	}
	isInvalid := func() bool { return query != "" && firstLocal == "" && firstRemote == "" }
	filterBox, in := w.newFilterInput("Search branches", isInvalid, func(text string) {
		query = strings.ToLower(strings.TrimSpace(text))
		scroll.Set(0)
		rebuild()
		ctx.Invalidate()
	}, func(string) {
		firstMatch := firstLocal
		if firstMatch == "" {
			firstMatch = firstRemote
		}
		if firstMatch != "" {
			toggle(firstMatch, !slices.Contains(filter.Refs, firstMatch))
			ctx.Invalidate()
		}
	})
	remote.header.Gap(4).PaddingRight(4)
	remote.header.AddChild(toggleswitch.New(toggleswitch.CheckedFn(func() bool { return filter.Remote }),
		toggleswitch.A11yHint("Show remote branches in the branch list and history"), toggleswitch.PainterOpt(w.paint.Switch),
		toggleswitch.OnToggle(func(checked bool) {
			filter.Remote = checked
			rebuildRemote()
			updateEmpty()
			all.SetNeedsRedraw(true)
			in.SetNeedsRedraw(true)
			w.refreshBranchHistory(filter)
			ctx.Invalidate()
		})).Padding(0))
	rebuild()
	rows := primitives.VBox(allSlot, localSlot, remote.box, emptySlot).
		PaddingTop(dropdownListPadding).PaddingBottom(dropdownListPadding).
		PaddingLeft(dropdownListPadding).PaddingRight(10).
		CrossAlign(primitives.CrossAxisStretch)
	tree := &branchViewport{
		Widget: scrollview.New(rows, scrollview.ScrollYSignal(scroll), scrollview.PainterOpt(w.paint.Scrollbar)),
		scroll: scroll,
	}
	panel := primitives.VBox(filterBox, tree).Width(330).CrossAlign(primitives.CrossAxisStretch)
	return panel, in
}

func branchRemote(ref string, remotes []string) string {
	short := strings.TrimPrefix(ref, repo.RemoteRefPrefix)
	var name string
	for _, remote := range remotes {
		if len(remote) > len(name) && strings.HasPrefix(short, remote+"/") {
			name = remote
		}
	}
	if name == "" {
		name, _, _ = strings.Cut(short, "/")
	}
	return name
}

type branchViewport struct {
	*scrollview.Widget
	scroll state.Signal[float32]
}

func (b *branchViewport) Layout(ctx widget.Context, constraints geometry.Constraints) geometry.Size {
	width := constraints.MaxWidth
	content := widget.LayoutChild(b.Content(), ctx, geometry.BoxConstraints(width, width, 0, geometry.Infinity))
	height := min(content.Height, branchViewportMaxHeight)
	size := constraints.Constrain(geometry.Sz(width, height))
	b.scroll.Set(min(b.scroll.Get(), max(0, content.Height-size.Height)))
	return b.Widget.Layout(ctx, geometry.Tight(size))
}

type branchGroupBody struct {
	*primitives.BoxWidget
	bench *workbench
}

func (b *branchGroupBody) Draw(ctx widget.Context, canvas widget.Canvas) {
	if bounds := b.Bounds(); !bounds.IsEmpty() {
		x := bounds.Min.X + 12
		canvas.DrawLine(geometry.Pt(x, bounds.Min.Y), geometry.Pt(x, bounds.Max.Y),
			b.bench.theme.Colors.OnSurface.WithAlpha(0.14), 1)
	}
	b.BoxWidget.Draw(ctx, canvas)
}

type branchGroup struct {
	box, header *primitives.BoxWidget
	body        *branchGroupBody
	collapsed   bool
	count       int
}

type branchGroupKey struct {
	path, ref string
}

func (w *workbench) newBranchGroup(label, ref string, folder bool) *branchGroup {
	key := branchGroupKey{path: w.path, ref: ref}
	bodyBox := primitives.VBox().PaddingLeft(18).CrossAlign(primitives.CrossAxisStretch)
	body := &branchGroupBody{BoxWidget: bodyBox, bench: w}
	body.SetSelf(body)
	g := &branchGroup{
		body: body, collapsed: w.collapsedBranchGroups[key],
	}
	caption := button.New(button.TextOpt(label), button.VariantOpt(button.TextOnly), button.SizeOpt(button.Small),
		button.DisabledFn(func() bool { return g.body.ChildCount() == 0 }),
		button.PainterOpt(branchGroupPainter{w.paint.Button, g, folder}), button.OnClick(func() {
			if g.body.ChildCount() > 0 {
				g.collapsed = g.expanded()
				g.showBody(!g.collapsed)
				if g.collapsed {
					if w.collapsedBranchGroups == nil {
						w.collapsedBranchGroups = make(map[branchGroupKey]bool)
					}
					w.collapsedBranchGroups[key] = true
				} else {
					delete(w.collapsedBranchGroups, key)
				}
			}
		}))
	g.header = row(primitives.Expanded(caption)).Height(branchRowHeight)
	g.box = primitives.VBox(g.header).CrossAlign(primitives.CrossAxisStretch)
	return g
}

func (g *branchGroup) expanded() bool { return g.box.ChildCount() > 1 }

func (g *branchGroup) setChildren(children []widget.Widget, count int, searching bool) {
	g.count = count
	g.body.ClearChildren()
	for _, child := range children {
		g.body.AddChild(child)
	}
	g.showBody(len(children) > 0 && (searching || !g.collapsed))
	widget.MarkRedrawInTree(g.header)
}

func (g *branchGroup) showBody(show bool) {
	if show && !g.expanded() {
		g.box.AddChild(g.body)
	} else if !show && g.expanded() {
		g.box.RemoveChild(g.body)
	}
}

type branchGroupPainter struct {
	devtools.ButtonPainter
	group  *branchGroup
	folder bool
}

func (branchGroupPainter) ButtonHeight(button.Size) float32             { return branchRowHeight }
func (branchGroupPainter) ButtonPadding(button.Size) (float32, float32) { return 6, 0 }
func (branchGroupPainter) ButtonRadius() float32                        { return dropdownItemRadius }

func (p branchGroupPainter) PaintButton(canvas widget.Canvas, s button.PaintState) {
	if s.Bounds.IsEmpty() {
		return
	}
	colors := p.Theme.Colors
	if !s.Disabled && (s.Hovered || s.Pressed) {
		bg := colors.ControlHover
		if s.Pressed {
			bg = bg.Lerp(widget.ColorBlack, 0.10)
		}
		canvas.DrawRoundRect(s.Bounds, bg, dropdownItemRadius)
	}
	x, y := s.Bounds.Min.X+6, s.Bounds.Center().Y
	if p.group.body.ChildCount() > 0 {
		chevron := icon.ExpChevronRight
		if p.group.expanded() {
			chevron = icon.ExpChevronDown
		}
		icon.Draw(canvas, chevron, geometry.NewRect(x, y-6, 12, 12), colors.OnSurfaceSecondary)
	}
	x += 18
	if p.folder {
		icon.Draw(canvas, icon.ExpFolder, geometry.NewRect(x, y-7, 14, 14), colors.OnSurfaceSecondary)
		x += 20
	}
	rightEdge := s.Bounds.Max.X - 6
	if p.group.count > 0 {
		countText := strconv.Itoa(p.group.count)
		countWidth := canvas.MeasureText(countText, 11, false)
		countRect := geometry.NewRect(rightEdge-countWidth, s.Bounds.Min.Y, countWidth, s.Bounds.Height())
		canvas.DrawText(countText, countRect, 11, colors.OnSurfaceSecondary, false, widget.TextAlignRight)
		rightEdge -= countWidth + 8
	}
	bounds := geometry.NewRect(x, s.Bounds.Min.Y, max(0, rightEdge-x), s.Bounds.Height())
	if !bounds.IsEmpty() {
		canvas.PushClip(bounds)
		canvas.DrawText(s.Text, bounds, 13, colors.OnSurface, !p.folder, widget.TextAlignLeft)
		canvas.PopClip()
	}
}

type branchCheckboxPainter struct {
	devtools.CheckboxPainter
	bold bool
}

func (branchCheckboxPainter) CheckboxBoxSize() float32  { return 14 }
func (branchCheckboxPainter) CheckboxLabelGap() float32 { return 8 }
func (branchCheckboxPainter) CheckboxFontSize() float32 { return 13 }

func (p branchCheckboxPainter) PaintCheckbox(canvas widget.Canvas, s checkbox.PaintState) {
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
	const boxSize float32 = 14
	const boxRadius float32 = 3
	boxY := s.Bounds.Min.Y + (s.Bounds.Height()-boxSize)/2
	boxRect := geometry.NewRect(s.Bounds.Min.X+6, boxY, boxSize, boxSize)
	if s.Checked || s.Indeterminate {
		bg := cs.Primary
		if s.Disabled {
			bg = cs.ControlFill
		}
		canvas.DrawRoundRect(boxRect, bg, boxRadius)
		fg := cs.OnPrimary
		if s.Disabled {
			fg = cs.OnSurfaceDisabled
		}
		cx, cy := boxRect.Min.X, boxRect.Min.Y
		if s.Indeterminate && !s.Checked {
			canvas.DrawLine(
				geometry.Pt(cx+boxSize*0.22, cy+boxSize*0.50),
				geometry.Pt(cx+boxSize*0.78, cy+boxSize*0.50),
				fg, 1.5,
			)
		} else {
			p1 := geometry.Pt(cx+boxSize*0.20, cy+boxSize*0.52)
			p2 := geometry.Pt(cx+boxSize*0.42, cy+boxSize*0.74)
			p3 := geometry.Pt(cx+boxSize*0.80, cy+boxSize*0.30)
			canvas.DrawLine(p1, p2, fg, 1.5)
			canvas.DrawLine(p2, p3, fg, 1.5)
		}
	} else {
		border := cs.BorderStrong
		if s.Disabled {
			border = cs.OnSurfaceDisabled
		}
		canvas.StrokeRoundRect(boxRect.Expand(-0.75), border, boxRadius, 1.5)
	}
	if s.Focused && !s.Disabled {
		canvas.StrokeRoundRect(boxRect.Expand(2), cs.BorderFocus, boxRadius+1, 1)
	}
	if s.Label != "" {
		fg := cs.OnSurface
		if s.Disabled {
			fg = cs.OnSurfaceDisabled
		}
		labelX := boxRect.Max.X + 8
		labelRect := geometry.NewRect(labelX, s.Bounds.Min.Y, max(0, s.Bounds.Max.X-labelX-6), s.Bounds.Height())
		if !labelRect.IsEmpty() {
			canvas.PushClip(labelRect)
			canvas.DrawText(s.Label, labelRect, 13, fg, p.bold, widget.TextAlignLeft)
			canvas.PopClip()
		}
	}
}
