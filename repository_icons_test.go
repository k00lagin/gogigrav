package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gogpu/ui/core/listview"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
)

const testRepoSVG = `<svg viewBox="0 0 40 20"><rect width="40" height="20" fill="#CC3333"/></svg>`

func TestRepositoryMonograms(t *testing.T) {
	for name, want := range map[string]string{"sier-anket": "SA", "_EqGwHw": "EG", "bs": "B", "mfc-plus": "MP", "мой-проект": "МП"} {
		if got := repositoryMonogram(name); got != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}
}

func putIconFile(t *testing.T, dir, path, data string) {
	t.Helper()
	path = filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryIconResolution(t *testing.T) {
	dir := t.TempDir()
	check := func(override, want string) {
		t.Helper()
		got := resolveRepositoryIcon(dir, override)
		if got.path != want || got.img == nil || got.img.Bounds().Dx() != 20 || got.img.Bounds().Dy() != 10 {
			t.Fatalf("resolved %q, want %q with a proportionate thumbnail", got.path, want)
		}
	}
	putIconFile(t, dir, "src/root.tsx", `links: [{rel: 'icon', href: '/brand.svg?version=2'}]`)
	putIconFile(t, dir, "public/brand.svg", testRepoSVG)
	check("", "public/brand.svg")
	putIconFile(t, dir, "assets/logo.svg", testRepoSVG)
	check("", "assets/logo.svg")
	putIconFile(t, dir, "t3.json", `{"iconPath":"public/brand.svg"}`)
	check("", "public/brand.svg")
	check("assets/logo.svg", "assets/logo.svg")
	check("removed.png", "public/brand.svg")
	putIconFile(t, dir, "t3.json", `{"iconPath":"../outside.svg"}`)
	putIconFile(t, dir, "favicon.ico", "broken icon")
	check("", "assets/logo.svg")
}

func TestChooseIconForInactiveRepository(t *testing.T) {
	active, other := t.TempDir(), t.TempDir()
	for _, path := range []string{active, other} {
		testGit(t, path, "init", "-b", "main")
	}
	putIconFile(t, other, "brand/logo.svg", testRepoSVG)
	putIconFile(t, other, "another.svg", testRepoSVG)
	putIconFile(t, other, ".gitignore", "ignored/\n")
	putIconFile(t, other, "ignored/hidden.svg", testRepoSVG)
	files, err := projectImageFiles(t.Context(), other)
	if err != nil || !slices.Equal(files, []string{"another.svg", "brand/logo.svg"}) {
		t.Fatalf("image index %q, error %v", files, err)
	}
	w := testWorkbench(t, active)
	w.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	w.prefs.Recent = []string{active, other}
	win := w.ui.Window()
	trigger := w.branchBar.ItemBounds(0).Translate(w.branchBar.ScreenOrigin()).Center()
	key := func(k event.Key) { win.HandleEvent(event.NewKeyEvent(event.KeyPress, k, 0, event.ModNone)) }
	paint := func() *uitest.MockCanvas {
		w.ui.Frame()
		canvas := &uitest.MockCanvas{}
		win.DrawOverlays(canvas)
		return canvas
	}
	findList := func() *listview.Widget {
		t.Helper()
		var list *listview.Widget
		for _, overlay := range win.OverlayContentWidgets() {
			walkWidgets(overlay, func(node widget.Widget) {
				if l, ok := node.(*listview.Widget); ok {
					list = l
				}
			})
		}
		if list == nil {
			t.Fatal("missing image or repository list")
		}
		return list
	}
	testClick(w, trigger)
	paint()
	repositories := findList().ScreenBounds()
	at := geometry.Pt(repositories.Center().X, repositories.Min.Y+63)
	win.HandleEvent(event.NewMouseEvent(event.MousePress, event.ButtonRight, event.ButtonStateRight, at, at, event.ModNone))
	win.HandleEvent(event.NewMouseEvent(event.MouseRelease, event.ButtonRight, 0, at, at, event.ModNone))
	key(event.KeyDown)
	key(event.KeyEnter)
	paint()
	input, ok := win.Context().FocusedWidget().(*textfield.Widget)
	if !ok || w.openPopup != nil {
		t.Fatal("repository settings did not focus the image search")
	}
	list := findList()
	waitRepositoryUI(t, w, func() bool { return list.GetItemCount() == 2 })
	for _, r := range "brand" {
		win.HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyUnknown, r, event.ModNone))
	}
	if input.Text() != "brand" || list.GetItemCount() != 1 {
		t.Fatalf("search %q returned %d files", input.Text(), list.GetItemCount())
	}
	key(event.KeyEnter)
	waitRepositoryUI(t, w, func() bool { return win.OverlayCount() == 0 })
	raw, err := os.ReadFile(w.settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved preferences
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.RepositoryIcons[repositoryKey(other)] != "brand/logo.svg" || len(saved.RepositoryIcons) != 1 || w.path != active {
		t.Fatalf("unexpected repository settings: %+v", saved.RepositoryIcons)
	}
	win.DrawTo(&uitest.MockCanvas{})
	trigger = w.branchBar.ItemBounds(0).Translate(w.branchBar.ScreenOrigin()).Center()
	testClick(w, trigger)
	if w.openPopup == nil {
		t.Fatalf("could not reopen repository menu at %v, popup icon %q, %d overlays", trigger, w.popupIcon, win.OverlayCount())
	}
	paint()
	recentList := findList()
	waitRepositoryUI(t, w, func() bool {
		canvas := &uitest.MockCanvas{}
		walkWidgets(recentList, func(node widget.Widget) {
			if badge, ok := node.(*repositoryBadge); ok && badge.thumbnail.path == "brand/logo.svg" {
				badge.Draw(win.Context(), canvas)
			}
		})
		return len(canvas.Images) == 1
	})
	key(event.KeyEscape)
	if err := w.setRepositoryIcon(other, "", iconThumbnail{}); err != nil || len(w.prefs.RepositoryIcons) != 0 {
		t.Fatalf("reset icon: %v", err)
	}
}

func TestToolbarShowsCurrentRepositoryIcon(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for _, path := range []string{first, second} {
		testGit(t, path, "init", "-b", "main")
	}
	putIconFile(t, first, "favicon.svg", testRepoSVG)
	putIconFile(t, first, "custom.svg", `<svg viewBox="0 0 12 12"><circle cx="6" cy="6" r="5"/></svg>`)
	w := testWorkbench(t, second)
	paint := func() *uitest.MockCanvas {
		canvas := &uitest.MockCanvas{}
		w.branchBar.Draw(w.ui.Window().Context(), canvas)
		return canvas
	}
	open := func(path string) {
		w.open(path)
		waitRepositoryUI(t, w, func() bool { return !w.busy })
		testActionFrame(w)
	}
	check := func(file string, height int) {
		t.Helper()
		canvas := paint()
		if w.repositoryIcon.path != file || len(canvas.Images) != 1 || canvas.Images[0].Image.Bounds().Dy() != height {
			t.Fatalf("toolbar image %q, want %q at height %d, drew %d images", w.repositoryIcon.path, file, height, len(canvas.Images))
		}
	}
	open(first)
	check("favicon.svg", 10)
	w.repositorySettings(first)
	for _, r := range "custom" {
		w.ui.Window().HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyUnknown, r, event.ModNone))
	}
	waitRepositoryUI(t, w, func() bool {
		for _, overlay := range w.ui.Window().OverlayContentWidgets() {
			found := false
			walkWidgets(overlay, func(node widget.Widget) {
				if list, ok := node.(*listview.Widget); ok {
					found = list.GetItemCount() == 1
				}
			})
			if found {
				return true
			}
		}
		return false
	})
	w.ui.Window().HandleEvent(event.NewKeyEvent(event.KeyPress, event.KeyEnter, 0, event.ModNone))
	waitRepositoryUI(t, w, func() bool { return w.ui.Window().OverlayCount() == 0 })
	check("custom.svg", 20)
	open(second)
	canvas := paint()
	if len(canvas.Images) != 0 || !slices.ContainsFunc(canvas.Texts, func(call uitest.DrawTextCall) bool { return call.Text == repositoryMonogram(filepath.Base(second)) }) {
		t.Fatal("expected the second repository's monogram")
	}
	open(first)
	check("custom.svg", 20)
}
