package main

import (
	"github.com/gogpu/gpucontext"
	"github.com/gogpu/ui/core/titlebar"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/gesture"
	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
)

const titleBarHeight = 42

type workbenchTitleBar struct {
	*titlebar.Widget
	bench *workbench
}

func (bar *workbenchTitleBar) GestureHitTest(position geometry.Point) []gesture.Recognizer {
	// The title bar's recognizer handles chrome only. Its child buttons
	// must be able to win the click when the pointer is over them.
	at := bar.ScreenOrigin().Add(position)
	switch bar.HitTest(float64(at.X), float64(at.Y)) {
	case titlebar.HitTestClose, titlebar.HitTestMaximize, titlebar.HitTestMinimize:
		return bar.Widget.GestureHitTest(position)
	default:
		return nil
	}
}

func (bar *workbenchTitleBar) Event(ctx widget.Context, e event.Event) bool {
	w := bar.bench
	if key, ok := e.(*event.KeyEvent); ok && key.KeyType == event.KeyPress &&
		w.searchField != nil && ctx.IsFocused(w.searchField) {
		if key.Key == event.KeyEscape {
			w.closeSearch()
			return true
		}
		if key.Key == event.KeyEnter && key.Modifiers().Has(event.ModShift) {
			w.stepSearch(-1)
			return true
		}
	}
	if mouse, ok := e.(*event.MouseEvent); ok && mouse.MouseType == event.MousePress && mouse.Button == event.ButtonRight {
		if bar.bench.repositoryTriggerMenu(mouse.GlobalPosition) {
			return true
		}
	}
	return bar.Widget.Event(ctx, e)
}

func (w *workbench) newTitleBar(selectors, actions widget.Widget) *workbenchTitleBar {
	chromeTheme := *w.theme
	chromeTheme.Colors.HeaderBackground = w.theme.Colors.Surface
	chromeTheme.Colors.HeaderForeground = w.theme.Colors.OnSurface
	bar := titlebar.New(
		titlebar.Title("Gogigrav"),
		titlebar.Height(titleBarHeight),
		titlebar.Leading(selectors),
		titlebar.Center(actions),
		titlebar.Chrome(w.host),
		titlebar.PainterOpt(devtools.TitleBarPainter{Theme: &chromeTheme}),
	)
	return &workbenchTitleBar{Widget: bar, bench: w}
}

// hitTestWindow reserves the frame edges for resize and the empty title-bar
// space for native window movement. The toolbar and drawn window controls
// receive client input so their widgets can handle hover and clicks.
func (w *workbench) hitTestWindow(x, y float64) gpucontext.HitTestResult {
	caption := w.titleBar.HitTest(x, y) == titlebar.HitTestCaption
	if !w.host.IsMaximized() {
		const resizeInset = 6
		width, height := w.host.Size()
		left := x < resizeInset
		right := x >= float64(width)-resizeInset
		top := y < resizeInset
		bottom := y >= float64(height)-resizeInset
		switch {
		case top && left:
			return gpucontext.HitTestResizeNW
		case top && right:
			return gpucontext.HitTestResizeNE
		case bottom && left:
			return gpucontext.HitTestResizeSW
		case bottom && right:
			return gpucontext.HitTestResizeSE
		case left:
			return gpucontext.HitTestResizeW
		case right:
			return gpucontext.HitTestResizeE
		case bottom:
			return gpucontext.HitTestResizeS
		case top && (caption || y < 0):
			return gpucontext.HitTestResizeN
		}
	}
	if caption {
		return gpucontext.HitTestCaption
	}
	return gpucontext.HitTestClient
}
