// Package repo reads local Git worktrees and runs commands used by the desktop UI.
package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

func Run(ctx context.Context, dir, tool string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, executable(tool), args...)
	hideConsole(cmd)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "GIT_EDITOR=true", "GIT_LITERAL_PATHSPECS=1")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%s is missing from PATH", tool)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s: %s", tool, msg)
	}
	return out.String(), nil
}

func Root(ctx context.Context, path string) (string, error) {
	s, err := Run(ctx, path, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Clean(strings.TrimRight(s, "\r\n")), nil
}

// Discover handles a worktree (including a nested directory) or a folder of worktrees.
func Discover(ctx context.Context, path string) ([]string, error) {
	if root, err := Root(ctx, path); err == nil {
		return []string{root}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(path, e.Name())
		if _, err := os.Stat(filepath.Join(p, ".git")); err != nil {
			continue
		}
		if root, err := Root(ctx, p); err == nil {
			roots = append(roots, root)
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no Git worktree found in %s", path)
	}
	return roots, nil
}

type File struct {
	Index, Work byte
	Path, From  string
}

func (f File) Staged() bool   { return f.Index != ' ' && f.Index != '?' }
func (f File) Unstaged() bool { return f.Work != ' ' }

type Commit struct{ Hash, Parents, Author, Email, Date, Refs, Subject, Body string }

func (c Commit) Short() string { return c.Hash[:min(8, len(c.Hash))] }

type Snapshot struct {
	Branch, Upstream, Head     string
	Ahead, Behind              int
	Branches, Remotes, Stashes []string
	Files                      []File
	Commits                    []Commit
	More                       bool
}
type Filter struct {
	Refs   []string
	Remote bool
	Limit  int
}

const RemoteRefPrefix = "refs/remotes/"

// VisibleRefs returns the selected refs that can contribute to the history.
func (f Filter) VisibleRefs() []string {
	if f.Remote {
		return f.Refs
	}
	return slices.DeleteFunc(slices.Clone(f.Refs), func(ref string) bool {
		return strings.HasPrefix(ref, RemoteRefPrefix)
	})
}

func Load(ctx context.Context, dir string, f Filter) (Snapshot, error) {
	var s Snapshot
	git := func(args ...string) (string, error) { return Run(ctx, dir, "git", args...) }
	status, err := git("status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return s, err
	}
	if err = parseStatus(status, &s); err != nil {
		return s, err
	}
	branches, err := git("for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes")
	if err != nil {
		return s, err
	}
	for _, b := range strings.Fields(branches) {
		if !strings.HasSuffix(b, "/HEAD") {
			s.Branches = append(s.Branches, b)
		}
	}
	remotes, err := git("remote")
	if err != nil {
		return s, err
	}
	s.Remotes = strings.Fields(remotes)
	stashes, err := git("stash", "list", "--format=%gd  %gs")
	if err != nil {
		return s, err
	}
	s.Stashes = lines(stashes)
	s.Commits, s.More, err = LoadHistory(ctx, dir, s, f)
	return s, err
}

// LoadHistory reads commits for the filter using the last known repository metadata.
func LoadHistory(ctx context.Context, dir string, s Snapshot, f Filter) ([]Commit, bool, error) {
	args := []string{"log", "--no-color", "--decorate=short", "--date-order", "-z", "--format=%H%x00%P%x00%an%x00%ae%x00%aI%x00%D%x00%s%x00%b", "-n", strconv.Itoa(f.Limit + 1)}
	if !f.Remote {
		args = append(args, "--decorate-refs-exclude="+RemoteRefPrefix+"*")
	}
	if refs := f.VisibleRefs(); len(refs) > 0 {
		for _, ref := range refs {
			if !slices.Contains(s.Branches, ref) {
				return nil, false, fmt.Errorf("branch %s is no longer available", ref)
			}
		}
		args = append(args, refs...)
	} else {
		args = append(args, "--branches", "--tags")
		if f.Remote {
			args = append(args, "--remotes")
		}
		if len(s.Stashes) > 0 {
			args = append(args, "refs/stash")
		}
		if s.Head != "" {
			args = append(args, "HEAD")
		}
	}
	raw, err := Run(ctx, dir, "git", append(args, "--")...)
	if err != nil {
		return nil, false, err
	}
	var commits []Commit
	fields := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	for len(fields) >= 8 {
		c := Commit{fields[0], fields[1], fields[2], fields[3], fields[4], fields[5], fields[6], strings.TrimSpace(fields[7])}
		commits = append(commits, c)
		fields = fields[8:]
	}
	if len(commits) > f.Limit {
		return commits[:f.Limit], true, nil
	}
	return commits, false, nil
}

func lines(s string) []string {
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func parseStatus(raw string, s *Snapshot) error {
	records := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	for i := 0; i < len(records); i++ {
		r := records[i]
		if r == "" {
			continue
		}
		if strings.HasPrefix(r, "# ") {
			key, val, _ := strings.Cut(r[2:], " ")
			switch key {
			case "branch.head":
				s.Branch = val
			case "branch.oid":
				if val != "(initial)" {
					s.Head = val
				}
			case "branch.upstream":
				s.Upstream = val
			case "branch.ab":
				fmt.Sscanf(val, "+%d -%d", &s.Ahead, &s.Behind)
			}
			continue
		}
		if strings.HasPrefix(r, "? ") {
			s.Files = append(s.Files, File{Index: '?', Work: '?', Path: r[2:]})
			continue
		}
		n := map[byte]int{'1': 9, '2': 10, 'u': 11}[r[0]]
		if n == 0 {
			return fmt.Errorf("unexpected status record %q", r)
		}
		parts := strings.SplitN(r, " ", n)
		if len(parts) != n || len(parts[1]) != 2 {
			return fmt.Errorf("incomplete status record")
		}
		xy := strings.ReplaceAll(parts[1], ".", " ")
		f := File{Index: xy[0], Work: xy[1], Path: parts[n-1]}
		if r[0] == '2' {
			i++
			if i >= len(records) {
				return errors.New("missing rename source")
			}
			f.From = records[i]
		}
		s.Files = append(s.Files, f)
	}
	return nil
}

// ChangedPaths compares a merge to its first parent and includes root commits.
func ChangedPaths(ctx context.Context, dir string, c Commit) ([]File, error) {
	args := []string{"diff-tree", "--no-commit-id", "--name-status", "-r", "-M", "-z"}
	if parents := strings.Fields(c.Parents); len(parents) > 0 {
		args = append(args, parents[0])
	} else {
		args = append(args, "--root")
	}
	raw, err := Run(ctx, dir, "git", append(args, c.Hash, "--")...)
	if err != nil {
		return nil, err
	}
	var result []File
	fields := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	for len(fields) >= 2 {
		f := File{Index: fields[0][0], Path: fields[1]}
		fields = fields[2:]
		if f.Index == 'R' || f.Index == 'C' {
			if len(fields) == 0 {
				return nil, errors.New("incomplete rename")
			}
			f.From, f.Path = f.Path, fields[0]
			fields = fields[1:]
		}
		result = append(result, f)
	}
	return result, nil
}

func ChangeIndex(ctx context.Context, dir string, f File, stage, unborn bool) error {
	args := []string{"add", "-A"}
	if !stage {
		if unborn {
			args = []string{"rm", "--cached", "-f"}
		} else {
			args = []string{"restore", "--staged"}
		}
	}
	args = append(args, "--", f.Path)
	// A staged rename already removed the source from the index. Only include it
	// when undoing that rename, or when staging a worktree rename.
	if f.From != "" && ((!stage && f.Index == 'R') || (stage && f.Work == 'R')) {
		args = append(args, f.From)
	}
	_, err := Run(ctx, dir, "git", args...)
	return err
}
