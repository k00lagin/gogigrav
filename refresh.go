package main

import (
	"context"
	"slices"
	"time"

	"gogigrav/internal/repo"
)

const autoRefreshInterval = 3 * time.Second

func (w *workbench) scheduleAutoRefresh() {
	if w.autoRefreshTimer != nil {
		w.autoRefreshTimer.Stop()
		w.autoRefreshTimer = nil
	}
	if w.path == "" || w.autoRefreshTick == nil || w.prefs.Settings.DisableAutoRefresh {
		return
	}
	w.autoRefreshTimer = time.AfterFunc(autoRefreshInterval, func() {
		select {
		case <-w.ctx.Done():
		case w.autoRefreshTick <- struct{}{}:
			w.host.RequestRedraw()
		default:
		}
	})
}

func (w *workbench) checkAutoRefresh() {
	w.scheduleAutoRefresh()
	if w.path == "" || w.autoChecking || w.busy || w.prefs.Settings.DisableAutoRefresh || w.ui.Window().Overlays().TopInteractive() != nil {
		return
	}
	w.autoChecking = true
	path, filter, revision, version := w.path, w.filter, w.data.Revision, w.repoVersion
	w.runJob(func(ctx context.Context) (func(), error) {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		snapshot, changed, err := repo.LoadIfChanged(ctx, path, filter, revision)
		if changed && err != nil {
			refs := slices.DeleteFunc(slices.Clone(filter.Refs), func(ref string) bool {
				return !slices.Contains(snapshot.Branches, ref)
			})
			if len(refs) != len(filter.Refs) {
				filter.Refs = refs
				snapshot.Commits, snapshot.More, err = repo.LoadHistory(ctx, path, snapshot, filter)
			}
		}
		if err != nil || !changed {
			return nil, err
		}
		return func() {
			w.data, w.filter = snapshot, filter
			w.file = nil
			w.filterRows()
			w.refreshPreview()
			w.redraw()
		}, nil
	}, func(apply func(), err error) {
		w.autoChecking = false
		if apply != nil && err == nil && w.repoVersion == version && !w.busy &&
			!w.prefs.Settings.DisableAutoRefresh && w.ui.Window().Overlays().TopInteractive() == nil {
			apply()
		}
	})
}
