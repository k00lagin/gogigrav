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
	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

type formField struct {
	label, value string
	choices      []string
	checkbox     bool
	checked      bool
}
type formAction struct {
	label string
	run   func() error
}
type actionForm struct {
	title, description string
	fields             []*formField
	actions            []formAction
}

func choices(label string, values []string) *formField {
	f := &formField{label: label, choices: append([]string{}, values...)}
	if len(values) > 0 {
		f.value = values[0]
	}
	return f
}
func flagField(label string, checked bool) *formField {
	return &formField{label: label, checkbox: true, checked: checked}
}

func (w *workbench) present(title string, fields []*formField, actions ...formAction) {
	if w.busy {
		return
	}
	w.form = &actionForm{title: title, fields: fields, actions: actions}
	w.page = "form"
	w.redraw()
}
func (w *workbench) formPanel() widget.Widget {
	var children []widget.Widget
	if w.form.description != "" {
		children = append(children, w.muted(w.form.description))
	}
	for _, f := range w.form.fields {
		if f.checkbox {
			children = append(children, w.check(f.label, f.checked, true, func(value bool) { f.checked = value }))
			continue
		}
		children = append(children, w.muted(f.label))
		if f.choices != nil {
			children = append(children, w.pick(f.label, f.choices, slices.Index(f.choices, f.value), func(i int) { f.value = f.choices[i] }))
		} else {
			children = append(children, compactInput(w.input(f.label, f.value, func(s string) { f.value = s }, nil)))
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
	return primitives.VBox(w.muted("Local folder"), compactInput(field), row(
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
	if w.busy || w.path == "" || ref == "" {
		return
	}
	w.clearToolbarTooltip()
	ctx := w.ui.Window().Context()
	previousFocus := ctx.FocusedWidget()
	var editor *dialog.Widget
	checkout := true
	message := state.NewSignal("")
	submit := func(value string) {
		if w.busy || !editor.IsOpen() {
			return
		}
		name := strings.TrimSpace(value)
		if err := validName(name); err != nil {
			message.Set(err.Error())
			return
		}
		args := []string{"branch", "--", name, ref}
		if checkout {
			args = []string{"switch", "-c", name, ref}
		}
		editor.Close(ctx)
		w.command("Create branch "+name, "git", args...)
	}
	name := w.input("feature/my-branch", "", func(string) { message.Set("") }, submit)
	panel := primitives.VBox(
		w.text("Create Branch").Bold().FontSize(15),
		line(w), w.muted("From commit "+ref[:min(8, len(ref))]),
		w.muted("Branch name"), compactInput(name),
		w.check("Check out after creating", true, true, func(on bool) { checkout = on }),
		w.text("").ContentSignal(message).Color(w.theme.Colors.Error),
		row(primitives.Expanded(primitives.Box()),
			w.btn("Cancel", button.TextOnly, true, func() { editor.Close(ctx) }),
			w.btn("Create Branch", button.Filled, true, func() { submit(name.Text()) })),
	).Width(460).Gap(8).Padding(16).CrossAlign(primitives.CrossAxisStretch)
	editor = dialog.New(dialog.ContentOnly(), dialog.Content(panel), dialog.MaxWidth(460), dialog.PainterOpt(w.paint.Dialog),
		dialog.OnClose(func() {
			if previousFocus != nil && !widget.IsDetached(previousFocus) {
				ctx.RequestFocus(previousFocus)
			}
		}))
	editor.Show(ctx)
	ctx.RequestFocus(name)
}
func (w *workbench) switchBranch(ref string) {
	if strings.HasPrefix(ref, repo.RemoteRefPrefix) {
		w.command("Track remote branch", "git", "switch", "--track", "--", ref)
	} else if branch, ok := strings.CutPrefix(ref, repo.LocalRefPrefix); ok && branch != w.data.CurrentBranch() {
		w.command("Switch to "+branch, "git", "switch", "--", branch)
	}
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
func (w *workbench) syncBranch(push bool, openOptions func()) {
	if w.busy || len(w.data.Remotes) == 0 {
		return
	}
	branch := w.data.CurrentBranch()
	if !push && branch != "" && w.data.Upstream != "" {
		w.pullBranch("Pull from "+w.data.Upstream, "", "", false)
		return
	}
	if push {
		if branch == "" || w.data.Head == "" {
			return
		}
		if remote, target := w.upstreamTarget(); remote != "" {
			w.pushBranch(remote, branch, target, false, false)
			return
		}
	}
	openOptions()
}

func (w *workbench) upstreamTarget() (string, string) {
	target, ok := strings.CutPrefix(w.data.UpstreamRef, repo.LocalRefPrefix)
	if ok && slices.Contains(w.data.Remotes, w.data.UpstreamRemote) {
		return w.data.UpstreamRemote, target
	}
	return "", ""
}

func (w *workbench) pushBranch(remote, source, target string, setUpstream, forceWithLease bool) {
	path := w.path
	w.changeRepository("Push "+source+" to "+remote+"/"+target, func(ctx context.Context) error {
		return repo.Push(ctx, path, remote, source, target, setUpstream, forceWithLease)
	})
}

func (w *workbench) pullBranch(label, remote, target string, rebase bool) {
	path := w.path
	w.changeRepository(label, func(ctx context.Context) error {
		return repo.Pull(ctx, path, remote, target, rebase)
	})
}

func (w *workbench) syncOptionsPanel(push bool) (widget.Widget, widget.Widget) {
	current := w.data.CurrentBranch()
	remotes := w.data.Remotes
	upstream, branch := w.upstreamTarget()
	remote := upstream
	if remote == "" && len(remotes) != 0 {
		remote, branch = remotes[0], current
		if slices.Contains(remotes, "origin") {
			remote = "origin"
		}
	}
	label, title := "Pull", "Pull options"
	track, force, rebase := upstream == "", false, false
	var flags []widget.Widget
	if push {
		label, title = "Push", "Push "+current
		flags = append(flags, w.check("Set upstream", track, true, func(on bool) { track = on }),
			w.check("Force push with lease", force, true, func(on bool) { force = on }))
	} else {
		flags = append(flags, w.check("Rebase instead of merge", rebase, true, func(on bool) { rebase = on }))
	}
	message := state.NewSignal("")
	submit := func(target string) {
		target = strings.TrimSpace(target)
		if remote == "" {
			message.Set("Select a remote.")
			return
		}
		if err := validName(target); err != nil {
			message.Set(err.Error())
			return
		}
		w.selectPopupAction(func() {
			if push {
				w.pushBranch(remote, current, target, track, force)
			} else {
				w.pullBranch("Pull from "+remote+"/"+target, remote, target, rebase)
			}
		})
	}
	dest := w.input("Remote branch", branch, nil, submit)
	children := []widget.Widget{
		w.text(title).Bold(),
		w.muted("Remote"), w.pick("Remote", remotes, slices.Index(remotes, remote), func(i int) { remote = remotes[i] }),
		w.muted("Remote branch"), compactInput(dest),
	}
	children = append(children, flags...)
	children = append(children, row(
		primitives.Expanded(w.text("").Color(w.theme.Colors.Error).ContentSignal(message)),
		w.btn(label, button.Filled, true, func() { submit(dest.Text()) }),
	))
	return primitives.VBox(children...).Width(320).Padding(12).Gap(6).CrossAlign(primitives.CrossAxisStretch), dest
}
func (w *workbench) addRemoteForm() {
	name := &formField{label: "Remote name"}
	if len(w.data.Remotes) == 0 {
		name.value = "origin"
	}
	url := &formField{label: "Remote URL"}
	w.present("Add remote", []*formField{name, url}, formAction{"Add remote", func() error {
		if err := validName(name.value); err != nil {
			return err
		}
		if strings.TrimSpace(url.value) == "" {
			return errors.New("enter a remote URL")
		}
		w.command("Add remote", "git", "remote", "add", "--", name.value, url.value)
		return nil
	}})
}

func (w *workbench) editRemoteForm(name string) {
	path := w.path
	w.job("Read remote URL", func(ctx context.Context) (func(), error) {
		value, err := repo.Run(ctx, path, "git", "remote", "get-url", "--", name)
		return func() {
			url := &formField{label: "Remote URL", value: strings.TrimSpace(value)}
			w.present("Edit "+name, []*formField{url}, formAction{"Save URL", func() error {
				value := strings.TrimSpace(url.value)
				if value == "" {
					return errors.New("enter a remote URL")
				}
				w.command("Update remote", "git", "remote", "set-url", "--", name, value)
				return nil
			}})
		}, err
	})
}
