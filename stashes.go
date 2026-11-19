package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func (w *workbench) stashesPanel() (widget.Widget, widget.Widget) {
	newStash := w.btn("Stash changes…", button.TextOnly, w.data.Head != "" && len(w.data.Files) > 0, func() {
		w.selectPopupAction(w.stashCreateForm)
	})
	if len(w.data.Stashes) == 0 {
		panel := primitives.VBox(w.muted("No saved stashes"), newStash).Width(300).Padding(8).Gap(6)
		return panel, nil
	}
	choices := make([]popupChoice, 0, len(w.data.Stashes))
	for _, stash := range w.data.Stashes {
		choices = append(choices, popupChoice{
			label: stash.Selector + "  " + stash.Message, glyph: stashIcon,
			action: func() { w.stashDetails(stash) },
		})
	}
	return w.choicePanel(400, 28, "Search stashes", choices, newStash)
}

func (w *workbench) stashCreateForm() {
	msg := &formField{label: "Message (optional)"}
	untracked := flagField("Include untracked files", true)
	keepIndex := flagField("Keep staged changes", false)
	w.present("Stash changes", []*formField{msg, untracked, keepIndex}, formAction{"Stash", func() error {
		args := []string{"stash", "push"}
		if untracked.checked {
			args = append(args, "--include-untracked")
		} else if !hasTrackedChanges(w.data.Files) {
			return errors.New("enable Include untracked files to stash these changes")
		}
		if keepIndex.checked {
			args = append(args, "--keep-index")
		}
		if msg.value != "" {
			args = append(args, "-m", msg.value)
		}
		w.command("Stash changes", "git", args...)
		return nil
	}})
}

func hasTrackedChanges(files []repo.File) bool {
	for _, f := range files {
		if f.Index != '?' {
			return true
		}
	}
	return false
}

func (w *workbench) stashDetails(stash repo.Stash) {
	if w.busy {
		return
	}
	reinstateIndex := flagField("Restore staged state", false)
	restore := func(op, label string) func() error {
		return func() error {
			args := []string{op}
			if reinstateIndex.checked {
				args = append(args, "--index")
			}
			ref := stash.Selector
			if op == "apply" {
				ref = stash.Hash
			}
			w.stashCommand(label+" "+stash.Selector, stash, append(args, ref)...)
			return nil
		}
	}
	w.present(stash.Selector, []*formField{reinstateIndex},
		formAction{"Apply stash", restore("apply", "Apply")},
		formAction{"Pop stash", restore("pop", "Pop")},
		formAction{"Create branch from stash…", func() error { w.stashBranchForm(stash); return nil }},
		formAction{"Drop stash…", func() error { w.confirmDropStash(stash); return nil }},
		formAction{"Copy stash name", func() error { widget.ClipboardWrite(stash.Selector); return nil }},
		formAction{"Copy commit hash", func() error { widget.ClipboardWrite(stash.Hash); return nil }},
	)
	w.form.description = stash.Message
	w.selected.Set(-1)
	for i, commit := range w.rows {
		if commit.Hash == stash.Hash {
			row := i + w.uncommittedRowCount()
			w.selected.Set(row)
			w.table.ScrollToRow(row)
			break
		}
	}
}

func (w *workbench) stashBranchForm(stash repo.Stash) {
	branch := &formField{label: "Branch name"}
	w.present("Branch from "+stash.Selector, []*formField{branch}, formAction{"Create branch", func() error {
		name := strings.TrimSpace(branch.value)
		if err := validName(name); err != nil {
			return err
		}
		w.stashCommand("Create branch "+name, stash, "branch", name, stash.Selector)
		return nil
	}})
}

func (w *workbench) confirmDropStash(stash repo.Stash) {
	d := dialog.New(dialog.Title("Drop "+stash.Selector+"?"), dialog.Content(w.text(stash.Message)),
		dialog.Actions(
			dialog.Action{Label: "Cancel", Default: true, Variant: dialog.VariantTextOnly},
			dialog.Action{Label: "Drop stash", Variant: dialog.VariantFilled, OnClick: func() {
				w.stashCommand("Drop "+stash.Selector, stash, "drop", stash.Selector)
			}},
		), dialog.PainterOpt(w.paint.Dialog))
	d.Show(w.ui.Window().Context())
}

func (w *workbench) stashCommand(label string, stash repo.Stash, args ...string) {
	path := w.path
	w.changeRepository(label, func(ctx context.Context) error {
		// Reflog selectors can change if another client creates or drops a stash.
		hash, err := repo.Run(ctx, path, "git", "rev-parse", "--verify", "--end-of-options", stash.Selector+"^{commit}")
		if err != nil || strings.TrimSpace(hash) != stash.Hash {
			return fmt.Errorf("%s changed. Open the stash menu and select it again", stash.Selector)
		}
		_, err = repo.Run(ctx, path, "git", append([]string{"stash"}, args...)...)
		return err
	})
}
