package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Repository struct {
	Tags                              []string
	TagObjects                        map[string]string
	Ahead                             int
	PushBase, PushError               string
	Path, Name, Branch, Status, Error string
	Branches                          []string
	RemoteBranches                    []string
}
type Entry struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

// Commands never pass through a shell; interactive authentication is disabled.
func git(path string, args ...string) (string, error) {
	return gitContext(context.Background(), path, args...)
}
func gitContext(ctx context.Context, path string, args ...string) (string, error) {
	out, err := commandGit(ctx, 90*time.Second, path, args...)
	return strings.TrimSpace(string(out)), err
}

func remoteName(remote, fallback string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return fallback
	}
	var name string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err == nil {
			name = u.Path
		}
	} else if i := strings.Index(remote, ":"); i >= 0 && !filepath.IsAbs(remote) {
		name = remote[i+1:]
	} else {
		name = filepath.Base(remote)
	}
	name = strings.Trim(strings.TrimSuffix(name, ".git"), "/")
	if name == "" {
		return fallback
	}
	return name
}
func inspect(e Entry) Repository {
	r := Repository{Path: e.Path, Name: e.Name}
	root, err := git(e.Path, "rev-parse", "--show-toplevel")
	if err != nil {
		r.Error = "仓库不可用：" + root
		return r
	}
	if r.Name == "" {
		remote, _ := git(root, "remote", "get-url", "origin")
		r.Name = remoteName(remote, filepath.Base(root))
	}
	branches, branchErr := git(root, "for-each-ref", "--format=%(refname:strip=2)", "refs/heads/")
	if branchErr != nil {
		r.Error = branches
	} else if branches != "" {
		r.Branches = strings.Split(branches, "\n")
	}
	remoteRefs, remoteErr := git(root, "for-each-ref", "--format=%(refname:strip=2) %(symref)", "refs/remotes/")
	if remoteErr != nil {
		r.Error = remoteRefs
	} else {
		for _, line := range strings.Split(remoteRefs, "\n") {
			fields := strings.Fields(line)
			// Remote HEAD aliases are pointers, not actionable branches.
			if len(fields) == 1 {
				r.RemoteBranches = append(r.RemoteBranches, fields[0])
			}
		}
	}
	tags, tagErr := git(root, "for-each-ref", "--format=%(refname:strip=2)%00%(objectname)", "refs/tags/")
	if tagErr != nil {
		r.Error = "读取 Tags：" + tagErr.Error()
	} else {
		r.TagObjects = map[string]string{}
		for _, line := range strings.Split(tags, "\n") {
			name, object, ok := strings.Cut(line, "\x00")
			if ok {
				r.Tags = append(r.Tags, name)
				r.TagObjects[name] = object
			}
		}
	}
	r.Branch, _ = git(root, "symbolic-ref", "--short", "HEAD")
	if r.Branch == "" {
		sha, _ := git(root, "rev-parse", "--short", "HEAD")
		r.Branch = "detached " + sha
	}
	tree := readWorkingTree(r)
	if tree.Error != "" {
		r.Error = tree.Error
	}
	var status []string
	for _, file := range tree.Files {
		status = append(status, file.Status+" "+file.Path)
	}
	r.Status = strings.Join(status, "\n")
	readPushStatus(&r)
	return r
}
func discover(roots []string) ([]Entry, error) {
	seen := map[string]bool{}
	var entries []Entry
	var problems []string
	for _, root := range roots {
		root, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		canonicalRoot, resolveErr := filepath.EvalSymlinks(root)
		if resolveErr != nil {
			problems = append(problems, root+": "+resolveErr.Error())
			continue
		}
		root = canonicalRoot
		info, statErr := os.Stat(root)
		if statErr != nil || !info.IsDir() {
			problems = append(problems, root+": 不是可读取的文件夹")
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				problems = append(problems, path+": "+walkErr.Error())
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if path != root && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				canonical, err := filepath.EvalSymlinks(path)
				if err != nil {
					return err
				}
				if !seen[canonical] {
					seen[canonical] = true
					entries = append(entries, Entry{Path: canonical})
				}
				return filepath.SkipDir
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			depth := 0
			if relative != "." {
				depth = len(strings.Split(relative, string(filepath.Separator)))
			}
			if depth >= 3 {
				return filepath.SkipDir
			}
			return nil
		})
		if err != nil {
			return entries, err
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	if len(problems) > 0 {
		return entries, fmt.Errorf("部分目录未扫描：%s", strings.Join(problems, "\n"))
	}
	return entries, nil
}
func configFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "mygo-git.json"
	}
	return filepath.Join(dir, "mygo-git", "repositories.json")
}
func loadEntries() ([]Entry, error) {
	data, err := os.ReadFile(configFile())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	err = json.Unmarshal(data, &entries)
	return entries, err
}
func saveEntries(entries []Entry) error {
	path := configFile()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "repos-*.json")
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

type TreeNode struct {
	Name, Key string
	Count     int
	Branch    string
	Children  []*TreeNode
}

// branchTree merges identical full branch names across repositories.
func branchTree(repos []Repository, remote bool) *TreeNode {
	root := &TreeNode{}
	for _, r := range repos {
		counted := map[string]bool{}
		for _, branch := range repositoryBranches(r, remote) {
			parts := strings.Split(branch, "/")
			node := root
			for _, part := range parts {
				key := node.Key + "/" + part
				var next *TreeNode
				for _, child := range node.Children {
					if child.Key == key {
						next = child
						break
					}
				}
				if next == nil {
					next = &TreeNode{Name: part, Key: key}
					node.Children = append(node.Children, next)
				}
				if !counted[key] {
					next.Count++
					counted[key] = true
				}
				node = next
			}
			node.Branch = branch
		}
	}
	var order func(*TreeNode)
	order = func(n *TreeNode) {
		sort.Slice(n.Children, func(i, j int) bool { return n.Children[i].Name < n.Children[j].Name })
		for _, c := range n.Children {
			order(c)
		}
	}
	order(root)
	return root
}

func repositoryBranches(r Repository, remote bool) []string {
	if remote {
		return r.RemoteBranches
	}
	return r.Branches
}

// branchTargets uses exact full names and intentionally ignores UI selection/search.
func branchTargets(repos []Repository, branch string, remote bool) []Repository {
	var targets []Repository
	for _, r := range repos {
		for _, b := range repositoryBranches(r, remote) {
			if b == branch {
				targets = append(targets, r)
				break
			}
		}
	}
	return targets
}
