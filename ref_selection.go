package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

type ReferenceChoice struct{ Kind, Name string }

func (r ReferenceChoice) Key() string { return r.Kind + "\x00" + r.Name }
func (r ReferenceChoice) Ref() string {
	switch r.Kind {
	case "head":
		return "HEAD"
	case "tags":
		return "refs/tags/" + r.Name
	case "remote":
		return fullBranchRef(r.Name, true)
	default:
		return fullBranchRef(r.Name, false)
	}
}
func (r ReferenceChoice) Label() string {
	if r.Kind == "head" {
		return "HEAD"
	}
	return map[string]string{"local": "Local ", "remote": "Remote ", "tags": "Tag "}[r.Kind] + r.Name
}
func (a *client) referenceRepositoriesFor(r ReferenceChoice) []Repository {
	if r.Kind == "head" {
		return append([]Repository(nil), a.repos...)
	}
	return referenceTargets(a.repos, r.Name, r.Kind)
}
func (a *client) referenceSelected(r ReferenceChoice) bool {
	for _, selected := range a.refSelection {
		if selected == r {
			return true
		}
	}
	return false
}
func (a *client) visibleReferenceChoices() []ReferenceChoice {
	result := []ReferenceChoice{{Kind: "head"}}
	for _, kind := range []string{"local", "remote", "tags"} {
		if !*a.referenceOpen("logs-section:" + kind) {
			continue
		}
		root := branchTree(referenceRepositories(a.repos, kind, a.logsQuery), false)
		var visit func(*TreeNode)
		visit = func(node *TreeNode) {
			for _, child := range node.Children {
				if child.Branch != "" {
					result = append(result, ReferenceChoice{Kind: kind, Name: child.Branch})
				}
				if len(child.Children) > 0 && *a.referenceOpen("logs:" + kind + child.Key) {
					visit(child)
				}
			}
		}
		visit(root)
	}
	return result
}
func (a *client) clickReference(r ReferenceChoice, mods ui.Modifiers) {
	switch {
	case mods&ui.Shift != 0:
		choices := a.visibleReferenceChoices()
		start, end := -1, -1
		for i, choice := range choices {
			if choice.Key() == a.refAnchor {
				start = i
			}
			if choice == r {
				end = i
			}
		}
		if mods&(ui.Ctrl|ui.Super) == 0 {
			a.refSelection = nil
		}
		if end < 0 {
			a.refSelection = []ReferenceChoice{r}
		} else {
			if start < 0 {
				start = end
			}
			for i := min(start, end); i <= max(start, end); i++ {
				if !a.referenceSelected(choices[i]) {
					a.refSelection = append(a.refSelection, choices[i])
				}
			}
		}
	case mods&(ui.Ctrl|ui.Super) != 0:
		if a.referenceSelected(r) {
			for i, item := range a.refSelection {
				if item == r {
					a.refSelection = append(a.refSelection[:i], a.refSelection[i+1:]...)
					break
				}
			}
		} else {
			a.refSelection = append(a.refSelection, r)
		}
		a.refAnchor = r.Key()
	default:
		a.refSelection = []ReferenceChoice{r}
		a.refAnchor = r.Key()
	}
	if r.Kind == "head" {
		a.selectReferenceHistory("", "", "")
	} else {
		a.selectReferenceHistory("/"+r.Name, r.Name, r.Kind)
	}
}
func (a *client) referenceContext(r ReferenceChoice) {
	if !a.referenceSelected(r) {
		a.refSelection = []ReferenceChoice{r}
		a.refAnchor = r.Key()
	}
}
func (a *client) selectedReferenceSources() []HistorySource {
	var sources []HistorySource
	for _, repo := range a.repos {
		var refs []string
		for _, choice := range a.refSelection {
			for _, target := range a.referenceRepositoriesFor(choice) {
				if target.Path == repo.Path {
					refs = append(refs, choice.Ref())
					break
				}
			}
		}
		if len(refs) > 0 {
			sources = append(sources, HistorySource{Repository: repo, Refs: refs})
		}
	}
	return sources
}
func (a *client) commonReferenceRepositories(choices []ReferenceChoice) []Repository {
	counts := map[string]int{}
	for _, choice := range choices {
		for _, repo := range a.referenceRepositoriesFor(choice) {
			counts[repo.Path]++
		}
	}
	var repos []Repository
	for _, repo := range a.repos {
		if counts[repo.Path] == len(choices) {
			repos = append(repos, repo)
		}
	}
	return repos
}
func (a *client) openReferenceComparison(target string, dialog bool) {
	choices := append([]ReferenceChoice(nil), a.refSelection...)
	if len(choices) < 1 || len(choices) > 2 {
		return
	}
	repos := a.commonReferenceRepositories(choices)
	if len(repos) == 0 {
		return
	}
	base := choices[0].Ref()
	kind := "direct"
	if len(choices) == 2 {
		target = choices[1].Ref()
	}
	if target == "Worktree" {
		kind = "workspace"
		target = ""
	}
	if dialog {
		a.openReviewDialog(repos, target)
		a.review.Kind = kind
		a.review.Base = base
		a.review.Target = target
		a.review.Net = true
	} else {
		a.beginReview(ReviewRequest{Kind: kind, Base: base, Target: target, Net: true, Repositories: repos})
	}
}
func (a *client) pruneReferenceSelection() {
	var retained []ReferenceChoice
	for _, r := range a.refSelection {
		if len(a.referenceRepositoriesFor(r)) > 0 {
			retained = append(retained, r)
		}
	}
	a.refSelection = retained
}
func referenceNameValid(name string) bool {
	return strings.TrimSpace(name) != "" && name == strings.TrimSpace(name)
}
