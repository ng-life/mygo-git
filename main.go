package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type client struct {
	tasks                                                                                                                          backgroundTasks
	taskPanelOpen, shortcutHelpOpen                                                                                                bool
	readTaskIDs                                                                                                                    map[string]uint64
	repositorySearchHandle, logsSearchHandle, logSearchHandle, compareSearchHandle, compareCommitSearchHandle, commitMessageHandle ui.Handle
	compare                                                                                                                        CompareState
	compareSplit                                                                                                                   float32
	messageHistory                                                                                                                 commitMessageHistoryState
	refSelection                                                                                                                   []ReferenceChoice
	refAnchor                                                                                                                      string
	refDialog                                                                                                                      referenceDialogState
	logGraphKey                                                                                                                    uint64
	logGraphRows                                                                                                                   []logGraphRow
	logGraphWidth                                                                                                                  float32
	logQuery                                                                                                                       string
	logFilesSplit                                                                                                                  float32
	changelists                                                                                                                    ChangelistState
	changelistDialog                                                                                                               changelistDialogState
	changelistsPath, changelistsError, changelistsLoadError                                                                        string
	changelistsSaveMu                                                                                                              sync.Mutex
	changelistsSaveVersion                                                                                                         atomic.Uint64
	changesSplit                                                                                                                   float32
	workingAmend                                                                                                                   bool
	outgoing                                                                                                                       []Outgoing
	outgoingSelection                                                                                                              map[string]bool
	changesDialog                                                                                                                  changesDialogState
	logsQuery                                                                                                                      string
	groupTags                                                                                                                      bool
	tagDialog                                                                                                                      tagDialogState
	settings                                                                                                                       scanSettingsState
	scanFolders                                                                                                                    []string
	scanSettingsPath                                                                                                               string
	reviewSaveMu                                                                                                                   sync.Mutex
	reviewSaveVersion                                                                                                              atomic.Uint64
	review                                                                                                                         ReviewSession
	reviewMarks                                                                                                                    map[string]string
	reviewProgressPath                                                                                                             string
	repositoryFiles                                                                                                                map[string]repositoryFileState
	repositoryQuery                                                                                                                string
	branchDeletion                                                                                                                 branchDeletion
	diffSideBySide                                                                                                                 bool
	sidebarWidths                                                                                                                  [3]float32
	codeNavigation                                                                                                                 navigationState
	workingCollapsed                                                                                                               map[string]bool
	workingPending                                                                                                                 []Repository
	readCancels                                                                                                                    map[string]context.CancelFunc
	historyCache                                                                                                                   map[string]historyCacheEntry
	historyScope                                                                                                                   string
	win                                                                                                                            *mygo.Window
	entries                                                                                                                        []Entry
	repos                                                                                                                          []Repository
	selected                                                                                                                       map[string]bool
	expanded                                                                                                                       map[string]*bool
	group, query, output, branch                                                                                                   string
	busy                                                                                                                           bool
	groupRemote                                                                                                                    bool
	repositoryPath                                                                                                                 string
	tab                                                                                                                            int
	commits                                                                                                                        []Commit
	details                                                                                                                        []CommitDetail
	files                                                                                                                          []MergedFile
	comparisons                                                                                                                    []CommitComparison
	commitSelection                                                                                                                ui.Selection[string]
	activeCommits                                                                                                                  []Commit
	commitAnchor                                                                                                                   int
	selectedCommit, selectedFile                                                                                                   string
	historyError, detailError                                                                                                      string
	historyLoading, detailLoading, comparisonLoading                                                                               bool
	historyGeneration, detailGeneration, fileGeneration                                                                            uint64
	closed                                                                                                                         bool
	readRunner                                                                                                                     func(func() func())
	logRows, fileRows                                                                                                              ui.ListState
	logIndex, fileIndex                                                                                                            int
	workspaceSplit                                                                                                                 float32
	showOutput                                                                                                                     bool
	writeRunner                                                                                                                    func(func() func())
	workingTrees                                                                                                                   []WorkingTree
	workingSelection                                                                                                               map[string]bool
	workingLoading                                                                                                                 bool
	workingGeneration, workingFileGeneration                                                                                       uint64
	workingFileKey                                                                                                                 string
	workingFileTree                                                                                                                WorkingTree
	workingFile                                                                                                                    WorkingFile
	workingComparison                                                                                                              FileComparison
	workingComparisonError                                                                                                         string
	workingComparisonLoading                                                                                                       bool
	commitMessage                                                                                                                  string
	workingResults                                                                                                                 []OperationResult
}

func (a *client) job(label string, work func() (func(), error)) {
	a.jobProgress(label, func(taskReporter) (func(), error) { return work() })
}
func (a *client) jobProgress(label string, work func(taskReporter) (func(), error)) {
	if a.busy {
		return
	}
	a.busy = true
	a.output = label + "…"
	taskID := a.tasks.begin(label, true)
	store := &a.tasks
	win := a.win
	report := taskReporter(func(detail string, done, total int, err error) {
		store.report(taskID, detail, done, total, err)
		if win != nil {
			win.Update(func() {})
		}
	})
	run := func() func() {
		apply, err := work(report)
		return func() {
			if a.closed {
				store.finish(taskID, context.Canceled)
				return
			}
			if apply != nil {
				apply()
			}
			if err != nil {
				a.output += "\n" + err.Error()
			}
			store.finish(taskID, err)
			a.busy = false
			a.reloadHistory()
			if a.review.Open {
				if a.compare.Active {
					a.beginReview(a.compare.Request)
				} else {
					a.beginReview(a.review.Request)
				}
			}
		}
	}
	if a.writeRunner != nil {
		a.writeRunner(run)
		return
	}
	go func() { apply := run(); win.Update(apply) }()
}
func inspectAll(entries []Entry) []Repository {
	repos := make([]Repository, 0, len(entries))
	for _, e := range entries {
		repos = append(repos, inspect(e))
	}
	return repos
}
func (a *client) refresh() {
	entries := append([]Entry(nil), a.entries...)
	a.jobProgress("刷新仓库", func(report taskReporter) (func(), error) {
		repos := inspectWithProgress(entries, report, 0, len(entries))
		return func() {
			a.repos = repos
			a.refreshRepositoryFiles(repos)
			a.reloadWorkingTrees()
			a.output = fmt.Sprintf("已刷新 %d 个仓库", len(repos))
		}, nil
	})
}
func (a *client) add(roots []string) { a.scanFoldersJob(roots, false) }
func (a *client) scanFoldersJob(roots []string, save bool) {
	roots = append([]string(nil), roots...)
	settingsPath := a.scanConfigPath()
	existing := append([]Entry(nil), a.entries...)
	a.jobProgress("扫描仓库", func(report taskReporter) (func(), error) {
		report("扫描目录", 0, 0, nil)
		if save {
			if err := saveScanFolders(settingsPath, roots); err != nil {
				return nil, err
			}
		}
		found, scanErr := discover(roots)
		seen := map[string]bool{}
		for _, e := range existing {
			seen[e.Path] = true
		}
		added := 0
		for _, e := range found {
			if !seen[e.Path] {
				existing = append(existing, e)
				seen[e.Path] = true
				added++
			}
		}
		if err := saveEntries(existing); err != nil {
			return nil, err
		}
		repos := inspectWithProgress(existing, report, 0, len(existing))
		return func() {
			if save {
				a.scanFolders = roots
				a.settings.Open = false
			}
			a.entries = existing
			a.repos = repos
			a.refreshRepositoryFiles(repos)
			a.reloadWorkingTrees()
			a.output = fmt.Sprintf("扫描完成，新增 %d 个仓库", added)
		}, scanErr
	})
}
func (a *client) visible() []Repository {
	var repos []Repository
	for _, r := range a.repos {
		if a.repositoryPath != "" && r.Path != a.repositoryPath {
			continue
		}
		name := r.Name
		if name == "" {
			name = filepath.Base(r.Path)
		}
		if a.group != "" {
			match := false
			branches := repositoryBranches(r, a.groupRemote)
			if a.groupTags {
				branches = r.Tags
			}
			for _, branch := range branches {
				key := "/" + branch
				if key == a.group || strings.HasPrefix(key, a.group+"/") {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		if a.query != "" && !strings.Contains(strings.ToLower(name+" "+r.Path+" "+r.Branch), strings.ToLower(a.query)) {
			continue
		}
		repos = append(repos, r)
	}
	return repos
}
func (a *client) targets() []Repository {
	var out []Repository
	for _, r := range a.repos {
		if a.selected[r.Path] {
			out = append(out, r)
		}
	}
	return out
}
func (a *client) runBranchAction(action, branch string, remote bool, targets []Repository) {
	if a.busy || len(targets) == 0 {
		return
	}
	entries := entriesFor(targets)
	label := fmt.Sprintf("%s %s · %d 个仓库", action, branch, len(targets))
	a.jobProgress(label, func(report taskReporter) (func(), error) {
		var results []OperationResult
		if action == "Fetch" || action == "Pull" || action == "刷新状态" {
			for i, r := range targets {
				report(action+" · "+r.Name, i, len(targets)*2, nil)
				out, err := runRepositoryOperation(r.Path, action)
				results = append(results, OperationResult{Repository: r, Output: out, Err: err})
				report(action+" · "+r.Name, i+1, len(targets)*2, err)
			}
		} else {
			results = executeBranchBatch(targets, action, branch, remote, report)
		}
		repos := inspectWithProgress(entries, report, len(targets), len(targets)*2)
		return func() {
			a.mergeRepositories(repos)
			if action == "delete" && !a.groupTags && a.groupRemote == remote && a.group == "/"+branch && len(branchTargets(a.repos, branch, remote)) == 0 {
				a.group = ""
				a.branch = ""
			}
			a.output = label + "\n\n" + formatResults(results)
		}, nil
	})
}

func main() {
	// Development restarts follow the normal quit path, including config flush.
	quitSignals := make(chan os.Signal, 1)
	signal.Notify(quitSignals, os.Interrupt, syscall.SIGTERM)
	quitDone := make(chan struct{})
	defer close(quitDone)
	defer signal.Stop(quitSignals)
	go func() {
		select {
		case <-quitSignals:
			mygo.App.Quit()
		case <-quitDone:
		}
	}()

	mygo.Theme.SetSource(mygo.ThemeDark)
	entries, err := loadEntries()
	a := &client{entries: entries, selected: map[string]bool{}, expanded: map[string]*bool{}, logIndex: -1, fileIndex: -1, workspaceSplit: 55, commitAnchor: -1, output: "从仓库列表管理单个仓库，或右键本地、远程分支批量操作。"}
	if err != nil {
		a.output = "配置读取失败：" + err.Error()
	}
	lists, listErr := loadChangelists(a.changelistConfigPath())
	a.changelists = lists
	a.ensureChangelists()
	a.commitMessage = a.changelists.list(a.changelists.Selected).Message
	if listErr != nil {
		a.changelistsLoadError = "Changelist 配置读取失败：" + listErr.Error()
		a.output += "\n" + a.changelistsLoadError
	}
	marks, markErr := loadReviewMarks(a.reviewMarksPath())
	a.reviewMarks = marks
	if markErr != nil {
		a.output += "\n审查进度读取失败：" + markErr.Error()
	}
	folders, folderErr := loadScanFolders(a.scanConfigPath())
	a.scanFolders = folders
	if folderErr != nil {
		a.output += "\n扫描设置读取失败：" + folderErr.Error()
	}
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{Title: "MyGo Git · 多仓库管理", Width: 1560, Height: 940, Content: ui.View(a.view)})
		a.win.OnClosed(func() {
			for lane := range a.readCancels {
				a.cancelRead(lane)
			}
			a.closed = true
			a.historyGeneration++
			a.detailGeneration++
			a.fileGeneration++
			a.workingGeneration++
			a.workingFileGeneration++
		})
		if len(a.scanFolders) > 0 {
			a.add(a.scanFolders)
		} else if len(entries) > 0 {
			a.refresh()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	if a.changelistsSaveVersion.Load() > 0 && a.changelistsLoadError == "" {
		a.rememberChangelistMessage()
		a.changelistsSaveVersion.Add(1)
		a.changelistsSaveMu.Lock()
		err := saveChangelists(a.changelistConfigPath(), a.changelists)
		a.changelistsSaveMu.Unlock()
		if err != nil {
			log.Printf("保存 Changelist 失败：%v", err)
		}
	}
	// The UI loop has stopped; flush the latest progress before process exit.
	if a.reviewSaveVersion.Load() > 0 {
		a.reviewSaveVersion.Add(1)
		a.reviewSaveMu.Lock()
		err := saveReviewMarks(a.reviewMarksPath(), a.reviewMarks)
		a.reviewSaveMu.Unlock()
		if err != nil {
			log.Printf("保存审查进度失败：%v", err)
		}
	}
}
