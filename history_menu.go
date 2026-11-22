package main

import (
	"slices"
	"strings"

	"github.com/gogpu/ui/core/menu"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func historyMenuItem(label string, enabled bool, action func()) menu.MenuItem {
	return menu.MenuItem{Label: label, Disabled: !enabled, OnAction: action}
}

func copyHistoryItem(label, value string) menu.MenuItem {
	return menu.Item(label, "", func() { widget.ClipboardWrite(value) })
}

func (w *workbench) showHistoryMenu(items []menu.MenuItem, at geometry.Point) {
	menu.NewContextMenu(items, menu.ContextPainterOpt(w.paint.Menu), menu.ContextCaptureKeys()).Show(w.ui.Window().Context(), at)
}

func (w *workbench) historyStash(hash string) repo.Stash {
	for _, s := range w.data.Stashes {
		if s.Hash == hash {
			return s
		}
	}
	return repo.Stash{}
}

func (w *workbench) openHistoryMenu(hit historyHit, at geometry.Point) {
	var items []menu.MenuItem
	switch stash := w.historyStash(hit.commit.Hash); {
	case hit.commit.Hash == "":
		items = w.workingHistoryMenu()
	case stash.Hash != "" && hit.label.ref == "":
		items = w.stashHistoryMenu(stash)
	case hit.label.ref != "":
		items = w.historyRefMenu(hit.label, hit.commit)
	default:
		items = w.commitHistoryMenu(hit.commit)
	}
	w.showHistoryMenu(items, at)
}

func historyBranchName(branch, fallback string) string {
	if branch == "" {
		return fallback
	}
	return branch
}

func (w *workbench) commitHistoryMenu(c repo.Commit) []menu.MenuItem {
	branch := w.data.CurrentBranch()
	current := historyBranchName(branch, "current branch")
	canIntegrate := branch != "" && c.Hash != w.data.Head
	items := []menu.MenuItem{
		menu.Item("Create Branch…", "", func() { w.branchAt(c.Hash) }),
		historyMenuItem("Checkout (detached HEAD)", c.Hash != w.data.Head, func() {
			w.command("Checkout "+c.Short(), "git", "switch", "--detach", c.Hash)
		}),
		menu.Sep(),
		menu.Item("Cherry Pick…", "", func() { w.pickHistoryCommit(c, false) }),
		menu.Item("Revert…", "", func() { w.pickHistoryCommit(c, true) }),
		historyMenuItem("Drop Commit…", branch != "" && len(strings.Fields(c.Parents)) == 1, func() { w.dropHistoryCommit(c) }),
		menu.Sep(),
		historyMenuItem("Merge into "+current+"…", canIntegrate, func() { w.mergeHistory(c.Hash, c.Short(), branch) }),
		historyMenuItem("Rebase "+current+" on this Commit…", canIntegrate, func() { w.rebaseHistory(c.Hash, c.Short(), branch) }),
		historyMenuItem("Reset "+historyBranchName(branch, "HEAD")+" to this Commit…", w.data.Head != "", func() { w.resetHistory(c) }),
		menu.Sep(),
		menu.Item("Create Tag…", "", func() { w.tagAt(c.Hash) }),
		menu.Sep(),
	}
	if i := w.selected.Get() - w.uncommittedRowCount(); i >= 0 && i < len(w.rows) && w.rows[i].Hash != c.Hash {
		selected := w.rows[i]
		j := slices.IndexFunc(w.rows, func(row repo.Commit) bool { return row.Hash == c.Hash })
		base, tip := selected.Hash, c.Hash
		if i < j {
			base, tip = tip, base
		}
		items = append(items, menu.Item("Compare with "+selected.Short(), "", func() {
			w.file = nil
			w.inlineClosed = false
			w.focusHistory = w.prefs.Settings.UnifiedDetails
			w.revealInline = w.focusHistory
			w.showHistoryCommit(selected, base, tip)
		}), menu.Sep())
	}
	return append(items, copyHistoryItem("Copy Commit Hash", c.Hash), copyHistoryItem("Copy Commit Subject", c.Subject))
}

func (w *workbench) openBranchDropMenu(from, to historyHit, at geometry.Point) {
	w.showHistoryMenu(w.branchDropMenu(from, to), at)
}

func (w *workbench) branchDropMenu(from, to historyHit) []menu.MenuItem {
	current, source := w.data.CurrentBranch(), shortRef(from.label.ref)
	target := shortRef(to.label.ref)
	if current != "" && (target == current || (target == "" && to.commit.Hash == w.data.Head)) {
		return []menu.MenuItem{
			menu.Item("Merge "+source+" into "+current+"…", "", func() { w.mergeHistory(from.label.ref, source, current) }),
			menu.Item("Rebase "+current+" on "+source+"…", "", func() { w.rebaseHistory(from.label.ref, source, current) }),
		}
	}
	ref, name := to.label.ref, target
	if target == "" {
		ref, name = to.commit.Hash, to.commit.Short()
	}
	rebase := menu.Item("Rebase "+source+" on "+name+"…", "", func() { w.rebaseHistory(ref, name, source) })
	switch {
	case target == "":
		return []menu.MenuItem{rebase}
	case source == current:
		return []menu.MenuItem{
			menu.Item("Merge "+target+" into "+source+"…", "", func() { w.mergeHistory(ref, target, source) }),
			rebase,
		}
	default:
		return []menu.MenuItem{
			menu.Item("Checkout "+target+" and merge "+source+"…", "", func() { w.mergeHistory(from.label.ref, source, target) }),
			rebase,
		}
	}
}

func (w *workbench) stashHistoryMenu(s repo.Stash) []menu.MenuItem {
	return []menu.MenuItem{
		menu.Item("Apply Stash", "", func() { w.stashCommand("Apply "+s.Selector, s, "apply", s.Hash) }),
		menu.Item("Apply Stash (reinstate index)", "", func() { w.stashCommand("Apply "+s.Selector, s, "apply", "--index", s.Hash) }),
		menu.Item("Pop Stash", "", func() { w.stashCommand("Pop "+s.Selector, s, "pop", s.Selector) }),
		menu.Item("Create Branch from Stash…", "", func() { w.stashBranchForm(s) }),
		menu.Sep(),
		menu.Item("Drop Stash…", "", func() { w.confirmDropStash(s) }),
		menu.Sep(),
		copyHistoryItem("Copy Stash Name", s.Selector),
		copyHistoryItem("Copy Commit Hash", s.Hash),
	}
}

func (w *workbench) workingHistoryMenu() []menu.MenuItem {
	return []menu.MenuItem{
		historyMenuItem("Stash Changes…", w.data.Head != "", w.stashCreateForm),
		historyMenuItem("Stage All Changes", slices.ContainsFunc(w.data.Files, repo.File.Unstaged), func() { w.changeAllIndex(true) }),
		historyMenuItem("Unstage All Changes", slices.ContainsFunc(w.data.Files, repo.File.Staged), func() { w.changeAllIndex(false) }),
		menu.Sep(),
		historyMenuItem("Discard All Changes…", len(w.data.Files) != 0, w.confirmDiscardAll),
	}
}
