package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexHunkBeforeFirstCommitAndStalePatch(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	if _, err := Run(ctx, dir, "git", "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	file := File{Path: "new [1].txt", Work: '?'}
	write := func(contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file.Path), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	patch := func(unstaged bool) string {
		t.Helper()
		got, err := IndexDiff(ctx, dir, file, unstaged)
		if err != nil || got == "" {
			t.Fatalf("expected applicable patch, got %q: %v", got, err)
		}
		return got
	}
	write("first")
	stale := patch(true)
	write("second")
	if err := ApplyIndexHunk(ctx, dir, file, true, stale, 0); err == nil || !strings.Contains(err.Error(), "diff changed") {
		t.Fatalf("applying stale hunk returned %v", err)
	}
	if err := ApplyIndexHunk(ctx, dir, file, true, patch(true), 0); err != nil {
		t.Fatal(err)
	}
	if staged, err := Run(ctx, dir, "git", "show", ":"+file.Path); err != nil || staged != "second" {
		t.Fatalf("unexpected staged content %q: %v", staged, err)
	}
	file.Index, file.Work = 'A', ' '
	if err := ApplyIndexHunk(ctx, dir, file, false, patch(false), 0); err != nil {
		t.Fatal(err)
	}
	if files, err := Run(ctx, dir, "git", "ls-files"); err != nil || files != "" {
		t.Fatalf("unexpected index after unstage %q: %v", files, err)
	}
	if working, err := os.ReadFile(filepath.Join(dir, file.Path)); err != nil || string(working) != "second" {
		t.Fatalf("unexpected working file %q: %v", working, err)
	}
}
