package main

import (
	"testing"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/gpucontext"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/core/menu"
	"github.com/gogpu/ui/core/popover"
	"github.com/gogpu/ui/core/toolbar"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/uitest"
)

type overlayTestPointer struct {
	dispatch func(gpucontext.PointerEvent)
}

func (s *overlayTestPointer) OnPointer(dispatch func(gpucontext.PointerEvent)) {
	s.dispatch = dispatch
}

func TestPointerSwitchesMenusAndRespectsDialogs(t *testing.T) {
	w := &workbench{host: gogpu.NewApp(gogpu.DefaultConfig())}
	w.setTheme()
	source := w.overlayEventSource(w.host.EventSource()).(*overlayEventSource)
	pointer := &overlayTestPointer{}
	source.PointerEventSource = pointer
	w.ui = app.New(app.WithEventSource(source))
	win := w.ui.Window()

	var bar *toolbarHints
	open := func(i int) func() {
		return func() {
			w.openToolbarPopup(bar, i, popover.BottomStart,
				primitives.VBox(w.text("Contents")).Width(160).Height(60), nil)
		}
	}
	bar = w.headerToolbar(state.NewSignal(""), []toolbar.Item{
		w.selector("Branches", branchIcon, 110, open(0)),
		w.selector("Repositories", icon.ExpFolder, 110, open(1)),
	})
	w.ui.SetRoot(primitives.VBox(primitives.VBox(bar).Width(240)).PaddingLeft(16))
	win.HandleResize(1080, 520)
	w.ui.Frame()
	win.DrawTo(&uitest.MockCanvas{})
	item := func(i int) geometry.Point { return bar.ItemBounds(i).Center().Add(bar.ScreenOrigin()) }
	click := func(at geometry.Point) {
		e := gpucontext.PointerEvent{PointerID: 1, PointerType: gpucontext.PointerTypeMouse,
			IsPrimary: true, X: float64(at.X), Y: float64(at.Y), Button: gpucontext.ButtonLeft}
		e.Type, e.Buttons = gpucontext.PointerDown, gpucontext.ButtonsLeft
		pointer.dispatch(e)
		e.Type, e.Buttons = gpucontext.PointerUp, 0
		pointer.dispatch(e)
	}

	click(item(0))
	first := w.openPopup
	if first == nil {
		t.Fatal("first trigger did not open its popup")
	}
	click(item(1))
	if first.IsOpen() || w.popupIcon != icon.ExpFolder.Name || win.OverlayCount() != 1 {
		t.Fatal("second trigger should close the first popup and open its own in the same click")
	}
	click(item(1))
	if win.OverlayCount() != 0 {
		t.Fatal("clicking the active trigger should close its popup")
	}

	contextMenu := menu.NewContextMenu([]menu.MenuItem{menu.Item("Action", "", func() {})})
	contextMenu.Show(win.Context(), geometry.Pt(400, 100))
	click(item(0))
	if contextMenu.IsOpen() || w.popupIcon != branchIcon.Name {
		t.Fatal("clicking a toolbar trigger should dismiss a context menu and open the toolbar popup")
	}
	w.closeToolbarPopup()

	modal := dialog.New(dialog.Title("Modal"), dialog.Content(w.text("Dialog contents")))
	modal.Show(win.Context())
	click(item(1))
	if modal.IsOpen() || w.openPopup != nil {
		t.Fatal("click outside a dismissible dialog should close it without activating the background")
	}

	modal.Show(win.Context())
	contextMenu.Show(win.Context(), geometry.Pt(400, 100))
	w.dismissCaptionOverlays()
	if modal.IsOpen() || contextMenu.IsOpen() || win.OverlayCount() != 0 {
		t.Fatal("caption input should dismiss menus and dialogs in stack order")
	}
}
