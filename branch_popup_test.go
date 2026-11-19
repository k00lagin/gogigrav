package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/checkbox"
	"github.com/gogpu/ui/core/toggleswitch"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/gesture"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func TestBranchPopup_ToggleAndSearch(t *testing.T) {
	path := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := repo.Run(t.Context(), path, "git", args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	git("init", "-b", "main")
	git("config", "user.name", "Tester")
	git("config", "user.email", "tester@example.test")
	git("config", "commit.gpgsign", "false")
	git("commit", "--allow-empty", "-m", "Local history")
	topic := git("commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "Remote history")
	git("remote", "add", "origin", path)
	git("update-ref", "refs/remotes/origin/topic", topic)

	f := repo.Filter{Remote: true, Limit: 250}
	data, err := repo.Load(t.Context(), path, f)
	if err != nil {
		t.Fatal(err)
	}
	w := &workbench{
		ctx: t.Context(), done: make(chan func(), 1),
		host: gogpu.NewApp(gogpu.DefaultConfig()), ui: app.New(),
		path: path, filter: f, data: data, selected: state.NewSignal(0), ratio: 0.65, page: "changes",
	}
	w.setTheme()
	w.filterRows()
	w.render()
	win := w.ui.Window()
	win.HandleResize(1080, 520)
	w.ui.Frame()
	win.DrawTo(&uitest.MockCanvas{})
	var field, content widget.Widget
	open := func() {
		t.Helper()
		w.branchesPopup(w.branchBar, 1)
		field = win.Context().FocusedWidget()
		overlays := win.OverlayContentWidgets()
		if len(overlays) != 1 || field == nil {
			t.Fatalf("expected one overlay with focused search, got %d and %T", len(overlays), field)
		}
		content = overlays[0]
	}
	open()

	var leaves []*checkbox.Widget
	var groups []*button.Widget
	var remote *toggleswitch.Widget
	var viewport *branchViewport
	var visit func(widget.Widget)
	visit = func(node widget.Widget) {
		switch v := node.(type) {
		case *branchViewport:
			viewport = v
		case *button.Widget:
			groups = append(groups, v)
		case *checkbox.Widget:
			leaves = append(leaves, v)
		case *toggleswitch.Widget:
			remote = v
		}
		for _, child := range node.Children() {
			visit(child)
		}
	}
	draw := func() *uitest.MockCanvas {
		w.ui.Frame()
		canvas := &uitest.MockCanvas{}
		win.DrawOverlays(canvas)
		leaves = nil
		groups = nil
		remote = nil
		visit(content)
		return canvas
	}
	click := func(pt geometry.Point) {
		pointer := gesture.PointerEvent{
			EventType: gesture.PointerDown, PointerID: 1, PointerType: gesture.PointerTypeMouse,
			Button: event.ButtonLeft, Buttons: event.ButtonStateLeft, Position: pt, GlobalPosition: pt,
		}
		win.HandlePointerEvent(&pointer)
		pointer.EventType = gesture.PointerUp
		pointer.Buttons = 0
		win.HandlePointerEvent(&pointer)
	}
	height := func() float32 { return content.(interface{ Bounds() geometry.Rect }).Bounds().Height() }
	waitHistory := func() {
		t.Helper()
		timeout := time.After(10 * time.Second)
		for w.branchFilter != nil {
			select {
			case apply := <-w.done:
				apply()
			case <-timeout:
				t.Fatal("branch history did not finish loading")
			}
		}
	}
	checkText := func(canvas *uitest.MockCanvas, label string, visible bool) {
		t.Helper()
		if got := slices.ContainsFunc(canvas.Texts, func(call uitest.DrawTextCall) bool { return call.Text == label }); got != visible {
			t.Errorf("label %q visible = %v, want %v", label, got, visible)
		}
	}
	if draw(); len(leaves) != 3 || len(groups) != 3 || remote == nil {
		t.Fatalf("initial controls: %d checkboxes, %d groups, remote switch %v", len(leaves), len(groups), remote != nil)
	}
	fullHeight := height()
	for _, group := range []struct {
		name                             string
		index, hiddenRows, visibleLeaves int
	}{
		{"Local", 0, 1, 2},
		{"origin", 2, 1, 2},
		{"Remote", 1, 2, 2},
	} {
		click(groups[group.index].ScreenBounds().Center())
		draw()
		if len(leaves) != group.visibleLeaves {
			t.Errorf("collapse %s: got %d checkboxes, want %d", group.name, len(leaves), group.visibleLeaves)
		}
		if want := fullHeight - branchRowHeight*float32(group.hiddenRows); height() != want {
			t.Errorf("collapse %s: height %v, want %v", group.name, height(), want)
		}
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))
		open()
		if draw(); len(leaves) != group.visibleLeaves || height() != fullHeight-branchRowHeight*float32(group.hiddenRows) {
			t.Fatalf("reopen with %s collapsed: got %d checkboxes and height %v", group.name, len(leaves), height())
		}
		click(groups[group.index].ScreenBounds().Center())
		if draw(); len(leaves) != 3 || height() != fullHeight {
			t.Fatalf("expand %s: got %d checkboxes and height %v", group.name, len(leaves), height())
		}
	}
	click(leaves[1].ScreenBounds().Center())
	draw()
	click(leaves[2].ScreenBounds().Center())
	waitHistory()
	draw()
	if !slices.Equal(w.filter.Refs, w.data.Branches) || win.OverlayCount() != 1 || len(w.rows) != 2 || w.dirty {
		t.Fatalf("selected %v, overlays %d, history rows %d", w.filter.Refs, win.OverlayCount(), len(w.rows))
	}
	localBranch := leaves[1]
	click(remote.ScreenBounds().Center())
	out := draw()
	checkText(out, "main", true)
	checkText(out, "topic", false)
	waitHistory()
	if draw(); w.filter.Remote || len(leaves) != 2 || leaves[1] != localBranch || remote == nil || remote.IsChecked() || len(w.rows) != 1 {
		t.Fatalf("remote off: filter %+v, checkboxes %d, history rows %d", w.filter, len(leaves), len(w.rows))
	}
	click(remote.ScreenBounds().Center())
	out = draw()
	checkText(out, "main", true)
	checkText(out, "topic", true)
	waitHistory()
	if draw(); !w.filter.Remote || len(leaves) != 3 || leaves[1] != localBranch || !slices.Equal(w.filter.Refs, w.data.Branches) || len(w.rows) != 2 {
		t.Fatalf("remote on: filter %+v, checkboxes %d, history rows %d", w.filter, len(leaves), len(w.rows))
	}

	win.Context().RequestFocus(field)
	for _, char := range "missing" {
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyUnknown, char, event.ModNone))
		win.HandleEvent(event.NewKeyEvent(event.KeyRelease, event.KeyUnknown, char, event.ModNone))
	}
	out = draw()
	if len(leaves) != 0 || remote == nil || win.Context().FocusedWidget() != field {
		t.Fatalf("empty search: %d checkboxes, remote switch %v, focus %T", len(leaves), remote != nil, win.Context().FocusedWidget())
	}
	if !slices.ContainsFunc(out.Texts, func(call uitest.DrawTextCall) bool { return strings.Contains(call.Text, "No items") }) {
		t.Fatal("empty search did not display No items")
	}
	if want := fullHeight - 4*branchRowHeight; height() != want {
		t.Errorf("empty search: height %v, want %v", height(), want)
	}

	w.closeToolbarPopup()
	for i := range 20 {
		w.data.Branches = append(w.data.Branches, fmt.Sprintf("refs/heads/feature-%d", i))
	}
	open()
	draw()
	if got := viewport.Bounds().Height(); got != branchViewportMaxHeight {
		t.Fatalf("large tree viewport = %v, want %v", got, branchViewportMaxHeight)
	}
	pt := viewport.ScreenBounds().Center()
	win.HandleEvent(event.NewWheelEvent(geometry.Pt(0, 3), pt, pt, event.ModNone))
	if _, y := viewport.ScrollOffset(); y <= 0 {
		t.Fatal("large tree did not scroll")
	}
	win.HandleEvent(event.NewWheelEvent(geometry.Pt(0, -3), pt, pt, event.ModNone))
	draw()
	click(groups[0].ScreenBounds().Center())
	if draw(); height() != fullHeight-branchRowHeight {
		t.Fatalf("collapsed large tree height = %v, want %v", height(), fullHeight-branchRowHeight)
	}
}
