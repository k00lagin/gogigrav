package main

import (
	"github.com/gogpu/gpucontext"
	"github.com/gogpu/ui/core/titlebar"
	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
)

const titleBarHeight = 42

func (w *workbench) newTitleBar(selectors, actions widget.Widget) *titlebar.Widget {
	chromeTheme := *w.theme
	chromeTheme.Colors.HeaderBackground = w.theme.Colors.Surface
	chromeTheme.Colors.HeaderForeground = w.theme.Colors.OnSurface
	return titlebar.New(
		titlebar.Title("Gogigrav"),
		titlebar.Height(titleBarHeight),
		titlebar.Leading(selectors),
		titlebar.Center(actions),
		titlebar.Chrome(w.host),
		titlebar.PainterOpt(devtools.TitleBarPainter{Theme: &chromeTheme}),
	)
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
