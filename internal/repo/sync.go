package repo

import (
	"context"
	"fmt"
)

func Pull(ctx context.Context, dir, remote, branch string, rebase bool) error {
	args := []string{"pull"}
	if rebase {
		args = append(args, "--rebase")
	}
	if remote != "" {
		if err := checkBranch(ctx, dir, branch); err != nil {
			return err
		}
		args = append(args, "--", remote, LocalRefPrefix+branch)
	}
	_, err := Run(ctx, dir, "git", args...)
	return err
}

func Push(ctx context.Context, dir, remote, source, target string, setUpstream, forceWithLease bool) error {
	if err := checkBranch(ctx, dir, target); err != nil {
		return err
	}
	args := []string{"push"}
	if setUpstream {
		args = append(args, "--set-upstream")
	}
	if forceWithLease {
		args = append(args, "--force-with-lease")
	}
	_, err := Run(ctx, dir, "git", append(args, "--", remote, LocalRefPrefix+source+":"+LocalRefPrefix+target)...)
	return err
}

func checkBranch(ctx context.Context, dir, name string) error {
	_, err := Run(ctx, dir, "git", "check-ref-format", LocalRefPrefix+name)
	if err != nil {
		return fmt.Errorf("invalid branch name %q: %w", name, err)
	}
	return nil
}
