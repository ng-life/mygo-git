package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type FileChange struct {
	Detail CommitDetail
	File   ChangedFile
}
type MergedFile struct {
	Repository Repository
	Path       string
	Changes    []FileChange
}

func (f MergedFile) Key() string { return f.Repository.Path + "\x00" + f.Path }
func (f MergedFile) Status() string {
	if len(f.Changes) == 1 {
		return f.Changes[0].File.Status
	}
	return fmt.Sprintf("%d attempts", len(f.Changes))
}

type CommitComparison struct {
	Change     FileChange
	Comparison FileComparison
	Error      string
}

// Keep each selected commit's delta. This also supports non-contiguous selections
// and commits on different branches without introducing intervening changes.
func readSelectedCommits(commits []Commit) ([]CommitDetail, []MergedFile, string) {
	return readSelectedCommitsContext(context.Background(), commits)
}
func readSelectedCommitsContext(ctx context.Context, commits []Commit) ([]CommitDetail, []MergedFile, string) {
	var details []CommitDetail
	var files []MergedFile
	var errors []string
	index := map[string]int{}
	for _, commit := range commits {
		detail, err := readCommitContext(ctx, commit)
		if err != nil {
			errors = append(errors, commit.Repository.Name+" · "+shortHash(commit.Hash)+": "+err.Error())
			continue
		}
		details = append(details, detail)
		for _, file := range detail.Files {
			key := commit.Repository.Path + "\x00" + file.Path
			i, ok := index[key]
			if !ok {
				i = len(files)
				index[key] = i
				files = append(files, MergedFile{Repository: commit.Repository, Path: file.Path})
			}
			files[i].Changes = append(files[i].Changes, FileChange{Detail: detail, File: file})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Repository.Path != files[j].Repository.Path {
			return files[i].Repository.Path < files[j].Repository.Path
		}
		return files[i].Path < files[j].Path
	})
	return details, files, strings.Join(errors, "\n")
}
func readMergedComparison(file MergedFile) []CommitComparison {
	return readMergedComparisonContext(context.Background(), file)
}
func readMergedComparisonContext(ctx context.Context, file MergedFile) []CommitComparison {
	results := make([]CommitComparison, 0, len(file.Changes))
	for _, change := range file.Changes {
		comparison, err := readComparisonContext(ctx, change.Detail, change.File)
		result := CommitComparison{Change: change, Comparison: comparison}
		if err != nil {
			result.Error = err.Error()
		}
		results = append(results, result)
	}
	return results
}
