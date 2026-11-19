package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/checkbox"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := repo.Run(t.Context(), dir, "git", args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(out)
}

func testWorkbench(t *testing.T, dir string) *workbench {
	t.Helper()
	filter := repo.Filter{Remote: true, Limit: 250}
	data, err := repo.Load(t.Context(), dir, filter)
	if err != nil {
		t.Fatal(err)
	}
	w := &workbench{
		ctx: t.Context(), done: make(chan func(), 1), path: dir, page: "changes", ratio: 0.65,
		host: gogpu.NewApp(gogpu.DefaultConfig()), ui: app.New(), selected: state.NewSignal(0), filter: filter, data: data,
	}
	w.setTheme()
	w.filterRows()
	w.ui.Window().HandleResize(1080, 520)
	testActionFrame(w)
	return w
}

func testActionFrame(w *workbench) {
	w.render()
	w.ui.Frame()
	w.ui.Window().DrawTo(&uitest.MockCanvas{})
}

func testClick(w *workbench, p geometry.Point) {
	win := w.ui.Window()
	win.HandleEvent(event.NewMouseEvent(event.MousePress, event.ButtonLeft, event.ButtonStateLeft, p, p, event.ModNone))
	win.HandleEvent(event.NewMouseEvent(event.MouseRelease, event.ButtonLeft, 0, p, p, event.ModNone))
}

func testFooterButton(t *testing.T, w *workbench, index int) geometry.Rect {
	t.Helper()
	root := w.ui.Window().Root().Children()
	for _, item := range root[len(root)-1].Children() {
		if btn, ok := item.(*button.Widget); ok {
			if index == 0 {
				bounds := btn.ScreenBounds()
				testClick(w, bounds.Center())
				return bounds
			}
			index--
		}
	}
	t.Fatal("status bar button not found")
	return geometry.Rect{}
}

func testToolbarAction(t *testing.T, w *workbench, glyph string) {
	t.Helper()
	var find func(widget.Widget) bool
	find = func(node widget.Widget) bool {
		if bar, ok := node.(*toolbarHints); ok {
			for i := range bar.ItemCount() {
				if bar.ItemAt(i).Icon.Name == glyph {
					testClick(w, bar.ItemBounds(i).Translate(bar.ScreenOrigin()).Center())
					return true
				}
			}
		}
		for _, child := range node.Children() {
			if find(child) {
				return true
			}
		}
		return false
	}
	if !find(w.titleBar) {
		t.Fatalf("toolbar action %q not found", glyph)
	}
}

func testPopupSelection(t *testing.T, w *workbench, query string) {
	t.Helper()
	win := w.ui.Window()
	if _, ok := win.Context().FocusedWidget().(*textfield.Widget); !ok {
		t.Fatal("popup search is not focused")
	}
	for _, char := range query {
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyUnknown, char, event.ModNone))
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	testApplyPopupAction(t, w)
}

func testApplyPopupAction(t *testing.T, w *workbench) {
	t.Helper()
	action := w.popupAction
	if action == nil {
		t.Fatal("popup did not select an action")
	}
	w.popupAction = nil
	action()
}

func testWaitForGit(t *testing.T, w *workbench) {
	t.Helper()
	if !w.busy {
		t.Fatal("action did not start a Git job")
	}
	select {
	case apply := <-w.done:
		apply()
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for Git")
	}
	if w.status != "" {
		t.Fatal(w.status)
	}
	testActionFrame(w)
}

func TestSyncOptionsOpenBesideTheirToolbarTriggers(t *testing.T) {
	path := t.TempDir()
	testGit(t, path, "init", "-b", "main")
	testGit(t, path, "-c", "user.name=Tester", "-c", "user.email=tester@example.test",
		"-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "First")
	testGit(t, path, "remote", "add", "origin", t.TempDir())
	w := testWorkbench(t, path)
	win := w.ui.Window()
	root := win.Root()

	for _, label := range []string{"Pull options", "Push options", "Pull", "Push"} {
		var anchor geometry.Rect
		walkWidgets(w.titleBar, func(node widget.Widget) {
			if bar, ok := node.(*toolbarHints); ok {
				for i := range bar.ItemCount() {
					if bar.ItemAt(i).Label == label {
						anchor = bar.ItemBounds(i).Translate(bar.ScreenOrigin())
					}
				}
			}
		})
		if anchor.IsEmpty() {
			t.Fatalf("missing %s trigger", label)
		}
		testClick(w, anchor.Center())
		w.ui.Frame()
		win.DrawOverlays(&uitest.MockCanvas{})
		controls := win.OverlayContentWidgets()
		if len(controls) != 1 || w.openPopup == nil || w.page != "changes" || win.Root() != root {
			t.Fatalf("%s should open a popup over the current page", label)
		}
		bounds := controls[0].(interface{ Bounds() geometry.Rect }).Bounds()
		if bounds.Min.Y < anchor.Max.Y || bounds.Min.Y > anchor.Max.Y+16 ||
			bounds.Max.X != anchor.Max.X {
			t.Fatalf("%s popup %v is not aligned to the right edge of trigger %v", label, bounds, anchor)
		}
		field, ok := win.Context().FocusedWidget().(*textfield.Widget)
		if !ok || field.Text() != "main" {
			t.Fatalf("%s popup should focus the target branch with the current branch default", label)
		}
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))
		if win.HasOverlays() || w.openPopup != nil || w.busy {
			t.Fatalf("Escape did not dismiss %s", label)
		}
	}
}

func TestStatusBranchAndSyncWithDifferentlyNamedUpstream(t *testing.T) {
	base, remote := t.TempDir(), t.TempDir()
	testGit(t, remote, "init", "--bare", "-b", "deployment")
	testGit(t, base, "init", "-b", "main")
	testGit(t, base, "config", "user.name", "Tester")
	testGit(t, base, "config", "user.email", "tester@example.test")
	testGit(t, base, "config", "commit.gpgsign", "false")
	testGit(t, base, "config", "push.default", "simple")
	testGit(t, base, "commit", "--allow-empty", "-m", "Root")
	testGit(t, base, "remote", "add", "origin", remote)
	testGit(t, base, "push", "-u", "origin", "main:deployment")
	testGit(t, base, "branch", "feature/topic")
	w := testWorkbench(t, base)

	branchButton := testFooterButton(t, w, 0)
	overlay := w.ui.Window().OverlayContentWidgets()
	if w.popupIcon != "checkout-branch" || len(overlay) != 1 ||
		overlay[0].(interface{ Bounds() geometry.Rect }).Bounds().Max.Y >= branchButton.Min.Y {
		t.Fatal("branch selector should open above the status bar")
	}
	testPopupSelection(t, w, "feature/topic")
	testWaitForGit(t, w)
	if w.data.CurrentBranch() != "feature/topic" {
		t.Fatalf("current branch = %s", w.data.Branch)
	}

	submitOptions := func(glyph, target string, toggleFirstFlag bool) {
		t.Helper()
		testToolbarAction(t, w, glyph)
		win := w.ui.Window()
		w.ui.Frame()
		win.DrawOverlays(&uitest.MockCanvas{})
		overlays := win.OverlayContentWidgets()
		field, focused := win.Context().FocusedWidget().(*textfield.Widget)
		if w.openPopup == nil || len(overlays) != 1 || !focused {
			t.Fatalf("%s without an upstream should open its options popup", glyph)
		}
		if toggleFirstFlag {
			var flag *checkbox.Widget
			walkWidgets(overlays[0], func(node widget.Widget) {
				if box, ok := node.(*checkbox.Widget); ok && flag == nil {
					flag = box
				}
			})
			if flag == nil {
				t.Fatalf("%s options did not display a checkbox", glyph)
			}
			testClick(w, flag.ScreenBounds().Center())
			win.Context().RequestFocus(field)
		}
		field.SetText(target)
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
		testApplyPopupAction(t, w)
		if win.HasOverlays() {
			t.Fatalf("%s did not dismiss the popup on submit", glyph)
		}
		testWaitForGit(t, w)
	}
	submitOptions(pullIcon.Name, "deployment", true)
	submitOptions(pushIcon.Name, "preview", false)
	if w.data.UpstreamRemote != "origin" || w.data.UpstreamRef != "refs/heads/preview" {
		t.Fatalf("unexpected upstream: %+v", w.data)
	}
	testGit(t, base, "commit", "--allow-empty", "-m", "Pushed from Gogigrav")
	testToolbarAction(t, w, pushIcon.Name)
	testWaitForGit(t, w)
	if got := testGit(t, remote, "rev-parse", "refs/heads/preview"); got != w.data.Head {
		t.Fatalf("push to preview = %s, want %s", got, w.data.Head)
	}

	other := t.TempDir()
	testGit(t, other, "clone", "--branch", "preview", remote, ".")
	testGit(t, other, "-c", "user.name=Tester", "-c", "user.email=tester@example.test",
		"-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "Pulled into Gogigrav")
	testGit(t, other, "push", "origin", "preview")
	testFooterButton(t, w, 2)
	testWaitForGit(t, w)
	if got := testGit(t, other, "rev-parse", "HEAD"); w.data.Head != got {
		t.Fatalf("pulled HEAD = %s, want %s", w.data.Head, got)
	}
}

func TestStashesMenuActions(t *testing.T) {
	dir := t.TempDir()
	testGit(t, dir, "init", "-b", "main")
	testGit(t, dir, "config", "user.name", "Tester")
	testGit(t, dir, "config", "user.email", "tester@example.test")
	testGit(t, dir, "config", "commit.gpgsign", "false")
	testGit(t, dir, "commit", "--allow-empty", "-m", "Root")
	file := filepath.Join(dir, "pending.txt")
	if err := os.WriteFile(file, []byte("work in progress"), 0600); err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "add", "--", "pending.txt")
	w := testWorkbench(t, dir)
	testToolbarAction(t, w, stashIcon.Name)
	win := w.ui.Window()
	canvas := &uitest.MockCanvas{}
	win.DrawOverlays(canvas)
	children := win.OverlayContentWidgets()[0].Children()[0].Children()
	testClick(w, children[1].(*button.Widget).ScreenBounds().Center())
	testApplyPopupAction(t, w)
	w.form.fields[0].value = "A pending change"
	if err := w.form.actions[0].run(); err != nil {
		t.Fatal(err)
	}
	testWaitForGit(t, w)
	if len(w.data.Stashes) != 1 || w.data.Stashes[0].Hash == "" || len(w.data.Files) != 0 {
		t.Fatalf("stash was not saved: %+v", w.data.Stashes)
	}

	testFooterButton(t, w, 1)
	testPopupSelection(t, w, "pending")
	if w.form == nil || w.form.title != "stash@{0}" || !strings.Contains(w.form.description, "A pending change") {
		t.Fatal("stash details did not open")
	}
	if err := w.form.actions[0].run(); err != nil {
		t.Fatal(err)
	}
	testWaitForGit(t, w)
	if len(w.data.Stashes) != 1 || len(w.data.Files) != 1 {
		t.Fatal("apply should restore the file and keep the stash")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	testFooterButton(t, w, 1)
	testPopupSelection(t, w, "pending")
	w.form.fields[0].checked = true
	if err := w.form.actions[1].run(); err != nil {
		t.Fatal(err)
	}
	testWaitForGit(t, w)
	if len(w.data.Stashes) != 0 || len(w.data.Files) != 1 || !w.data.Files[0].Staged() {
		t.Fatalf("pop should remove the stash and restore staging: %+v", w.data)
	}
	testGit(t, dir, "stash", "push", "-m", "Branch from this change")
	w.refresh(w.filter)
	testWaitForGit(t, w)
	testFooterButton(t, w, 1)
	testPopupSelection(t, w, "Branch from")
	if err := w.form.actions[2].run(); err != nil {
		t.Fatal(err)
	}
	w.form.fields[0].value = "restored"
	if err := w.form.actions[0].run(); err != nil {
		t.Fatal(err)
	}
	testWaitForGit(t, w)
	if w.data.CurrentBranch() != "restored" || len(w.data.Stashes) != 0 || len(w.data.Files) != 1 {
		t.Fatalf("stash branch should switch to restored, restore the file and consume the stash: %+v", w.data)
	}

	testGit(t, dir, "stash", "push", "-m", "Drop this change")
	w.refresh(w.filter)
	testWaitForGit(t, w)
	testFooterButton(t, w, 1)
	testPopupSelection(t, w, "Drop this")
	drop := w.form.actions[3].run
	if err := drop(); err != nil {
		t.Fatal(err)
	}
	if win.OverlayCount() != 1 || w.busy {
		t.Fatal("drop should wait for confirmation")
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	if w.busy || len(w.data.Stashes) != 1 {
		t.Fatal("the default confirmation action should cancel")
	}
	if err := drop(); err != nil {
		t.Fatal(err)
	}
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyTab, 0, event.ModNone))
	win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	testWaitForGit(t, w)
	if len(w.data.Stashes) != 0 {
		t.Fatalf("confirmed drop should remove the stash: %+v", w.data.Stashes)
	}
}
