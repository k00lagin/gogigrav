package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

type repositoryLocation struct {
	path, directory, prefix string
	exact                   bool
}

func repositoryLocationFor(text string) (repositoryLocation, error) {
	path := repositoryQuery(text)
	if path == "" {
		return repositoryLocation{}, errors.New("Enter a repository path.")
	}
	exact := path == "~" || os.IsPathSeparator(path[len(path)-1])
	if path == "~" || len(path) > 1 && path[0] == '~' && os.IsPathSeparator(path[1]) {
		home, err := os.UserHomeDir()
		if err != nil {
			return repositoryLocation{}, err
		}
		path = filepath.Join(home, path[1:])
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return repositoryLocation{}, err
	}
	location := repositoryLocation{path: path, directory: path, exact: exact || filepath.Dir(path) == path}
	if !location.exact {
		location.directory = filepath.Dir(path)
		location.prefix = strings.ToLower(filepath.Base(path))
	}
	return location, nil
}

func directoryPath(path string) string {
	if path != "" && !os.IsPathSeparator(path[len(path)-1]) {
		return path + string(filepath.Separator)
	}
	return path
}

type repositoryFolder struct {
	path   string
	isRepo bool
}

func hasGitMarker(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func browseRepositoryFolders(ctx context.Context, location repositoryLocation) ([]repositoryFolder, bool, error) {
	entries, err := os.ReadDir(location.directory)
	if err != nil {
		return nil, false, err
	}
	var folders []repositoryFolder
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		name := entry.Name()
		if !entry.IsDir() || !strings.HasPrefix(strings.ToLower(name), location.prefix) ||
			(strings.HasPrefix(name, ".") && !strings.HasPrefix(location.prefix, ".")) {
			continue
		}
		path := filepath.Join(location.directory, name)
		folders = append(folders, repositoryFolder{path, hasGitMarker(path)})
	}
	compare := repositoryNameOrder()
	slices.SortStableFunc(folders, func(a, b repositoryFolder) int { return compare(a.path, b.path) })
	canInitialize := location.exact
	for dir := location.directory; canInitialize; dir = filepath.Dir(dir) {
		canInitialize = !hasGitMarker(dir)
		if dir == filepath.Dir(dir) {
			break
		}
	}
	return folders, canInitialize, nil
}

type repositoryPicker struct {
	*primitives.BoxWidget
	bench  *workbench
	dialog *dialog.Widget
	field  *textfield.Widget
	list   *primitives.BoxWidget
	view   *scrollview.Widget
	icons  *repositoryIconGallery
	scroll state.Signal[float32]
	status state.Signal[string]

	rows            []*repositoryFolderRow
	hovered         *repositoryFolderRow
	selected        int
	parent, initDir string
	cancelBrowse    context.CancelFunc
}

const repositoryPickerHelp = "↑↓ Select · Enter Browse · Backspace Parent · Ctrl+Enter Add path"
const repositoryPickerWidth = 640
const pickerInset = 13

var parentFolderIcon = icon.FromSVGXML("parent_folder", []byte(`<svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
<path d="M8.10584 4.34613L8.25344 4.5H13C13.8284 4.5 14.5 5.17157 14.5 6V12.1333C14.5 12.9529 13.932 13.5 13.3667 13.5H2.63333C2.06804 13.5 1.5 12.9529 1.5 12.1333V3.86667C1.5 3.04707 2.06804 2.5 2.63333 2.5H6.1217C6.25792 2.5 6.38824 2.55557 6.48253 2.65387L8.10584 4.34613Z" stroke="#6C707E"/>
<path d="M8 11.25V7M8 7L5.75 9.25M8 7L10.25 9.25" stroke="#6C707E" stroke-linecap="round" stroke-linejoin="round"/>
</svg>`))

func (w *workbench) showRepositoryPicker() {
	if w.busy {
		return
	}
	w.closeToolbarPopup()
	w.clearToolbarTooltip()
	ctx := w.ui.Window().Context()
	previousFocus := ctx.FocusedWidget()
	initial := filepath.Dir(w.path)
	if w.path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			initial = home
		}
	}
	p := &repositoryPicker{
		bench: w, selected: -1, icons: newRepositoryIconGallery(w),
		scroll: state.NewSignal(float32(0)), status: state.NewSignal(repositoryPickerHelp),
	}
	p.field = w.input("Repository path", directoryPath(initial), p.browse, nil)
	p.list = primitives.VBox().CrossAlign(primitives.CrossAxisStretch)
	p.view = scrollview.New(p.list, scrollview.ScrollYSignal(p.scroll), scrollview.PainterOpt(w.paint.Scrollbar))
	header := w.dialogHeader("Add repository", func() { p.dialog.Close(ctx) }).Height(44).PaddingXY(pickerInset, 0)
	up := w.iconButton(parentFolderIcon, "Parent folder (Backspace)", 28, 16,
		func() bool { return w.busy || p.parent == "" },
		func() { p.navigate(p.parent) })
	controls := row(primitives.Expanded(compactInput(p.field)),
		up,
		p.button("Add", "Add path (Ctrl+Enter)", button.Filled,
			func() bool { return repositoryQuery(p.field.Text()) == "" }, func() { p.submit(p.field.Text(), false) })).
		Height(46).PaddingXY(pickerInset, 0)
	footer := primitives.VBox(w.muted("").FontSize(11).ContentSignal(p.status).MaxLines(2).Ellipsis()).PaddingXY(pickerInset, 7)
	height := max(320, ctx.WindowSize().Height*0.70)
	p.BoxWidget = primitives.VBox(header, line(w), controls, line(w), primitives.Expanded(p.view), line(w),
		footer).Width(repositoryPickerWidth).Height(height).CrossAlign(primitives.CrossAxisStretch)
	p.dialog = dialog.New(dialog.ContentOnly(), dialog.Content(p), dialog.MaxWidth(repositoryPickerWidth),
		dialog.PainterOpt(w.paint.Dialog), dialog.OnClose(func() {
			if p.cancelBrowse != nil {
				p.cancelBrowse()
			}
			if previousFocus != nil && !widget.IsDetached(previousFocus) {
				ctx.RequestFocus(previousFocus)
			}
		}))
	p.dialog.Show(ctx)
	ctx.RequestFocus(p.field)
	p.field.Event(ctx, event.NewKeyEvent(event.KeyPress, event.KeyEnd, 0, event.ModNone))
	p.browse(p.field.Text())
}

func (p *repositoryPicker) button(label, hint string, style button.Variant, disabled func() bool, action func()) *button.Widget {
	return button.New(button.TextOpt(label), button.A11yHint(hint), button.VariantOpt(style), button.SizeOpt(button.Small),
		button.DisabledFn(func() bool { return p.bench.busy || disabled != nil && disabled() }),
		button.PainterOpt(p.bench.paint.Button), button.OnClick(action))
}

func (p *repositoryPicker) browse(text string) {
	if p.cancelBrowse != nil {
		p.cancelBrowse()
	}
	p.parent, p.initDir = "", ""
	location, err := repositoryLocationFor(text)
	if err != nil {
		p.status.Set(err.Error())
		p.setFolders(nil, "")
		return
	}
	if parent := filepath.Dir(location.directory); parent != location.directory {
		p.parent = parent
	}
	p.status.Set(repositoryPickerHelp)
	p.setFolders(nil, "Reading folder…")
	ctx, cancel := context.WithCancel(p.bench.ctx)
	p.cancelBrowse = cancel
	p.bench.runJob(func(context.Context) (func(), error) {
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, nil
		case <-timer.C:
		}
		folders, canInitialize, err := browseRepositoryFolders(ctx, location)
		return func() {
			if canInitialize {
				p.initDir = location.directory
				p.status.Set(repositoryPickerHelp + " · Alt+Enter Initialize")
			} else {
				p.status.Set(repositoryPickerHelp + " · Add a folder to register its repositories")
			}
			p.setFolders(folders, "No folders")
		}, err
	}, func(apply func(), err error) {
		if ctx.Err() != nil || !p.dialog.IsOpen() {
			return
		}
		if err != nil {
			p.status.Set(err.Error())
			p.setFolders(nil, "")
		} else if apply != nil {
			apply()
		}
	})
}

func (p *repositoryPicker) setFolders(folders []repositoryFolder, emptyText string) {
	w := p.bench
	folderIcon := func() widget.Widget {
		return primitives.VBox(icon.NewIcon(icon.ExpFolder, icon.Size(16),
			icon.Color(w.theme.Colors.OnSurfaceSecondary))).
			Width(repositoryIconSize).Height(16).CrossAlign(primitives.CrossAxisCenter)
	}
	p.list.ClearChildren()
	p.rows = nil
	p.hovered = nil
	p.selected = -1
	p.scroll.Set(0)
	if p.initDir != "" {
		init := p.button("Initialize", "Initialize repository (Alt+Enter)", button.Filled, nil,
			func() { p.submit(p.initDir, true) })
		p.list.AddChild(row(folderIcon(),
			primitives.Expanded(w.muted("This folder has no repository")), init).Height(34).PaddingXY(pickerInset, 0))
		p.list.AddChild(line(w))
	}
	if len(folders) == 0 && emptyText != "" {
		p.list.AddChild(primitives.VBox(w.muted(emptyText)).PaddingXY(pickerInset, 6))
	}
	for i, folder := range folders {
		badge := folderIcon()
		if folder.isRepo {
			badge = p.icons.repository(folder.path)
		}
		item := row(badge, primitives.Expanded(w.text(filepath.Base(folder.path)).MaxLines(1).Ellipsis())).
			Height(28).PaddingXY(pickerInset, 0)
		r := &repositoryFolderRow{BoxWidget: item, path: folder.path, picker: p, index: i}
		r.SetSelf(r)
		if folder.isRepo {
			add := p.button("Add", "Add "+filepath.Base(folder.path), button.Filled, func() bool { return p.hovered != r },
				func() { p.submit(folder.path, false) })
			r.add = &repositoryFolderAdd{Widget: add, row: r}
			item.AddChild(r.add)
		}
		p.rows = append(p.rows, r)
		p.list.AddChild(r)
	}
	if len(p.rows) != 0 {
		p.selectFolder(0, false)
	}
	p.list.MarkNeedsLayout()
	w.ui.Window().Context().Invalidate()
}

func (p *repositoryPicker) selectFolder(index int, reveal bool) {
	if index < 0 || index >= len(p.rows) || index == p.selected {
		return
	}
	ctx := p.bench.ui.Window().Context()
	if p.selected >= 0 {
		old := p.rows[p.selected]
		old.Background(widget.ColorTransparent).SetNeedsRedraw(true)
		ctx.InvalidateRect(old.ScreenBounds())
	}
	p.selected = index
	active := p.rows[index]
	active.Background(p.bench.theme.Colors.Selection).SetNeedsRedraw(true)
	ctx.InvalidateRect(active.ScreenBounds())
	if reveal {
		top, bottom := active.Bounds().Min.Y, active.Bounds().Max.Y
		height := p.view.ViewportSize().Height
		p.scroll.Set(max(0, min(top, max(bottom-height, p.scroll.Get()))))
	}
}

func (p *repositoryPicker) navigate(path string) {
	if path == "" || p.bench.busy {
		return
	}
	text := directoryPath(path)
	p.field.SetText(text)
	ctx := p.bench.ui.Window().Context()
	ctx.RequestFocus(p.field)
	p.field.Event(ctx, event.NewKeyEvent(event.KeyPress, event.KeyEnd, 0, event.ModNone))
	p.browse(text)
}

func (p *repositoryPicker) submit(text string, initialize bool) {
	w := p.bench
	if w.busy {
		return
	}
	location, err := repositoryLocationFor(text)
	if err != nil {
		p.status.Set(err.Error())
		return
	}
	label := "Add repository"
	if initialize {
		label = "Initialize repository"
	}
	p.status.Set(label + "…")
	p.invalidateControls()
	filter, icons := w.prefs.Settings.historyFilter(), maps.Clone(w.prefs.RepositoryIcons)
	w.jobWithError(label, func(ctx context.Context) (func(), error) {
		path := location.path
		if initialize {
			if root, err := repo.Root(ctx, path); err == nil {
				if !sameRepositoryPath(root, path) {
					return nil, fmt.Errorf("%s is inside the repository at %s", path, root)
				}
			} else if _, err := repo.Run(ctx, path, "git", "init"); err != nil {
				return nil, err
			}
		}
		paths, err := repo.Discover(ctx, path)
		if err != nil {
			return nil, err
		}
		slices.SortStableFunc(paths, repositoryNameOrder())
		apply, err := w.readRepository(ctx, paths, filter, icons, true)
		if err != nil {
			return nil, err
		}
		return func() {
			p.dialog.Close(w.ui.Window().Context())
			apply()
		}, nil
	}, func(err error) {
		if !p.dialog.IsOpen() {
			w.showError(err)
			return
		}
		p.status.Set(fmt.Sprintf("%s: %v", label, err))
		p.invalidateControls()
	})
}

func (p *repositoryPicker) invalidateControls() {
	widget.MarkRedrawInTree(p)
	p.bench.ui.Window().Context().Invalidate()
}

func (p *repositoryPicker) Event(ctx widget.Context, e event.Event) bool {
	key, ok := e.(*event.KeyEvent)
	if !ok || key.KeyType == event.KeyRelease || !ctx.IsFocused(p.field) {
		return p.BoxWidget.Event(ctx, e)
	}
	switch key.Key {
	case event.KeyDown, event.KeyUp:
		next := p.selected + 1
		if key.Key == event.KeyUp {
			next = p.selected - 1
		}
		p.selectFolder(max(0, min(next, len(p.rows)-1)), true)
		return true
	case event.KeyEnter:
		switch {
		case key.IsAlt() && p.initDir != "":
			p.submit(p.initDir, true)
		case key.IsCtrl() || key.IsSuper() || p.selected < 0:
			p.submit(p.field.Text(), false)
		default:
			p.navigate(p.rows[p.selected].path)
		}
		return true
	case event.KeyBackspace:
		text := p.field.Text()
		start, end := p.field.Selection()
		if start == end && p.field.CursorPosition() == len([]rune(text)) &&
			p.parent != "" && text != "" && os.IsPathSeparator(text[len(text)-1]) {
			p.navigate(p.parent)
			return true
		}
	}
	return p.BoxWidget.Event(ctx, e)
}

type repositoryFolderRow struct {
	*primitives.BoxWidget
	add     *repositoryFolderAdd
	picker  *repositoryPicker
	path    string
	index   int
	pressed bool
}

// The row owns hover for its icon and label. Add is the only interactive child.
func (r *repositoryFolderRow) Children() []widget.Widget {
	if r.add == nil {
		return nil
	}
	return []widget.Widget{r.add}
}

func (r *repositoryFolderRow) setHovered(ctx widget.Context, hovered bool) {
	p := r.picker
	if hovered {
		p.hovered = r
		p.selectFolder(r.index, false)
		ctx.SetCursor(widget.CursorPointer)
	} else if p.hovered == r {
		p.hovered = nil
	} else {
		return
	}
	r.SetNeedsRedraw(true)
	ctx.InvalidateRect(r.ScreenBounds())
}

func (r *repositoryFolderRow) endHoverOutside(ctx widget.Context, at geometry.Point) {
	if !r.ScreenBounds().Contains(at) {
		r.pressed = false
		r.setHovered(ctx, false)
	}
}

func (r *repositoryFolderRow) Event(ctx widget.Context, e event.Event) bool {
	mouse, ok := e.(*event.MouseEvent)
	if ok {
		switch mouse.MouseType {
		case event.MouseEnter:
			r.setHovered(ctx, true)
		case event.MouseLeave:
			r.endHoverOutside(ctx, mouse.GlobalPosition)
		}
	}
	if r.BoxWidget.Event(ctx, e) {
		return true
	}
	if ok && mouse.Button == event.ButtonLeft {
		switch mouse.MouseType {
		case event.MousePress:
			r.pressed = true
			return true
		case event.MouseRelease:
			if r.pressed {
				r.pressed = false
				r.picker.navigate(r.path)
				return true
			}
		}
	}
	return false
}

type repositoryFolderAdd struct {
	*button.Widget
	row *repositoryFolderRow
}

func (a *repositoryFolderAdd) IsVisible() bool { return a.row.picker.hovered == a.row }

func (a *repositoryFolderAdd) Draw(ctx widget.Context, canvas widget.Canvas) {
	if a.IsVisible() {
		a.Widget.Draw(ctx, canvas)
	}
}

func (a *repositoryFolderAdd) Event(ctx widget.Context, e event.Event) bool {
	handled := a.Widget.Event(ctx, e)
	if mouse, ok := e.(*event.MouseEvent); ok && mouse.MouseType == event.MouseLeave {
		a.row.endHoverOutside(ctx, mouse.GlobalPosition)
	}
	return handled
}
