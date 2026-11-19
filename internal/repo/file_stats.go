package repo

import (
	"context"
	"slices"
	"strconv"
	"strings"
)

type DiffStat struct {
	Added, Deleted int
	Text           bool // Git reports no line counts for binary files.
}

// readWorkStats fills both sides of a partially staged file independently.
// The raw stats are also part of the snapshot revision, since a new worktree
// edit can change the line counts without changing the porcelain status.
func readWorkStats(ctx context.Context, dir string, files []File) (indexRaw, workRaw string, err error) {
	args := []string{"--no-ext-diff", "--no-textconv", "--numstat", "-z", "-M", "--"}
	if slices.ContainsFunc(files, File.Staged) {
		indexRaw, err = Run(ctx, dir, "git", append([]string{"diff", "--cached"}, args...)...)
		if err != nil {
			return "", "", err
		}
	}
	if slices.ContainsFunc(files, func(f File) bool { return f.Unstaged() && f.Work != '?' && !f.Conflicted }) {
		workRaw, err = Run(ctx, dir, "git", append([]string{"diff"}, args...)...)
		if err != nil {
			return "", "", err
		}
	}
	index, work := parseNumstat(indexRaw), parseNumstat(workRaw)
	for i := range files {
		f := &files[i]
		if f.Staged() {
			f.IndexStat = index[f.Path]
		}
		if f.Unstaged() && !f.Conflicted {
			f.WorkStat = work[f.Path]
		}
	}
	return indexRaw, workRaw, nil
}

func parseNumstat(raw string) map[string]DiffStat {
	result := make(map[string]DiffStat)
	for raw != "" {
		entry, rest, _ := strings.Cut(raw, "\x00")
		raw = rest
		fields := strings.SplitN(entry, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		path := fields[2]
		if path == "" {
			// A rename or copy has two separate NUL-delimited paths.
			_, afterSource, _ := strings.Cut(raw, "\x00")
			path, raw, _ = strings.Cut(afterSource, "\x00")
		}
		added, addedErr := strconv.Atoi(fields[0])
		deleted, deletedErr := strconv.Atoi(fields[1])
		if path != "" && addedErr == nil && deletedErr == nil {
			result[path] = DiffStat{Added: added, Deleted: deleted, Text: true}
		}
	}
	return result
}
