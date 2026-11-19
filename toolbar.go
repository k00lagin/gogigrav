package main

import (
	"embed"
	"fmt"

	"github.com/gogpu/ui/core/menu"
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

const remotesLabel = "Remotes"

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
	var bar *toolbar.Widget
	if w.path != "" {
		hasRemote := len(w.data.Remotes) > 0
		hasHead := w.data.Head != ""
		items = append(items,
			action("Find in history (Ctrl+F)", icon.ExpSearch, true, w.toggleSearch),
			action("Branches", branchIcon, hasHead, w.branchForm),
			action("Stashes", stashIcon, true, w.stashForm),
			action("Fetch all remotes", fetchIcon, hasRemote, func() {
				w.command("Fetch", "git", "fetch", "--all", "--prune")
			}),
			action(syncLabel("Pull", w.data.Behind), pullIcon, hasRemote, func() { w.syncBranch(false) }),
			options("Pull options", hasRemote, func() { w.syncBranchForm(false) }),
		)
		if hasRemote {
			items = append(items,
				action(syncLabel("Push", w.data.Ahead), pushIcon, hasHead, func() { w.syncBranch(true) }),
				options("Push options", hasHead, func() { w.syncBranchForm(true) }),
			)
		} else {
			items = append(items, action("Publish to GitHub", pushIcon, hasHead, w.publishForm))
		}
		index := len(items)
		remotes := toolbar.TextIconButton(remotesLabel, remotesIcon, func() {
			w.showRemotesMenu(bar, index)
		})
		remotes.Enabled = !w.busy
		remotes.Width = 100
		items = append(items, remotes,
			action("Refresh (F5)", icon.ExpRefresh, true, func() { w.refresh(w.filter) }),
		)
	}
	items = append(items,
		action("Settings", icon.ExpSettings, true, func() { w.show("settings") }),
		toolbar.Custom(primitives.Box().Width(4).Height(1)),
	)
	painter := w.paint.Toolbar
	painter.IconSize = 20
	bar = toolbar.New(toolbar.Items(items...), toolbar.Height(titleBarHeight),
		toolbar.ButtonSize(32), toolbar.ButtonHeight(32), toolbar.Gap(6),
		toolbar.PainterOpt(actionToolbarPainter{painter}))
	return &toolbarHints{Widget: bar, hint: hint}
}

func syncLabel(label string, count int) string {
	if count > 0 {
		return fmt.Sprintf("%s (%d commits)", label, count)
	}
	return label
}

func (w *workbench) showRemotesMenu(bar *toolbar.Widget, index int) {
	var items []menu.MenuItem
	for _, name := range w.data.Remotes {
		items = append(items, menu.Item("Edit "+name+"…", "", func() { w.editRemoteForm(name) }))
	}
	if len(items) > 0 {
		items = append(items, menu.Sep())
	}
	items = append(items, menu.Item("Add remote…", "", w.addRemoteForm))
	if len(w.data.Remotes) == 0 {
		publish := menu.Item("Publish to GitHub…", "", w.publishForm)
		publish.Disabled = w.data.Head == ""
		items = append(items, publish)
	}
	bounds := bar.ItemBounds(index).Translate(bar.ScreenOrigin())
	popup := menu.NewContextMenu(items, menu.ContextPainterOpt(w.paint.Menu))
	popup.Show(w.ui.Window().Context(), geometry.Pt(bounds.Min.X, bar.ScreenBounds().Max.Y+3))
}

// Show the command name in the status bar on hover, including disabled actions.
type toolbarHints struct {
	*toolbar.Widget
	hint state.Signal[string]
}

func (t *toolbarHints) Event(ctx widget.Context, e event.Event) bool {
	if mouse, ok := e.(*event.MouseEvent); ok {
		switch mouse.MouseType {
		case event.MouseEnter, event.MouseMove:
			point := mouse.Position.Sub(t.Bounds().Min)
			t.hint.Set(t.ItemAt(t.HitTestItem(point)).Label)
		case event.MouseLeave:
			t.hint.Set("")
		}
	}
	return t.Widget.Event(ctx, e)
}

type actionToolbarPainter struct {
	devtools.ToolbarPainter
}

// The title bar draws the shared background and separator.
func (p actionToolbarPainter) PaintToolbar(widget.Canvas, toolbar.PaintToolbarState) {}

func (p actionToolbarPainter) PaintButtonItem(canvas widget.Canvas, item toolbar.PaintButtonState) {
	if item.Label != remotesLabel {
		p.ToolbarPainter.PaintButtonItem(canvas, item)
		return
	}
	colors := p.Theme.Colors
	bounds := item.Bounds
	canvas.DrawRoundRect(bounds, colors.Background, 4)
	p.ToolbarPainter.PaintButtonItem(canvas, item)
	canvas.StrokeRoundRect(bounds, colors.Border, 4, 1)
	color := colors.OnSurfaceSecondary
	if item.Disabled {
		color = colors.OnSurfaceDisabled
	}
	chevron := geometry.NewRect(bounds.Max.X-18, bounds.Center().Y-6, 12, 12)
	icon.Draw(canvas, icon.ExpChevronDown, chevron, color)
}
