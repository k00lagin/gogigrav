package main

import (
	"cmp"
	"math"
	"path/filepath"
	"slices"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

const repositoryVisitHalfLife = 7 * 24 * time.Hour

type repositoryVisit struct {
	Score float64
	Time  time.Time
}

func (v repositoryVisit) scoreAt(now time.Time) float64 {
	if v.Score <= 0 {
		return 0
	}
	return math.Exp2(-float64(now.Sub(v.Time))/float64(repositoryVisitHalfLife)) * v.Score
}

func (p *preferences) recordRepositoryVisit(path string, now time.Time) {
	if p.RepositoryVisits == nil {
		p.RepositoryVisits = make(map[string]repositoryVisit)
	}
	key := repositoryKey(path)
	p.RepositoryVisits[key] = repositoryVisit{Score: p.RepositoryVisits[key].scoreAt(now) + 1, Time: now}
}

func (p *preferences) rememberRepositories(paths []string) {
	for _, path := range paths {
		if !slices.ContainsFunc(p.Recent, func(known string) bool { return sameRepositoryPath(known, path) }) {
			p.Recent = append(p.Recent, path)
		}
	}
}

func (p preferences) repositoryGroups(now time.Time) (recent, others []string) {
	type ranked struct {
		path  string
		score float64
	}
	paths := slices.Clone(p.Recent)
	slices.SortStableFunc(paths, repositoryNameOrder())
	visited := make([]ranked, 0, len(paths))
	for _, path := range paths {
		visit := p.RepositoryVisits[repositoryKey(path)]
		if visit.Score > 0 {
			visited = append(visited, ranked{path, visit.scoreAt(now)})
		} else {
			others = append(others, path)
		}
	}
	slices.SortStableFunc(visited, func(a, b ranked) int { return cmp.Compare(b.score, a.score) })
	for _, v := range visited {
		recent = append(recent, v.path)
	}
	return recent, others
}

func repositoryNameOrder() func(a, b string) int {
	names := collate.New(language.Und)
	return func(a, b string) int {
		if order := names.CompareString(filepath.Base(a), filepath.Base(b)); order != 0 {
			return order
		}
		return cmp.Compare(a, b)
	}
}
