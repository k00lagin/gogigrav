package main

import (
	"github.com/gogpu/gpucontext"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/overlay"
)

type overlayEventSource struct {
	gpucontext.EventSource
	gpucontext.PointerEventSource
	gpucontext.ScrollEventSource
	gpucontext.GestureEventSource
	bench *workbench
}

func (w *workbench) overlayEventSource(source gpucontext.EventSource) gpucontext.EventSource {
	// Gogpu's desktop event source supplies all three extensions.
	return &overlayEventSource{
		EventSource:        source,
		PointerEventSource: source.(gpucontext.PointerEventSource),
		ScrollEventSource:  source.(gpucontext.ScrollEventSource),
		GestureEventSource: source.(gpucontext.GestureEventSource),
		bench:              w,
	}
}

func (s *overlayEventSource) OnPointer(dispatch func(gpucontext.PointerEvent)) {
	s.PointerEventSource.OnPointer(func(e gpucontext.PointerEvent) {
		if e.Type == gpucontext.PointerDown {
			s.bench.dismissPopupsOutside(geometry.Pt(float32(e.X), float32(e.Y)))
		}
		dispatch(e)
	})
}

// Dismiss popups before the UI starts routing the pointer sequence. The
// original press and release then reach the widget beneath the popup.
func (w *workbench) dismissPopupsOutside(at geometry.Point) {
	stack := w.ui.Window().Overlays()
	for top := stack.TopInteractive(); top != nil; top = stack.TopInteractive() {
		container, ok := top.(*overlay.Container)
		if !ok || container.Modal() {
			return
		}
		content := container.Content()
		if modal, ok := content.(interface{ Modal() bool }); ok && modal.Modal() {
			return
		}
		if bounds, ok := content.(interface{ Bounds() geometry.Rect }); !ok || bounds.Bounds().Contains(at) {
			return
		}
		// Let the normal outside-click path consume the click on the active
		// trigger so the release cannot reopen the popup we just closed.
		if top == w.popupLayer && w.popupAnchor != nil && w.popupAnchor().Contains(at) {
			return
		}
		top.Dismiss()
		if stack.TopInteractive() == top {
			return
		}
	}
}

func (w *workbench) dismissCaptionOverlays() {
	stack := w.ui.Window().Overlays()
	for top := stack.TopInteractive(); top != nil; top = stack.TopInteractive() {
		top.Dismiss()
		if stack.TopInteractive() == top {
			break
		}
	}
	w.clearToolbarTooltip()
}
