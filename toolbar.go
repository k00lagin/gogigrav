package main

import (
	"embed"
	"fmt"

	"github.com/gogpu/ui/core/popover"
	"github.com/gogpu/ui/core/toolbar"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
)

//go:embed assets/intui/*.svg
var toolbarSVG embed.FS

var (
	branchIcon  = intIcon("vcs")
	stashIcon   = intIcon("stash")
	fetchIcon   = intIcon("fetch")
	pullIcon    = intIcon("download")
	pushIcon    = intIcon("upload")
	remotesIcon = intIcon("cloudService")
)

func intIcon(name string) icon.IconData {
	svg, err := toolbarSVG.ReadFile("assets/intui/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return icon.FromSVGXML(name, svg)
}

func (w *workbench) actionToolbar(hint state.Signal[string]) widget.Widget {
	action := func(label string, image icon.IconData, enabled bool, fn func()) toolbar.Item {
		item := toolbar.IconButton(label, image, fn)
		item.Enabled = enabled && !w.busy
		return item
	}
	options := func(label string, enabled bool, fn func()) toolbar.Item {
		item := action(label, icon.ExpChevronDown, enabled, fn)
		item.Width = 20
		return item
	}
	items := []toolbar.Item{toolbar.Spacer()}
	var bar *toolbarHints
	if w.path != "" {
		hasRemote := len(w.data.Remotes) > 0
		hasHead := w.data.Head != ""
		hasBranch := hasHead && w.data.CurrentBranch() != ""
		stashIndex := len(items) + 2
		items = append(items,
			action("Find in history (Ctrl+F)", icon.ExpSearch, true, w.toggleSearch),
			action("Branches", branchIcon, hasHead, w.branchForm),
			action("Stashes", stashIcon, true, func() {
				content, focus := w.stashesPanel()
				w.openToolbarPopup(bar, stashIndex, popover.BottomStart, content, focus)
			}),
			action("Fetch all remotes", fetchIcon, hasRemote, w.fetch),
			action(syncLabel("Pull", w.data.Behind), pullIcon, hasRemote, func() { w.syncBranch(false) }),
			options("Pull options", hasRemote, func() { w.syncBranchForm(false) }),
		)
		if hasRemote {
			items = append(items,
				action(syncLabel("Push", w.data.Ahead), pushIcon, hasBranch, func() { w.syncBranch(true) }),
				options("Push options", hasBranch, func() { w.syncBranchForm(true) }),
			)
		} else {
			items = append(items, action("Publish to GitHub", pushIcon, hasBranch, w.publishForm))
		}
		index := len(items)
		remotes := w.selector("Remotes", remotesIcon, 118, func() {
			w.remotesPopup(bar, index)
		})
		items = append(items, remotes,
			action("Refresh (F5)", icon.ExpRefresh, true, func() { w.refresh(w.filter) }),
		)
	}
	items = append(items,
		action("Settings", icon.ExpSettings, true, w.showSettings),
		toolbar.Custom(primitives.Box().Width(4).Height(1)),
	)
	bar = w.headerToolbar(hint, items)
	return bar
}

func (w *workbench) headerToolbar(hint state.Signal[string], items []toolbar.Item) *toolbarHints {
	painter := w.paint.Toolbar
	painter.IconSize = 20
	bar := toolbar.New(toolbar.Items(items...), toolbar.Height(titleBarHeight),
		toolbar.ButtonSize(32), toolbar.ButtonHeight(32), toolbar.Gap(6),
		toolbar.PainterOpt(actionToolbarPainter{painter, w}))
	return &toolbarHints{Widget: bar, hint: hint, bench: w}
}

func syncLabel(label string, count int) string {
	if count > 0 {
		return fmt.Sprintf("%s (%d commits)", label, count)
	}
	return label
}

// Show the command name in the status bar on hover, including disabled actions.
type toolbarHints struct {
	*toolbar.Widget
	bench       *workbench
	hint        state.Signal[string]
	branchLabel func() string
	repoPath    string
}

func (t *toolbarHints) Event(ctx widget.Context, e event.Event) bool {
	if key, ok := e.(*event.KeyEvent); ok && t.repoPath != "" && (ctx.IsFocused(t) || ctx.IsFocused(t.Widget)) && isContextMenuKey(key) {
		bounds := t.ItemBounds(0).Translate(t.ScreenOrigin())
		t.bench.repositoryMenu(t.bench.path, geometry.Pt(bounds.Min.X, bounds.Max.Y))
		return true
	}
	if mouse, ok := e.(*event.MouseEvent); ok {
		switch mouse.MouseType {
		case event.MouseEnter, event.MouseMove:
			point := mouse.Position.Sub(t.Bounds().Min)
			index := t.HitTestItem(point)
			item := t.ItemAt(index)
			label := item.Label
			if t.branchLabel != nil && item.Icon.Name == branchIcon.Name {
				label = t.branchLabel()
			}
			t.hint.Set(label)
			if index == 0 && t.repoPath != "" {
				label = t.repoPath
			}
			if mouse.Buttons != 0 || index < 0 || item.Kind != toolbar.ItemButton {
				label = ""
			}
			t.bench.hoverToolbarItem(t, index, label, func() geometry.Rect {
				return t.ItemBounds(index).Translate(t.ScreenOrigin())
			}, popover.BottomStart)
		case event.MouseLeave:
			t.hint.Set("")
			if t.bench.toolbarHover.bar == t {
				t.bench.clearToolbarTooltip()
			}
		case event.MousePress:
			t.bench.clearToolbarTooltip()
		}
	}
	return t.Widget.Event(ctx, e)
}

type actionToolbarPainter struct {
	devtools.ToolbarPainter
	bench *workbench
}

// The title bar draws the shared background and separator.
func (p actionToolbarPainter) PaintToolbar(widget.Canvas, toolbar.PaintToolbarState) {}

func (p actionToolbarPainter) PaintButtonItem(canvas widget.Canvas, item toolbar.PaintButtonState) {
	if !item.ShowLabel {
		p.ToolbarPainter.PaintButtonItem(canvas, item)
		return
	}
	colors := p.Theme.Colors
	bounds := item.Bounds
	if item.Hovered || p.bench.popupIcon == item.Icon.Name {
		canvas.DrawRoundRect(bounds, colors.ControlHover, 5)
	}
	color := colors.OnSurface
	if item.Disabled {
		color = colors.OnSurfaceDisabled
	}
	center := bounds.Center().Y
	icon.Draw(canvas, item.Icon, geometry.NewRect(bounds.Min.X+7, center-9, 18, 18), color)
	chevron := geometry.NewRect(bounds.Max.X-19, center-7, 14, 14)
	label := geometry.NewRect(bounds.Min.X+29, bounds.Min.Y, chevron.Min.X-bounds.Min.X-30, bounds.Height())
	if !label.IsEmpty() {
		canvas.PushClip(label)
		text := item.Label
		if item.Icon.Name == branchIcon.Name {
			text = p.bench.branchFilterLabel()
		}
		canvas.DrawText(text, label, 13, color, false, widget.TextAlignLeft)
		canvas.PopClip()
	}
	icon.Draw(canvas, icon.ExpChevronDown, chevron, color)
}
