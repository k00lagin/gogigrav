package repo

import (
	"context"
	"errors"
)

// DiscardWithoutStash is used after explicit confirmation in an unborn or
// conflicted worktree. Git applies literal paths and leaves ignored files alone.
func DiscardWithoutStash(ctx context.Context, dir string, paths []string, unborn bool) error {
	if len(paths) == 0 {
		return errors.New("no paths selected")
	}
	tracked, err := Run(ctx, dir, "git", append([]string{"ls-files", "--cached", "-z", "--"}, paths...)...)
	if err != nil {
		return err
	}
	restore := []string{"rm", "-f"}
	if !unborn {
		original, err := Run(ctx, dir, "git", append([]string{"ls-tree", "-r", "--name-only", "-z", "HEAD", "--"}, paths...)...)
		if err != nil {
			return err
		}
		tracked += original
		restore = []string{"restore", "--source=HEAD", "--staged", "--worktree"}
	}
	if tracked != "" {
		_, err = run(ctx, dir, "git", tracked, append(restore, "--pathspec-from-file=-", "--pathspec-file-nul")...)
		if err != nil {
			return err
		}
	}
	_, err = Run(ctx, dir, "git", append([]string{"clean", "-fd", "--"}, paths...)...)
	return err
}
