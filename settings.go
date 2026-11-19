package main

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/core/dropdown"
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

type viewSettings struct {
	InitialCommits     int
	Order              string
	HideRemotes        bool
	HideTags           bool
	HideStashes        bool
	HideUncommitted    bool
	DisableAutoRefresh bool
	DateFormat         string
	CommitDate         bool
	UnifiedDetails     bool
	ShowCommitter      bool
	DisablePrune       bool
	ImportedTheme      *importedTheme `json:",omitempty"`
	UseImportedTheme   bool
}

func (s viewSettings) historyFilter() repo.Filter {
	count := s.InitialCommits
	if count <= 0 || count > 50_000 {
		count = 300
	}
	return repo.Filter{Limit: count, Remote: !s.HideRemotes, Order: s.Order, HideTags: s.HideTags, HideStashes: s.HideStashes}
}

func (w *workbench) showSettings() {
	if w.busy {
		return
	}
	w.closeToolbarPopup()
	if w.path == "" {
		w.settingsDialog(repo.Identity{})
		return
	}
	path := w.path
	w.job("Read repository user", func(ctx context.Context) (func(), error) {
		identity, err := repo.ReadIdentity(ctx, path)
		return func() { w.settingsDialog(identity) }, err
	})
}

func (w *workbench) settingsDialog(identity repo.Identity) {
	next := w.prefs.Settings
	light := w.prefs.Light
	ctx := w.ui.Window().Context()
	previousFocus := ctx.FocusedWidget()
	var window *dialog.Widget
	var textLabels, mutedLabels []*primitives.TextWidget
	text := func(value string) *primitives.TextWidget {
		control := w.text(value)
		textLabels = append(textLabels, control)
		return control
	}
	muted := func(value string) *primitives.TextWidget {
		control := w.muted(value)
		mutedLabels = append(mutedLabels, control)
		return control
	}
	separator := primitives.Box().Height(1).Background(w.theme.Colors.Border)
	apply := func() {
		if w.applySettings(next, light) {
			colors := w.theme.Colors
			for _, control := range textLabels {
				control.Color(colors.OnSurface)
			}
			for _, control := range mutedLabels {
				control.Color(colors.OnSurfaceSecondary)
			}
			separator.Background(colors.Border)
			for _, overlay := range w.ui.Window().OverlayContentWidgets() {
				widget.MarkRedrawInTree(overlay)
			}
			ctx.Invalidate()
		}
	}

	label := func(title string) widget.Widget {
		return primitives.VBox(text(title).MaxLines(1)).Width(160).CrossAlign(primitives.CrossAxisStretch)
	}
	chosen := func(title string, labels []string, index int, change func(int)) widget.Widget {
		picker := dropdown.New(dropdown.Items(labels...), dropdown.Placeholder(title), dropdown.Selected(max(0, index)),
			dropdown.MaxVisibleItems(12), dropdown.PainterOpt(w.paint.Dropdown), dropdown.OnChange(func(i int, _ string) {
				change(i)
				apply()
			}))
		width := float32(72)
		for _, option := range labels {
			width = max(width, float32(len(option))*7+44)
		}
		return row(label(title), primitives.VBox(picker).Width(width).Height(28))
	}
	section := func(name string, children ...widget.Widget) widget.Widget {
		return primitives.VBox(append([]widget.Widget{muted(strings.ToUpper(name)).FontSize(10).Bold()}, children...)...).
			Gap(4).CrossAlign(primitives.CrossAxisStretch)
	}
	hint := func(value string) *primitives.TextWidget { return muted(value).FontSize(11) }
	check := func(title, description string, checked bool, change func(bool)) widget.Widget {
		control := w.check(title, checked, true, func(on bool) {
			change(on)
			apply()
		})
		if description == "" {
			return control
		}
		return primitives.VBox(control, primitives.VBox(hint(description)).PaddingLeft(20).
			CrossAlign(primitives.CrossAxisStretch)).CrossAlign(primitives.CrossAxisStretch)
	}

	order := []string{"date", "author-date", "topo"}
	counts := []string{"100", "300", "500", "1000", "2000", "5000"}
	currentCount := strconv.Itoa(next.historyFilter().Limit)
	if !slices.Contains(counts, currentCount) {
		counts = append(counts, currentCount)
	}
	history := section("History",
		chosen("Commit ordering", []string{"Date order", "Author date order", "Topological order"}, slices.Index(order, next.Order), func(i int) { next.Order = order[i] }),
		chosen("Initial commit count", counts, slices.Index(counts, currentCount), func(i int) { next.InitialCommits, _ = strconv.Atoi(counts[i]) }),
		check("Show remote branches", "", !next.HideRemotes, func(on bool) { next.HideRemotes = !on }),
		check("Show tags", "", !next.HideTags, func(on bool) { next.HideTags = !on }),
		check("Show stashes", "", !next.HideStashes, func(on bool) { next.HideStashes = !on }),
		check("Show uncommitted changes", "Display a working tree row at the top of history.", !next.HideUncommitted, func(on bool) { next.HideUncommitted = !on }),
		check("Auto refresh", "Reload history when the repository changes on disk.", !next.DisableAutoRefresh, func(on bool) { next.DisableAutoRefresh = !on }),
	)
	dateFormats := []string{"datetime", "date", "relative"}
	diff := dropdown.New(dropdown.Items("Unified", "Split"), dropdown.Selected(0), dropdown.Disabled(true), dropdown.PainterOpt(w.paint.Dropdown))
	display := section("Display",
		chosen("Date format", []string{"Date & time", "Date only", "Relative"}, slices.Index(dateFormats, next.DateFormat), func(i int) { next.DateFormat = dateFormats[i] }),
		chosen("Date type", []string{"Author date", "Commit date"}, boolIndex(next.CommitDate), func(i int) { next.CommitDate = i == 1 }),
		chosen("Commit view", []string{"Split", "Unified"}, boolIndex(next.UnifiedDetails), func(i int) { next.UnifiedDetails = i == 1 }),
		row(label("Diff view"), primitives.VBox(diff).Width(110).Height(28), hint("Unavailable")),
		check("Show committer", "Show the committer next to the author when they differ.", next.ShowCommitter, func(on bool) { next.ShowCommitter = on }),
	)

	themeItems := func() []string {
		items := []string{"DevTools", "DevTools Dark"}
		if next.ImportedTheme != nil {
			items = append(items, next.ImportedTheme.Name+" (Imported)")
		}
		return items
	}
	index := boolIndex(!light)
	if next.UseImportedTheme && next.ImportedTheme != nil {
		index = 2
	}
	themes := state.NewSignal(themeItems())
	selectedTheme := state.NewSignal(index)
	picker := dropdown.New(dropdown.ItemsSignal(themes), dropdown.SelectedSignal(selectedTheme),
		dropdown.PainterOpt(w.paint.Dropdown), dropdown.OnChange(func(i int, _ string) {
			next.UseImportedTheme = i == 2
			if i != 2 {
				light = i == 0
			}
			apply()
		}))
	importTheme := w.btn("Import…", button.Outlined, true, func() {
		paths, err := w.host.ShowOpenFileDialog(gogpu.FileDialogOptions{Title: "Import VS Code theme",
			Filters: []gogpu.FileTypeFilter{{Name: "VS Code themes", Extensions: []string{"json", "jsonc"}}}})
		if err != nil {
			w.showError(err)
		} else if len(paths) > 0 {
			theme, err := readTheme(paths[0])
			if err != nil {
				w.showError(err)
				return
			}
			next.ImportedTheme, next.UseImportedTheme = theme, true
			themes.Set(themeItems())
			picker.SetSelectedIndex(2)
			apply()
		}
	})
	theme := section("Theme",
		row(label("Colour theme"), primitives.Expanded(primitives.VBox(picker).Height(28).CrossAlign(primitives.CrossAxisStretch)), importTheme),
		hint("Import workbench colors from a VS Code JSON or JSONC theme."),
	)
	remote := section("Remote",
		check("Prune when fetching", "Remove remote-tracking references that no longer exist.", !next.DisablePrune, func(on bool) { next.DisablePrune = !on }),
	)
	user := state.NewSignal(identityLabel(identity))
	repository := section("Repository",
		row(label("User"), primitives.Expanded(muted("").ContentSignal(user).MaxLines(1).Ellipsis()),
			w.btn("Edit…", button.Outlined, w.path != "", func() {
				w.editIdentity(identity, func(saved repo.Identity) {
					identity = saved
					user.Set(identityLabel(saved))
				})
			})),
		row(w.btn("Check GitHub sign-in", button.TextOnly, true, w.checkGitHub)),
	)
	content := primitives.VBox(history, display, theme, remote, repository).Gap(12).PaddingXY(16, 12).
		CrossAlign(primitives.CrossAxisStretch)
	closeButton := w.btn("×", button.TextOnly, true, func() { window.Close(ctx) })
	header := row(primitives.Expanded(text("Settings").Bold().FontSize(15)), closeButton).Height(43).PaddingXY(12, 4)
	body := scrollview.New(content, scrollview.PainterOpt(w.paint.Scrollbar))
	window = dialog.New(dialog.ContentOnly(), dialog.Content(primitives.VBox(header, separator, body).CrossAlign(primitives.CrossAxisStretch)),
		dialog.MaxWidth(550), dialog.MaxHeight(720), dialog.PainterOpt(w.paint.Dialog), dialog.OnClose(func() {
			if previousFocus != nil && !widget.IsDetached(previousFocus) {
				ctx.RequestFocus(previousFocus)
			}
		}))
	window.Show(ctx)
	ctx.RequestFocus(closeButton)
}

func boolIndex(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (w *workbench) refreshSettingsHistory() {
	if w.settingsRefresh && !w.busy {
		w.settingsRefresh = false
		w.refresh(w.filter)
	}
}

func (w *workbench) applySettings(next viewSettings, light bool) (themeChanged bool) {
	prev := w.prefs.Settings
	if prev == next && w.prefs.Light == light {
		return false
	}
	themeChanged = w.prefs.Light != light || prev.UseImportedTheme != next.UseImportedTheme ||
		(next.UseImportedTheme && prev.ImportedTheme != next.ImportedTheme)
	w.prefs.Light, w.prefs.Settings = light, next
	w.prefs.save(w.settingsPath)
	w.repoVersion++
	w.scheduleAutoRefresh()
	if themeChanged {
		wasDark := w.theme.IsDark()
		w.setTheme()
		w.ui.SetTheme(w.theme.AsTheme())
		if wasDark != w.theme.IsDark() {
			w.refreshPreview()
		}
	}
	if themeChanged || prev.ShowCommitter != next.ShowCommitter {
		w.table = nil
	}

	want := next.historyFilter()
	f := &w.filter
	countChanged := prev.historyFilter().Limit != want.Limit
	historyChanged := countChanged || f.Remote != want.Remote || f.Order != want.Order ||
		f.HideTags != want.HideTags || f.HideStashes != want.HideStashes
	f.Remote, f.Order, f.HideTags, f.HideStashes = want.Remote, want.Order, want.HideTags, want.HideStashes
	if countChanged {
		f.Limit = want.Limit
	}
	w.settingsRefresh = w.settingsRefresh || (w.path != "" && historyChanged)
	w.filterRows()
	w.redraw()
	return themeChanged
}

func identityLabel(identity repo.Identity) string {
	if identity.Name == "" && identity.Email == "" {
		return "Not set"
	}
	if identity.Email == "" {
		return identity.Name
	}
	return strings.TrimSpace(identity.Name + "  <" + identity.Email + ">")
}

func (w *workbench) editIdentity(identity repo.Identity, onSave func(repo.Identity)) {
	if w.busy || w.path == "" {
		return
	}
	ctx := w.ui.Window().Context()
	previousFocus := ctx.FocusedWidget()
	var editor *dialog.Widget
	name := w.input("Name", identity.Name, nil, nil)
	email := w.input("Email", identity.Email, nil, nil)
	message := state.NewSignal("")
	save := func() {
		saved := repo.Identity{Name: strings.TrimSpace(name.Text()), Email: strings.TrimSpace(email.Text())}
		if saved.Name == "" || saved.Email == "" || strings.ContainsAny(saved.Name+saved.Email, "\r\n\x00") {
			message.Set("Enter a name and email.")
			return
		}
		editor.Close(ctx)
		path := w.path
		w.job("Save repository user", func(ctx context.Context) (func(), error) {
			err := repo.WriteIdentity(ctx, path, saved)
			return func() { onSave(saved) }, err
		})
	}
	panel := primitives.VBox(w.text("Repository user").Bold().FontSize(15),
		w.muted("Name"), compactInput(name), w.muted("Email"), compactInput(email),
		w.text("").ContentSignal(message).Color(w.theme.Colors.Error),
		row(primitives.Expanded(primitives.Box()), w.btn("Cancel", button.TextOnly, true, func() { editor.Close(ctx) }),
			w.btn("Save", button.Filled, true, save))).Gap(6).Padding(16)
	editor = dialog.New(dialog.ContentOnly(), dialog.Content(panel), dialog.MaxWidth(400), dialog.PainterOpt(w.paint.Dialog),
		dialog.OnClose(func() {
			if previousFocus != nil && !widget.IsDetached(previousFocus) {
				ctx.RequestFocus(previousFocus)
			}
		}))
	editor.Show(ctx)
	ctx.RequestFocus(name)
}

func (w *workbench) fetch() {
	args := []string{"fetch", "--all", "--prune"}
	if w.prefs.Settings.DisablePrune {
		args[2] = "--no-prune"
	}
	w.command("Fetch", "git", args...)
}
