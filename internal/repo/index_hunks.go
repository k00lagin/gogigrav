package repo

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

var indexHunkHeading = regexp.MustCompile(`(?m)^@@ -[0-9]+(?:,[0-9]+)? \+[0-9]+(?:,[0-9]+)? @@[^\n]*\n`)

// IndexDiff supplies Git's patch for a worktree or index diff. Empty means the
// file needs a whole-file action (e.g. binary content, rename, symlink, or conflict).
func IndexDiff(ctx context.Context, dir string, file File, unstaged bool) (string, error) {
	status := file.Index
	if unstaged {
		status = file.Work
	}
	if file.Conflicted || status == 'R' || status == 'C' {
		return "", nil
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames",
		"--src-prefix=a/", "--dst-prefix=b/", "--unified=3", "--inter-hunk-context=0"}
	if unstaged && file.Work == '?' {
		args = append(args, "--no-index", "--", "/dev/null")
	} else if !unstaged {
		args = append(args, "--cached", "--")
	} else {
		args = append(args, "--")
	}
	patch, err := runCommand(ctx, dir, "git", "", unstaged && file.Work == '?', append(args, file.Path)...)
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(patch) || len(indexHunkOffsets(patch)) == 0 {
		return "", nil
	}
	return patch, nil
}

func indexHunkOffsets(patch string) [][]int {
	hunks := indexHunkHeading.FindAllStringIndex(patch, -1)
	if len(hunks) == 0 {
		return nil
	}
	header := patch[:hunks[0][0]]
	if !strings.HasPrefix(header, "diff --git ") || strings.Contains(patch, "\ndiff --git ") {
		return nil
	}
	for _, line := range strings.Split(header, "\n") {
		if (strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "old mode ") || strings.HasPrefix(line, "new mode ") ||
			strings.HasPrefix(line, "new file mode ") || strings.HasPrefix(line, "deleted file mode ")) &&
			(strings.HasSuffix(line, " 120000") || strings.HasSuffix(line, " 160000")) {
			return nil
		}
	}
	return hunks
}

// ApplyIndexHunk rechecks the patch before applying one hunk to the index only.
func ApplyIndexHunk(ctx context.Context, dir string, file File, unstaged bool, expected string, index int) error {
	patch, err := IndexDiff(ctx, dir, file, unstaged)
	if err != nil {
		return err
	}
	if patch != expected {
		return errors.New("the diff changed. Review it before staging or unstaging a hunk")
	}
	hunks := indexHunkOffsets(patch)
	if index < 0 || index >= len(hunks) {
		return errors.New("this change cannot be staged or unstaged as a hunk")
	}
	end := len(patch)
	if index+1 < len(hunks) {
		end = hunks[index+1][0]
	}
	var content strings.Builder
	for line := range strings.SplitAfterSeq(patch[:hunks[0][0]], "\n") {
		// Applying a content hunk must leave an independent executable-bit change alone.
		if !strings.HasPrefix(line, "old mode ") && !strings.HasPrefix(line, "new mode ") {
			content.WriteString(line)
		}
	}
	content.WriteString(patch[hunks[index][0]:end])
	args := []string{"apply", "--cached", "--whitespace=nowarn"}
	if !unstaged {
		args = append(args, "--reverse")
	}
	_, err = run(ctx, dir, "git", content.String(), append(args, "-")...)
	return err
}
