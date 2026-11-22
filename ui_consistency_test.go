package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/dropdown"
	"github.com/gogpu/ui/core/menu"
	"github.com/gogpu/ui/core/toolbar"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
)

type svgDrawCall struct {
	bounds geometry.Rect
	color  widget.Color
}

type svgMockCanvas struct {
	uitest.MockCanvas
	svgs []svgDrawCall
}

func (c *svgMockCanvas) RenderSVG(_ []byte, bounds geometry.Rect, color widget.Color) {
	c.svgs = append(c.svgs, svgDrawCall{bounds: bounds, color: color})
}

func setupPolishRepo(t *testing.T) (dir string, w *workbench) {
	t.Helper()
	dir = t.TempDir()
	remote := t.TempDir()
	testGit(t, remote, "init", "--bare", "-b", "main")
	testGit(t, dir, "init", "-b", "main")
	testGit(t, dir, "config", "user.name", "Tester")
	testGit(t, dir, "config", "user.email", "tester@example.test")
	testGit(t, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\n"), 0600); err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "add", "tracked.txt")
	testGit(t, dir, "commit", "-m", "Initial commit")
	testGit(t, dir, "remote", "add", "origin", remote)
	testGit(t, dir, "push", "-u", "origin", "main")

	if err := os.WriteFile(filepath.Join(dir, "stashed.txt"), []byte("stash\n"), 0600); err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "add", "stashed.txt")
	testGit(t, dir, "stash", "push", "-m", "Saved work")

	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\ntwo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir, testWorkbench(t, dir)
}

func TestToolbarSquareIconButtonsAndConsistentIcons(t *testing.T) {
	_, w := setupPolishRepo(t)
	var bars []*toolbarHints
	walkWidgets(w.titleBar, func(node widget.Widget) {
		if bar, ok := node.(*toolbarHints); ok {
			bars = append(bars, bar)
		}
	})
	if len(bars) != 2 {
		t.Fatalf("expected 2 toolbars (selectors + actions), got %d", len(bars))
	}
	actions := bars[1]
	for i := range actions.ItemCount() {
		item := actions.ItemAt(i)
		if item.Kind != toolbar.ItemButton || item.ShowLabel {
			continue
		}
		bounds := actions.ItemBounds(i)
		if item.Attached {
			if bounds.Width() != toolbarSplitOptionsWidth || bounds.Height() != toolbarButtonWidth {
				t.Errorf("split options button %q bounds = %v, want %v×%v",
					item.Label, bounds, toolbarSplitOptionsWidth, toolbarButtonWidth)
			}
			prev := actions.ItemBounds(i - 1)
			if prev.Max.X != bounds.Min.X {
				t.Errorf("split pair %q/%q has gap: prev=%v options=%v",
					actions.ItemAt(i-1).Label, item.Label, prev, bounds)
			}
			continue
		}
		if bounds.Width() != toolbarButtonWidth || bounds.Height() != toolbarButtonWidth {
			t.Errorf("toolbar icon button %q bounds = %v, want %v×%v square",
				item.Label, bounds, toolbarButtonWidth, toolbarButtonWidth)
		}
	}

	canvas := &svgMockCanvas{}
	actions.Draw(w.ui.Window().Context(), canvas)
	for _, call := range canvas.svgs {
		size := call.bounds.Size()
		if size.Width != size.Height || size.Width > 16 {
			t.Errorf("action toolbar SVG icon rendered at %v, want crisp square <= 16px", size)
		}
	}
}

func TestSearchAndFilterButtonsAreSquareWithVectors(t *testing.T) {
	_, w := setupPolishRepo(t)
	win := w.ui.Window()

	// History search controls (up, down, close).
	w.showSearch()
	testActionFrame(w)
	var searchBtns []*button.Widget
	walkWidgets(w.titleBar, func(node widget.Widget) {
		if btn, ok := node.(*button.Widget); ok {
			searchBtns = append(searchBtns, btn)
		}
	})
	if len(searchBtns) != 3 {
		t.Fatalf("history search buttons = %d, want 3", len(searchBtns))
	}
	for i, btn := range searchBtns {
		sz := btn.Bounds().Size()
		if sz.Width != 22 || sz.Height != 22 {
			t.Errorf("history search button %d size = %v, want 22×22", i, sz)
		}
		canvas := &svgMockCanvas{}
		btn.Draw(win.Context(), canvas)
		if len(canvas.svgs) != 1 || canvas.svgs[0].bounds.Size() != (geometry.Size{Width: 14, Height: 14}) {
			t.Errorf("history search button %d SVG calls = %+v, want one 14×14 icon", i, canvas.svgs)
		}
	}

	// Dropdown filter clear button.
	w.closeSearch()
	w.branchesPopup(w.branchBar, 1)
	w.ui.Frame()
	win.DrawOverlays(&uitest.MockCanvas{})
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyUnknown, 'm', event.ModNone))
	w.ui.Frame()
	win.DrawOverlays(&uitest.MockCanvas{})
	var clearBtn *button.Widget
	walkWidgets(win.OverlayContentWidgets()[0], func(node widget.Widget) {
		if b, ok := node.(*button.Widget); ok && b.Bounds().Width() == 22 {
			clearBtn = b
		}
	})
	if clearBtn == nil {
		t.Fatal("filter clear button not found")
	}
	if sz := clearBtn.Bounds().Size(); sz.Width != 22 || sz.Height != 22 {
		t.Errorf("filter clear button size = %v, want 22×22", sz)
	}
}

func TestDialogCloseAndPickerUpButtonsAreSquareIcons(t *testing.T) {
	_, w := setupPolishRepo(t)
	win := w.ui.Window()
	assertSquareClose := func(name string, btn *button.Widget, want float32) {
		t.Helper()
		if btn == nil {
			t.Fatalf("%s: missing close button", name)
		}
		sz := btn.Bounds().Size()
		if sz.Width != want || sz.Height != want {
			t.Errorf("%s close button size = %v, want %v×%v", name, sz, want, want)
		}
		canvas := &svgMockCanvas{}
		btn.Draw(win.Context(), canvas)
		for _, dt := range canvas.Texts {
			if dt.Text == "×" || dt.Text != "" {
				t.Errorf("%s close button drew text %q instead of icon", name, dt.Text)
			}
		}
		if len(canvas.svgs) != 1 || canvas.svgs[0].bounds.Width() != canvas.svgs[0].bounds.Height() {
			t.Errorf("%s close button SVG calls = %+v, want 1 square icon", name, canvas.svgs)
		}
	}

	// 1. Settings dialog close button.
	w.showSettings()
	testWaitForGit(t, w)
	w.ui.Frame()
	win.DrawOverlays(&uitest.MockCanvas{})
	var settingsClose *button.Widget
	walkWidgets(win.OverlayContentWidgets()[0], func(node widget.Widget) {
		if b, ok := node.(*button.Widget); ok && settingsClose == nil {
			settingsClose = b
		}
	})
	assertSquareClose("Settings", settingsClose, 28)
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))

	// 2. Repository picker close & up buttons.
	w.showRepositoryPicker()
	w.ui.Frame()
	win.DrawOverlays(&uitest.MockCanvas{})
	var pickerBtns []*button.Widget
	walkWidgets(win.OverlayContentWidgets()[0], func(node widget.Widget) {
		if b, ok := node.(*button.Widget); ok {
			pickerBtns = append(pickerBtns, b)
		}
	})
	if len(pickerBtns) < 2 {
		t.Fatalf("picker buttons = %d, want >= 2", len(pickerBtns))
	}
	assertSquareClose("Repository picker", pickerBtns[0], 28)
	if sz := pickerBtns[1].Bounds().Size(); sz.Width != 28 || sz.Height != 28 {
		t.Errorf("Repository picker up button size = %v, want 28×28", sz)
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))

	// 3. Stash details dialog close button.
	w.stashDetails(w.data.Stashes[0])
	w.ui.Frame()
	win.DrawOverlays(&uitest.MockCanvas{})
	var stashClose *button.Widget
	walkWidgets(win.OverlayContentWidgets()[0], func(node widget.Widget) {
		if b, ok := node.(*button.Widget); ok && stashClose == nil {
			stashClose = b
		}
	})
	assertSquareClose("Stash details", stashClose, 28)
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))

	// 4. Inline history details close button.
	w.prefs.Settings.UnifiedDetails = true
	w.page = "changes"
	testActionFrame(w)
	var inlineClose *button.Widget
	walkWidgets(w.inlineContent, func(node widget.Widget) {
		if b, ok := node.(*button.Widget); ok && inlineClose == nil {
			inlineClose = b
		}
	})
	assertSquareClose("Inline details", inlineClose, 24)

	// 5. File preview close button (matches dialog close icon button).
	w.openPreview(w.data.Files, 0, true, false, diffMode)
	testActionFrame(w)
	var previewClose *button.Widget
	walkWidgets(win.Root(), func(node widget.Widget) {
		if b, ok := node.(*button.Widget); ok {
			canvas := &uitest.MockCanvas{}
			b.Draw(win.Context(), canvas)
			for _, dt := range canvas.Texts {
				if dt.Text == "Close" {
					t.Error("file preview still renders a text 'Close' button instead of an icon button")
				}
			}
			if b.Bounds().Height() == 28 && previewClose == nil {
				previewClose = b
			}
		}
	})
	assertSquareClose("File preview", previewClose, 28)
	testClick(w, previewClose.ScreenBounds().Center())
	testActionFrame(w)
	if w.viewer != nil {
		t.Error("clicking file preview close icon button did not close preview")
	}
}

func TestPopupTriggersHaveNoStuckOutline(t *testing.T) {
	_, w := setupPolishRepo(t)
	win := w.ui.Window()

	var bars []*toolbarHints
	walkWidgets(w.titleBar, func(node widget.Widget) {
		if bar, ok := node.(*toolbarHints); ok {
			bars = append(bars, bar)
		}
	})
	if len(bars) != 2 {
		t.Fatalf("expected 2 toolbars, got %d", len(bars))
	}

	checkNoFocusStroke := func(stage string, bar *toolbarHints) {
		t.Helper()
		canvas := &uitest.MockCanvas{}
		bar.Draw(win.Context(), canvas)
		if len(canvas.StrokeRoundRects) != 0 {
			t.Errorf("%s: toolbar drew unexpected outline(s): %+v", stage, canvas.StrokeRoundRects)
		}
	}

	// Open and dismiss branch selector popup via mouse click + Escape.
	branchTrigger := bars[0].ItemBounds(1).Translate(bars[0].ScreenOrigin())
	testClick(w, branchTrigger.Center())
	w.ui.Frame()
	if w.openPopup == nil {
		t.Fatal("branch popup did not open")
	}
	checkNoFocusStroke("branch popup open", bars[0])
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))
	w.ui.Frame()
	if w.openPopup != nil {
		t.Fatal("Escape did not close branch popup")
	}
	checkNoFocusStroke("branch popup closed", bars[0])

	// Open and dismiss Pull options popup via mouse click + Escape.
	var pullOpt geometry.Rect
	for i := range bars[1].ItemCount() {
		if bars[1].ItemAt(i).Label == "Pull options" {
			pullOpt = bars[1].ItemBounds(i).Translate(bars[1].ScreenOrigin())
		}
	}
	if pullOpt.IsEmpty() {
		t.Fatal("missing Pull options button")
	}
	testClick(w, pullOpt.Center())
	w.ui.Frame()
	if w.openPopup == nil {
		t.Fatal("Pull options popup did not open")
	}
	checkNoFocusStroke("pull options open", bars[1])
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))
	w.ui.Frame()
	checkNoFocusStroke("pull options closed", bars[1])
}

func TestPopupIconsAreNotStretched(t *testing.T) {
	_, w := setupPolishRepo(t)
	win := w.ui.Window()

	// Status bar stashes popup ([0]=branch, [1]=push, [2]=pull, [3]=stashes).
	testFooterButton(t, w, 3)
	w.ui.Frame()
	win.DrawOverlays(&uitest.MockCanvas{})
	if win.OverlayCount() == 0 {
		t.Fatal("stashes popup did not open")
	}
	var stashIcons []*icon.IconWidget
	walkWidgets(win.OverlayContentWidgets()[0], func(node widget.Widget) {
		if ic, ok := node.(*icon.IconWidget); ok && ic.Data().Name == stashIcon.Name {
			stashIcons = append(stashIcons, ic)
		}
	})
	if len(stashIcons) == 0 {
		t.Fatal("missing stash icon in stashes popup")
	}
	for _, ic := range stashIcons {
		if sz := ic.Bounds().Size(); sz.Width != 14 || sz.Height != 14 {
			t.Errorf("popup row icon stretched to %v, want 14×14", sz)
		}
	}
}

func TestDropdownTriggersMenusAndColumnMenuCompactHeight(t *testing.T) {
	_, w := setupPolishRepo(t)
	win := w.ui.Window()

	// Open settings and inspect dropdown trigger and opened dropdown menu height.
	w.showSettings()
	testWaitForGit(t, w)
	w.ui.Frame()
	win.DrawOverlays(&uitest.MockCanvas{})
	var pickers []*dropdown.Widget
	walkWidgets(win.OverlayContentWidgets()[0], func(node widget.Widget) {
		if dd, ok := node.(*dropdown.Widget); ok {
			pickers = append(pickers, dd)
		}
	})
	if len(pickers) == 0 {
		t.Fatal("missing dropdowns in settings")
	}
	for i, dd := range pickers {
		if h := dd.Bounds().Height(); h != 28 {
			t.Errorf("settings dropdown %d height = %v, want 28", i, h)
		}
	}
	pickers[0].Open(win.Context())
	w.ui.Frame()
	if win.OverlayCount() < 2 {
		t.Fatal("dropdown menu did not open")
	}
	menuOverlay := win.OverlayContentWidgets()[win.OverlayCount()-1]
	menuBounds := menuOverlay.(interface{ Bounds() geometry.Rect }).Bounds()
	// "Commit ordering" has 3 items: 3*28 + 2*4 = 92px.
	if menuBounds.Height() != 92 {
		t.Errorf("dropdown menu height = %v, want 92 (3×28 + 8 padding)", menuBounds.Height())
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))

	// File preview controls: File view + compact Diff layout.
	w.openPreview(w.data.Files, 0, true, false, diffMode)
	testActionFrame(w)
	var previewPickers []*dropdown.Widget
	walkWidgets(win.Root(), func(node widget.Widget) {
		if dd, ok := node.(*dropdown.Widget); ok {
			previewPickers = append(previewPickers, dd)
		}
	})
	if len(previewPickers) != 3 {
		t.Fatalf("preview dropdowns = %d, want 3", len(previewPickers))
	}
	if h := previewPickers[2].Bounds().Height(); h != 28 {
		t.Errorf("Diff layout dropdown height = %v, want 28", h)
	}
	if width := previewPickers[2].Bounds().Width(); width >= 120 {
		t.Errorf("Diff layout dropdown width = %v, want compact < 120", width)
	}

	// Column menu uses native Checkable items without space/checkmark string prefixes.
	w.closePreview()
	w.historyColumnMenu(geometry.Pt(100, 10))
	w.ui.Frame()
	canvas := &svgMockCanvas{}
	win.DrawOverlays(canvas)
	for _, call := range canvas.Texts {
		if strings.HasPrefix(call.Text, "✓") || strings.HasPrefix(call.Text, "  ") {
			t.Errorf("column menu item %q still uses ASCII space/checkmark padding", call.Text)
		}
		if call.Text == "Reset Columns" {
			t.Errorf("column menu used inconsistent capitalization %q", call.Text)
		}
	}
	if !slices.ContainsFunc(canvas.Texts, func(call uitest.DrawTextCall) bool { return call.Text == "Diff" }) {
		t.Error("expected column menu item label 'Diff'")
	}
	panelBounds := win.OverlayContentWidgets()[0].(menu.PanelState).Bounds()
	// 4 columns + 1 separator + 1 "Reset columns": 5*28 + 9 + 4*2 = 157px.
	if panelBounds.Height() != 157 {
		t.Errorf("column menu height = %v, want 157 (5×28 + 9 + 8)", panelBounds.Height())
	}
}
