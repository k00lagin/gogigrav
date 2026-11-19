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
	staged := slices.DeleteFunc(s.Files, func(f File) bool { return !f.Staged() })
	if len(staged) != 1 || staged[0].IndexStat != (DiffStat{Added: 1, Text: true}) {
		t.Fatalf("unborn index should contain one staged file with one added line: %+v", staged)
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
	if got := paths[0].IndexStat; got != (DiffStat{Added: 1, Text: true}) {
		t.Errorf("root commit line counts = %+v", got)
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
	if selected.IndexStat != (DiffStat{Text: true}) {
		t.Errorf("unchanged rename line counts = %+v", selected.IndexStat)
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
	s, err = Load(t.Context(), dir, Filter{Refs: []string{"refs/heads/older"}, Limit: 10})
	if err != nil || len(s.Commits) != 1 || s.Commits[0].Subject != "Root" {
		t.Fatal(s.Commits, err)
	}

	git("switch", "--detach", "HEAD~")
	git("commit", "--allow-empty", "-m", "Remote only")
	git("update-ref", "refs/remotes/origin/topic", "HEAD")
	git("update-ref", "refs/remotes/origin/shared", "HEAD~")
	git("switch", "main")
	f := Filter{Refs: []string{"refs/heads/main", "refs/remotes/origin/topic"}, Remote: true, Limit: 10}
	s, err = Load(t.Context(), dir, f)
	if err != nil || len(s.Commits) != 3 || !slices.ContainsFunc(s.Commits, func(c Commit) bool {
		return c.Subject == "Remote only" && strings.Contains(c.Refs, "origin/topic")
	}) {
		t.Fatal(s.Commits, err)
	}
	f.Remote = false
	commits, more, err := LoadHistory(t.Context(), dir, s, f)
	if err != nil || more || len(commits) != 2 || slices.ContainsFunc(commits, func(c Commit) bool {
		return c.Subject == "Remote only" || strings.Contains(c.Refs, "origin/")
	}) {
		t.Fatal(commits, more, err)
	}
}

func TestWorktreeLineCountsRefreshWithoutAStatusChange(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if _, err := Run(t.Context(), dir, "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	put := func(text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "lines.txt"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-b", "main")
	git("config", "user.name", "Tester")
	git("config", "user.email", "tester@example.test")
	put("one\ntwo\n")
	git("add", "lines.txt")
	git("-c", "commit.gpgsign=false", "commit", "-m", "Base")
	put("one\nnew\nthree\n")
	git("add", "lines.txt")
	put("one\nnew\n")
	filter := Filter{Limit: 10}
	first, err := Load(t.Context(), dir, filter)
	if err != nil || len(first.Files) != 1 {
		t.Fatalf("worktree snapshot: %v, %v", first.Files, err)
	}
	file := first.Files[0]
	if file.IndexStat != (DiffStat{Added: 2, Deleted: 1, Text: true}) ||
		file.WorkStat != (DiffStat{Deleted: 1, Text: true}) {
		t.Fatalf("partially staged stats: %+v", file)
	}

	put("one\nnew\nthree\nfour\nfive\n")
	second, changed, err := LoadIfChanged(t.Context(), dir, filter, first.Revision)
	if err != nil || !changed || len(second.Files) != 1 || second.Files[0].WorkStat != (DiffStat{Added: 2, Text: true}) {
		t.Fatalf("updated worktree snapshot = %v, changed=%v, err=%v", second.Files, changed, err)
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

func TestHistoryVisibilityAndCommitter(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := Run(t.Context(), dir, "git", args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	git("init", "-b", "main")
	git("config", "user.name", "Committer")
	git("config", "user.email", "committer@example.test")
	git("config", "commit.gpgsign", "false")
	git("commit", "--allow-empty", "--author=Author <author@example.test>", "--date=2023-01-01T12:00:00Z", "-m", "Root")
	root := git("rev-parse", "HEAD")
	onlyTag := git("commit-tree", "HEAD^{tree}", "-m", "Tag-only commit")
	git("tag", "root-tag", root)
	git("tag", "unmerged-tag", onlyTag)
	if err := os.WriteFile(filepath.Join(dir, "pending.txt"), []byte("local work"), 0600); err != nil {
		t.Fatal(err)
	}
	git("stash", "push", "-u", "-m", "Stashed change")
	stash := git("rev-parse", "refs/stash")
	if err := os.WriteFile(filepath.Join(dir, "another.txt"), []byte("more work"), 0600); err != nil {
		t.Fatal(err)
	}
	git("stash", "push", "-u", "-m", "Another stash")
	latestStash := git("rev-parse", "refs/stash")
	visible, err := Load(t.Context(), dir, Filter{Limit: 100, Order: "topo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(visible.Commits) != 4 {
		t.Fatalf("history should omit stash helpers: %+v", visible.Commits)
	}
	for _, hash := range []string{root, onlyTag, stash, latestStash} {
		if !slices.ContainsFunc(visible.Commits, func(c Commit) bool { return c.Hash == hash }) {
			t.Errorf("missing visible commit %s", hash)
		}
	}
	for _, c := range visible.Commits {
		if (c.Hash == stash || c.Hash == latestStash) && c.Parents != root {
			t.Errorf("stash %s parents = %q, want only its base %s", c.Hash, c.Parents, root)
		}
	}
	page, err := Load(t.Context(), dir, Filter{Limit: 3, Order: "topo"})
	if err != nil || len(page.Commits) != 3 || !page.More {
		t.Fatalf("first history page size = %d, more = %v, error = %v", len(page.Commits), page.More, err)
	}
	hidden, err := Load(t.Context(), dir, Filter{Limit: 100, Order: "author-date", HideTags: true, HideStashes: true})
	if err != nil || len(hidden.Commits) != 1 || len(hidden.Stashes) != 2 {
		t.Fatalf("filtered history: %+v, err = %v", hidden, err)
	}
	commit := hidden.Commits[0]
	if commit.Hash != root || strings.Contains(commit.Refs, "root-tag") || commit.Author != "Author" ||
		commit.Committer != "Committer" || commit.CommitterEmail != "committer@example.test" ||
		!strings.HasPrefix(commit.Date, "2023-01-01T12:00:00") || commit.CommitDate == "" || commit.CommitDate == commit.Date {
		t.Fatalf("filtered root commit = %+v", commit)
	}
	_, changed, err := LoadIfChanged(t.Context(), dir, Filter{Limit: 100}, hidden.Revision)
	if err != nil || changed {
		t.Fatalf("unchanged repository reported changed = %v, err = %v", changed, err)
	}
}
