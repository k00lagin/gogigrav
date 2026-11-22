package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "github.com/gogpu/gg/gpu"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/core/datatable"
	"github.com/gogpu/ui/core/popover"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/desktop"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/focus"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/overlay"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/theme/devtools"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

type preferences struct {
	Light            bool
	Recent           []string
	RepositoryVisits map[string]repositoryVisit `json:",omitempty"`
	RepositoryIcons  map[string]string          `json:",omitempty"`
	Settings         viewSettings
	Columns          columnLayout
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "gogigrav", "settings.json")
}
func (p preferences) save(path string) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		log.Print(err)
		return
	}
	if err := writePreferences(path, p); err != nil {
		log.Print(err)
	}
}

func writePreferences(path string, p preferences) error {
	f, err := os.CreateTemp(filepath.Dir(path), "settings-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	writeErr := encoder.Encode(p)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

type workbench struct {
	host           *gogpu.App
	ui             *app.App
	theme          *devtools.Theme
	paint          devtools.Painters
	prefs          preferences
	ctx            context.Context
	done           chan func()
	busy, dirty    bool
	status         string
	path           string
	repositoryIcon iconThumbnail
	data           repo.Snapshot
	filter         repo.Filter
	page           string
	ratio          float32
	table          *datatable.Widget
	searchField    *textfield.Widget
	titleBar       *workbenchTitleBar
	openPopup      *popover.Popover
	popupIcon      string
	popupAnchor    func() geometry.Rect
	popupLayer     overlay.Overlay
	popupAction    func()
	searchOpen     bool
	focusSearch    bool
	searchMatches  []int
	searchIndex    int
	searchSummary  state.Signal[string]
	searchDisabled state.Signal[bool]
	rows           []repo.Commit
	graph          historyGraph
	graphPalette   [graphColorCount]widget.Color
	search         string
	selected       state.Signal[int]
	commit         repo.Commit
	compareBase    string
	compareTip     string
	changed        []repo.File
	file           *fileItem
	viewer         *filePreview
	drafts         map[string]string
	amend          bool

	historyHeader    *historyHeader
	repoSearchCancel context.CancelFunc

	previewWrap, previewSidebar bool

	inlineClosed  bool
	inlineContent widget.Widget
	inlineGraph   *historyGraphGutter
	focusHistory  bool
	revealInline  bool

	collapsedBranchGroups map[branchGroupKey]bool

	branchFilter     *repo.Filter
	branchBar        *toolbarHints
	toolbarHover     toolbarHover
	loadMoreDisabled state.Signal[bool]
	statusText       state.Signal[string]

	settingsPath     string
	settingsRefresh  bool
	repoVersion      uint64
	autoRefreshTick  chan struct{}
	autoRefreshTimer *time.Timer
	autoChecking     bool
}

func main() {
	runSetup := installationCLI()
	themeName := flag.String("theme", "", "DevTools or DevTools Dark")
	flag.Parse()
	if runSetup() {
		return
	}
	settingsPath := configPath()
	var prefs preferences
	if data, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(data, &prefs)
	}
	if *themeName != "" {
		prefs.Settings.UseImportedTheme = false
		switch strings.ToLower(*themeName) {
		case "devtools", "light":
			prefs.Light = true
		case "devtools dark", "dark":
			prefs.Light = false
		default:
			log.Fatal("unknown theme: ", *themeName)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &workbench{ctx: ctx, prefs: prefs, settingsPath: settingsPath, autoRefreshTick: make(chan struct{}, 1),
		done: make(chan func(), 1), dirty: true, page: "open", ratio: 0.65, drafts: map[string]string{}, selected: state.NewSignal(-1)}
	w.filter = prefs.Settings.historyFilter()
	w.setTheme()
	w.host = gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle("Gogigrav").WithSize(1280, 760).WithMinSize(1080, 520).WithFrameless(true))
	w.ui = app.New(app.WithWindowProvider(w.host), app.WithPlatformProvider(w.host),
		app.WithEventSource(w.overlayEventSource(w.host.EventSource())), app.WithTheme(w.theme.AsTheme()),
		app.WithTextRasterizer(newPlatformTextRasterizer()))
	w.render()
	w.host.SetHitTestCallback(w.hitTestWindow)
	shortcuts := w.ui.Window().FocusManager()
	shortcuts.RegisterShortcut(focus.Shortcut{Key: event.KeyF, Ctrl: true}, w.showSearch)
	shortcuts.RegisterShortcut(focus.Shortcut{Key: event.KeyO, Ctrl: true}, w.showRepositoryPicker)
	shortcuts.RegisterShortcut(focus.Shortcut{Key: event.KeyF5}, func() {
		if w.path != "" && !w.busy {
			w.refresh(w.filter)
		}
	})
	w.host.OnUpdate(func(_ float64) {
		if action := w.popupAction; action != nil {
			w.popupAction = nil
			action()
		}
		select {
		case apply := <-w.done:
			apply()
		default:
		}
		w.refreshSettingsHistory()
		select {
		case <-w.autoRefreshTick:
			w.checkAutoRefresh()
		default:
		}
		if w.dirty {
			w.render()
		}
		w.updateToolbarTooltip(time.Now())
	})
	paths := flag.Args()
	if len(paths) == 0 {
		cwd, err := os.Getwd()
		if err == nil {
			w.drafts["openPath"] = cwd
			if root, err := repo.Root(ctx, cwd); err == nil {
				w.openRepository(root, false)
			} else if len(w.prefs.Recent) > 0 {
				w.openRepository(w.prefs.Recent[0], false)
			}
		}
	}
	if len(paths) > 0 {
		w.discover(paths)
	}
	unhook := w.watchCaptionClicks()
	err := desktop.Run(w.host, w.ui)
	unhook()
	if err != nil {
		log.Fatal(err)
	}
}

func (w *workbench) setTheme() {
	var theme *devtools.Theme
	if w.prefs.Light {
		theme = devtools.NewTheme()
	} else {
		theme = devtools.NewDarkTheme()
	}
	if settings := w.prefs.Settings; settings.UseImportedTheme && settings.ImportedTheme != nil {
		theme = settings.ImportedTheme.theme(theme)
	}
	if w.theme == nil {
		w.theme = theme
	} else {
		// Open overlays retain painters that share this theme pointer.
		*w.theme = *theme
	}
	w.paint = devtools.NewPainters(w.theme)
	w.setGraphPalette()
}

func (w *workbench) redraw() { w.dirty = true; w.host.RequestRedraw() }
func (w *workbench) show(page string) {
	if w.busy {
		return
	}
	w.page = page
	w.closePreview()
	w.inlineClosed = false
	w.redraw()
}

// The event loop owns the model and widgets. Workers return a completion to it.
func (w *workbench) job(label string, work func(context.Context) (func(), error)) {
	w.jobWithError(label, work, w.showError)
}

func (w *workbench) jobWithError(label string, work func(context.Context) (func(), error), onError func(error)) {
	if w.busy {
		return
	}
	w.busy = true
	w.repoVersion++
	w.status = label + "…"
	w.redraw()
	w.runJob(work, func(apply func(), err error) {
		w.busy = false
		w.status = ""
		if err != nil {
			w.status = err.Error()
			onError(err)
		} else if apply != nil {
			apply()
		}
		w.redraw()
	})
}

func (w *workbench) runJob(work func(context.Context) (func(), error), finish func(func(), error)) {
	go func() {
		ctx, cancel := context.WithTimeout(w.ctx, 5*time.Minute)
		defer cancel()
		apply, err := work(ctx)
		select {
		case w.done <- func() { finish(apply, err) }:
			w.host.RequestRedraw()
		case <-w.ctx.Done():
		}
	}()
}
func (w *workbench) discover(paths []string) {
	w.job("Open repository", func(ctx context.Context) (func(), error) {
		var roots []string
		var problems []string
		for _, p := range paths {
			found, err := repo.Discover(ctx, p)
			if err != nil {
				problems = append(problems, err.Error())
				continue
			}
			for _, r := range found {
				if !slices.Contains(roots, r) {
					roots = append(roots, r)
				}
			}
		}
		if len(roots) == 0 {
			return nil, fmt.Errorf("%s", strings.Join(problems, "\n"))
		}
		return func() {
			w.prefs.rememberRepositories(roots)
			w.open(roots[0])
			if len(problems) > 0 {
				w.showError(fmt.Errorf("%s", strings.Join(problems, "\n")))
			}
		}, nil
	})
}
func (w *workbench) open(path string) {
	w.openRepository(path, true)
}

func (w *workbench) openRepository(path string, visit bool) {
	if w.busy {
		return
	}
	filter, icons := w.prefs.Settings.historyFilter(), maps.Clone(w.prefs.RepositoryIcons)
	w.job("Read repository", func(ctx context.Context) (func(), error) {
		return w.readRepository(ctx, []string{path}, filter, icons, visit)
	})
}

func (w *workbench) readRepository(ctx context.Context, paths []string, filter repo.Filter, icons map[string]string, visit bool) (func(), error) {
	path := paths[0]
	data, err := repo.Load(ctx, path, filter)
	if err != nil {
		return nil, err
	}
	thumbnail := resolveRepositoryIcon(path, icons[repositoryKey(path)])
	return func() {
		w.path, w.filter, w.data = path, filter, data
		w.repositoryIcon = thumbnail
		w.closePreview()
		w.search = ""
		w.table = nil
		w.page = "history"
		w.inlineClosed = false
		w.amend = false
		w.file = nil
		w.commit = repo.Commit{}
		w.prefs.rememberRepositories(paths)
		w.prefs.Recent = append([]string{path}, slices.DeleteFunc(w.prefs.Recent, func(p string) bool { return sameRepositoryPath(p, path) })...)
		if visit {
			w.prefs.recordRepositoryVisit(path, time.Now())
		}
		w.prefs.save(w.settingsPath)
		w.filterRows()
		w.scheduleAutoRefresh()
	}, nil
}
func (w *workbench) refresh(f repo.Filter) {
	path := w.path
	w.job("Refresh history", func(ctx context.Context) (func(), error) {
		data, err := repo.Load(ctx, path, f)
		return func() {
			if w.settingsRefresh {
				return // A newer set of history settings is waiting to reload.
			}
			w.data, w.filter = data, f
			w.file = nil
			w.filterRows()
			w.refreshPreview()
		}, err
	})
}

func (w *workbench) refreshBranchHistory(f repo.Filter) {
	if w.busy && w.branchFilter == nil {
		return
	}
	loading := w.branchFilter != nil
	w.repoVersion++
	w.branchFilter = &f
	if loading {
		return
	}
	w.busy = true
	w.statusText.Set("Refresh history…")
	w.loadMoreDisabled.Set(true)
	w.loadBranchHistory()
}

func (w *workbench) loadBranchHistory() {
	filter, path, data := w.branchFilter, w.path, w.data
	w.runJob(func(ctx context.Context) (func(), error) {
		commits, more, err := repo.LoadHistory(ctx, path, data, *filter)
		return func() {
			if w.filter.Remote != filter.Remote {
				w.prefs.Settings.HideRemotes = !filter.Remote
				w.prefs.save(w.settingsPath)
			}
			w.filter = *filter
			w.data.Commits, w.data.More = commits, more
			w.filterRows()
		}, err
	}, func(apply func(), err error) {
		// Coalesce checkbox and switch changes made while Git was reading.
		if filter != w.branchFilter {
			w.loadBranchHistory()
			return
		}
		w.branchFilter, w.busy, w.status = nil, false, ""
		if err != nil {
			w.closeToolbarPopup()
			w.status = err.Error()
			w.showError(err)
			w.redraw()
			return
		}
		apply()
		if w.popupIcon != branchIcon.Name {
			w.redraw()
			return
		}
		w.statusText.Set(w.historyStatus())
		w.loadMoreDisabled.Set(!w.data.More)
		w.branchBar.SetNeedsRedraw(true)
		w.ui.Window().Context().InvalidateRect(w.branchBar.ScreenBounds())
	})
}

func (w *workbench) filterRows() {
	w.rows = nil
	row := w.uncommittedRowCount() - 1
	for _, c := range w.data.Commits {
		w.rows = append(w.rows, c)
		if c.Hash == w.commit.Hash {
			row = len(w.rows) - 1 + w.uncommittedRowCount()
		}
	}
	w.graph = buildHistoryGraph(w.data, w.rows, w.uncommittedRowCount() != 0)
	if w.page == "history" || (w.page == "commit" && row < w.uncommittedRowCount()) || (w.usesInlineHistory() && w.inlineClosed) {
		w.selected.Set(-1)
	} else {
		w.selected.Set(row)
	}
	if w.table != nil {
		w.syncInlineDetails()
		w.table.InvalidateData()
	}
	w.updateHistorySearch()
}

func (w *workbench) operation(label string, op func(context.Context) error, after func()) {
	path, f := w.path, w.filter
	w.job(label, func(ctx context.Context) (func(), error) {
		actionErr := op(ctx)
		// Even a failed operation can change HEAD, refs or the index.
		data, readErr := repo.Load(ctx, path, f)
		if readErr != nil && len(f.Refs) > 0 {
			f.Refs = nil
			data, readErr = repo.Load(ctx, path, f)
		}
		return func() {
			if readErr == nil {
				w.data, w.filter = data, f
				w.file = nil
				w.filterRows()
				w.refreshPreview()
			}
			if actionErr != nil {
				w.status = actionErr.Error()
				w.showError(actionErr)
				return
			}
			if after != nil {
				after()
			}
			if readErr != nil {
				w.status = readErr.Error()
				w.showError(readErr)
			}
		}, nil
	})
}

func (w *workbench) command(label, tool string, args ...string) {
	path := w.path
	w.changeRepository(label, func(ctx context.Context) error { _, err := repo.Run(ctx, path, tool, args...); return err })
}

func (w *workbench) changeRepository(label string, op func(context.Context) error) {
	w.operation(label, op, func() {
		if w.page == "commit" && !slices.ContainsFunc(w.rows, func(c repo.Commit) bool { return c.Hash == w.commit.Hash }) {
			w.page = "history"
			w.commit = repo.Commit{}
			w.compareBase, w.compareTip = "", ""
			w.changed = nil
			w.selected.Set(-1)
		}
	})
}
