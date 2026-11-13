package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"unicode"
	"unicode/utf8"
)

type compareLimitKey struct{}

func comparisonBlobLimit(ctx context.Context) int {
	if limit, ok := ctx.Value(compareLimitKey{}).(int); ok {
		return limit
	}
	return blobLimit
}
func ignoreComparisonWhitespace(ctx context.Context, path string, f *FileComparison) error {
	if f.Notice != "" {
		return nil
	}
	dir, err := os.MkdirTemp("", "mygo-compare-whitespace-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	before, after := filepath.Join(dir, "before"), filepath.Join(dir, "after")
	if err = os.WriteFile(before, []byte(f.Before), 0600); err != nil {
		return err
	}
	if err = os.WriteFile(after, []byte(f.After), 0600); err != nil {
		return err
	}
	data, err := rawGitContext(ctx, path, "diff", "--no-index", "--no-ext-diff", "--no-textconv", "--no-color", "-w", "--unified=0", "--", before, after)
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return err
		}
	}
	f.Hunks = parseDiffHunks(string(data))
	f.IgnoreWhitespace = true
	f.prepareReview()
	f.prepareInlineHighlights()
	return nil
}

type inlineChange struct{ Start, End int }

func lineChangeRange(a, b string) (inlineChange, inlineChange) {
	ar, br := []rune(a), []rune(b)
	prefix := 0
	for prefix < len(ar) && prefix < len(br) && ar[prefix] == br[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(ar)-prefix && suffix < len(br)-prefix && ar[len(ar)-1-suffix] == br[len(br)-1-suffix] {
		suffix++
	}
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
	left := prefix
	for left > 0 && ((left < len(ar) && word(ar[left]) && word(ar[left-1])) || (left < len(br) && word(br[left]) && word(br[left-1]))) {
		left--
	}
	endA, endB := len(ar)-suffix, len(br)-suffix
	for endA < len(ar) && endA > 0 && word(ar[endA-1]) && word(ar[endA]) {
		endA++
	}
	for endB < len(br) && endB > 0 && word(br[endB-1]) && word(br[endB]) {
		endB++
	}
	offset := func(runes []rune, n int) int {
		result := 0
		for _, r := range runes[:n] {
			result += utf8.RuneLen(r)
		}
		return result
	}
	return inlineChange{offset(ar, left), offset(ar, endA)}, inlineChange{offset(br, left), offset(br, endB)}
}
func (f *FileComparison) prepareInlineHighlights() {
	f.BeforeDocument.Changes = map[int]inlineChange{}
	f.AfterDocument.Changes = map[int]inlineChange{}
	for _, pair := range pairReviewRows(f.ReviewRows) {
		if pair.Old == nil || pair.New == nil || pair.Old.Kind != '-' || pair.New.Kind != '+' {
			continue
		}
		old, new := pair.Old.OldLine, pair.New.NewLine
		if old < 0 || new < 0 || old >= len(f.BeforeDocument.Lines) || new >= len(f.AfterDocument.Lines) {
			continue
		}
		a, b := lineChangeRange(f.BeforeDocument.Lines[old], f.AfterDocument.Lines[new])
		f.BeforeDocument.Changes[old] = a
		f.AfterDocument.Changes[new] = b
	}
}
