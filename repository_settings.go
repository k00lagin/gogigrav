package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/core/listview"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
)

// Each open menu shares thumbnails across its virtual rows and search results.
type repositoryIconGallery struct {
	w     *workbench
	cache map[thumbnailKey]*iconThumbnail
}
type thumbnailKey struct {
	root, path string
	automatic  bool
}

var iconWorkers = make(chan struct{}, 3)

func newRepositoryIconGallery(w *workbench) *repositoryIconGallery {
	return &repositoryIconGallery{w: w, cache: make(map[thumbnailKey]*iconThumbnail)}
}
func (g *repositoryIconGallery) repository(root string) widget.Widget {
	return g.badge(thumbnailKey{root: root, path: g.w.prefs.RepositoryIcons[repositoryKey(root)], automatic: true}, filepath.Base(root))
}
func (g *repositoryIconGallery) badge(key thumbnailKey, name string) widget.Widget {
	entry := g.cache[key]
	if entry == nil {
		entry = &iconThumbnail{}
		if key.automatic && sameRepositoryPath(key.root, g.w.path) {
			*entry = g.w.repositoryIcon
		}
		g.cache[key] = entry
		g.w.runJob(func(ctx context.Context) (func(), error) {
			select {
			case iconWorkers <- struct{}{}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			defer func() { <-iconWorkers }()
			var result iconThumbnail
			if key.automatic {
				result = resolveRepositoryIcon(key.root, key.path)
			} else {
				root, err := os.OpenRoot(key.root)
				if err != nil {
					return nil, err
				}
				defer root.Close()
				result, err = loadThumbnail(root, key.path, false)
				if err != nil {
					return nil, err
				}
			}
			return func() { *entry = result }, nil
		}, func(apply func(), err error) {
			if err != nil || apply == nil {
				return
			}
			apply()
			if key.automatic && sameRepositoryPath(key.root, g.w.path) && key.path == g.w.prefs.RepositoryIcons[repositoryKey(key.root)] {
				g.w.updateRepositoryIcon(*entry)
			}
			for _, overlay := range g.w.ui.Window().OverlayContentWidgets() {
				widget.MarkRedrawInTree(overlay)
			}
			g.w.ui.Window().Context().Invalidate()
		})
	}
	return &repositoryBadge{BoxWidget: primitives.Box().Width(repositoryIconSize).Height(repositoryIconSize),
		thumbnail: entry, name: name}
}

type repositoryBadge struct {
	*primitives.BoxWidget
	thumbnail *iconThumbnail
	name      string
}

func (b *repositoryBadge) Draw(_ widget.Context, canvas widget.Canvas) {
	b.thumbnail.draw(canvas, b.Bounds(), b.name)
}

func (t iconThumbnail) draw(canvas widget.Canvas, bounds geometry.Rect, name string) {
	if img := t.img; img != nil {
		center := bounds.Center()
		canvas.DrawImage(img, geometry.Pt(center.X-float32(img.Bounds().Dx())/2, center.Y-float32(img.Bounds().Dy())/2))
		return
	}
	canvas.DrawRoundRect(bounds, widget.Hex(repositoryColor(name)), 4)
	canvas.DrawText(repositoryMonogram(name), bounds, 11, widget.ColorWhite, true, widget.TextAlignCenter)
}

func (w *workbench) updateRepositoryIcon(thumbnail iconThumbnail) {
	w.repositoryIcon = thumbnail
	if bar := w.branchBar; bar != nil {
		bar.SetNeedsRedraw(true)
		w.ui.Window().Context().InvalidateRect(bar.ScreenBounds())
	}
}

func (w *workbench) setRepositoryIcon(root, path string, thumbnail iconThumbnail) error {
	next := w.prefs
	next.RepositoryIcons = maps.Clone(next.RepositoryIcons)
	key := repositoryKey(root)
	if path == "" {
		delete(next.RepositoryIcons, key)
	} else {
		if next.RepositoryIcons == nil {
			next.RepositoryIcons = make(map[string]string)
		}
		next.RepositoryIcons[key] = path
	}
	if w.settingsPath != "" {
		if err := os.MkdirAll(filepath.Dir(w.settingsPath), 0700); err != nil {
			return err
		}
		if err := writePreferences(w.settingsPath, next); err != nil {
			return err
		}
	}
	w.prefs = next
	if sameRepositoryPath(root, w.path) {
		w.updateRepositoryIcon(thumbnail)
	}
	return nil
}

func (w *workbench) repositorySettings(path string) {
	ctx := w.ui.Window().Context()
	previous := ctx.FocusedWidget()
	var window *dialog.Widget
	icons := newRepositoryIconGallery(w)
	var files, matches []string
	var query string
	ready := false
	message := state.NewSignal("Indexing project images…")
	index := state.NewSignal(0)
	scroll := state.NewSignal(float32(0))
	var selectionVersion int
	save := func(value string, thumbnail iconThumbnail) {
		if err := w.setRepositoryIcon(path, value, thumbnail); err != nil {
			w.showError(err)
			return
		}
		window.Close(ctx)
	}
	selectFile := func(value string) {
		selectionVersion++
		version := selectionVersion
		w.runJob(func(context.Context) (func(), error) {
			if value == "" {
				thumbnail := resolveRepositoryIcon(path, "")
				return func() { save("", thumbnail) }, nil
			}
			root, err := os.OpenRoot(path)
			if err != nil {
				return nil, err
			}
			defer root.Close()
			thumbnail, err := loadThumbnail(root, value, true)
			return func() { save(value, thumbnail) }, err
		}, func(apply func(), err error) {
			if !window.IsOpen() || version != selectionVersion {
				return
			}
			if err != nil {
				w.showError(fmt.Errorf("cannot use %s as an icon: %w", value, err))
				return
			}
			apply()
		})
	}
	choose := func(i int) {
		if i >= 0 && i < len(matches) {
			selectFile(matches[i])
		}
	}
	const rowHeight = 38
	list := listview.New(listview.ItemCountFn(func() int { return len(matches) }), listview.FixedItemHeight(rowHeight),
		listview.SelectionModeOpt(listview.SelectionSingle), listview.SelectedIndexSignal(index), listview.ScrollYSignal(scroll),
		listview.OnItemClick(choose), listview.PainterOpt(w.paint.ListView), listview.BuildItem(func(item listview.ItemContext) widget.Widget {
			file := matches[item.Index]
			name := filepath.Base(file)
			detail := primitives.VBox(w.text(name).MaxLines(1).Ellipsis(), w.muted(file).FontSize(11).MaxLines(1).Ellipsis())
			return row(icons.badge(thumbnailKey{root: path, path: file}, name), primitives.Expanded(detail)).PaddingXY(8, 2)
		}))
	filter := func(value string) {
		query = value
		needle := strings.ToLower(strings.TrimSpace(filepath.ToSlash(query)))
		matches = nil
		for _, file := range files {
			if strings.Contains(strings.ToLower(file), needle) {
				matches = append(matches, file)
			}
		}
		index.Set(0)
		scroll.Set(0)
		list.InvalidateData()
		if ready {
			switch {
			case len(files) == 0:
				message.Set("No image files found")
			case len(matches) == 0:
				message.Set("No matching images")
			default:
				message.Set(fmt.Sprintf("%d images", len(matches)))
			}
		}
		ctx.Invalidate()
	}
	input := w.input("Search image files…", "", filter, func(string) { choose(index.Get()) })
	browse := w.btn("Browse…", button.Outlined, true, func() {
		selected, err := w.host.ShowOpenFileDialog(gogpu.FileDialogOptions{Title: "Choose repository icon", InitialDirectory: path,
			Filters: []gogpu.FileTypeFilter{{Name: "Images", Extensions: iconExtensions}}})
		if err != nil {
			w.showError(err)
			return
		}
		if len(selected) == 0 {
			return
		}
		value := selected[0]
		if rel, err := filepath.Rel(path, value); err == nil && filepath.IsLocal(rel) {
			value = filepath.ToSlash(rel)
		}
		selectFile(value)
	})
	override := w.prefs.RepositoryIcons[repositoryKey(path)]
	current := override
	if current == "" {
		current = "Automatic"
	}
	header := row(icons.repository(path), primitives.Expanded(primitives.VBox(w.text(filepath.Base(path)).Bold(), w.muted(path).FontSize(11).MaxLines(1).Ellipsis())))
	mode := row(w.text("Icon"), primitives.Expanded(w.muted(current).MaxLines(1).Ellipsis()),
		w.btn("Use automatic", button.TextOnly, override != "", func() { selectFile("") }))
	content := primitives.VBox(w.text("Repository settings").Bold().FontSize(15), header, line(w), mode, compactInput(input), primitives.VBox(list).Height(190),
		row(primitives.Expanded(w.muted("").ContentSignal(message).FontSize(11)), browse, w.btn("Close", button.TextOnly, true, func() { window.Close(ctx) }))).
		Padding(14).Gap(6).CrossAlign(primitives.CrossAxisStretch)
	keys := &choicePanelKeys{BoxWidget: content, focus: input, list: list, selection: index, scroll: scroll}
	indexCtx, cancel := context.WithCancel(w.ctx)
	window = dialog.New(dialog.ContentOnly(), dialog.Content(keys), dialog.MaxWidth(490), dialog.PainterOpt(w.paint.Dialog),
		dialog.OnClose(func() {
			cancel()
			if previous != nil && !widget.IsDetached(previous) {
				ctx.RequestFocus(previous)
			}
		}))
	window.Show(ctx)
	ctx.RequestFocus(input)
	w.runJob(func(context.Context) (func(), error) {
		result, err := projectImageFiles(indexCtx, path)
		return func() { files = result; ready = true; filter(query) }, err
	}, func(apply func(), err error) {
		if !window.IsOpen() {
			return
		}
		if err != nil {
			message.Set(err.Error())
			ctx.Invalidate()
			return
		}
		apply()
	})
}
