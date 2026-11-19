package main

import (
	"slices"
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
	branchRowHeight         = 26
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
	var query, firstLocal, firstRemote string
	toggle := func(ref string, checked bool) {
		if checked {
			filter.Refs = append(slices.Clone(filter.Refs), ref)
		} else {
			filter.Refs = slices.DeleteFunc(slices.Clone(filter.Refs), func(selected string) bool { return selected == ref })
		}
		w.refreshBranchHistory(filter)
	}
	check := func(label string, checked func() bool, onToggle func(bool)) *checkbox.Widget {
		return checkbox.New(checkbox.Label(label), checkbox.CheckedFn(checked), checkbox.OnToggle(onToggle),
			checkbox.PainterOpt(w.paint.Checkbox))
	}
	all := check("All branches", func() bool { return len(filter.VisibleRefs()) == 0 }, func(checked bool) {
		if checked {
			filter.Refs = nil
			w.refreshBranchHistory(filter)
			ctx.Invalidate()
		}
	})
	leaf := func(label, ref string) widget.Widget {
		return primitives.VBox(check(label, func() bool { return slices.Contains(filter.Refs, ref) },
			func(checked bool) { toggle(ref, checked); all.SetNeedsRedraw(true) })).Height(branchRowHeight)
	}
	branchRows := func(refs []string, prefix string) ([]widget.Widget, string) {
		var rows []widget.Widget
		var first string
		for _, ref := range refs {
			name := shortRef(ref)
			if strings.Contains(strings.ToLower(name), query) {
				if first == "" {
					first = ref
				}
				rows = append(rows, leaf(strings.TrimPrefix(name, prefix), ref))
			}
		}
		return rows, first
	}

	localSlot, allSlot, emptySlot := primitives.VBox(), primitives.VBox(), primitives.VBox()
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
			allSlot.AddChild(primitives.VBox(all).Height(branchRowHeight).PaddingLeft(5))
		}
		children, first := branchRows(localRefs, "")
		firstLocal = first
		localSlot.ClearChildren()
		local.setChildren(children, query != "")
		if len(children) != 0 || query == "" {
			localSlot.AddChild(local.box)
		}
	}
	rebuildRemote := func() {
		firstRemote = ""
		var groups []widget.Widget
		if filter.Remote {
			for _, name := range remoteNames {
				children, first := branchRows(remoteRefs[name], name+"/")
				if first != "" {
					if firstRemote == "" {
						firstRemote = first
					}
					group := remoteGroups[name]
					group.setChildren(children, query != "")
					groups = append(groups, group.box)
				}
			}
		}
		remote.setChildren(groups, query != "")
	}
	updateEmpty := func() {
		emptySlot.ClearChildren()
		if firstLocal == "" && firstRemote == "" {
			emptySlot.AddChild(primitives.VBox(w.muted("No items")).Height(branchRowHeight).PaddingXY(6, 4))
		}
	}
	rebuild := func() {
		rebuildLocal()
		rebuildRemote()
		updateEmpty()
	}
	remote.header.AddChild(toggleswitch.New(toggleswitch.CheckedFn(func() bool { return filter.Remote }),
		toggleswitch.A11yHint("Show remote branches in the branch list and history"), toggleswitch.PainterOpt(w.paint.Switch),
		toggleswitch.OnToggle(func(checked bool) {
			filter.Remote = checked
			rebuildRemote()
			updateEmpty()
			all.SetNeedsRedraw(true)
			w.refreshBranchHistory(filter)
			ctx.Invalidate()
		})).Padding(0))
	in := w.input("Search branches", "", func(text string) {
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
	rebuild()
	rows := primitives.VBox(allSlot, localSlot, remote.box, emptySlot).PaddingRight(10)
	tree := &branchViewport{
		Widget: scrollview.New(rows, scrollview.ScrollYSignal(scroll), scrollview.PainterOpt(w.paint.Scrollbar)),
		scroll: scroll,
	}
	panel := primitives.VBox(compactInput(in), tree).Width(330).Padding(8).Gap(6)
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

type branchGroup struct {
	box, header, body *primitives.BoxWidget
	collapsed         bool
}

type branchGroupKey struct {
	path, ref string
}

func (w *workbench) newBranchGroup(label, ref string, folder bool) *branchGroup {
	indent := float32(20)
	if folder {
		indent = 40
	}
	key := branchGroupKey{path: w.path, ref: ref}
	g := &branchGroup{
		body: primitives.VBox().PaddingLeft(indent), collapsed: w.collapsedBranchGroups[key],
	}
	caption := button.New(button.TextOpt(label), button.VariantOpt(button.TextOnly), button.SizeOpt(button.Small),
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
	g.box = primitives.VBox(g.header)
	return g
}

func (g *branchGroup) expanded() bool { return g.box.ChildCount() > 1 }

func (g *branchGroup) setChildren(children []widget.Widget, searching bool) {
	g.body.ClearChildren()
	for _, child := range children {
		g.body.AddChild(child)
	}
	g.showBody(len(children) > 0 && (searching || !g.collapsed))
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

func (p branchGroupPainter) PaintButton(canvas widget.Canvas, s button.PaintState) {
	label := s.Text
	s.Text = ""
	p.ButtonPainter.PaintButton(canvas, s)
	x, y := s.Bounds.Min.X+4, s.Bounds.Center().Y
	colors := p.Theme.Colors
	if p.group.body.ChildCount() > 0 {
		chevron := icon.ExpChevronRight
		if p.group.expanded() {
			chevron = icon.ExpChevronDown
		}
		icon.Draw(canvas, chevron, geometry.NewRect(x, y-7, 14, 14), colors.OnSurfaceSecondary)
	}
	x += 20
	if p.folder {
		icon.Draw(canvas, icon.ExpFolder, geometry.NewRect(x, y-8, 16, 16), colors.OnSurfaceSecondary)
		x += 22
	}
	bounds := geometry.NewRect(x, s.Bounds.Min.Y, s.Bounds.Max.X-x, s.Bounds.Height())
	if !bounds.IsEmpty() {
		canvas.PushClip(bounds)
		canvas.DrawText(label, bounds, 13, colors.OnSurface, false, widget.TextAlignLeft)
		canvas.PopClip()
	}
}
