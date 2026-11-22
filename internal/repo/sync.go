package repo

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"
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

// FetchBranch returns the commit fetched from an exact remote branch.
func FetchBranch(ctx context.Context, dir, remote, branch string) (string, error) {
	if err := checkBranch(ctx, dir, branch); err != nil {
		return "", err
	}
	// FETCH_HEAD and remote-tracking refs may be overwritten by another fetch.
	ref := "refs/gogigrav/fetch/" + rand.Text()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = Run(cleanup, dir, "git", "update-ref", "-d", ref)
	}()
	if _, err := Run(ctx, dir, "git", "fetch", "--no-write-fetch-head", "--no-tags", "--no-recurse-submodules",
		"--", remote, LocalRefPrefix+branch+":"+ref); err != nil {
		return "", fmt.Errorf("fetch %s/%s: %w", remote, branch, err)
	}
	hash, err := Run(ctx, dir, "git", "rev-parse", "--verify", ref+"^{commit}")
	return strings.TrimSpace(hash), err
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
