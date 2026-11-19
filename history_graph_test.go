package main

import (
	"slices"
	"testing"

	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
	"gogigrav/internal/repo"
)

func TestHistoryGraphConnectsMergeParents(t *testing.T) {
	graph := layoutHistoryGraph([]graphInput{
		{hash: "merge", parents: []string{"main", "side"}},
		{hash: "main", parents: []string{"root"}},
		{hash: "side", parents: []string{"root"}},
		{hash: "root"},
	})
	if graph.maxLanes != 2 {
		t.Fatalf("expected two lanes, got %d", graph.maxLanes)
	}
	want := [][]graphSegment{
		{{0, 0, graphBottom, 0}, {0, 1, graphBottom, 1}},
		{{0, 0, graphTop, 0}, {1, 1, graphThrough, 1}, {0, 0, graphBottom, 0}},
		{{0, 0, graphThrough, 0}, {1, 1, graphTop, 1}, {1, 0, graphBottom, 0}},
		{{0, 0, graphTop, 0}},
	}
	for i, row := range graph.rows {
		if !slices.Equal(row.segments, want[i]) {
			t.Errorf("row %d segments = %v, want %v", i, row.segments, want[i])
		}
	}
}

func TestHistoryGraphProjectsHiddenAncestors(t *testing.T) {
	data := repo.Snapshot{Head: "head", Commits: []repo.Commit{
		{Hash: "stash", Parents: "head"},
		{Hash: "head", Parents: "hidden"},
		{Hash: "hidden", Parents: "root"},
		{Hash: "root"},
		{Hash: "unrelated"},
	}, Stashes: []repo.Stash{{Hash: "stash"}}}
	visible := []repo.Commit{data.Commits[0], data.Commits[3], data.Commits[4]}
	graph := buildHistoryGraph(data, visible, true)
	if len(graph.rows) != 4 || graph.rows[0].kind != graphWorkingTreeNode || graph.rows[1].kind != graphStashNode {
		t.Fatalf("unexpected graph rows: %+v", graph.rows)
	}
	if r := graph.rows[2]; !slices.Contains(r.segments, graphSegment{0, 0, graphTop, 0}) {
		t.Fatalf("working tree and stash did not connect through hidden commits: %+v", r)
	}
	if r := graph.rows[3]; len(r.segments) != 0 {
		t.Fatalf("unrelated history gained an edge: %+v", r)
	}
	graph = buildHistoryGraph(data, data.Commits, false)
	if graph.rows[1].kind != graphHeadNode {
		t.Fatalf("HEAD marker missing: %+v", graph.rows[1])
	}
}

func TestWorkingTreeGraphLinesStartAtTheRing(t *testing.T) {
	w := &workbench{graph: layoutHistoryGraph([]graphInput{
		{kind: graphWorkingTreeNode, parents: []string{"left", "right"}},
		{hash: "left"},
		{hash: "right"},
	})}
	canvas := &uitest.MockCanvas{}
	bounds := geometry.NewRect(5, 7, w.graph.width(), 27)
	w.paintGraphRow(canvas, bounds, 0, widget.ColorWhite)
	if len(canvas.StrokeCircles) != 1 || len(canvas.Lines) < 2 {
		t.Fatal("expected the working tree ring and its outgoing connections")
	}
	ring := canvas.StrokeCircles[0]
	join := ring.Center.Add(geometry.Pt(0, ring.Radius))
	if got := canvas.Lines[0].From; got != join {
		t.Errorf("straight connection starts at %v, want %v", got, join)
	}
	if got := canvas.Lines[1].From; got != join {
		t.Errorf("curved connection starts at %v, want %v", got, join)
	}
	for _, line := range canvas.Lines {
		if min(line.From.Y, line.To.Y) < join.Y {
			t.Fatalf("connection extends into the working tree ring: %v -> %v", line.From, line.To)
		}
	}
}

func TestFileIconsUseNamesExtensionsAndFallback(t *testing.T) {
	for path, want := range map[string]string{
		"cmd/main.GO":           "go",
		"config/.gitignore":     "git",
		"package.json":          "json",
		"src/widget.tsx":        "react",
		"src/widget.mdx.tsx":    "markdown",
		"src/styles.module.css": "css",
		"no.extension-known":    "default",
	} {
		glyph := fileIcons.forPath(path)
		if glyph.image.Name != "file-"+want || len(glyph.image.SVGXML) == 0 || glyph.light == glyph.dark {
			t.Errorf("%s: expected %s icon with theme colors, got %s", path, want, glyph.image.Name)
		}
	}
}
