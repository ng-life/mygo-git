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
	quitPending                                                                                                                    bool
	changeViewMode                                                                                                                 string
	changeViewTrees                                                                                                                []WorkingTree
	changeViewLoading                                                                                                              bool
	editorTabs                                                                                                                     []*editorTab
	editorActive                                                                                                                   string
	editorSequence                                                                                                                 uint64
	applicationMenu                                                                                                                *mygo.Menu
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
			if a.quitPending && a.win != nil {
				mygo.App.Quit()
				return
			}
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
	if len(a.scanFolders) > 0 {
		a.scanFoldersJob(a.scanFolders, false)
		return
	}
	entries := append([]Entry(nil), a.entries...)
	a.jobProgress("Refresh Repositories", func(report taskReporter) (func(), error) {
		repos := inspectWithProgress(entries, report, 0, len(entries))
		return func() {
			a.repos = repos
			a.refreshRepositoryFiles(repos)
			a.reloadWorkingTrees()
			a.output = fmt.Sprintf("Refreshed %d repositories", len(repos))
		}, nil
	})
}
func (a *client) add(roots []string) { a.scanFoldersJob(roots, false) }
func (a *client) scanFoldersJob(roots []string, save bool) {
	a.scanFoldersJobOptions(roots, save, true)
}
func (a *client) scanFoldersJobOptions(roots []string, save, closeSettings bool) {
	roots = append([]string(nil), roots...)
	settingsPath := a.scanConfigPath()
	existing := append([]Entry(nil), a.entries...)
	a.jobProgress("Scan Repositories", func(report taskReporter) (func(), error) {
		report("Scan Directories", 0, 0, nil)
		if save {
			if err := saveScanFolders(settingsPath, roots); err != nil {
				return func() {
					if save {
						a.settings.Error = err.Error()
					}
				}, err
			}
		}
		found, scanErr := discover(roots)
		entries, added, removed := reconcileScanEntries(existing, found, roots, scanErr != nil)
		if err := saveEntries(entries); err != nil {
			return func() {
				if save {
					a.settings.Error = err.Error()
				}
			}, err
		}
		repos := inspectWithProgress(entries, report, 0, len(entries))
		return func() {
			if save {
				a.scanFolders = roots
				a.settings.Open = !closeSettings
				a.settings.Error = ""
				if scanErr != nil {
					a.settings.Error = scanErr.Error()
					a.settings.Open = true
				}
			}
			a.pruneRemovedRepositories(repos)
			a.entries = entries
			a.repos = repos
			a.refreshRepositoryFiles(repos)
			a.reloadWorkingTrees()
			a.output = fmt.Sprintf("Scan complete: %d added, %d removed, %d repositories", added, removed, len(entries))
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
	label := fmt.Sprintf("%s %s · %d repositories", action, branch, len(targets))
	a.jobProgress(label, func(report taskReporter) (func(), error) {
		var results []OperationResult
		if action == "Fetch" || action == "Pull" || action == "Refresh Status" {
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
	a := &client{entries: entries, selected: map[string]bool{}, expanded: map[string]*bool{}, logIndex: -1, fileIndex: -1, workspaceSplit: 55, commitAnchor: -1, output: "Manage individual repositories in Repositories, or right-click local and remote branches for batch operations."}
	if err != nil {
		a.output = "Failed to load configuration: " + err.Error()
	}
	lists, listErr := loadChangelists(a.changelistConfigPath())
	a.changelists = lists
	a.ensureChangelists()
	a.commitMessage = a.changelists.list(a.changelists.Selected).Message
	if listErr != nil {
		a.changelistsLoadError = "Failed to load changelist configuration: " + listErr.Error()
		a.output += "\n" + a.changelistsLoadError
	}
	marks, markErr := loadReviewMarks(a.reviewMarksPath())
	a.reviewMarks = marks
	if markErr != nil {
		a.output += "\nFailed to load review progress: " + markErr.Error()
	}
	folders, folderErr := loadScanFolders(a.scanConfigPath())
	a.scanFolders = folders
	if folderErr != nil {
		a.output += "\nFailed to load scan settings: " + folderErr.Error()
	}
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !a.canQuitSafely() {
			e.PreventDefault()
		}
	})
	mygo.App.WhenReady(func() {
		a.installApplicationMenu()
		a.win = mygo.NewWindow(mygo.WindowOptions{Title: "MyGo Git · Multi-Repository Management", Width: 1560, Height: 940, Content: ui.View(a.view)})
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if !a.canQuitSafely() {
				e.PreventDefault()
			}
		})
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
		a.add(a.scanFolders)
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
			log.Printf("Failed to save changelists: %v", err)
		}
	}
	// The UI loop has stopped; flush the latest progress before process exit.
	if a.reviewSaveVersion.Load() > 0 {
		a.reviewSaveVersion.Add(1)
		a.reviewSaveMu.Lock()
		err := saveReviewMarks(a.reviewMarksPath(), a.reviewMarks)
		a.reviewSaveMu.Unlock()
		if err != nil {
			log.Printf("Failed to save review progress: %v", err)
		}
	}
}
