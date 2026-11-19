package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/unicode"
)

const (
	WorkingFile = "WORKING"
	IndexFile   = "INDEX"
)

// FileSide identifies the exact version of a path, including the source of a rename.
// An empty revision denotes the absent side of an addition or deletion.
type FileSide struct{ Revision, Path string }

func ReadFileSide(ctx context.Context, dir string, side FileSide) (string, error) {
	if side.Revision == "" {
		return "", nil
	}
	var contents string
	var err error
	if side.Revision == WorkingFile {
		if !filepath.IsLocal(filepath.FromSlash(side.Path)) {
			return "", fmt.Errorf("invalid repository path %q", side.Path)
		}
		path := filepath.Join(dir, filepath.FromSlash(side.Path))
		// Read link text as Git does, and never follow a replaced parent directory outside the worktree.
		parent, e := filepath.EvalSymlinks(filepath.Dir(path))
		if e != nil {
			return "", e
		}
		root, e := filepath.EvalSymlinks(dir)
		if e != nil {
			return "", e
		}
		rel, e := filepath.Rel(root, parent)
		if e != nil || (rel != "." && !filepath.IsLocal(rel)) {
			return "", fmt.Errorf("%s is outside the working tree", side.Path)
		}
		info, e := os.Lstat(path)
		if e != nil {
			return "", e
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			contents, err = os.Readlink(path)
		case info.Mode().IsRegular():
			var raw []byte
			raw, err = os.ReadFile(path)
			contents = string(raw)
		default:
			return "", fmt.Errorf("%s is not a regular file", side.Path)
		}
	} else {
		rev := side.Revision
		if rev == IndexFile {
			rev = ":0"
		}
		contents, err = Run(ctx, dir, "git", "cat-file", "blob", rev+":"+side.Path)
	}
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(contents, "\xff\xfe") || strings.HasPrefix(contents, "\xfe\xff") {
		contents, err = unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder().String(contents)
		if err != nil {
			return "", err
		}
	}
	contents = strings.TrimPrefix(contents, "\ufeff")
	if strings.IndexByte(contents, 0) >= 0 || !utf8.ValidString(contents) {
		return "", fmt.Errorf("%s contains binary or non-UTF-8 data", side.Path)
	}
	return strings.ReplaceAll(contents, "\r\n", "\n"), nil
}

// DiffSnapshots compares the same two snapshots that supply the highlighted lines.
func DiffSnapshots(ctx context.Context, before, after string) (string, error) {
	if before == after {
		return "", nil
	}
	dir, err := os.MkdirTemp("", "gogigrav-diff-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	for name, contents := range map[string]string{"before": before, "after": after} {
		if err = os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			return "", err
		}
	}
	cmd := exec.CommandContext(ctx, executable("git"), "-c", "core.autocrlf=false", "diff", "--no-index", "--text", "--no-color", "--no-ext-diff", "--no-textconv", "--unified=3", "--", "before", "after")
	cmd.Dir = dir
	hideConsole(cmd)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return "", fmt.Errorf("compare snapshots: %w", err)
	}
	return string(out), nil
}
