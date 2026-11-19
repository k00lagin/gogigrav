package main

import (
	"testing"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/compositor"
	"github.com/gogpu/ui/core/popover"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/core/toolbar"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/uitest"
)

func TestToolbarPopup_SearchAndSelect(t *testing.T) {
	w := &workbench{host: gogpu.NewApp(gogpu.DefaultConfig()), ui: app.New()}
	w.setTheme()
	var selected string
	var bar *toolbarHints
	open := func() {
		content, focus := w.choicePanel(320, 28, "Search branches", []popupChoice{
			{label: "main", current: true, action: func() { selected = "main" }},
			{label: "feature/sidebar", action: func() { selected = "feature/sidebar" }},
			{label: "origin/feature/sidebar", action: func() { selected = "origin/feature/sidebar" }},
		})
		w.openToolbarPopup(bar, 0, popover.BottomStart, content, focus)
	}
	bar = w.headerToolbar(state.NewSignal(""), []toolbar.Item{w.selector("All branches", icon.ExpFolder, 142, open)})
	w.ui.SetRoot(primitives.VBox(primitives.VBox(bar).Width(142)).PaddingLeft(16))
	win := w.ui.Window()
	win.HandleResize(1080, 520)
	w.ui.Frame()
	win.DrawTo(&uitest.MockCanvas{})

	pt := geometry.Pt(40, 10)
	win.HandleEvent(event.NewMouseEvent(event.MousePress, event.ButtonLeft, event.ButtonStateLeft, pt, pt, event.ModNone))
	win.HandleEvent(event.NewMouseEvent(event.MouseRelease, event.ButtonLeft, 0, pt, pt, event.ModNone))
	if w.openPopup == nil || win.OverlayCount() != 1 {
		t.Fatal("toolbar click did not open a popup")
	}
	field, ok := win.Context().FocusedWidget().(*textfield.Widget)
	if !ok || field.Bounds().IsEmpty() {
		t.Fatal("search field should be laid out and focused on open")
	}
	if height := field.Bounds().Height(); height != 32 {
		t.Fatalf("search field height = %v, want 32", height)
	}
	for _, char := range "FEATURE" {
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyUnknown, char, event.ModNone))
	}
	w.ui.Frame()
	overlays := win.OverlayContentWidgets()
	positioned := overlays[0].(interface {
		Bounds() geometry.Rect
		SetScreenOrigin(geometry.Point)
	})
	popupBounds := positioned.Bounds()
	// Desktop stamps the window-space overlay origin before recording.
	positioned.SetScreenOrigin(popupBounds.Min)
	app.PaintOverlayBoundaries(overlays, win.Context())
	layers := compositor.NewOffsetLayer(geometry.Point{})
	app.AppendOverlaysToLayerTree(layers, overlays, nil)
	var rows []geometry.Point
	var collectRows func(compositor.Layer)
	collectRows = func(layer compositor.Layer) {
		switch layer := layer.(type) {
		case *compositor.PictureLayerImpl:
			if layer.ScreenOrigin() != popupBounds.Min {
				rows = append(rows, layer.ScreenOrigin())
			}
		case compositor.ContainerLayer:
			for _, child := range layer.Children() {
				collectRows(child)
			}
		}
	}
	collectRows(layers)
	for i, origin := range rows {
		want := popupBounds.Min.Add(geometry.Pt(8, 8+field.Bounds().Height()+6+float32(i)*28))
		if origin != want {
			t.Errorf("row %d origin = %v, want %v", i, origin, want)
		}
	}
	if len(rows) != 2 {
		t.Errorf("compositor recorded %d filtered rows, want 2", len(rows))
	}
	if origin := field.ScreenBounds().Min; origin != popupBounds.Min.Add(geometry.Pt(8, 8)) {
		t.Errorf("search field origin = %v, popup starts at %v", origin, popupBounds.Min)
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyDown, 0, event.ModNone))
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	if w.popupAction == nil {
		t.Fatal("Enter did not select the highlighted search result")
	}
	w.popupAction()
	if selected != "origin/feature/sidebar" || win.OverlayCount() != 0 || win.Context().FocusedWidget() != bar {
		t.Fatalf("selection = %q, overlays = %d, focus = %T", selected, win.OverlayCount(), win.Context().FocusedWidget())
	}
	open()
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))
	if w.openPopup != nil || win.OverlayCount() != 0 {
		t.Fatal("Escape did not dismiss the popup")
	}
}
