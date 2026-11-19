package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
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
	"github.com/gogpu/ui/core/titlebar"
	"github.com/gogpu/ui/desktop"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/focus"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/theme/devtools"
	"gogigrav/internal/repo"
)

type preferences struct {
	Light  bool
	Recent []string
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "gogigrav", "settings.json")
}
func (p preferences) save() {
	path := configPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		log.Print(err)
		return
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err == nil {
		err = os.WriteFile(path, data, 0600)
	}
	if err != nil {
		log.Print(err)
	}
}

type workbench struct {
	host        *gogpu.App
	ui          *app.App
	theme       *devtools.Theme
	paint       devtools.Painters
	prefs       preferences
	ctx         context.Context
	done        chan func()
	busy, dirty bool
	status      string
	path        string
	data        repo.Snapshot
	filter      repo.Filter
	page        string
	ratio       float32
	table       *datatable.Widget
	searchField *textfield.Widget
	titleBar    *titlebar.Widget
	openPopup   *popover.Popover
	popupIcon   string
	popupAction func()
	searchOpen  bool
	focusSearch bool
	rows        []repo.Commit
	search      string
	selected    state.Signal[int]
	commit      repo.Commit
	changed     []repo.File
	file        *fileItem
	drafts      map[string]string
	form        *actionForm
	amend       bool

	collapsedBranchGroups map[branchGroupKey]bool

	branchFilter     *repo.Filter
	branchBar        *toolbarHints
	loadMoreDisabled state.Signal[bool]
	statusText       state.Signal[string]
}

func main() {
	themeName := flag.String("theme", "", "DevTools or DevTools Dark")
	flag.Parse()
	var prefs preferences
	if data, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(data, &prefs)
	}
	if *themeName != "" {
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
	w := &workbench{ctx: ctx, prefs: prefs, done: make(chan func(), 1), dirty: true, page: "open", ratio: 0.65, drafts: map[string]string{}, selected: state.NewSignal(0), filter: repo.Filter{Remote: true, Limit: 250}}
	w.setTheme()
	w.host = gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle("Gogigrav").WithSize(1280, 760).WithMinSize(1080, 520).WithFrameless(true))
	w.ui = app.New(app.WithWindowProvider(w.host), app.WithPlatformProvider(w.host), app.WithEventSource(w.host.EventSource()), app.WithTheme(w.theme.AsTheme()))
	w.render()
	w.host.SetHitTestCallback(w.hitTestWindow)
	shortcuts := w.ui.Window().FocusManager()
	shortcuts.RegisterShortcut(focus.Shortcut{Key: event.KeyF, Ctrl: true}, w.showSearch)
	shortcuts.RegisterShortcut(focus.Shortcut{Key: event.KeyO, Ctrl: true}, func() { w.show("open") })
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
		if w.dirty {
			w.render()
		}
	})
	paths := flag.Args()
	if len(paths) == 0 {
		cwd, err := os.Getwd()
		if err == nil {
			w.drafts["openPath"] = cwd
			if root, err := repo.Root(ctx, cwd); err == nil {
				paths = []string{root}
			} else if len(w.prefs.Recent) > 0 {
				paths = []string{w.prefs.Recent[0]}
			}
		}
	}
	if len(paths) > 0 {
		w.discover(paths)
	}
	if err := desktop.Run(w.host, w.ui); err != nil {
		log.Fatal(err)
	}
}

func (w *workbench) setTheme() {
	if w.prefs.Light {
		w.theme = devtools.NewTheme()
	} else {
		w.theme = devtools.NewDarkTheme()
	}
	w.paint = devtools.NewPainters(w.theme)
}

func (w *workbench) redraw() { w.dirty = true; w.host.RequestRedraw() }
func (w *workbench) show(page string) {
	if w.busy {
		return
	}
	w.page = page
	w.form = nil
	w.redraw()
}

// The event loop owns the model and widgets. Workers return a completion to it.
func (w *workbench) job(label string, work func(context.Context) (func(), error)) {
	if w.busy {
		return
	}
	w.busy = true
	w.status = label + "…"
	w.redraw()
	w.runJob(work, func(apply func(), err error) {
		w.busy = false
		w.status = ""
		if err != nil {
			w.status = err.Error()
			w.showError(err)
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
		w.done <- func() { finish(apply, err) }
		w.host.RequestRedraw()
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
			for _, r := range roots {
				if !slices.Contains(w.prefs.Recent, r) {
					w.prefs.Recent = append(w.prefs.Recent, r)
				}
			}
			w.open(roots[0])
			if len(problems) > 0 {
				w.showError(fmt.Errorf("%s", strings.Join(problems, "\n")))
			}
		}, nil
	})
}
func (w *workbench) open(path string) {
	if w.busy {
		return
	}
	f := repo.Filter{Remote: w.filter.Remote, Limit: 250}
	w.job("Read repository", func(ctx context.Context) (func(), error) {
		data, err := repo.Load(ctx, path, f)
		return func() {
			w.path, w.filter, w.data = path, f, data
			w.search = ""
			w.table = nil
			w.page = "changes"
			w.amend = false
			w.form = nil
			w.file = nil
			w.commit = repo.Commit{}
			w.prefs.Recent = append([]string{path}, slices.DeleteFunc(w.prefs.Recent, func(p string) bool { return p == path })...)
			w.prefs.save()
			w.filterRows()
		}, err
	})
}
func (w *workbench) refresh(f repo.Filter) {
	path := w.path
	w.job("Refresh history", func(ctx context.Context) (func(), error) {
		data, err := repo.Load(ctx, path, f)
		return func() { w.data, w.filter = data, f; w.file = nil; w.filterRows() }, err
	})
}

func (w *workbench) refreshBranchHistory(f repo.Filter) {
	if w.busy && w.branchFilter == nil {
		return
	}
	loading := w.branchFilter != nil
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
	needle := strings.ToLower(strings.TrimSpace(w.search))
	w.rows = nil
	row := 0
	for _, c := range w.data.Commits {
		if needle != "" && !strings.Contains(strings.ToLower(strings.Join([]string{c.Subject, c.Author, c.Email, c.Hash, c.Refs, c.Body}, " ")), needle) {
			continue
		}
		w.rows = append(w.rows, c)
		if c.Hash == w.commit.Hash {
			row = len(w.rows)
		}
	}
	if w.page == "commit" && row == 0 {
		w.selected.Set(-1)
	} else {
		w.selected.Set(row)
	}
	if w.table != nil {
		w.table.InvalidateData()
	}
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
	w.operation(label, func(ctx context.Context) error { _, err := repo.Run(ctx, path, tool, args...); return err }, func() { w.page = "changes"; w.form = nil; w.commit = repo.Commit{}; w.selected.Set(0) })
}
