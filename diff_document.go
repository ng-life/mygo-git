package main

import (
	"crypto/sha256"
	"fmt"
	"github.com/egoist/mygo/ui/editor"
	"strings"
	"unicode"
)

type DiffDocument struct {
	Changes  map[int]inlineChange
	Analysis *CodeAnalysis
	Tokens   [][]editor.Token
	Language string
	Lines    []string
	Width    float32
}

func prepareDiff(text string) DiffDocument {
	document := DiffDocument{Width: 100}
	if text == "" {
		return document
	}
	document.Lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, line := range document.Lines {
		line = strings.ReplaceAll(line, "\t", "    ")
		document.Lines[i] = line
		cells := 0
		for _, r := range line {
			if unicode.Is(unicode.Han, r) || r >= 0x3000 && r <= 0xffef {
				cells += 2
			} else {
				cells++
			}
		}
		document.Width = max(document.Width, float32(cells)*7.5+76)
	}
	return document
}
func (f *FileComparison) prepare() {
	f.Version = fmt.Sprintf("%x", sha256.Sum256([]byte(f.Before+"\x00"+f.After)))
	f.BeforeDocument = prepareDiff(f.Before)
	f.AfterDocument = prepareDiff(f.After)
	beforeName, afterName := f.BeforeSource.Path, f.AfterSource.Path
	if beforeName == "" {
		beforeName = f.Filename
	}
	if afterName == "" {
		afterName = f.Filename
	}
	highlightDocument(&f.BeforeDocument, beforeName)
	highlightDocument(&f.AfterDocument, afterName)
	f.prepareReview()
	f.prepareInlineHighlights()
}
