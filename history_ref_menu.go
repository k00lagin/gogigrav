package main

import (
	"context"
	"slices"
	"strings"

	"github.com/gogpu/ui/core/menu"
	"gogigrav/internal/repo"
)

func (w *workbench) historyRefMenu(label historyRef, c repo.Commit) []menu.MenuItem {
	ref := label.ref
	current := w.data.CurrentBranch()
	var items []menu.MenuItem
	copyLabel := "Copy Branch Name"
	switch {
	case strings.HasPrefix(ref, repo.LocalRefPrefix):
		name := shortRef(ref)
		if name != current {
			items = append(items, menu.Item("Checkout "+name, "", func() { w.switchBranch(ref) }))
		}
		items = append(items,
			menu.Item("Rename Branch…", "", func() { w.renameHistoryBranch(name) }),
			historyMenuItem("Delete Branch…", name != current, func() { w.deleteHistoryBranch(name, label.synced) }),
			menu.Sep(),
		)
		if current != "" && current != name {
			items = append(items, w.integrateHistoryRef(ref, name, current)...)
			items = append(items, menu.Sep())
		}
		items = append(items, historyMenuItem("Push Branch…", len(w.data.Remotes) > 0, func() { w.pushHistoryBranch(name) }))
		if current == name {
			items = append(items, historyMenuItem("Pull…", len(w.data.Remotes) > 0, func() {
				remote, branch := w.upstreamTarget()
				w.pullHistoryBranch(remote, branch, current)
			}))
		}
		items = append(items, historyMenuItem("Set Upstream…", slices.ContainsFunc(w.data.Branches, func(ref string) bool {
			return strings.HasPrefix(ref, repo.RemoteRefPrefix)
		}), func() { w.setHistoryUpstream(name) }))
		if label.synced != "" {
			remote := branchRemote(label.synced, w.data.Remotes)
			items = append(items, menu.Sep(), w.deleteRemoteHistoryItem(label.synced), w.fetchHistoryItem(remote),
				copyHistoryItem("Copy Remote Branch Name", shortRef(label.synced)))
		}
	case strings.HasPrefix(ref, repo.RemoteRefPrefix):
		remote := branchRemote(ref, w.data.Remotes)
		name := strings.TrimPrefix(shortRef(ref), remote+"/")
		checkout := "Checkout " + name
		exists := slices.Contains(w.data.Branches, repo.LocalRefPrefix+name)
		if !exists {
			checkout = "Checkout " + shortRef(ref) + " (create local branch)"
		}
		items = append(items, historyMenuItem(checkout, name != current, func() {
			if exists {
				w.switchBranch(repo.LocalRefPrefix + name)
				return
			}
			local := &formField{label: "Local branch name", value: name}
			w.historyDialog("Checkout "+shortRef(ref), "", "Checkout", []*formField{local}, func() {
				if newName, ok := w.historyRefName(local); ok {
					w.command("Checkout "+newName, "git", "switch", "--track", "-c", newName, "--", ref)
				}
			})
		}), w.deleteRemoteHistoryItem(ref), menu.Sep())
		if current != "" {
			items = append(items, w.integrateHistoryRef(ref, shortRef(ref), current)...)
			items = append(items, historyMenuItem("Pull into "+current+"…", slices.Contains(w.data.Remotes, remote), func() {
				w.pullHistoryBranch(remote, name, current)
			}), menu.Sep())
		}
		items = append(items, w.fetchHistoryItem(remote))
	default:
		copyLabel = "Copy Tag Name"
		items = append(items,
			menu.Item("Checkout Tag (detached HEAD)", "", func() {
				w.command("Checkout "+label.text, "git", "switch", "--detach", ref)
			}),
			historyMenuItem("Push Tag…", len(w.data.Remotes) > 0, func() {
				remote := choices("Remote", w.data.Remotes)
				w.historyDialog("Push tag "+label.text, "", "Push", []*formField{remote}, func() {
					w.command("Push tag "+label.text, "git", "push", "--", remote.value, ref)
				})
			}),
			menu.Item("Delete Tag…", "", func() { w.deleteHistoryTag(label.text, ref) }),
		)
	}
	return append(items, menu.Sep(), copyHistoryItem(copyLabel, label.text), copyHistoryItem("Copy Commit Hash", c.Hash))
}

func (w *workbench) integrateHistoryRef(ref, name, current string) []menu.MenuItem {
	return []menu.MenuItem{
		menu.Item("Merge into "+current+"…", "", func() { w.mergeHistory(ref, name, current) }),
		menu.Item("Rebase "+current+" on "+name+"…", "", func() { w.rebaseHistory(ref, name, current) }),
	}
}

func (w *workbench) historyRefName(field *formField) (string, bool) {
	name := strings.TrimSpace(field.value)
	if err := validName(name); err != nil {
		w.showError(err)
		return "", false
	}
	return name, true
}

func (w *workbench) renameHistoryBranch(name string) {
	newName := &formField{label: "New name", value: name}
	w.historyDialog("Rename "+name, "", "Rename", []*formField{newName}, func() {
		if next, ok := w.historyRefName(newName); ok && next != name {
			w.command("Rename "+name, "git", "branch", "-m", "--", name, next)
		}
	})
}

func (w *workbench) deleteHistoryBranch(name, synced string) {
	force := flagField("Force delete (even if not fully merged)", false)
	fields := []*formField{force}
	counterpart := synced
	if counterpart == "" {
		for _, ref := range w.data.Branches {
			if strings.HasPrefix(ref, repo.RemoteRefPrefix) && strings.TrimPrefix(shortRef(ref), branchRemote(ref, w.data.Remotes)+"/") == name {
				counterpart = ref
				break
			}
		}
	}
	remote := branchRemote(counterpart, w.data.Remotes)
	remoteName := strings.TrimPrefix(shortRef(counterpart), remote+"/")
	deleteRemote := flagField("Also delete "+shortRef(counterpart)+" on the remote", false)
	if counterpart != "" && slices.Contains(w.data.Remotes, remote) {
		fields = append(fields, deleteRemote)
	}
	w.historyDialog("Delete branch "+name+"?", "", "Delete", fields, func() {
		flag := "-d"
		if force.checked {
			flag = "-D"
		}
		w.deleteHistoryRef(name, []string{"branch", flag, "--", name}, remote, repo.LocalRefPrefix+remoteName, deleteRemote.checked)
	})
}

func (w *workbench) deleteHistoryTag(name, ref string) {
	deleteRemote := flagField("Also delete the tag on a remote", false)
	remote := choices("Remote", w.data.Remotes)
	var fields []*formField
	if len(w.data.Remotes) != 0 {
		fields = []*formField{deleteRemote, remote}
	}
	w.historyDialog("Delete tag "+name+"?", "", "Delete", fields, func() {
		w.deleteHistoryRef(name, []string{"tag", "-d", "--", name}, remote.value, ref, deleteRemote.checked)
	})
}

func (w *workbench) deleteHistoryRef(name string, args []string, remote, ref string, deleteRemote bool) {
	path := w.path
	w.changeRepository("Delete "+name, func(ctx context.Context) error {
		if _, err := repo.Run(ctx, path, "git", args...); err != nil {
			return err
		}
		if deleteRemote {
			_, err := repo.Run(ctx, path, "git", "push", "--", remote, ":"+ref)
			return err
		}
		return nil
	})
}

func (w *workbench) deleteRemoteHistoryItem(ref string) menu.MenuItem {
	remote := branchRemote(ref, w.data.Remotes)
	branch := strings.TrimPrefix(shortRef(ref), remote+"/")
	return historyMenuItem("Delete Remote Branch…", slices.Contains(w.data.Remotes, remote), func() {
		w.historyDialog("Delete "+shortRef(ref)+"?", "The branch will be deleted on "+remote+".", "Delete", nil, func() {
			w.command("Delete "+shortRef(ref), "git", "push", "--", remote, ":"+repo.LocalRefPrefix+branch)
		})
	})
}

func (w *workbench) fetchHistoryItem(remote string) menu.MenuItem {
	return historyMenuItem("Fetch "+remote, slices.Contains(w.data.Remotes, remote), func() {
		flag := "--prune"
		if w.prefs.Settings.DisablePrune {
			flag = "--no-prune"
		}
		w.command("Fetch "+remote, "git", "fetch", flag, "--", remote)
	})
}

func (w *workbench) setHistoryUpstream(name string) {
	var remotes []string
	for _, ref := range w.data.Branches {
		if strings.HasPrefix(ref, repo.RemoteRefPrefix) {
			remotes = append(remotes, shortRef(ref))
		}
	}
	upstream := choices("Upstream branch", remotes)
	for _, ref := range remotes {
		if strings.HasSuffix(ref, "/"+name) {
			upstream.value = ref
			break
		}
	}
	w.historyDialog("Set upstream for "+name, "", "Set Upstream", []*formField{upstream}, func() {
		w.command("Set upstream for "+name, "git", "branch", "--set-upstream-to="+repo.RemoteRefPrefix+upstream.value, "--", name)
	})
}

func (w *workbench) pushHistoryBranch(name string) {
	remote := choices("Remote", w.data.Remotes)
	target := &formField{label: "Remote branch", value: name}
	setUpstream := flagField("Set upstream", true)
	force := flagField("Force push with lease", false)
	if name == w.data.CurrentBranch() {
		if r, dest := w.upstreamTarget(); r != "" && slices.Contains(remote.choices, r) {
			remote.value, target.value, setUpstream.checked = r, dest, false
		}
	}
	w.historyDialog("Push "+name, "", "Push", []*formField{remote, target, setUpstream, force}, func() {
		if dest, ok := w.historyRefName(target); ok {
			w.pushBranch(remote.value, name, dest, setUpstream.checked, force.checked)
		}
	})
}

func (w *workbench) pullHistoryBranch(remote, name, current string) {
	remotes := choices("Remote", w.data.Remotes)
	if slices.Contains(remotes.choices, remote) {
		remotes.value = remote
	}
	if name == "" {
		name = current
	}
	branch := &formField{label: "Remote branch", value: name}
	rebase := flagField("Rebase instead of merge (--rebase)", false)
	w.historyDialog("Pull into "+current, "", "Pull", []*formField{remotes, branch, rebase}, func() {
		if dest, ok := w.historyRefName(branch); ok {
			w.pullBranch("Pull from "+remotes.value+"/"+dest, remotes.value, dest, rebase.checked)
		}
	})
}
