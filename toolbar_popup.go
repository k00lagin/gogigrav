package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
		popover.PainterOpt(dropdownPopoverPainter{w.paint.Popover}),
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
	recent      bool
	section     bool
	status      bool
	empty       bool
	action      func()
	contextMenu func(geometry.Point)
}

func (c popupChoice) selectable() bool { return !c.section && !c.status && !c.empty }

func (w *workbench) repositoriesPopup(bar *toolbarHints, index int) {
	icons := newRepositoryIconGallery(w)
	choices := make([]popupChoice, 0, len(w.prefs.Recent))
	appendChoices := func(paths []string, recent bool) {
		for _, path := range paths {
			choices = append(choices, popupChoice{
				label: filepath.Base(path), detail: path, current: sameRepositoryPath(path, w.path), recent: recent,
				leading:     func() widget.Widget { return icons.repository(path) },
				action:      func() { w.open(path) },
				contextMenu: func(at geometry.Point) { w.repositoryMenu(path, at) },
			})
		}
	}
	recent, others := w.prefs.repositoryGroups(time.Now())
	appendChoices(recent, true)
	appendChoices(others, false)
	add := w.dropdownActionButton(icon.Add, "Add repository…", true, func() {
		w.selectPopupAction(w.showRepositoryPicker)
	})
	content, focus := w.searchChoicePanel(380, 44, "Search repositories or paste a path", choices, w.repositorySearch(icons), add)
	content.triggerMenu = w.repositoryTriggerMenu
	w.openToolbarPopup(bar, index, popover.BottomStart, content, focus)
}

func repositoryMenuRows(choices []popupChoice) []popupChoice {
	if len(choices) == 0 || !choices[0].recent {
		return choices
	}
	rows := []popupChoice{{label: "Recent repositories", section: true}}
	end := slices.IndexFunc(choices, func(choice popupChoice) bool { return !choice.recent })
	if end == -1 {
		return append(rows, choices...)
	}
	rows = append(rows, choices[:end]...)
	rows = append(rows, popupChoice{section: true})
	return append(rows, choices[end:]...)
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

func firstSelectableChoice(choices []popupChoice) int {
	return slices.IndexFunc(choices, popupChoice.selectable)
}

func initialChoiceSelection(choices []popupChoice) int {
	for i, choice := range choices {
		if choice.current && choice.selectable() {
			return i
		}
	}
	return firstSelectableChoice(choices)
}

func (w *workbench) searchChoicePanel(width, rowHeight float32, placeholder string, choices []popupChoice, search func(string, func([]popupChoice)), footer ...widget.Widget) (*choicePanelKeys, widget.Widget) {
	matches := choices
	if search != nil {
		matches = repositoryMenuRows(choices)
		if len(matches) == 0 {
			matches = []popupChoice{{label: "No repositories", empty: true}}
		}
	}
	selection := state.NewSignal(initialChoiceSelection(matches))
	scroll := state.NewSignal(float32(0))
	var needle string
	selectableAt := func(index int) bool {
		return index >= 0 && index < len(matches) && matches[index].selectable()
	}
	isInvalid := func() bool {
		if needle == "" {
			return false
		}
		for _, choice := range matches {
			if choice.selectable() || choice.status {
				return false
			}
		}
		return true
	}
	choose := func(index int) {
		if index >= 0 && index < len(matches) && matches[index].action != nil {
			w.selectPopupAction(matches[index].action)
		}
	}
	heightAt := func(index int) float32 {
		choice := matches[index]
		if choice.section {
			if choice.label == "" {
				return dropdownSeparatorHeight
			}
			return dropdownHeadingHeight
		}
		if choice.status || choice.empty {
			return 32
		}
		return rowHeight
	}
	heightOption := listview.FixedItemHeight(rowHeight)
	selectionOption := listview.SelectedIndexSignal(selection)
	if search != nil {
		heightOption = listview.ItemHeightFn(heightAt)
		selectionOption = listview.SelectedIndexReadonlySignal(selection)
	}
	list := listview.New(
		listview.ItemCountFn(func() int { return len(matches) }),
		heightOption,
		listview.SelectionModeOpt(listview.SelectionSingle),
		selectionOption,
		listview.ScrollYSignal(scroll),
		listview.OnItemClick(choose),
		listview.PainterOpt(dropdownListPainter{ListViewPainter: w.paint.ListView, selectable: selectableAt}),
		listview.BuildItem(func(item listview.ItemContext) widget.Widget {
			choice := matches[item.Index]
			if choice.section {
				if choice.label == "" {
					return dropdownSeparator(w)
				}
				return primitives.VBox(w.muted(choice.label).FontSize(11).Bold()).
					Height(dropdownHeadingHeight).PaddingTop(8).PaddingBottom(4).PaddingLeft(8).PaddingRight(8)
			}
			if choice.status || choice.empty {
				return primitives.VBox(w.muted(choice.label)).Height(32).PaddingXY(8, 7)
			}
			label := w.text(choice.label).MaxLines(1).Ellipsis()
			if choice.action == nil {
				label.Color(w.theme.Colors.OnSurfaceDisabled)
			}
			if choice.current && choice.leading == nil {
				label.Bold()
			}
			content := []widget.Widget{label}
			if choice.detail != "" {
				content = append(content, w.muted(choice.detail).FontSize(11).MaxLines(1).Ellipsis())
			}
			text := primitives.VBox(content...).Gap(1)
			if choice.leading != nil {
				items := []widget.Widget{choice.leading(), primitives.Expanded(text)}
				if choice.current {
					checkColor := w.theme.Colors.Primary
					if item.Selected {
						checkColor = w.theme.Colors.OnSurface
					}
					check := icon.NewIcon(icon.Check, icon.Size(14), icon.Color(checkColor))
					items = append(items, primitives.VBox(check).Width(14).Height(14))
				}
				return row(items...).Gap(10).Height(rowHeight).PaddingXY(8, 0)
			}
			if choice.glyph.Name != "" {
				color := w.theme.Colors.OnSurfaceSecondary
				if choice.action == nil {
					color = w.theme.Colors.OnSurfaceDisabled
				}
				return row(icon.NewIcon(choice.glyph, icon.Size(14), icon.Color(color)),
					primitives.Expanded(text)).Gap(8).Height(rowHeight).PaddingXY(8, 0)
			}
			return row(primitives.Expanded(text)).Height(rowHeight).PaddingXY(8, 0)
		}),
	)
	panel := primitives.VBox().Width(width).CrossAlign(primitives.CrossAxisStretch)
	visibleHeight := func() float32 {
		var height float32
		for i := range min(8, len(matches)) {
			height += heightAt(i)
		}
		if len(matches) == 0 {
			return rowHeight
		}
		return height
	}
	listBox := primitives.VBox(list).Height(visibleHeight()).CrossAlign(primitives.CrossAxisStretch)
	var input widget.Widget
	refresh := func() {
		if search != nil {
			listBox.Height(visibleHeight())
			listBox.MarkNeedsLayout()
		}
		if input != nil {
			widget.MarkRedrawInTree(input)
		}
		list.InvalidateData()
		w.ui.Window().Context().Invalidate()
	}
	var focus widget.Widget = list
	if placeholder != "" {
		filterBox, field := w.newFilterInput(placeholder, isInvalid, func(query string) {
			q := strings.TrimSpace(query)
			if search != nil {
				q = repositoryQuery(query)
			}
			needle = strings.ToLower(q)
			filterChoices := func(resolved string) []popupChoice {
				var filtered []popupChoice
				for _, choice := range choices {
					if strings.Contains(strings.ToLower(choice.label+" "+choice.detail), needle) ||
						resolved != "" && sameRepositoryPath(choice.detail, resolved) {
						filtered = append(filtered, choice)
					}
				}
				return filtered
			}
			local := filterChoices("")
			matches = local
			if search != nil {
				matches = repositoryMenuRows(local)
				if len(matches) == 0 {
					label := "No matching repositories"
					if needle == "" {
						label = "No repositories"
					}
					matches = []popupChoice{{label: label, empty: true}}
				}
			}
			selection.Set(firstSelectableChoice(matches))
			scroll.Set(0)
			refresh()
			if search != nil {
				search(query, func(results []popupChoice) {
					var resolved string
					var extra []popupChoice
					for _, result := range results {
						if result.contextMenu != nil {
							resolved = result.detail
						} else {
							extra = append(extra, result)
						}
					}
					if resolved != "" {
						local = filterChoices(resolved)
					}
					matches = repositoryMenuRows(append(local, extra...))
					if resolved != "" {
						selection.Set(slices.IndexFunc(matches, func(choice popupChoice) bool {
							return sameRepositoryPath(choice.detail, resolved)
						}))
					} else {
						selection.Set(firstSelectableChoice(matches))
					}
					refresh()
					if sel := selection.Get(); resolved != "" && sel >= 0 {
						list.ScrollToIndex(sel)
					}
				})
			}
		}, func(string) { choose(selection.Get()) })
		panel.AddChild(filterBox)
		input = field
		focus = field
	}
	if len(choices) > 0 || placeholder == "" || search != nil {
		panel.AddChild(primitives.VBox(listBox).Padding(dropdownListPadding).CrossAlign(primitives.CrossAxisStretch))
	}
	if len(footer) > 0 {
		panel.AddChild(w.dropdownFooter(footer...))
	}
	return &choicePanelKeys{BoxWidget: panel, focus: focus, list: list, selection: selection, scroll: scroll, heightAt: heightAt, wrapSelection: search != nil,
		choose: choose, sectionAt: func(i int) bool { return i >= 0 && i < len(matches) && matches[i].section },
		selectableAt: selectableAt,
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
	heightAt      func(int) float32
	choose        func(int)
	sectionAt     func(int) bool
	selectableAt  func(int) bool
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
			y := mouse.GlobalPosition.Y - bounds.Min.Y + p.scroll.Get()
			for i := range p.list.GetItemCount() {
				y -= p.heightAt(i)
				if y < 0 {
					return p.menu(i, mouse.GlobalPosition)
				}
			}
		}
	}
	if key, ok := e.(*event.KeyEvent); ok && p.menu != nil && ctx.IsFocused(p.list) && isContextMenuKey(key) {
		selected := p.selection.Get()
		if selected >= 0 && selected < p.list.GetItemCount() {
			y := bounds.Min.Y - p.scroll.Get()
			for i := range selected + 1 {
				y += p.heightAt(i)
			}
			return p.menu(selected, geometry.Pt(bounds.Min.X, max(bounds.Min.Y, min(y, bounds.Max.Y))))
		}
	}
	if key, ok := e.(*event.KeyEvent); ok && key.KeyType != event.KeyRelease &&
		(ctx.IsFocused(p.focus) || p.wrapSelection && ctx.IsFocused(p.list)) && p.list.GetItemCount() > 0 {
		delta := 0
		switch key.Key {
		case event.KeyDown:
			delta = 1
		case event.KeyUp:
			delta = -1
		case event.KeyEnter, event.KeySpace:
			if p.wrapSelection && ctx.IsFocused(p.list) {
				p.choose(p.selection.Get())
				return true
			}
		}
		if delta != 0 {
			n := p.list.GetItemCount()
			i := p.selection.Get()
			canSelect := func(idx int) bool {
				if p.selectableAt != nil {
					return p.selectableAt(idx)
				}
				if p.sectionAt != nil {
					return !p.sectionAt(idx)
				}
				return true
			}
			if p.wrapSelection {
				if i < 0 {
					if delta > 0 {
						i = -1
					} else {
						i = 0
					}
				}
				found := false
				for range n {
					i = (i + delta + n) % n
					if canSelect(i) {
						found = true
						break
					}
				}
				if !found {
					return true
				}
			} else {
				i = max(0, min(i+delta, n-1))
			}
			p.selection.Set(i)
			p.list.ScrollToIndex(i)
			return true
		}
	}
	return p.BoxWidget.Event(ctx, e)
}
