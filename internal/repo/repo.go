// Package repo reads local Git worktrees and runs commands used by the desktop UI.
package repo

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	return run(ctx, dir, tool, "", args...)
}

func run(ctx context.Context, dir, tool, input string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, executable(tool), args...)
	hideConsole(cmd)
	cmd.Dir = dir
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
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
	Index, Work         byte
	Path, From          string
	IndexStat, WorkStat DiffStat
	Conflicted          bool
}

func (f File) Staged() bool   { return !f.Conflicted && f.Index != ' ' && f.Index != '?' }
func (f File) Unstaged() bool { return f.Conflicted || f.Work != ' ' }

type Commit struct {
	Hash, Parents, Author, Email, Date, Refs, Subject, Body string
	Committer, CommitterEmail, CommitDate                   string
}

func (c Commit) Short() string { return c.Hash[:min(8, len(c.Hash))] }

type Stash struct {
	Selector, Hash, Message string
}

type Snapshot struct {
	Branch, Upstream, Head      string
	UpstreamRemote, UpstreamRef string
	Ahead, Behind               int
	Branches, Remotes           []string
	Stashes                     []Stash
	Files                       []File
	Commits                     []Commit
	More                        bool
	Revision                    [sha256.Size]byte
}
type Filter struct {
	Refs                  []string
	Remote                bool
	Limit                 int
	Order                 string
	HideTags, HideStashes bool
}

const (
	LocalRefPrefix  = "refs/heads/"
	RemoteRefPrefix = "refs/remotes/"
)

func (s Snapshot) CurrentBranch() string {
	if s.Branch == "(detached)" {
		return ""
	}
	return s.Branch
}

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
	s, _, err := LoadIfChanged(ctx, dir, f, [sha256.Size]byte{})
	return s, err
}

// LoadIfChanged checks the status and refs before re-reading the history.
func LoadIfChanged(ctx context.Context, dir string, f Filter, previous [sha256.Size]byte) (Snapshot, bool, error) {
	var s Snapshot
	git := func(args ...string) (string, error) { return Run(ctx, dir, "git", args...) }
	status, err := git("--no-optional-locks", "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return s, false, err
	}
	refs, err := git("for-each-ref", "--format=%(refname)%00%(symref)%00%(upstream:remotename)%00%(upstream:remoteref)%00%(objectname)", "refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return s, false, err
	}
	remotes, err := git("remote")
	if err != nil {
		return s, false, err
	}
	stashes, err := git("stash", "list", "--format=%gd%x00%H%x00%gs")
	if err != nil {
		return s, false, err
	}
	if err = parseStatus(status, &s); err != nil {
		return s, false, err
	}
	indexStats, workStats, err := readWorkStats(ctx, dir, s.Files)
	if err != nil {
		return s, false, err
	}
	s.Revision = sha256.Sum256([]byte(strings.Join([]string{status, refs, remotes, stashes, indexStats, workStats}, "\x00")))
	if s.Revision == previous {
		return s, false, nil
	}
	for _, line := range lines(refs) {
		fields := strings.Split(line, "\x00")
		if len(fields) != 5 || fields[1] != "" || strings.HasPrefix(fields[0], "refs/tags/") {
			continue
		}
		s.Branches = append(s.Branches, fields[0])
		if fields[0] == LocalRefPrefix+s.CurrentBranch() {
			s.UpstreamRemote, s.UpstreamRef = fields[2], fields[3]
		}
	}
	s.Remotes = strings.Fields(remotes)
	for _, line := range lines(stashes) {
		if fields := strings.SplitN(line, "\x00", 3); len(fields) == 3 {
			s.Stashes = append(s.Stashes, Stash{Selector: fields[0], Hash: fields[1], Message: fields[2]})
		}
	}
	s.Commits, s.More, err = LoadHistory(ctx, dir, s, f)
	return s, true, err
}

// LoadHistory reads commits for the filter using the last known repository metadata.
func LoadHistory(ctx context.Context, dir string, s Snapshot, f Filter) ([]Commit, bool, error) {
	order := "--date-order"
	switch f.Order {
	case "author-date":
		order = "--author-date-order"
	case "topo":
		order = "--topo-order"
	}
	refs := f.VisibleRefs()
	var stashes []Stash
	if len(refs) == 0 && !f.HideStashes {
		stashes = s.Stashes
	}
	// Git can return an index and an untracked-files commit for each stash.
	// Leave room for them so they don't consume the visible history limit.
	rawLimit := f.Limit + 1 + 2*len(stashes)
	args := []string{"log", "--no-color", "--decorate=short", order, "-z", "--format=%H%x00%P%x00%an%x00%ae%x00%aI%x00%D%x00%s%x00%b%x00%cn%x00%ce%x00%cI", "-n", strconv.Itoa(rawLimit)}
	if !f.Remote {
		args = append(args, "--decorate-refs-exclude="+RemoteRefPrefix+"*")
	}
	if f.HideTags {
		args = append(args, "--decorate-refs-exclude=refs/tags/*")
	}
	if f.HideStashes {
		args = append(args, "--decorate-refs-exclude=refs/stash")
	}
	if len(refs) > 0 {
		for _, ref := range refs {
			if !slices.Contains(s.Branches, ref) {
				return nil, false, fmt.Errorf("branch %s is no longer available", ref)
			}
		}
		args = append(args, refs...)
	} else {
		args = append(args, "--branches")
		if !f.HideTags {
			args = append(args, "--tags")
		}
		if f.Remote {
			args = append(args, "--remotes")
		}
		if s.Head != "" {
			args = append(args, "HEAD")
		}
	}
	var input strings.Builder
	stashHashes := make(map[string]bool, len(stashes))
	for _, stash := range stashes {
		if stash.Hash != "" && !stashHashes[stash.Hash] {
			stashHashes[stash.Hash] = true
			input.WriteString(stash.Hash + "\n")
		}
	}
	if input.Len() > 0 {
		args = append(args, "--stdin")
	}
	raw, err := run(ctx, dir, "git", input.String(), append(args, "--")...)
	if err != nil {
		return nil, false, err
	}
	var commits []Commit
	fields := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	for len(fields) >= 11 {
		c := Commit{
			Hash: fields[0], Parents: fields[1], Author: fields[2], Email: fields[3], Date: fields[4], Refs: fields[5],
			Subject: fields[6], Body: strings.TrimSpace(fields[7]), Committer: fields[8], CommitterEmail: fields[9], CommitDate: fields[10],
		}
		commits = append(commits, c)
		fields = fields[11:]
	}
	if len(stashHashes) != 0 {
		commits = hideStashHelpers(commits, stashHashes)
	}
	if len(commits) > f.Limit {
		return commits[:f.Limit], true, nil
	}
	return commits, false, nil
}

func hideStashHelpers(commits []Commit, stashes map[string]bool) []Commit {
	helpers := make(map[string]bool)
	referenced := make(map[string]bool)
	for i, c := range commits {
		parents := strings.Fields(c.Parents)
		if stashes[c.Hash] && len(parents) > 1 {
			commits[i].Parents = parents[0]
			for _, parent := range parents[1:] {
				helpers[parent] = true
			}
			parents = parents[:1]
		}
		for _, parent := range parents {
			referenced[parent] = true
		}
	}
	return slices.DeleteFunc(commits, func(c Commit) bool {
		return helpers[c.Hash] && !referenced[c.Hash] && c.Refs == "" && !stashes[c.Hash]
	})
}

func lines(s string) []string {
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
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
		f := File{Index: xy[0], Work: xy[1], Path: parts[n-1], Conflicted: r[0] == 'u'}
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
	args := []string{"--no-commit-id", "--no-ext-diff", "--no-textconv", "-r", "-M", "-z"}
	if parents := strings.Fields(c.Parents); len(parents) > 0 {
		args = append(args, parents[0])
	} else {
		args = append(args, "--root")
	}
	args = append(args, c.Hash, "--")
	raw, err := Run(ctx, dir, "git", append([]string{"diff-tree", "--name-status"}, args...)...)
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
	if len(result) > 0 {
		numstat, err := Run(ctx, dir, "git", append([]string{"diff-tree", "--numstat"}, args...)...)
		if err != nil {
			return nil, err
		}
		stats := parseNumstat(numstat)
		for i := range result {
			result[i].IndexStat = stats[result[i].Path]
		}
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
