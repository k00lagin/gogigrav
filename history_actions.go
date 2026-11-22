package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func (w *workbench) historyDialog(title, description, submit string, fields []*formField, action func()) {
	if w.busy {
		return
	}
	ctx := w.ui.Window().Context()
	previous := ctx.FocusedWidget()
	var editor *dialog.Widget
	content := []widget.Widget{w.dialogHeader(title, func() { editor.Close(ctx) }), line(w)}
	if description != "" {
		content = append(content, w.text(description))
	}
	content = append(content, w.formFields(fields)...)
	content = append(content, line(w), row(primitives.Expanded(primitives.Box()),
		w.btn("Cancel", button.TextOnly, true, func() { editor.Close(ctx) }),
		w.btn(submit, button.Filled, true, func() {
			editor.Close(ctx)
			if !w.busy {
				action()
			}
		})))
	editor = dialog.New(dialog.ContentOnly(),
		dialog.Content(primitives.VBox(content...).Width(540).Padding(16).Gap(8).CrossAlign(primitives.CrossAxisStretch)),
		dialog.MaxWidth(540), dialog.PainterOpt(w.paint.Dialog),
		dialog.OnClose(func() {
			if previous != nil && !widget.IsDetached(previous) {
				ctx.RequestFocus(previous)
			}
		}),
	)
	editor.Show(ctx)
}

func (w *workbench) pickHistoryCommit(c repo.Commit, revert bool) {
	parents := strings.Fields(c.Parents)
	var fields []*formField
	var mainline *formField
	if len(parents) > 1 {
		options := make([]string, 0, len(parents))
		for i, hash := range parents {
			options = append(options, fmt.Sprintf("%d · %s", i+1, hash[:min(len(hash), 8)]))
		}
		mainline = choices("Mainline parent (merge commit)", options)
		fields = append(fields, mainline)
	}
	origin := flagField("Record origin commit in message (-x)", false)
	noCommit := flagField("Do not commit automatically (--no-commit)", false)
	label, description := "Cherry-pick", c.Subject
	if revert {
		label, description = "Revert", fmt.Sprintf("Create a new commit that reverts %q.", c.Subject)
	} else {
		fields = append(fields, origin, noCommit)
	}
	w.historyDialog(label+" "+c.Short(), description, label, fields, func() {
		args := []string{"cherry-pick"}
		if revert {
			args = []string{"revert", "--no-edit"}
		}
		if origin.checked {
			args = append(args, "-x")
		}
		if noCommit.checked {
			args = append(args, "--no-commit")
		}
		if mainline != nil {
			args = append(args, "-m", strconv.Itoa(slices.Index(mainline.choices, mainline.value)+1))
		}
		w.command(label+" "+c.Short(), "git", append(args, c.Hash)...)
	})
}

// A local destination may need to be checked out before Git can merge into it.
func (w *workbench) mergeHistory(ref, name, into string) {
	noFF := flagField("Create a merge commit even if fast-forward is possible (--no-ff)", false)
	squash := flagField("Squash commits (--squash)", false)
	noCommit := flagField("Do not commit automatically (--no-commit)", false)
	current, path := w.data.CurrentBranch(), w.path
	w.historyDialog("Merge into "+into, fmt.Sprintf("Merge %q into %q.", name, into), "Merge",
		[]*formField{noFF, squash, noCommit}, func() {
			args := []string{"merge"}
			if squash.checked && noFF.checked {
				w.showError(errors.New("--squash and --no-ff cannot be combined"))
				return
			}
			if noFF.checked {
				args = append(args, "--no-ff")
			}
			if squash.checked {
				args = append(args, "--squash")
			}
			if noCommit.checked {
				args = append(args, "--no-commit")
			}
			w.changeRepository("Merge "+name+" into "+into, func(ctx context.Context) error {
				if into != current {
					if _, err := repo.Run(ctx, path, "git", "switch", "--", into); err != nil {
						return err
					}
				}
				if err := checkHistoryHead(ctx, path, into, ""); err != nil {
					return err
				}
				_, err := repo.Run(ctx, path, "git", append(args, "--", ref)...)
				return err
			})
		})
}

func (w *workbench) rebaseHistory(ref, name, branch string) {
	preserve := flagField("Preserve merge commits (--rebase-merges)", false)
	current, path := w.data.CurrentBranch(), w.path
	w.historyDialog("Rebase "+branch, fmt.Sprintf("Rebase %q on %q.", branch, name), "Rebase",
		[]*formField{preserve}, func() {
			args := []string{"rebase"}
			if preserve.checked {
				args = append(args, "--rebase-merges")
			}
			args = append(args, "--", ref)
			if branch != current {
				args = append(args, branch)
			}
			w.changeRepository("Rebase "+branch+" on "+name, func(ctx context.Context) error {
				if branch == current {
					if err := checkHistoryHead(ctx, path, current, ""); err != nil {
						return err
					}
				}
				_, err := repo.Run(ctx, path, "git", args...)
				return err
			})
		})
}

func checkHistoryHead(ctx context.Context, path, branch, hash string) error {
	want := "HEAD"
	if branch != "" {
		want = repo.LocalRefPrefix + branch
	}
	got, err := repo.Run(ctx, path, "git", "rev-parse", "--symbolic-full-name", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(got) != want {
		return errors.New("the checked-out branch changed")
	}
	if hash != "" {
		got, err = repo.Run(ctx, path, "git", "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		if strings.TrimSpace(got) != hash {
			return errors.New("HEAD changed. Reopen the action and try again")
		}
	}
	return nil
}

func (w *workbench) resetHistory(c repo.Commit) {
	mode := choices("Reset mode", []string{"Soft · Keep changes staged", "Mixed · Keep changes unstaged", "Hard · Discard tracked changes"})
	mode.value = mode.choices[1]
	path, branch, head := w.path, w.data.CurrentBranch(), w.data.Head
	title := "Reset " + historyBranchName(branch, "HEAD") + " to " + c.Short()
	w.historyDialog(title, c.Subject, "Reset", []*formField{mode}, func() {
		flag := [...]string{"--soft", "--mixed", "--hard"}[slices.Index(mode.choices, mode.value)]
		w.changeRepository(title, func(ctx context.Context) error {
			if err := checkHistoryRewrite(ctx, path, branch, head); err != nil {
				return err
			}
			if flag == "--hard" {
				if err := saveResetChanges(ctx, path); err != nil {
					return err
				}
			}
			_, err := repo.Run(ctx, path, "git", "reset", flag, c.Hash)
			return err
		})
	})
}

func checkHistoryRewrite(ctx context.Context, path, branch, head string) error {
	if err := checkHistoryHead(ctx, path, branch, head); err != nil {
		return err
	}
	gitPaths, err := repo.Run(ctx, path, "git", "rev-parse", "--path-format=absolute",
		"--git-path", "MERGE_HEAD", "--git-path", "rebase-merge", "--git-path", "rebase-apply",
		"--git-path", "CHERRY_PICK_HEAD", "--git-path", "REVERT_HEAD", "--git-path", "sequencer")
	if err != nil {
		return err
	}
	for _, marker := range strings.Split(strings.TrimRight(gitPaths, "\r\n"), "\n") {
		if _, err := os.Stat(strings.TrimSuffix(marker, "\r")); err == nil {
			return errors.New("finish or abort the Git operation in progress first")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func saveResetChanges(ctx context.Context, path string) error {
	message := "gogigrav: before hard reset"
	hash, err := repo.Run(ctx, path, "git", "stash", "create", message)
	if err != nil || strings.TrimSpace(hash) == "" {
		return err
	}
	_, err = repo.Run(ctx, path, "git", "stash", "store", "-m", message, strings.TrimSpace(hash))
	return err
}

func (w *workbench) dropHistoryCommit(c repo.Commit) {
	path, branch, head := w.path, w.data.CurrentBranch(), w.data.Head
	parent := strings.Fields(c.Parents)[0]
	title := "Drop commit " + c.Short()
	description := fmt.Sprintf("%q will be removed from %q by rebasing later commits. Use this only for unpublished commits.", c.Subject, branch)
	w.historyDialog(title+"?", description, "Drop commit", nil, func() {
		w.changeRepository(title, func(ctx context.Context) error {
			if err := checkHistoryRewrite(ctx, path, branch, head); err != nil {
				return err
			}
			chain, err := repo.Run(ctx, path, "git", "rev-list", "--first-parent", head, "^"+parent)
			if err != nil {
				return err
			}
			if !slices.Contains(strings.Fields(chain), c.Hash) {
				return errors.New("the selected commit is outside the checked-out branch's first-parent history")
			}
			_, err = repo.Run(ctx, path, "git", "rebase", "--rebase-merges", "--onto", parent, c.Hash)
			return err
		})
	})
}
