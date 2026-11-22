package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func (w *workbench) stashesPanel() (widget.Widget, widget.Widget) {
	newStash := w.dropdownActionButton(stashChangesIcon, "Stash changes…", w.data.Head != "" && len(w.data.Files) > 0, func() {
		w.selectPopupAction(w.stashCreateForm)
	})
	if len(w.data.Stashes) == 0 {
		panel := primitives.VBox(
			primitives.VBox(w.muted("No saved stashes")).PaddingXY(12, 10),
			w.dropdownFooter(newStash),
		).Width(300).CrossAlign(primitives.CrossAxisStretch)
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
	if w.busy || w.data.Head == "" || len(w.data.Files) == 0 {
		return
	}
	w.clearToolbarTooltip()
	ctx := w.ui.Window().Context()
	previousFocus := ctx.FocusedWidget()
	var editor *dialog.Widget
	untracked, keepIndex := true, false
	message := state.NewSignal("")
	submit := func(text string) {
		if w.busy || !editor.IsOpen() {
			return
		}
		args := []string{"stash", "push"}
		if untracked {
			args = append(args, "--include-untracked")
		} else if !hasTrackedChanges(w.data.Files) {
			message.Set("Enable Include untracked files to stash these changes.")
			return
		}
		if keepIndex {
			args = append(args, "--keep-index")
		}
		if text != "" {
			args = append(args, "-m", text)
		}
		editor.Close(ctx)
		w.command("Stash changes", "git", args...)
	}
	input := w.input("Message (optional)", "", nil, submit)
	panel := primitives.VBox(
		w.dialogHeader("Stash changes", func() { editor.Close(ctx) }),
		line(w), w.muted("Message (optional)"), compactInput(input),
		w.check("Include untracked files", true, true, func(on bool) {
			untracked = on
			message.Set("")
		}),
		w.check("Keep staged changes in the index (--keep-index)", false, true, func(on bool) { keepIndex = on }),
		w.text("").ContentSignal(message).Color(w.theme.Colors.Error),
		line(w),
		row(primitives.Expanded(primitives.Box()),
			w.btn("Cancel", button.TextOnly, true, func() { editor.Close(ctx) }),
			w.btn("Stash", button.Filled, true, func() { submit(input.Text()) })),
	).Width(460).Gap(8).Padding(16).CrossAlign(primitives.CrossAxisStretch)
	editor = dialog.New(dialog.ContentOnly(), dialog.Content(panel), dialog.MaxWidth(460), dialog.PainterOpt(w.paint.Dialog),
		dialog.OnClose(func() {
			if previousFocus != nil && !widget.IsDetached(previousFocus) {
				ctx.RequestFocus(previousFocus)
			}
		}))
	editor.Show(ctx)
	ctx.RequestFocus(input)
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
	if w.busy || w.path == "" {
		return
	}
	w.clearToolbarTooltip()
	if w.table != nil {
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
	ctx := w.ui.Window().Context()
	previousFocus := ctx.FocusedWidget()
	var editor *dialog.Widget
	reinstateIndex := false
	closeDialog := func() { editor.Close(ctx) }
	restore := func(op, label string) func() {
		return func() {
			if w.busy || !editor.IsOpen() {
				return
			}
			args := []string{op}
			if reinstateIndex {
				args = append(args, "--index")
			}
			ref := stash.Selector
			if op == "apply" {
				ref = stash.Hash
			}
			editor.Close(ctx)
			w.stashCommand(label+" "+stash.Selector, stash, append(args, ref)...)
		}
	}
	applyBtn := w.btn("Apply stash", button.Filled, true, restore("apply", "Apply"))
	children := []widget.Widget{
		w.dialogHeader(stash.Selector, closeDialog),
		line(w),
	}
	if stash.Message != "" {
		children = append(children, w.muted(stash.Message).MaxLines(3))
	}
	children = append(children,
		w.check("Restore staged state", false, true, func(on bool) { reinstateIndex = on }),
		row(applyBtn,
			w.btn("Pop stash", button.Outlined, true, restore("pop", "Pop")),
			w.btn("Create branch from stash…", button.Outlined, true, func() {
				editor.Close(ctx)
				w.stashBranchForm(stash)
			}),
			w.btn("Drop stash…", button.Outlined, true, func() {
				editor.Close(ctx)
				w.confirmDropStash(stash)
			})),
		line(w),
		row(
			w.btn("Copy stash name", button.TextOnly, true, func() {
				widget.ClipboardWrite(stash.Selector)
				editor.Close(ctx)
			}),
			w.btn("Copy commit hash", button.TextOnly, true, func() {
				widget.ClipboardWrite(stash.Hash)
				editor.Close(ctx)
			}),
			primitives.Expanded(primitives.Box()),
		),
	)
	panel := primitives.VBox(children...).Width(520).Gap(8).Padding(16).CrossAlign(primitives.CrossAxisStretch)
	editor = dialog.New(dialog.ContentOnly(), dialog.Content(panel), dialog.MaxWidth(520), dialog.PainterOpt(w.paint.Dialog),
		dialog.OnClose(func() {
			if previousFocus != nil && !widget.IsDetached(previousFocus) {
				ctx.RequestFocus(previousFocus)
			}
		}))
	editor.Show(ctx)
	ctx.RequestFocus(applyBtn)
}

func (w *workbench) stashBranchForm(stash repo.Stash) {
	if w.busy || w.path == "" {
		return
	}
	branch := &formField{label: "Branch name"}
	w.formDialog(formDialogOptions{
		title:       "Branch from " + stash.Selector,
		description: stash.Message,
		submit:      "Create branch",
		fields:      []*formField{branch},
		onSubmit: func() (func(), error) {
			name := strings.TrimSpace(branch.value)
			if err := validName(name); err != nil {
				return nil, err
			}
			return func() { w.stashCommand("Create branch "+name, stash, "branch", name, stash.Selector) }, nil
		},
	})
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
