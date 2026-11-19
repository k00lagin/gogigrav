package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

type formField struct {
	label, value string
	choices      []string
}
type formAction struct {
	label string
	run   func() error
}
type actionForm struct {
	title   string
	fields  []*formField
	actions []formAction
}

func choices(label string, values []string) *formField {
	f := &formField{label: label, choices: append([]string{}, values...)}
	if len(values) > 0 {
		f.value = values[0]
	}
	return f
}
func (w *workbench) present(title string, fields []*formField, actions ...formAction) {
	if w.busy {
		return
	}
	w.form = &actionForm{title, fields, actions}
	w.page = "form"
	w.redraw()
}
func (w *workbench) formPanel() widget.Widget {
	var children []widget.Widget
	for _, f := range w.form.fields {
		children = append(children, w.muted(f.label))
		if f.choices != nil {
			children = append(children, w.pick(f.label, f.choices, slices.Index(f.choices, f.value), func(i int) { f.value = f.choices[i] }))
		} else {
			children = append(children, w.input(f.label, f.value, func(s string) { f.value = s }, nil))
		}
	}
	for i, a := range w.form.actions {
		v := button.Outlined
		if i == 0 {
			v = button.Filled
		}
		children = append(children, w.btn(a.label, v, true, func() {
			if err := a.run(); err != nil {
				w.showError(err)
			}
		}))
	}
	return scrollview.New(primitives.VBox(children...).Gap(8).Padding(12), scrollview.PainterOpt(w.paint.Scrollbar))
}

func (w *workbench) openPanel() widget.Widget {
	path := w.drafts["openPath"]
	open := func(s string) {
		p := strings.TrimSpace(s)
		if p != "" {
			w.discover([]string{p})
		}
	}
	field := w.input("Repository or parent folder", path, func(s string) { w.drafts["openPath"] = s }, open)
	return primitives.VBox(w.muted("Local folder"), field, row(
		w.btn("Open", button.Filled, true, func() { open(field.Text()) }),
		w.btn("Initialize…", button.Outlined, true, func() {
			p, err := filepath.Abs(strings.TrimSpace(field.Text()))
			if err != nil || strings.TrimSpace(field.Text()) == "" {
				w.showError(errors.New("enter an existing folder"))
				return
			}
			w.present("Initialize Git repository", []*formField{{label: "Directory", value: p}}, formAction{"Initialize", func() error {
				dst := strings.TrimSpace(w.form.fields[0].value)
				if dst == "" {
					return errors.New("enter an existing directory")
				}
				w.job("Initialize repository", func(ctx context.Context) (func(), error) {
					if root, err := repo.Root(ctx, dst); err == nil {
						return nil, fmt.Errorf("already part of worktree %s", root)
					}
					_, err := repo.Run(ctx, dst, "git", "init")
					return func() { w.discover([]string{dst}) }, err
				})
				return nil
			}})
		}), w.btn("Clone from GitHub…", button.Outlined, true, func() { w.cloneForm(field.Text()) }))).Gap(8).Padding(14)
}

var slugPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)

func (w *workbench) cloneForm(path string) {
	slug := &formField{label: "GitHub repository (owner/name)"}
	dir := &formField{label: "Destination folder", value: path}
	w.present("Clone from GitHub", []*formField{slug, dir}, formAction{"Clone", func() error {
		name, dst := strings.TrimSpace(slug.value), strings.TrimSpace(dir.value)
		if !slugPattern.MatchString(name) || dst == "" {
			return errors.New("enter owner/name and a destination folder")
		}
		dst, err := filepath.Abs(dst)
		if err != nil {
			return err
		}
		w.job("Clone from GitHub", func(ctx context.Context) (func(), error) {
			_, err := repo.Run(ctx, "", "gh", "repo", "clone", name, dst)
			return func() { w.discover([]string{dst}) }, err
		})
		return nil
	}})
}
func (w *workbench) publishForm() {
	slug := &formField{label: "GitHub repository (owner/name)"}
	visibility := choices("Visibility", []string{"Private", "Public"})
	path := w.path
	w.present("Publish to GitHub", []*formField{slug, visibility}, formAction{"Create repository and push", func() error {
		name := strings.TrimSpace(slug.value)
		if !slugPattern.MatchString(name) {
			return errors.New("enter a GitHub repository as owner/name")
		}
		w.command("Publish to GitHub", "gh", "repo", "create", name, "--"+strings.ToLower(visibility.value), "--source", path, "--remote", "origin", "--push")
		return nil
	}}, formAction{"Check GitHub sign-in", func() error {
		w.checkGitHub()
		return nil
	}})
}
func (w *workbench) checkGitHub() {
	path := w.path
	w.job("GitHub sign-in", func(ctx context.Context) (func(), error) {
		login, err := repo.Run(ctx, path, "gh", "api", "user", "--jq", ".login")
		return func() { w.status = "GitHub: " + strings.TrimSpace(login) }, err
	})
}
func validName(name string) error {
	if strings.TrimSpace(name) == "" || strings.HasPrefix(name, "-") {
		return errors.New("enter a name that does not start with a dash")
	}
	return nil
}
func (w *workbench) branchAt(ref string) {
	name := &formField{label: "New branch"}
	w.present("Branch from "+ref[:min(8, len(ref))], []*formField{name}, formAction{"Create and switch", func() error {
		if err := validName(name.value); err != nil {
			return err
		}
		w.command("Create branch", "git", "switch", "-c", name.value, ref)
		return nil
	}})
}
func (w *workbench) branchForm() {
	name := &formField{label: "New branch from HEAD"}
	existing := choices("Existing branch", w.data.Branches)
	current := "refs/heads/" + w.data.Branch
	if slices.Contains(existing.choices, current) {
		existing.value = current
	}
	w.present("Branches", []*formField{name, existing}, formAction{"Create and switch", func() error {
		if err := validName(name.value); err != nil {
			return err
		}
		w.command("Create branch", "git", "switch", "-c", name.value, "HEAD")
		return nil
	}}, formAction{"Switch to selected branch", func() error {
		if existing.value == "" {
			return errors.New("select a branch")
		}
		if strings.HasPrefix(existing.value, "refs/remotes/") {
			w.command("Track remote branch", "git", "switch", "--track", existing.value)
		} else {
			w.command("Switch branch", "git", "switch", "--", shortRef(existing.value))
		}
		return nil
	}})
}
func (w *workbench) tagAt(ref string) {
	name := &formField{label: "Tag name"}
	w.present("Tag "+ref[:min(8, len(ref))], []*formField{name}, formAction{"Create tag", func() error {
		if err := validName(name.value); err != nil {
			return err
		}
		w.command("Create tag", "git", "tag", "--", name.value, ref)
		return nil
	}})
}
func (w *workbench) stashForm() {
	msg := &formField{label: "Stash message (optional)"}
	list := choices("Saved stashes", w.data.Stashes)
	actions := []formAction{{"Stash working changes", func() error {
		if len(w.data.Files) == 0 {
			return errors.New("there are no changes to stash")
		}
		args := []string{"stash", "push", "--include-untracked"}
		if msg.value != "" {
			args = append(args, "-m", msg.value)
		}
		w.command("Stash changes", "git", args...)
		return nil
	}}}
	if len(list.choices) > 0 {
		for _, op := range []string{"apply", "pop"} {
			actions = append(actions, formAction{strings.ToUpper(op[:1]) + op[1:] + " selected stash", func() error {
				ref, _, _ := strings.Cut(list.value, " ")
				w.command("Restore stash", "git", "stash", op, ref)
				return nil
			}})
		}
	}
	w.present("Stashes", []*formField{msg, list}, actions...)
}
func (w *workbench) syncBranch(push bool) {
	label, sub := "Pull", "pull"
	if push {
		label, sub = "Push", "push"
	}
	if w.data.Upstream != "" {
		if push {
			w.command(label, "git", "push")
		} else {
			w.command(label, "git", "pull", "--ff-only")
		}
		return
	}
	remote := choices("Remote", w.data.Remotes)
	branch := &formField{label: "Remote branch", value: w.data.Branch}
	w.present(label, []*formField{remote, branch}, formAction{label, func() error {
		if remote.value == "" {
			return errors.New("select a remote")
		}
		if err := validName(branch.value); err != nil {
			return err
		}
		if push {
			w.command(label, "git", sub, "--set-upstream", "--", remote.value, "HEAD:refs/heads/"+branch.value)
		} else {
			w.command(label, "git", sub, "--ff-only", "--", remote.value, branch.value)
		}
		return nil
	}})
}
func (w *workbench) remotesForm() {
	name := &formField{label: "New remote name", value: "origin"}
	url := &formField{label: "Remote URL"}
	remote := choices("Existing remote", w.data.Remotes)
	actions := []formAction{{"Add remote", func() error {
		if err := validName(name.value); err != nil {
			return err
		}
		if strings.TrimSpace(url.value) == "" {
			return errors.New("enter a remote URL")
		}
		w.command("Add remote", "git", "remote", "add", "--", name.value, url.value)
		return nil
	}}}
	if len(remote.choices) > 0 {
		actions = append(actions, formAction{"Show selected URL", func() error {
			path, r := w.path, remote.value
			w.job("Read remote URL", func(ctx context.Context) (func(), error) {
				out, err := repo.Run(ctx, path, "git", "remote", "get-url", "--", r)
				return func() { url.value = strings.TrimSpace(out) }, err
			})
			return nil
		}}, formAction{"Update selected URL", func() error {
			if strings.TrimSpace(url.value) == "" {
				return errors.New("enter a remote URL")
			}
			w.command("Update remote", "git", "remote", "set-url", "--", remote.value, url.value)
			return nil
		}})
	} else {
		actions = append(actions, formAction{"Publish to GitHub…", func() error { w.publishForm(); return nil }})
	}
	w.present("Remotes", []*formField{remote, name, url}, actions...)
}
