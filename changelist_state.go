package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultChangelist = "default"

type Changelist struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Message string `json:"message,omitempty"`
}

type ChangelistState struct {
	Messages []string     `json:"commitMessages,omitempty"`
	Lists    []Changelist `json:"lists"`
	Active   string       `json:"active"`
	Selected string       `json:"selected"`
	Next     uint64       `json:"next"`
	// Include branch identity: moving a file on one branch must not regroup
	// another branch's independent changes to the same path.
	Files map[string]string `json:"files"`
}

type changelistDialogState struct {
	Open                          bool
	Kind, ID, Name, Target, Error string
	Activate                      bool
	Requests                      []CommitRequest
}

func (s *ChangelistState) normalize() {
	if s.Files == nil {
		s.Files = map[string]string{}
	}
	if s.list(defaultChangelist) == nil {
		s.Lists = append([]Changelist{{ID: defaultChangelist, Name: "Default"}}, s.Lists...)
	}
	if s.list(s.Active) == nil {
		s.Active = defaultChangelist
	}
	if s.list(s.Selected) == nil {
		s.Selected = s.Active
	}
	for key, id := range s.Files {
		if s.list(id) == nil {
			s.Files[key] = defaultChangelist
		}
	}
}
func (s *ChangelistState) list(id string) *Changelist {
	for i := range s.Lists {
		if s.Lists[i].ID == id {
			return &s.Lists[i]
		}
	}
	return nil
}
func changelistFileKey(tree WorkingTree, file WorkingFile) string {
	ref := tree.Ref
	if ref == "" {
		ref = "detached:" + tree.Head
	}
	return tree.Repository.Path + "\x00" + ref + "\x00" + file.Path
}
func (a *client) ensureChangelists() { a.changelists.normalize() }
func (a *client) fileChangelist(tree WorkingTree, file WorkingFile) string {
	id := a.changelists.Files[changelistFileKey(tree, file)]
	if a.changelists.list(id) == nil {
		id = a.changelists.Active
	}
	if id == "" {
		id = defaultChangelist
	}
	return id
}
func (a *client) changelistTrees(id string) []WorkingTree {
	var trees []WorkingTree
	for _, tree := range a.workingTrees {
		copy := tree
		copy.Files = nil
		for _, file := range tree.Files {
			if a.fileChangelist(tree, file) == id {
				copy.Files = append(copy.Files, file)
			}
		}
		if len(copy.Files) > 0 {
			trees = append(trees, copy)
		}
	}
	return trees
}
func (a *client) reconcileChangelists() {
	a.ensureChangelists()
	valid := map[string]bool{}
	scopes := map[string]bool{}
	changed := false
	for _, tree := range a.workingTrees {
		if tree.Error != "" {
			continue
		}
		prefix := changelistFileKey(tree, WorkingFile{})
		scopes[prefix] = true
		for _, file := range tree.Files {
			key := changelistFileKey(tree, file)
			valid[key] = true
			if _, exists := a.changelists.Files[key]; exists {
				continue
			}
			id := a.changelists.Active
			if file.OldPath != "" {
				old := file
				old.Path = file.OldPath
				if previous := a.changelists.Files[changelistFileKey(tree, old)]; previous != "" {
					id = previous
				}
			}
			a.changelists.Files[key] = id
			changed = true
		}
	}
	for key := range a.changelists.Files {
		at := strings.LastIndexByte(key, 0)
		if at >= 0 && scopes[key[:at+1]] && !valid[key] {
			delete(a.changelists.Files, key)
			changed = true
		}
	}
	if changed {
		a.persistChangelists()
	}
}
func (a *client) rememberChangelistMessage() {
	a.ensureChangelists()
	a.changelists.list(a.changelists.Selected).Message = a.commitMessage
}
func (a *client) selectChangelist(id string) {
	if a.busy || a.changelists.list(id) == nil {
		return
	}
	a.rememberChangelistMessage()
	a.changelists.Selected = id
	a.commitMessage = a.changelists.list(id).Message
	a.workingAmend = false
	// Match Commit's list selection: choosing a list selects its changes only.
	a.workingSelection = map[string]bool{}
	for _, tree := range a.changelistTrees(id) {
		a.toggleWorkingFiles(tree.Repository.Path, tree.Files, true)
	}
	a.persistChangelists()
}
func (a *client) activateChangelist(id string) {
	a.ensureChangelists()
	if a.busy || a.changelists.list(id) == nil {
		return
	}
	a.changelists.Active = id
	a.selectChangelist(id)
}
func (a *client) moveToChangelist(requests []CommitRequest, id string) {
	a.ensureChangelists()
	if a.busy || a.changelists.list(id) == nil {
		return
	}
	// Move only changes still present in the same branch; stale dialog data
	// must never recreate membership for a committed or reverted file.
	wanted := map[string]bool{}
	for _, request := range requests {
		for _, file := range request.Files {
			wanted[changelistFileKey(request.Tree, file)] = true
		}
	}
	for _, tree := range a.workingTrees {
		for _, file := range tree.Files {
			key := changelistFileKey(tree, file)
			if wanted[key] {
				a.changelists.Files[key] = id
			}
		}
	}
	a.persistChangelists()
}
func (a *client) applyChangelistDialog() {
	a.ensureChangelists()
	d := a.changelistDialog
	if a.busy {
		return
	}
	if d.Kind == "move" {
		if a.changelists.list(d.Target) == nil {
			a.changelistDialog.Error = "请选择目标 Changelist"
			return
		}
		a.moveToChangelist(d.Requests, d.Target)
	} else if d.Kind == "delete" {
		if d.ID == defaultChangelist || a.changelists.list(d.ID) == nil {
			return
		}
		for key, id := range a.changelists.Files {
			if id == d.ID {
				a.changelists.Files[key] = defaultChangelist
			}
		}
		if a.changelists.Selected == d.ID {
			a.selectChangelist(defaultChangelist)
		}
		if a.changelists.Active == d.ID {
			a.changelists.Active = defaultChangelist
		}
		lists := a.changelists.Lists[:0]
		for _, list := range a.changelists.Lists {
			if list.ID != d.ID {
				lists = append(lists, list)
			}
		}
		a.changelists.Lists = lists
	} else {
		name := strings.TrimSpace(d.Name)
		if name == "" {
			a.changelistDialog.Error = "名称不能为空"
			return
		}
		for _, list := range a.changelists.Lists {
			if list.ID != d.ID && strings.EqualFold(list.Name, name) {
				a.changelistDialog.Error = "Changelist 名称已存在"
				return
			}
		}
		if d.Kind == "rename" {
			if list := a.changelists.list(d.ID); list != nil {
				list.Name = name
			}
		} else {
			var id string
			for {
				a.changelists.Next++
				id = fmt.Sprintf("list-%d", a.changelists.Next)
				if a.changelists.list(id) == nil {
					break
				}
			}
			a.changelists.Lists = append(a.changelists.Lists, Changelist{ID: id, Name: name})
			if len(d.Requests) > 0 {
				a.moveToChangelist(d.Requests, id)
			}
			if d.Activate {
				a.activateChangelist(id)
			}
		}
	}
	a.changelistDialog.Open = false
	a.persistChangelists()
}
func (a *client) changelistConfigPath() string {
	if a.changelistsPath != "" {
		return a.changelistsPath
	}
	return filepath.Join(filepath.Dir(configFile()), "changelists.json")
}
func loadChangelists(path string) (ChangelistState, error) {
	var s ChangelistState
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		s.normalize()
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	seen := map[string]bool{}
	for _, list := range s.Lists {
		if list.ID == "" || strings.TrimSpace(list.Name) == "" || seen[list.ID] {
			return ChangelistState{}, fmt.Errorf("Changelist 配置包含无效或重复列表")
		}
		seen[list.ID] = true
	}
	s.normalize()
	return s, nil
}
func saveChangelists(path string, s ChangelistState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "changelists-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (a *client) persistChangelists() {
	a.rememberChangelistMessage()
	// Headless fixtures remain in memory unless explicitly given a config path.
	if a.changelistsLoadError != "" || (a.win == nil && a.changelistsPath == "") {
		return
	}
	data, _ := json.Marshal(a.changelists)
	var snapshot ChangelistState
	_ = json.Unmarshal(data, &snapshot)
	path := a.changelistConfigPath()
	version := a.changelistsSaveVersion.Add(1)
	a.startRead("changelists-save", func(ctx context.Context) func() {
		a.changelistsSaveMu.Lock()
		var err error
		if a.changelistsSaveVersion.Load() == version {
			err = saveChangelists(path, snapshot)
		}
		a.changelistsSaveMu.Unlock()
		return func() {
			if err != nil {
				a.changelistsError = "保存 Changelist 失败：" + err.Error()
			} else {
				a.changelistsError = ""
			}
		}
	})
}

func (a *client) commitChangelistRepository(tree WorkingTree, push bool) {
	if a.busy || a.workingLoading {
		return
	}
	request := CommitRequest{Tree: tree, Amend: a.workingAmend}
	for _, file := range tree.Files {
		if a.workingSelection[workingFileKey(tree.Repository.Path, file.Path)] {
			request.Files = append(request.Files, file)
		}
	}
	if len(request.Files) == 0 {
		return
	}
	if push {
		source := a.outgoingFor(tree.Repository)
		a.changesDialog = changesDialogState{Open: true, Kind: "commit-push", Message: a.commitMessage, Requests: []CommitRequest{request}, PushTargets: []PushTarget{{Source: source, Remote: source.Remote, Destination: strings.TrimPrefix(source.Destination, "refs/heads/")}}}
	} else if strings.TrimSpace(a.commitMessage) != "" {
		a.beginWorkingCommit([]CommitRequest{request}, a.commitMessage, nil)
	}
}

func (a *client) workingTreeScope(tree WorkingTree) string {
	if len(tree.Files) > 0 {
		id := a.fileChangelist(tree, tree.Files[0])
		if id != defaultChangelist {
			return "changelist:" + id + "\x00" + tree.Repository.Path
		}
	}
	return tree.Repository.Path
}
