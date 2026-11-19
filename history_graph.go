package main

import (
	"slices"
	"strings"

	"gogigrav/internal/repo"
)

const graphColorCount = 12

type graphNodeKind uint8

const (
	graphCommitNode graphNodeKind = iota
	graphHeadNode
	graphWorkingTreeNode
	graphStashNode
)

type graphInput struct {
	hash    string
	parents []string
	kind    graphNodeKind
}

type graphSegmentPart uint8

const (
	graphThrough graphSegmentPart = iota
	graphTop
	graphBottom
)

type graphSegment struct {
	from, to int
	part     graphSegmentPart
	color    int
}

type graphLane struct {
	hash  string
	color int
}

type historyGraphRow struct {
	lane, color int
	kind        graphNodeKind
	segments    []graphSegment
}

type historyGraph struct {
	rows     []historyGraphRow
	maxLanes int
}

// Git's history orders put children before their parents. Each lane holds the
// next parent it needs to reach, so edges can be drawn one row at a time.
func layoutHistoryGraph(commits []graphInput) historyGraph {
	graph := historyGraph{rows: make([]historyGraphRow, 0, len(commits)), maxLanes: 1}
	var lanes []graphLane
	nextColor := 0
	newColor := func() int {
		color := nextColor
		nextColor = (nextColor + 1) % graphColorCount
		return color
	}
	firstFree := func() int {
		for i, lane := range lanes {
			if lane.hash == "" {
				return i
			}
		}
		lanes = append(lanes, graphLane{})
		return len(lanes) - 1
	}

	for _, commit := range commits {
		row := historyGraphRow{lane: -1, kind: commit.kind}
		for i, lane := range lanes {
			if lane.hash != "" && lane.hash == commit.hash {
				row.lane, row.color = i, lane.color
				break
			}
		}
		if row.lane < 0 {
			row.lane, row.color = firstFree(), newColor()
		}
		for i, lane := range lanes {
			switch {
			case lane.hash == "":
			case lane.hash == commit.hash:
				row.segments = append(row.segments, graphSegment{i, row.lane, graphTop, lane.color})
				lanes[i] = graphLane{}
			default:
				row.segments = append(row.segments, graphSegment{i, i, graphThrough, lane.color})
			}
		}
		for i, parent := range commit.parents {
			target := slices.IndexFunc(lanes, func(lane graphLane) bool { return lane.hash == parent })
			if target < 0 {
				target = row.lane
				color := row.color
				if i != 0 {
					target, color = firstFree(), newColor()
				}
				lanes[target] = graphLane{parent, color}
			}
			row.segments = append(row.segments, graphSegment{row.lane, target, graphBottom, lanes[target].color})
		}
		graph.maxLanes = max(graph.maxLanes, len(lanes))
		for len(lanes) != 0 && lanes[len(lanes)-1].hash == "" {
			lanes = lanes[:len(lanes)-1]
		}
		graph.rows = append(graph.rows, row)
	}
	return graph
}

func buildHistoryGraph(data repo.Snapshot, visible []repo.Commit, workingTree bool) historyGraph {
	offset := 0
	if workingTree {
		offset = 1
	}
	shown := make(map[string]int, len(visible))
	for i, c := range visible {
		shown[c.Hash] = i + offset
	}
	stashes := make(map[string]bool, len(data.Stashes))
	for _, stash := range data.Stashes {
		stashes[stash.Hash] = true
	}

	// Work backwards so a hidden commit can be replaced by its nearest
	// visible ancestors. Keep unloaded parents when more history is available.
	ancestors := make(map[string][]string, len(data.Commits))
	rows := make([]graphInput, len(visible)+offset)
	if workingTree {
		rows[0].kind = graphWorkingTreeNode
	}
	for i := len(data.Commits) - 1; i >= 0; i-- {
		c := data.Commits[i]
		parents := strings.Fields(c.Parents)
		kind := graphCommitNode
		switch {
		case c.Hash == data.Head:
			kind = graphHeadNode
		case stashes[c.Hash]:
			kind = graphStashNode
		}
		var projected []string
		for _, parent := range parents {
			targets, loaded := ancestors[parent]
			if !loaded && data.More {
				targets = []string{parent}
			}
			for _, target := range targets {
				if !slices.Contains(projected, target) {
					projected = append(projected, target)
				}
			}
		}
		if row, ok := shown[c.Hash]; ok {
			rows[row] = graphInput{hash: c.Hash, parents: projected, kind: kind}
			ancestors[c.Hash] = []string{c.Hash}
		} else {
			ancestors[c.Hash] = projected
		}
	}
	if workingTree && len(visible) != 0 {
		rows[0].parents = ancestors[data.Head]
	}
	return layoutHistoryGraph(rows)
}
