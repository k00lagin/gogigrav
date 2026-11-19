package repo

import (
	"context"
	"strconv"
	"strings"
)

type ChangeSummary struct{ Files, Added, Deleted int }

func readCommitStats(ctx context.Context, dir string, commits []Commit) error {
	if len(commits) == 0 {
		return nil
	}
	var hashes strings.Builder
	for _, c := range commits {
		hashes.WriteString(c.Hash)
		hashes.WriteByte('\n')
	}
	raw, err := run(ctx, dir, "git", hashes.String(), "log", "--no-walk=unsorted", "--stdin", "--root", "--diff-merges=first-parent", "--no-color", "--no-ext-diff", "--no-textconv", "--format=%x00%H", "--numstat", "-z", "-M", "--")
	if err != nil {
		return err
	}
	byHash := make(map[string]*ChangeSummary, len(commits))
	for i := range commits {
		byHash[commits[i].Hash] = &commits[i].Diff
	}
	var stat *ChangeSummary
	for raw != "" {
		entry, rest, _ := strings.Cut(raw, "\x00")
		raw = rest
		if entry == "" {
			hash, rest, _ := strings.Cut(raw, "\x00")
			raw = rest
			stat = byHash[hash]
			continue
		}
		fields := strings.SplitN(strings.TrimPrefix(entry, "\n"), "\t", 3)
		if len(fields) != 3 {
			continue
		}
		if fields[2] == "" {
			// Rename and copy records carry the source and destination separately.
			_, raw, _ = strings.Cut(raw, "\x00")
			_, raw, _ = strings.Cut(raw, "\x00")
		}
		if stat != nil {
			a, _ := strconv.Atoi(fields[0])
			d, _ := strconv.Atoi(fields[1])
			stat.Files++
			stat.Added += a
			stat.Deleted += d
		}
	}
	return nil
}
