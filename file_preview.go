package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/focus"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

const (
	diffMode    = "Diff"
	beforeMode  = "File before change"
	afterMode   = "File at this revision"
	workingMode = "Working file"
)

type fileTarget struct {
	before, after repo.FileSide
	file          repo.File
}

func (w *workbench) target(f repo.File, unstaged bool) fileTarget {
	item := fileItem{file: f, stage: unstaged}
	status, _ := item.change()
	before, after := w.data.Head, repo.IndexFile
	if w.page == "commit" {
		before = ""
		if parents := strings.Fields(w.commit.Parents); len(parents) > 0 {
			before = parents[0]
		}
		after = w.commit.Hash
		if w.compareBase != "" {
			before, after = w.compareBase, w.compareTip
		}
	} else if unstaged {
		before, after = repo.IndexFile, repo.WorkingFile
		if f.Conflicted {
			before = w.data.Head
			if f.Index == 'D' || (f.Index == 'U' && f.Work == 'A') {
				before = ""
			}
		}
	}
	old := f.Path
	if (status == 'R' || status == 'C') && f.From != "" {
		old = f.From
	}
	if status == '?' || status == 'A' {
		before = ""
	}
	if status == 'D' {
		after = ""
	}
	return fileTarget{repo.FileSide{Revision: before, Path: old}, repo.FileSide{Revision: after, Path: f.Path}, f}
}

type filePreview struct {
	files             []repo.File
	unstaged, compare bool
	index             int
	mode              string
	loading           bool
	err               error
	rows              []previewRow
}

func (v *filePreview) target(w *workbench) fileTarget {
	t := w.target(v.files[v.index], v.unstaged)
	if v.compare {
		if t.after.Revision != "" {
			t.before = t.after
		}
		t.after = repo.FileSide{Revision: repo.WorkingFile, Path: t.before.Path}
	}
	return t
}
func (w *workbench) openPreview(files []repo.File, index int, unstaged, compare bool, mode string) {
	if w.viewer == nil {
		w.ui.Window().FocusManager().RegisterShortcut(focus.Shortcut{Key: event.KeyEscape}, w.closePreview)
	}
	v := &filePreview{files: files, index: index, unstaged: unstaged, compare: compare, mode: mode, loading: true}
	w.viewer = v
	t := v.target(w)
	if mode == beforeMode && t.before.Revision == "" {
		v.mode = afterMode
	}
	if mode == afterMode && t.after.Revision == "" {
		v.mode = beforeMode
	}
	dir, dark := w.path, w.theme.IsDark()
	w.redraw()
	w.runJob(func(ctx context.Context) (func(), error) {
		rows, err := previewLines(ctx, dir, t, v.mode, dark, v.compare)
		return func() { v.loading = false; v.rows = rows; v.err = err }, nil
	}, func(apply func(), _ error) {
		if w.viewer == v {
			apply()
			w.redraw()
		}
	})
}
func (w *workbench) closePreview() {
	if w.viewer != nil {
		w.ui.Window().FocusManager().UnregisterShortcut(focus.Shortcut{Key: event.KeyEscape})
		w.viewer = nil
		w.redraw()
	}
}

func (w *workbench) refreshPreview() {
	v := w.viewer
	if v == nil {
		return
	}
	files, index := v.files, v.index
	if w.page != "commit" {
		files = nil
		for _, f := range w.data.Files {
			if (v.unstaged && f.Unstaged()) || (!v.unstaged && f.Staged()) {
				files = append(files, f)
			}
		}
		index = slices.IndexFunc(files, func(f repo.File) bool { return f.Path == v.files[v.index].Path })
		if index < 0 {
			w.closePreview()
			return
		}
	}
	w.openPreview(files, index, v.unstaged, v.compare, v.mode)
}
func (w *workbench) previewPanel() widget.Widget {
	v := w.viewer
	t := v.target(w)
	change := func(index int, mode string) { w.openPreview(v.files, index, v.unstaged, v.compare, mode) }
	names := make([]string, len(v.files))
	for i, f := range v.files {
		names[i] = f.Path
	}
	modes := []string{diffMode}
	if t.before.Revision != "" {
		modes = append(modes, beforeMode)
	}
	if t.after.Revision != "" {
		modes = append(modes, afterMode)
	}
	modes = append(modes, workingMode)
	selected := 0
	for i, m := range modes {
		if m == v.mode {
			selected = i
		}
	}
	controls := []widget.Widget{primitives.Expanded(w.pick("File", names, v.index, func(i int) { change(i, v.mode) })), w.pick("File view", modes, selected, func(i int) { change(v.index, modes[i]) })}
	if v.mode == diffMode {
		style := 0
		if w.prefs.Settings.SplitDiff {
			style = 1
		}
		controls = append(controls, w.pick("Diff layout", []string{"Unified", "Split"}, style, func(i int) {
			w.prefs.Settings.SplitDiff = i == 1
			w.prefs.save(w.settingsPath)
			w.redraw()
		}))
	}
	controls = append(controls, w.check("Wrap", w.previewWrap, true, func(b bool) { w.previewWrap = b; w.redraw() }), w.check("Files", w.previewSidebar, true, func(b bool) { w.previewSidebar = b; w.redraw() }), w.btn("Close", button.Outlined, true, w.closePreview))
	var content widget.Widget
	switch {
	case v.loading:
		content = w.muted("Loading…")
	case v.err != nil:
		content = row(w.text(v.err.Error()), w.btn("Retry", button.Outlined, true, func() { change(v.index, v.mode) }))
	case len(v.rows) == 0:
		msg := "Empty file"
		if v.mode == diffMode {
			msg = "No textual differences"
		}
		content = w.muted(msg)
	default:
		split := v.mode == diffMode && w.prefs.Settings.SplitDiff
		rows := layoutDiffRows(v.rows, split)
		offset := state.NewSignal(float32(0))
		pane := func(side int) widget.Widget {
			code := &codePane{rows: rows, side: side, wrap: w.previewWrap, colors: w.theme.Colors}
			code.SetVisible(true)
			direction := scrollview.Both
			if code.wrap {
				direction = scrollview.Vertical
			}
			code.scroll = scrollview.New(code, scrollview.DirectionOpt(direction), scrollview.ScrollYSignal(offset), scrollview.PainterOpt(w.paint.Scrollbar))
			return code.scroll
		}
		content = pane(0)
		if split {
			content = primitives.HBox(primitives.Expanded(content), primitives.Box().Width(1).Background(w.theme.Colors.Border), primitives.Expanded(pane(1))).CrossAlign(primitives.CrossAxisStretch)
		}
	}
	if w.previewSidebar {
		side := w.fileSection("Files", "", v.files, v.unstaged, 0, state.NewSignal("Files/"+t.file.Path))
		content = primitives.HBox(primitives.Expanded(content), primitives.VBox(side).Width(250)).CrossAlign(primitives.CrossAxisStretch)
	}
	describe := func(side repo.FileSide) string {
		switch side.Revision {
		case "":
			return "Empty"
		case repo.IndexFile:
			return "Index"
		case repo.WorkingFile:
			return "Working tree"
		default:
			return side.Revision[:min(10, len(side.Revision))]
		}
	}
	path, revision := t.file.Path, describe(t.after)
	switch v.mode {
	case beforeMode:
		path, revision = t.before.Path, describe(t.before)
	case workingMode:
		revision = "Working tree"
	case diffMode:
		revision = describe(t.before) + "  →  " + describe(t.after)
	}
	return primitives.VBox(row(controls...).PaddingXY(8, 4), row(w.muted(revision), w.text(path)).PaddingXY(10, 2), line(w), primitives.Expanded(content)).Background(w.theme.Colors.Background)
}

type codeSpan struct {
	text  string
	color widget.Color
}
type codeLine struct {
	spans  []codeSpan
	number int
	kind   byte
	width  int
}
type previewRow struct{ left, right codeLine }

func highlighted(path, src string, dark bool) ([]codeLine, error) {
	if src == "" {
		return nil, nil
	}
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iter, err := chroma.Coalesce(lexer).Tokenise(nil, src)
	if err != nil {
		return nil, err
	}
	theme := styles.Get("github")
	if dark {
		theme = styles.Get("github-dark")
	}
	lines := []codeLine{{number: 1}}
	for token := iter(); token != chroma.EOF; token = iter() {
		col := theme.Get(token.Type).Colour
		for i, s := range strings.Split(token.Value, "\n") {
			if i > 0 {
				lines = append(lines, codeLine{number: len(lines) + 1})
			}
			l := &lines[len(lines)-1]
			if s != "" {
				var b strings.Builder
				for _, r := range s {
					if r == '\t' {
						n := 4 - l.width%4
						b.WriteString(strings.Repeat(" ", n))
						l.width += n
					} else {
						b.WriteRune(r)
						l.width++
					}
				}
				var c widget.Color
				if col.IsSet() {
					c = widget.RGBA8(col.Red(), col.Green(), col.Blue(), 255)
				}
				l.spans = append(l.spans, codeSpan{b.String(), c})
			}
		}
	}
	count := strings.Count(src, "\n")
	if !strings.HasSuffix(src, "\n") {
		count++
	}
	return lines[:min(count, len(lines))], nil
}

var hunkStart = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func previewLines(ctx context.Context, dir string, t fileTarget, mode string, dark, compare bool) ([]previewRow, error) {
	if mode != diffMode {
		side := t.after
		switch mode {
		case beforeMode:
			side = t.before
		case workingMode:
			side = repo.FileSide{Revision: repo.WorkingFile, Path: t.file.Path}
		}
		s, err := repo.ReadFileSide(ctx, dir, side)
		if err != nil {
			return nil, err
		}
		lines, err := highlighted(side.Path, s, dark)
		var rows []previewRow
		for _, line := range lines {
			rows = append(rows, previewRow{left: line})
		}
		return rows, err
	}
	old, err := repo.ReadFileSide(ctx, dir, t.before)
	if err != nil {
		return nil, err
	}
	next, err := repo.ReadFileSide(ctx, dir, t.after)
	if err != nil && !((compare || t.file.Conflicted) && errors.Is(err, os.ErrNotExist)) {
		return nil, err
	}
	patch, err := repo.DiffSnapshots(ctx, old, next)
	if err != nil || patch == "" {
		return nil, err
	}
	a, err := highlighted(t.before.Path, old, dark)
	if err != nil {
		return nil, err
	}
	b, err := highlighted(t.after.Path, next, dark)
	if err != nil {
		return nil, err
	}
	var rows []previewRow
	ai, bi := 0, 0
	inHunk := false
	for _, line := range strings.Split(patch, "\n") {
		if m := hunkStart.FindStringSubmatch(line); m != nil {
			ai, _ = strconv.Atoi(m[1])
			bi, _ = strconv.Atoi(m[2])
			inHunk = true
			header := codeLine{kind: '@', spans: []codeSpan{{text: line}}, width: len(line)}
			rows = append(rows, previewRow{header, header})
			continue
		}
		if !inHunk || line == "" {
			continue
		}
		r := previewRow{}
		switch line[0] {
		case ' ', '-', '+':
			if line[0] != '+' && ai > 0 && ai <= len(a) {
				r.left = a[ai-1]
				r.left.kind = line[0]
				ai++
			}
			if line[0] != '-' && bi > 0 && bi <= len(b) {
				r.right = b[bi-1]
				r.right.kind = line[0]
				bi++
			}
		case '\\':
			r.left = codeLine{kind: '@', spans: []codeSpan{{text: line}}, width: len(line)}
			r.right = r.left
		default:
			continue
		}
		rows = append(rows, r)
	}
	return rows, nil
}
func layoutDiffRows(src []previewRow, split bool) []previewRow {
	rows := make([]previewRow, 0, len(src))
	for i := 0; i < len(src); {
		r := src[i]
		i++
		if !split {
			if r.right.number != 0 {
				r.left = r.right
			}
			r.right = codeLine{}
		}
		if split && r.left.kind == '-' {
			start := len(rows)
			rows = append(rows, r)
			for i < len(src) && src[i].left.kind == '-' {
				rows = append(rows, src[i])
				i++
			}
			for j := start; i < len(src) && src[i].right.kind == '+'; i, j = i+1, j+1 {
				if j < len(rows) {
					rows[j].right = src[i].right
				} else {
					rows = append(rows, src[i])
				}
			}
		} else {
			rows = append(rows, r)
		}
	}
	return rows
}
