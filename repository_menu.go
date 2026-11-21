package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/gogpu/ui/core/menu"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/icon"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func isContextMenuKey(key *event.KeyEvent) bool {
	return key.KeyType == event.KeyPress && (key.Key == event.KeyMenu || key.Key == event.KeyF10 && key.Modifiers().Has(event.ModShift))
}

func (w *workbench) repositoryTriggerMenu(at geometry.Point) bool {
	if w.path == "" || w.branchBar == nil {
		return false
	}
	bar := w.branchBar
	if !bar.ItemBounds(0).Translate(bar.ScreenOrigin()).Contains(at) {
		return false
	}
	w.repositoryMenu(w.path, at)
	return true
}

func (w *workbench) repositoryMenu(path string, at geometry.Point) {
	w.clearToolbarTooltip()
	menu.NewContextMenu([]menu.MenuItem{
		menu.Item("Repository settings…", "", func() {
			w.closeToolbarPopup()
			w.repositorySettings(path)
		}),
		menu.Sep(),
		menu.Item("Reveal in File Explorer", "", func() {
			if err := revealFile(path); err != nil {
				w.showError(err)
			}
		}),
		menu.Item("Copy Path", "", func() { widget.ClipboardWrite(path) }),
		menu.Sep(),
		menu.Item("Remove from list", "", func() { w.removeRecentRepository(path) }),
	}, menu.ContextPainterOpt(w.paint.Menu)).Show(w.ui.Window().Context(), at)
}

func (w *workbench) removeRecentRepository(path string) {
	w.closeToolbarPopup()
	w.prefs.Recent = slices.DeleteFunc(w.prefs.Recent, func(p string) bool { return sameRepositoryPath(p, path) })
	delete(w.prefs.RepositoryIcons, repositoryKey(path))
	delete(w.prefs.RepositoryVisits, repositoryKey(path))
	w.prefs.save(w.settingsPath)
	if !sameRepositoryPath(path, w.path) {
		w.redraw()
		return
	}
	w.repoVersion++
	w.path = ""
	w.repositoryIcon = iconThumbnail{}
	w.data = repo.Snapshot{}
	w.table = nil
	w.file = nil
	w.commit = repo.Commit{}
	w.changed = nil
	w.scheduleAutoRefresh()
	w.filterRows()
	w.show("open")
	if len(w.prefs.Recent) > 0 {
		w.openRepository(w.prefs.Recent[0], false)
	}
}

func sameRepositoryPath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func repositoryQuery(query string) string {
	query = strings.TrimSpace(query)
	if len(query) >= 2 && (query[0] == '"' || query[0] == '\'') && query[0] == query[len(query)-1] {
		return strings.TrimSpace(query[1 : len(query)-1])
	}
	return query
}

func (w *workbench) repositorySearch(icons *repositoryIconGallery) func(string, func([]popupChoice)) {
	return func(query string, publish func([]popupChoice)) {
		if w.repoSearchCancel != nil {
			w.repoSearchCancel()
			w.repoSearchCancel = nil
		}
		path := repositoryQuery(query)
		if !filepath.IsAbs(path) && !strings.HasPrefix(path, "./") && !strings.HasPrefix(path, "../") &&
			!strings.HasPrefix(path, `.\`) && !strings.HasPrefix(path, `..\`) && !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, `\`) {
			return
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return
		}
		path = absolute
		popup := w.openPopup
		ctx, cancel := context.WithCancel(w.ctx)
		w.repoSearchCancel = cancel
		current := func() bool { return ctx.Err() == nil && popup != nil && w.openPopup == popup }
		publish([]popupChoice{{label: "Checking repository…", status: true}})
		w.runJob(func(_ context.Context) (func(), error) {
			timer := time.NewTimer(150 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil, nil
			case <-timer.C:
			}
			probe, stop := context.WithTimeout(ctx, 5*time.Second)
			defer stop()
			root, _ := repo.Root(probe, path)
			if root != "" {
				return func() {
					known := slices.ContainsFunc(w.prefs.Recent, func(path string) bool { return sameRepositoryPath(path, root) })
					label := filepath.Base(root)
					if !known {
						label = "Add " + label
					}
					choice := popupChoice{label: label, detail: root, glyph: icon.ExpFolder, current: sameRepositoryPath(root, w.path), action: func() { w.open(root) }}
					if known {
						choice.leading = func() widget.Widget { return icons.repository(root) }
						choice.contextMenu = func(at geometry.Point) { w.repositoryMenu(root, at) }
					} else {
						choice.leading = func() widget.Widget {
							return row(icon.NewIcon(icon.Add, icon.Size(14), icon.Color(w.theme.Colors.OnSurfaceSecondary)),
								icons.repository(root)).Gap(6)
						}
					}
					publish([]popupChoice{choice})
				}, nil
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				return func() { publish([]popupChoice{{label: "No Git repository at this path", empty: true}}) }, nil
			}
			return func() {
				init := popupChoice{label: "Initialize " + filepath.Base(path), detail: path, glyph: icon.Add, action: func() {
					w.job("Initialize repository", func(ctx context.Context) (func(), error) {
						if root, err := repo.Root(ctx, path); err == nil {
							return func() { w.open(root) }, nil
						}
						_, err := repo.Run(ctx, path, "git", "init")
						return func() { w.open(path) }, err
					})
				}}
				publish([]popupChoice{init})
				name := filepath.Base(path)
				if len(entries) == 0 && slugPattern.MatchString("owner/"+name) {
					w.searchGitHubRepositories(ctx, path, name, current, publish, init)
				}
			}, nil
		}, func(apply func(), _ error) {
			if apply != nil && current() {
				apply()
			}
		})
	}
}

func (w *workbench) searchGitHubRepositories(ctx context.Context, path, name string, current func() bool, publish func([]popupChoice), init popupChoice) {
	publish([]popupChoice{init, {label: "Looking for " + name + " on GitHub…", status: true}})
	w.runJob(func(_ context.Context) (func(), error) {
		lookup, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		query := fmt.Sprintf(`.[] | select((.name | ascii_downcase) == %q) | .full_name`, strings.ToLower(name))
		raw, _ := repo.Run(lookup, "", "gh", "api", "user/repos", "--method", "GET", "--paginate",
			"-f", "affiliation=owner,organization_member", "-f", "per_page=100", "--jq", query)
		return func() {
			results := []popupChoice{init}
			seen := make(map[string]bool)
			for _, slug := range strings.Fields(raw) {
				_, repoName, _ := strings.Cut(slug, "/")
				if !slugPattern.MatchString(slug) || !strings.EqualFold(repoName, name) || seen[slug] {
					continue
				}
				seen[slug] = true
				results = append(results, popupChoice{label: "Clone " + slug, detail: path, glyph: remotesIcon, action: func() {
					w.job("Clone "+slug, func(ctx context.Context) (func(), error) {
						_, err := repo.Run(ctx, "", "gh", "repo", "clone", slug, path)
						if err != nil {
							return nil, fmt.Errorf("clone %s: %w", slug, err)
						}
						return func() { w.open(path) }, nil
					})
				}})
			}
			publish(results)
		}, nil
	}, func(apply func(), _ error) {
		if apply != nil && current() {
			apply()
		}
	})
}
