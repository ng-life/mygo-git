package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type CompareCommit struct {
	Commit     Commit
	Side       string
	Equivalent bool
}
type CompareEndpoint struct {
	Repository     Repository
	A, B           string
	Bases          []string
	CountA, CountB int
}
type CompareResult struct {
	Endpoints []CompareEndpoint
	Commits   []CompareCommit
	Error     string
}
type CompareState struct {
	Active, Loading                      bool
	selectingCommit                      bool
	Generation                           uint64
	A, B, Mode, Query, FileQuery, Status string
	Limit                                int
	Request                              ReviewRequest
	Result                               CompareResult
	Selected                             map[string]bool
	Anchor                               int
}

func readCompare(ctx context.Context, request ReviewRequest, limit int) CompareResult {
	var result CompareResult
	var problems []string
	for _, repo := range request.Repositories {
		if request.Kind == "workspace" {
			continue
		}
		a, err := compareEndpoint(ctx, repo, request, 0)
		if err != nil {
			problems = append(problems, repo.Name+": "+err.Error())
			continue
		}
		b, err := compareEndpoint(ctx, repo, request, 1)
		if err != nil {
			problems = append(problems, repo.Name+": "+err.Error())
			continue
		}
		endpoint := CompareEndpoint{Repository: repo, A: a, B: b}
		bases, e := gitContext(ctx, repo.Path, "merge-base", "--all", a, b)
		if e == nil {
			endpoint.Bases = strings.Fields(bases)
		}
		for _, side := range []string{"A", "B"} {
			tip, other := a, b
			if side == "B" {
				tip, other = b, a
			}
			count, e := gitContext(ctx, repo.Path, "rev-list", "--count", tip, "--not", other, "--")
			if e != nil {
				problems = append(problems, repo.Name+": "+e.Error())
				continue
			}
			total, _ := strconv.Atoi(count)
			if side == "A" {
				endpoint.CountA = total
			} else {
				endpoint.CountB = total
			}
			equivalent := map[string]bool{}
			cherry, e := gitContext(ctx, repo.Path, "cherry", other, tip)
			if e != nil {
				problems = append(problems, repo.Name+" 补丁等价检查："+e.Error())
			}
			for _, line := range strings.Split(cherry, "\n") {
				fields := strings.Fields(line)
				if len(fields) == 2 && fields[0] == "-" {
					equivalent[fields[1]] = true
				}
			}
			data, e := rawGitContext(ctx, repo.Path, "log", fmt.Sprintf("-%d", limit), "--format=%H%x00%ct%x00%an%x00%s%x00%P%x00", tip, "--not", other, "--")
			if e != nil {
				problems = append(problems, repo.Name+": "+e.Error())
				continue
			}
			fields := strings.Split(string(data), "\x00")
			for i := 0; i+4 < len(fields); i += 5 {
				timestamp, e := strconv.ParseInt(fields[i+1], 10, 64)
				if e != nil {
					continue
				}
				hash := strings.TrimSpace(fields[i])
				result.Commits = append(result.Commits, CompareCommit{Commit: Commit{Repository: repo, Hash: hash, Timestamp: timestamp, Author: fields[i+2], Subject: fields[i+3], Parents: strings.Fields(fields[i+4])}, Side: side, Equivalent: equivalent[hash]})
			}
		}
		result.Endpoints = append(result.Endpoints, endpoint)
	}
	sort.SliceStable(result.Commits, func(i, j int) bool { return result.Commits[i].Commit.Timestamp > result.Commits[j].Commit.Timestamp })
	result.Error = strings.Join(problems, "\n")
	return result
}
func (a *client) initializeCompare(request ReviewRequest) {
	if request.Kind == "commits" {
		if !a.compare.selectingCommit {
			a.compare.Active = false
			a.compare.Generation++
			a.cancelRead("compare-commits")
		}
		return
	}
	a.compare.Generation++
	a.cancelRead("compare-commits")
	a.compare.Result = CompareResult{}
	a.compare.Active = true
	a.compare.Mode = "files"
	if !request.Net && request.Kind != "workspace" {
		a.compare.Mode = "onlyB"
	}
	a.compare.A = request.Base
	a.compare.B = request.Target
	if request.Kind == "workspace" {
		a.compare.B = "Worktree"
		if request.Reverse {
			a.compare.A = "Worktree"
			a.compare.B = request.Base
		}
	}
	a.compare.Request = request
	a.compare.Selected = map[string]bool{}
	a.compare.Anchor = -1
	if a.compare.Limit == 0 {
		a.compare.Limit = 200
	}
	a.compare.Loading = request.Kind != "workspace"
	if request.Kind == "workspace" {
		a.compare.Result = CompareResult{}
		a.compare.Loading = false
	}
}
func (a *client) reloadCompare() {
	a.compare.Generation++
	generation := a.compare.Generation
	a.compare.Loading = true
	request := a.compare.Request
	request.Repositories = append([]Repository(nil), request.Repositories...)
	request.Pinned = map[string][2]string{}
	for _, endpoint := range a.compare.Result.Endpoints {
		request.Pinned[endpoint.Repository.Path] = [2]string{endpoint.A, endpoint.B}
	}
	limit := a.compare.Limit
	a.startRead("compare-commits", func(ctx context.Context) func() {
		result := readCompare(ctx, request, limit)
		return func() {
			if !a.compare.Active || generation != a.compare.Generation {
				return
			}
			a.compare.Result = result
			a.compare.Loading = false
		}
	})
}
func (a *client) applyCompare(swap bool) {
	left, right := strings.TrimSpace(a.compare.A), strings.TrimSpace(a.compare.B)
	if swap {
		left, right = right, left
	}
	if left == "" || right == "" {
		return
	}
	request := a.compare.Request
	if request.Kind != "merge-base" {
		request.Kind = "direct"
	}
	request.Net = true
	request.Commits = nil
	request.Reverse = false
	if left == "Worktree" || right == "Worktree" {
		if left == "Worktree" && right == "Worktree" {
			return
		}
		request.Kind = "workspace"
		if left == "Worktree" {
			request.Base = right
			request.Reverse = true
		} else {
			request.Base = left
		}
		request.Target = ""
	} else {
		request.Base = left
		request.Target = right
	}
	a.beginReview(request)
	if request.Reverse {
		a.compare.A = left
		a.compare.B = right
	}
}
func (a *client) compareCommits() []CompareCommit {
	var result []CompareCommit
	q := strings.ToLower(strings.TrimSpace(a.compare.Query))
	for _, entry := range a.compare.Result.Commits {
		if a.compare.Mode == "onlyA" && entry.Side != "A" || a.compare.Mode == "onlyB" && entry.Side != "B" {
			continue
		}
		c := entry.Commit
		if q != "" && !strings.Contains(strings.ToLower(c.Hash+" "+c.Author+" "+c.Subject+" "+c.Repository.Name), q) {
			continue
		}
		result = append(result, entry)
	}
	return result
}
func (a *client) selectCompareCommit(index int, mods int) {
	rows := a.compareCommits()
	if index < 0 || index >= len(rows) {
		return
	}
	if a.compare.Selected == nil {
		a.compare.Selected = map[string]bool{}
	}
	// Modifiers are passed as a plain value to keep this state helper UI-independent.
	additive := mods&1 != 0
	shift := mods&2 != 0
	if !additive {
		a.compare.Selected = map[string]bool{}
	}
	if shift {
		anchor := a.compare.Anchor
		if anchor < 0 || anchor >= len(rows) {
			anchor = index
		}
		for i := min(anchor, index); i <= max(anchor, index); i++ {
			a.compare.Selected[rows[i].Commit.Key()] = true
		}
	} else {
		key := rows[index].Commit.Key()
		if a.compare.Selected[key] {
			delete(a.compare.Selected, key)
		} else {
			a.compare.Selected[key] = true
		}
		a.compare.Anchor = index
	}
	var commits []Commit
	for _, row := range rows {
		if a.compare.Selected[row.Commit.Key()] {
			commits = append(commits, row.Commit)
		}
	}
	a.compare.selectingCommit = true
	a.beginReview(ReviewRequest{Kind: "commits", Net: false, IgnoreWhitespace: a.compare.Request.IgnoreWhitespace, LargeFiles: a.compare.Request.LargeFiles, Repositories: reviewCommitRepositories(commits), Commits: commits})
	a.compare.selectingCommit = false
}

// Resolve each endpoint once per repository so the commit sets and file diff
// use identical revisions even when a branch moves during the background read.
func pinCompareEndpoints(ctx context.Context, request ReviewRequest) ReviewRequest {
	request.Pinned = map[string][2]string{}
	request.ResolveErrors = map[string]string{}
	if request.Kind == "commits" {
		return request
	}
	for _, repo := range request.Repositories {
		a, err := resolveReviewRef(ctx, repo, request.Base)
		if err != nil {
			request.ResolveErrors[repo.Path] = err.Error()
			continue
		}
		b := ""
		if request.Kind != "workspace" {
			b, err = resolveReviewRef(ctx, repo, request.Target)
			if err != nil {
				request.ResolveErrors[repo.Path] = err.Error()
				continue
			}
		}
		request.Pinned[repo.Path] = [2]string{a, b}
	}
	return request
}
func compareEndpoint(ctx context.Context, repo Repository, request ReviewRequest, side int) (string, error) {
	if problem := request.ResolveErrors[repo.Path]; problem != "" {
		return "", fmt.Errorf("%s", problem)
	}
	if pins, ok := request.Pinned[repo.Path]; ok {
		return pins[side], nil
	}
	ref := request.Base
	if side == 1 {
		ref = request.Target
	}
	return resolveReviewRef(ctx, repo, ref)
}

func reverseCompareFile(file ChangedFile) ChangedFile {
	if strings.HasPrefix(file.Status, "A") {
		file.Status = "D"
	} else if strings.HasPrefix(file.Status, "D") {
		file.Status = "A"
	}
	if strings.HasPrefix(file.Status, "R") {
		file.Path, file.OldPath = file.OldPath, file.Path
	}
	return file
}

func (a *client) clearCompareCommitSelection() {
	a.compare.Selected = map[string]bool{}
	a.compare.Anchor = -1
	if a.review.Request.Kind == "commits" {
		a.cancelRead("review")
		a.review.Generation++
		a.review.Loading = false
		a.review.Items = nil
	}
	a.clearComparison()
}
func (a *client) reconcileCompareFileFilter() {
	if a.selectedFile == "" {
		return
	}
	for _, item := range a.reviewVisibleItems() {
		if item.File.Key() == a.selectedFile {
			return
		}
	}
	a.clearComparison()
}
