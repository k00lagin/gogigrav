package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/listview"
	"github.com/gogpu/ui/core/popover"
	"github.com/gogpu/ui/core/toolbar"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
)

func (w *workbench) selector(label string, glyph icon.IconData, width float32, open func()) toolbar.Item {
	item := toolbar.TextIconButton(label, glyph, open)
	item.Width = width
	item.Enabled = !w.busy
	return item
}

func (w *workbench) repositoryToolbar(hint state.Signal[string]) widget.Widget {
	const repoWidth, branchWidth, gap = 160, 142, 6
	var bar *toolbarHints
	name := "Repository"
	if w.path != "" {
		name = filepath.Base(w.path)
	}
	items := []toolbar.Item{w.selector(name, icon.ExpFolder, repoWidth, func() { w.repositoriesPopup(bar, 0) })}
	width := float32(repoWidth)
	if w.path != "" {
		items = append(items, w.selector(w.branchFilterLabel(), branchIcon, branchWidth, func() { w.branchesPopup(bar, 1) }))
		width += branchWidth + gap
	}
	bar = w.headerToolbar(hint, items)
	bar.branchLabel = w.branchFilterLabel
	bar.repoPath = filepath.ToSlash(w.path)
	w.branchBar = bar
	return primitives.VBox(bar).Width(width)
}

func (w *workbench) branchFilterLabel() string {
	refs := w.filter.VisibleRefs()
	switch len(refs) {
	case 0:
		return "All branches"
	case 1:
		return shortRef(refs[0])
	default:
		return fmt.Sprintf("%d branches", len(refs))
	}
}

// openToolbarPopup accepts any widget tree as its content. The optional focus
// target lets a menu put the caret in a search field as soon as it opens.
func (w *workbench) openToolbarPopup(bar *toolbarHints, index int, placement popover.Placement, content, focus widget.Widget) {
	anchor := func() geometry.Rect {
		bounds := bar.ItemBounds(index).Translate(bar.ScreenOrigin())
		panel := bar.ScreenBounds()
		bounds.Min.Y, bounds.Max.Y = panel.Min.Y, panel.Max.Y
		return bounds
	}
	w.openAnchoredPopup(bar, bar.ItemAt(index).Icon.Name, anchor, placement, content, focus)
}

type popupAnchor interface {
	widget.Widget
	ScreenBounds() geometry.Rect
	SetNeedsRedraw(bool)
}

func (w *workbench) openAnchoredPopup(trigger popupAnchor, id string, anchor func() geometry.Rect, placement popover.Placement, content, focus widget.Widget) {
	w.clearToolbarTooltip()
	ctx := w.ui.Window().Context()
	alreadyOpen := w.popupIcon == id
	w.closeToolbarPopup()
	if alreadyOpen || w.busy {
		return
	}

	popup := popover.NewPopover(
		popover.AnchorBounds(anchor),
		popover.Content(content),
		popover.PlacementOpt(placement),
		popover.PainterOpt(w.paint.Popover),
		popover.OnHide(func() {
			if w.repoSearchCancel != nil {
				w.repoSearchCancel()
				w.repoSearchCancel = nil
			}
			w.openPopup, w.popupIcon = nil, ""
			w.popupAnchor = nil
			w.popupLayer = nil
			ctx.RequestFocus(trigger)
			trigger.SetNeedsRedraw(true)
			ctx.InvalidateRect(trigger.ScreenBounds())
		}),
	)
	popup.Show(ctx)
	if popup.IsOpen() {
		w.openPopup, w.popupIcon = popup, id
		w.popupAnchor = anchor
		w.popupLayer = w.ui.Window().Overlays().TopInteractive()
		if focus != nil {
			ctx.RequestFocus(focus)
		}
		trigger.SetNeedsRedraw(true)
		ctx.InvalidateRect(trigger.ScreenBounds())
	}
}

func (w *workbench) closeToolbarPopup() {
	if w.openPopup != nil {
		w.openPopup.Hide(w.ui.Window().Context())
	}
}

func (w *workbench) selectPopupAction(action func()) {
	// A ListView grants itself focus after it calls OnItemClick. Let the event
	// finish before dismissing the overlay so focus returns to the toolbar.
	popup := w.openPopup
	w.popupAction = func() {
		if popup != nil && w.openPopup == popup {
			w.closeToolbarPopup()
			action()
		}
	}
	w.host.RequestRedraw()
}

type popupChoice struct {
	label       string
	detail      string
	leading     func() widget.Widget
	glyph       icon.IconData
	current     bool
	action      func()
	contextMenu func(geometry.Point)
}

func (w *workbench) repositoriesPopup(bar *toolbarHints, index int) {
	icons := newRepositoryIconGallery(w)
	choices := make([]popupChoice, 0, len(w.prefs.Recent))
	for _, path := range w.prefs.Recent {
		choices = append(choices, popupChoice{
			label: filepath.Base(path), detail: path, current: path == w.path,
			leading:     func() widget.Widget { return icons.repository(path) },
			action:      func() { w.open(path) },
			contextMenu: func(at geometry.Point) { w.repositoryMenu(path, at) },
		})
	}
	add := w.btn("Open repository…", button.TextOnly, true, func() {
		w.selectPopupAction(func() { w.show("open") })
	})
	content, focus := w.searchChoicePanel(400, 42, "Search repositories or paste a path", choices, w.repositorySearch(icons), add)
	content.triggerMenu = w.repositoryTriggerMenu
	w.openToolbarPopup(bar, index, popover.BottomStart, content, focus)
}

func (w *workbench) remotesPopup(bar *toolbarHints, index int) {
	choices := make([]popupChoice, 0, len(w.data.Remotes)+2)
	for _, name := range w.data.Remotes {
		choices = append(choices, popupChoice{label: "Edit " + name + "…", action: func() { w.editRemoteForm(name) }})
	}
	choices = append(choices, popupChoice{label: "Add remote…", action: w.addRemoteForm})
	if len(w.data.Remotes) == 0 {
		publish := popupChoice{label: "Publish to GitHub…"}
		if w.data.Head != "" {
			publish.action = w.publishForm
		}
		choices = append(choices, publish)
	}
	content, focus := w.choicePanel(240, 28, "", choices)
	w.openToolbarPopup(bar, index, popover.BottomEnd, content, focus)
}

func (w *workbench) choicePanel(width, rowHeight float32, placeholder string, choices []popupChoice, footer ...widget.Widget) (widget.Widget, widget.Widget) {
	return w.searchChoicePanel(width, rowHeight, placeholder, choices, nil, footer...)
}

func (w *workbench) searchChoicePanel(width, rowHeight float32, placeholder string, choices []popupChoice, search func(string, func([]popupChoice)), footer ...widget.Widget) (*choicePanelKeys, widget.Widget) {
	matches := choices
	selection := state.NewSignal(0)
	for i, choice := range choices {
		if choice.current {
			selection.Set(i)
			break
		}
	}
	scroll := state.NewSignal(float32(0))
	choose := func(index int) {
		if index >= 0 && index < len(matches) && matches[index].action != nil {
			w.selectPopupAction(matches[index].action)
		}
	}
	list := listview.New(
		listview.ItemCountFn(func() int { return len(matches) }),
		listview.FixedItemHeight(rowHeight),
		listview.SelectionModeOpt(listview.SelectionSingle),
		listview.SelectedIndexSignal(selection),
		listview.ScrollYSignal(scroll),
		listview.OnItemClick(choose),
		listview.PainterOpt(w.paint.ListView),
		listview.BuildItem(func(item listview.ItemContext) widget.Widget {
			choice := matches[item.Index]
			label := w.text(choice.label).MaxLines(1).Ellipsis()
			if choice.action == nil {
				label.Color(w.theme.Colors.OnSurfaceDisabled)
			}
			if choice.current {
				label.Bold()
			}
			content := []widget.Widget{label}
			if choice.detail != "" {
				content = append(content, w.muted(choice.detail).FontSize(11).MaxLines(1).Ellipsis())
			}
			text := primitives.VBox(content...)
			if choice.leading != nil {
				return row(choice.leading(), primitives.Expanded(text)).Gap(8).PaddingXY(8, 3)
			}
			if choice.glyph.Name != "" {
				color := w.theme.Colors.OnSurfaceSecondary
				if choice.action == nil {
					color = w.theme.Colors.OnSurfaceDisabled
				}
				return row(icon.NewIcon(choice.glyph, icon.Size(14), icon.Color(color)),
					primitives.Expanded(text)).PaddingXY(6, 3)
			}
			return text.PaddingXY(8, 4)
		}),
	)
	panel := primitives.VBox().Width(width).Padding(8).Gap(6)
	listBox := primitives.VBox(list).Height(rowHeight * float32(max(1, min(8, len(choices)))))
	refresh := func() {
		if search != nil {
			listBox.Height(rowHeight * float32(max(1, min(8, len(matches)))))
			listBox.MarkNeedsLayout()
		}
		list.InvalidateData()
		w.ui.Window().Context().Invalidate()
	}
	var focus widget.Widget = list
	if placeholder != "" {
		input := w.input(placeholder, "", func(query string) {
			needle := strings.TrimSpace(query)
			if search != nil {
				needle = repositoryQuery(query)
			}
			needle = strings.ToLower(needle)
			matches = nil
			for _, choice := range choices {
				if strings.Contains(strings.ToLower(choice.label+" "+choice.detail), needle) {
					matches = append(matches, choice)
				}
			}
			selection.Set(0)
			scroll.Set(0)
			refresh()
			if search != nil {
				local := matches
				if len(local) == 0 {
					matches = []popupChoice{{label: "No matching repositories"}}
					refresh()
				}
				search(query, func(results []popupChoice) {
					var extra []popupChoice
					for _, result := range results {
						found := false
						for _, known := range local {
							if result.detail != "" && known.contextMenu != nil && sameRepositoryPath(result.detail, known.detail) {
								found = true
								break
							}
						}
						if !found {
							extra = append(extra, result)
						}
					}
					matches = append(local, extra...)
					refresh()
				})
			}
		}, func(string) { choose(selection.Get()) })
		panel.AddChild(compactInput(input))
		focus = input
	}
	if len(choices) > 0 || placeholder == "" || search != nil {
		panel.AddChild(listBox)
	}
	for _, child := range footer {
		panel.AddChild(child)
	}
	return &choicePanelKeys{BoxWidget: panel, focus: focus, list: list, selection: selection, scroll: scroll, rowHeight: rowHeight, wrapSelection: search != nil,
		menu: func(i int, at geometry.Point) bool {
			if i < 0 || i >= len(matches) || matches[i].contextMenu == nil {
				return false
			}
			matches[i].contextMenu(at)
			return true
		}}, focus
}

type choicePanelKeys struct {
	*primitives.BoxWidget
	focus         widget.Widget
	list          *listview.Widget
	selection     state.Signal[int]
	scroll        state.Signal[float32]
	rowHeight     float32
	menu          func(int, geometry.Point) bool
	triggerMenu   func(geometry.Point) bool
	wrapSelection bool
}

func (p *choicePanelKeys) Event(ctx widget.Context, e event.Event) bool {
	bounds := p.list.ScreenBounds()
	if mouse, ok := e.(*event.MouseEvent); ok && mouse.MouseType == event.MousePress && mouse.Button == event.ButtonRight {
		if p.triggerMenu != nil && p.triggerMenu(mouse.GlobalPosition) {
			return true
		}
		if p.menu != nil && bounds.Contains(mouse.GlobalPosition) {
			i := int((mouse.GlobalPosition.Y - bounds.Min.Y + p.scroll.Get()) / p.rowHeight)
			if p.menu(i, mouse.GlobalPosition) {
				return true
			}
		}
	}
	if key, ok := e.(*event.KeyEvent); ok && p.menu != nil && ctx.IsFocused(p.list) && isContextMenuKey(key) {
		y := bounds.Min.Y + float32(p.selection.Get()+1)*p.rowHeight - p.scroll.Get()
		return p.menu(p.selection.Get(), geometry.Pt(bounds.Min.X, max(bounds.Min.Y, min(y, bounds.Max.Y))))
	}
	if key, ok := e.(*event.KeyEvent); ok && key.KeyType != event.KeyRelease &&
		ctx.IsFocused(p.focus) && p.list.GetItemCount() > 0 {
		delta := 0
		switch key.Key {
		case event.KeyDown:
			delta = 1
		case event.KeyUp:
			delta = -1
		}
		if delta != 0 {
			n := p.list.GetItemCount()
			i := p.selection.Get() + delta
			if p.wrapSelection {
				i = (i + n) % n
			} else {
				i = max(0, min(i, n-1))
			}
			p.selection.Set(i)
			p.list.ScrollToIndex(i)
			return true
		}
	}
	return p.BoxWidget.Event(ctx, e)
}
