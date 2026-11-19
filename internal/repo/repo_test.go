package repo

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestIndexHistoryAndRenames(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		s, err := Run(t.Context(), dir, "git", args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(s)
	}
	put := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-b", "main")
	git("config", "user.name", "Tester")
	git("config", "user.email", "tester@example.test")
	git("config", "commit.gpgsign", "false")
	load := func(limit int) Snapshot {
		t.Helper()
		s, err := Load(t.Context(), dir, Filter{Limit: limit, Remote: true})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	put("a [1].txt", "one\n")
	put("a 1.txt", "other\n")
	s := load(10)
	if s.Branch != "main" || s.Head != "" || len(s.Files) != 2 || len(s.Commits) != 0 {
		t.Fatalf("unborn: %+v", s)
	}
	selected := File{Path: "a [1].txt"}
	index := func(stage, unborn bool) {
		t.Helper()
		if err := ChangeIndex(t.Context(), dir, selected, stage, unborn); err != nil {
			t.Fatal(err)
		}
	}
	index(true, true)
	s = load(10)
	if n := len(slices.DeleteFunc(s.Files, func(f File) bool { return !f.Staged() })); n != 1 {
		t.Fatalf("staged %d files, want only literal path", n)
	}
	index(false, true)
	if files := load(10).Files; slices.ContainsFunc(files, File.Staged) {
		t.Fatal(files)
	}
	index(true, true)
	git("commit", "-m", "Root")
	s = load(10)
	if len(s.Commits) != 1 {
		t.Fatal(s.Commits)
	}
	paths, err := ChangedPaths(t.Context(), dir, s.Commits[0])
	if err != nil || len(paths) != 1 || paths[0].Path != selected.Path {
		t.Fatal(paths, err)
	}
	git("mv", "--", selected.Path, "renamed.txt")
	s = load(10)
	i := slices.IndexFunc(s.Files, func(f File) bool { return f.Index == 'R' })
	if i < 0 {
		t.Fatal(s.Files)
	}
	selected = s.Files[i]
	if selected.From != "a [1].txt" || selected.Path != "renamed.txt" {
		t.Fatal(selected)
	}
	index(false, false)
	if files := load(10).Files; slices.ContainsFunc(files, File.Staged) {
		t.Fatal(files)
	}
	git("add", "-A")
	git("commit", "-m", "Rename", "-m", "Line one\nLine two")
	s = load(1)
	if !s.More || len(s.Commits) != 1 || s.Commits[0].Body != "Line one\nLine two" {
		t.Fatalf("paged history: %+v", s.Commits)
	}
	paths, err = ChangedPaths(t.Context(), dir, s.Commits[0])
	if err != nil || !slices.ContainsFunc(paths, func(f File) bool { return f.From == "a [1].txt" && f.Path == "renamed.txt" }) {
		t.Fatal(paths, err)
	}
	git("branch", "older", "HEAD~")
	s, err = Load(t.Context(), dir, Filter{Ref: "refs/heads/older", Limit: 10})
	if err != nil || len(s.Commits) != 1 || s.Commits[0].Subject != "Root" {
		t.Fatal(s.Commits, err)
	}
}

func TestDiscoverWorktrees(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"one", "two"} {
		if _, err := Run(t.Context(), parent, "git", "init", name); err != nil {
			t.Fatal(err)
		}
	}
	roots, err := Discover(t.Context(), parent)
	if err != nil || len(roots) != 2 {
		t.Fatal(roots, err)
	}
	child := filepath.Join(roots[0], "nested")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	got, err := Discover(t.Context(), child)
	if err != nil || !slices.Equal(got, roots[:1]) {
		t.Fatal(got, err)
	}
}
