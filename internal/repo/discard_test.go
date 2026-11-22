package repo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscardFolderBeforeFirstCommit(t *testing.T) {
	dir := t.TempDir()
	if _, err := Run(t.Context(), dir, "git", "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(dir, "src [1]")
	if err := os.Mkdir(selected, 0700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"src [1]/index", "src [1]/untracked", "other"} {
		if err := os.WriteFile(filepath.Join(dir, p), []byte("keep unrelated files"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Run(t.Context(), dir, "git", "add", "--", "src [1]/index", "other"); err != nil {
		t.Fatal(err)
	}
	if err := DiscardWithoutStash(t.Context(), dir, []string{"src [1]"}, true, true); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"index", "untracked"} {
		if _, err := os.Stat(filepath.Join(selected, p)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: %v", p, err)
		}
	}
	got, err := Run(t.Context(), dir, "git", "ls-files", "-z")
	if err != nil || got != "other\x00" {
		t.Fatalf("unexpected index %q: %v", got, err)
	}
}
