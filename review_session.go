package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Review requests retain symbolic refs for refresh; results pin immutable SHAs.
type ReviewRequest struct {
	Pinned           map[string][2]string
	ResolveErrors    map[string]string
	Kind             string
	Base, Target     string
	Repositories     []Repository
	Commits          []Commit
	Net              bool
	Reverse          bool
	IgnoreWhitespace bool
	LargeFiles       bool
}
type ReviewItem struct {
	File        MergedFile
	Parts       []CommitComparison
	Fingerprint string
}
type ReviewSession struct {
	DialogRepositories                        []Repository
	Open, Dialog, Loading, UnreadOnly         bool
	Kind, Base, Target, Error, Scope, Summary string
	Net                                       bool
	Request                                   ReviewRequest
	Items                                     []ReviewItem
	Generation                                uint64
}

func resolveReviewRef(ctx context.Context, repo Repository, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("Ref is required")
	}
	return gitContext(ctx, repo.Path, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
}
func reviewChangedFiles(ctx context.Context, repo Repository, base, target string) ([]ChangedFile, error) {
	args := []string{"diff", "--name-status", "-z", "-M", base}
	if target != "" {
		args = append(args, target)
	}
	args = append(args, "--")
	out, err := rawGitContext(ctx, repo.Path, args...)
	if err != nil {
		return nil, err
	}
	tokens := strings.Split(string(out), "\x00")
	var files []ChangedFile
	for i := 0; i+1 < len(tokens) && tokens[i] != ""; {
		f := ChangedFile{Status: tokens[i], Path: tokens[i+1]}
		i += 2
		if strings.HasPrefix(f.Status, "R") || strings.HasPrefix(f.Status, "C") {
			if i >= len(tokens) {
				return nil, fmt.Errorf("Incomplete rename record")
			}
			f.OldPath = f.Path
			f.Path = tokens[i]
			i++
		}
		files = append(files, f)
	}
	return files, nil
}
func readReview(ctx context.Context, request ReviewRequest) (items []ReviewItem, summary, problem string) {
	if request.LargeFiles {
		ctx = context.WithValue(ctx, compareLimitKey{}, 8*1024*1024)
	}
	var files []MergedFile
	var errors, summaries []string
	if request.Kind == "commits" && !request.Net {
		_, files, problem = readSelectedCommitsContext(ctx, request.Commits)
		for _, c := range request.Commits {
			summaries = append(summaries, c.Repository.Name+": "+c.Hash+"(compared with first parent)")
		}
	} else {
		for _, repo := range request.Repositories {
			if ctx.Err() != nil {
				return nil, "", ctx.Err().Error()
			}
			base, target := "", ""
			resolvedBase := ""
			var err error
			if request.Kind == "commits" {
				var commits []Commit
				for _, commit := range request.Commits {
					if commit.Repository.Path == repo.Path {
						commits = append(commits, commit)
					}
				}
				if len(commits) == 0 {
					continue
				}
				// Determine endpoints by ancestry, never timestamps or table order.
				oldest, newest := commits[0].Hash, commits[0].Hash
				for _, commit := range commits[1:] {
					if _, e := gitContext(ctx, repo.Path, "merge-base", "--is-ancestor", commit.Hash, oldest); e == nil {
						oldest = commit.Hash
					}
					if _, e := gitContext(ctx, repo.Path, "merge-base", "--is-ancestor", newest, commit.Hash); e == nil {
						newest = commit.Hash
					}
				}
				for _, commit := range commits {
					_, e1 := gitContext(ctx, repo.Path, "merge-base", "--is-ancestor", oldest, commit.Hash)
					_, e2 := gitContext(ctx, repo.Path, "merge-base", "--is-ancestor", commit.Hash, newest)
					if e1 != nil || e2 != nil {
						err = fmt.Errorf("Selected commits are not on the same ancestor chain. Use Per Commit mode")
						break
					}
				}
				if err == nil {
					d, e := readCommitContext(ctx, Commit{Repository: repo, Hash: oldest})
					err = e
					base = d.Parent
					target = newest
					if err == nil && base == "" {
						base, err = gitContext(ctx, repo.Path, "hash-object", "-w", "-t", "tree", "--stdin")
					}
				}
			} else {
				base, err = compareEndpoint(ctx, repo, request, 0)
				if err != nil && request.Kind == "workspace" && request.Base == "HEAD" {
					tree := readWorkingTreeContext(ctx, repo)
					if tree.Error == "" && tree.Head == "" {
						base, err = gitContext(ctx, repo.Path, "hash-object", "-w", "-t", "tree", "--stdin")
					}
				}
				if err == nil && request.Kind != "workspace" {
					target, err = compareEndpoint(ctx, repo, request, 1)
				}
				resolvedBase = base
				if err == nil && request.Kind == "merge-base" {
					// Multiple merge bases require an explicit choice rather than an arbitrary one.
					var bases string
					bases, err = gitContext(ctx, repo.Path, "merge-base", "--all", base, target)
					if err == nil {
						if len(strings.Fields(bases)) != 1 {
							err = fmt.Errorf("Multiple merge bases found. Use direct comparison")
						} else {
							base = strings.TrimSpace(bases)
						}
					}
				}
			}
			if err != nil {
				errors = append(errors, repo.Name+": "+err.Error())
				continue
			}
			label := target
			if label == "" {
				label = "Worktree (including untracked files)"
			}
			baselineLabel := request.Base + " [" + base + "]"
			if request.Kind == "merge-base" {
				baselineLabel = request.Base + " [" + resolvedBase + "] · Merge bases [" + base + "]"
			}
			if request.Reverse {
				summaries = append(summaries, repo.Name+": Worktree → "+request.Base+" ["+base+"]")
			} else {
				summaries = append(summaries, repo.Name+": "+baselineLabel+" → "+request.Target+" ["+label+"]")
			}
			if !request.Net && (request.Kind == "direct" || request.Kind == "merge-base") {
				commits, problem := readHistoryContext(ctx, []HistorySource{{Repository: repo, Refs: []string{target, "^" + base}}})
				if problem != "" {
					errors = append(errors, repo.Name+": "+problem)
					continue
				}
				_, repoFiles, problem := readSelectedCommitsContext(ctx, commits)
				if problem != "" {
					errors = append(errors, repo.Name+": "+problem)
				}
				files = append(files, repoFiles...)
				count, e := gitContext(ctx, repo.Path, "rev-list", "--count", target, "^"+base, "--")
				if e != nil {
					errors = append(errors, repo.Name+": Cannot verify commit range: "+e.Error())
				} else if count != fmt.Sprint(len(commits)) {
					errors = append(errors, repo.Name+": Range contains "+count+" commits; Per Commit mode shows the latest 200. Narrow the range or use Net Diff for the complete result")
				}
				continue
			}
			changed, e := reviewChangedFiles(ctx, repo, base, target)
			if e != nil {
				errors = append(errors, repo.Name+": "+e.Error())
				continue
			}
			if request.Kind == "workspace" {
				tree := readWorkingTreeContext(ctx, repo)
				if tree.Error != "" {
					errors = append(errors, repo.Name+": "+tree.Error)
					continue
				}
				for _, f := range tree.Files {
					if !strings.Contains(f.Status, "?") {
						continue
					}
					found := false
					for i := range changed {
						if changed[i].Path == f.Path {
							changed[i].Status = "M"
							found = true
							break
						}
					}
					if !found {
						changed = append(changed, ChangedFile{Status: "A", Path: f.Path})
					}
				}
			}
			d := CommitDetail{Commit: Commit{Repository: repo, Hash: target, Subject: "Review · " + request.Kind}, Parent: base}
			for _, f := range changed {
				files = append(files, MergedFile{Repository: repo, Path: f.Path, Changes: []FileChange{{Detail: d, File: f}}})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Key() < files[j].Key() })
	for _, file := range files {
		if ctx.Err() != nil {
			return nil, "", ctx.Err().Error()
		}
		item := ReviewItem{File: file}
		for _, change := range file.Changes {
			var comparison FileComparison
			var err error
			if request.Kind == "workspace" {
				comparison, err = readWorkingComparisonContext(ctx, WorkingTree{Repository: file.Repository, Head: change.Detail.Parent}, WorkingFile{Path: change.File.Path, OldPath: change.File.OldPath, Status: " M"})
			} else {
				comparison, err = readComparisonContext(ctx, change.Detail, change.File)
			}
			if err == nil && request.IgnoreWhitespace {
				err = ignoreComparisonWhitespace(ctx, file.Repository.Path, &comparison)
			}
			part := CommitComparison{Change: change, Comparison: comparison}
			if err != nil {
				part.Error = err.Error()
			}
			item.Parts = append(item.Parts, part)
		}
		// Include raw Git patch bytes so binary, permission and rename changes also
		// invalidate read marks, even when the text renderer cannot show their content.
		var stamp strings.Builder
		valid := true
		for _, part := range item.Parts {
			args := []string{"diff", "--binary", "--no-ext-diff", "--no-textconv", part.Change.Detail.Parent}
			if part.Change.Detail.Commit.Hash != "" {
				args = append(args, part.Change.Detail.Commit.Hash)
			}
			args = append(args, "--", ":(literal)"+file.Path)
			if part.Change.File.OldPath != "" {
				args = append(args, ":(literal)"+part.Change.File.OldPath)
			}
			raw, err := rawGitContext(ctx, file.Repository.Path, args...)
			if err != nil || part.Error != "" {
				valid = false
			}
			stamp.Write(raw)
			stamp.WriteString(part.Comparison.Version)
			if request.Kind == "workspace" && part.Change.File.Status == "A" {
				// Untracked content is absent from git diff, including binary/large files.
				path := filepath.Join(file.Repository.Path, filepath.FromSlash(file.Path))
				info, e := os.Lstat(path)
				if e != nil {
					valid = false
				} else if info.Mode()&os.ModeSymlink != 0 {
					target, e := os.Readlink(path)
					if e != nil {
						valid = false
					}
					stamp.WriteString(target)
				} else {
					f, e := os.Open(path)
					if e != nil {
						valid = false
					} else {
						h := sha256.New()
						_, e = io.Copy(h, f)
						f.Close()
						if e != nil {
							valid = false
						}
						stamp.Write(h.Sum(nil))
					}
				}
			}
		}
		if valid {
			item.Fingerprint = fmt.Sprintf("%x", sha256.Sum256([]byte(stamp.String())))
		}
		if request.Reverse {
			for i := range item.Parts {
				f := &item.Parts[i].Comparison
				f.Before, f.After = f.After, f.Before
				f.BeforeSource, f.AfterSource = f.AfterSource, f.BeforeSource
				f.OldLines, f.NewLines = f.NewLines, f.OldLines
				for j, h := range f.Hunks {
					f.Hunks[j] = DiffHunk{h.NewStart, h.NewEnd, h.OldStart, h.OldEnd}
				}
				f.prepare()
			}
			for i := range item.File.Changes {
				item.File.Changes[i].File = reverseCompareFile(item.File.Changes[i].File)
			}
			for i := range item.Parts {
				item.Parts[i].Change.File = reverseCompareFile(item.Parts[i].Change.File)
			}
			if len(item.File.Changes) > 0 {
				item.File.Path = item.File.Changes[0].File.Path
			}
		}
		items = append(items, item)
	}
	if problem != "" {
		errors = append(errors, problem)
	}
	return items, strings.Join(summaries, "\n"), strings.Join(errors, "\n")
}
func reviewScope(request ReviewRequest) string {
	if request.Kind == "workspace" {
		request.Target = ""
		request.Net = true
	}
	if request.Kind == "commits" {
		request.Base = ""
		request.Target = ""
	}

	var keys []string
	for _, r := range request.Repositories {
		keys = append(keys, r.Path)
	}
	sort.Strings(keys)
	var commits []string
	for _, c := range request.Commits {
		commits = append(commits, c.Key())
	}
	sort.Strings(commits)
	data, _ := json.Marshal(struct {
		Kind, Base, Target                    string
		Net                                   bool
		Reverse, IgnoreWhitespace, LargeFiles bool `json:",omitempty"`
		Repositories, Commits                 []string
	}{request.Kind, request.Base, request.Target, request.Net, request.Reverse, request.IgnoreWhitespace, request.LargeFiles, keys, commits})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
