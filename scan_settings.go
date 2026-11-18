package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type scanSettingsState struct {
	Open        bool
	Folders     []string
	Path, Error string
}

func (a *client) scanConfigPath() string {
	if a.scanSettingsPath != "" {
		return a.scanSettingsPath
	}
	return filepath.Join(filepath.Dir(configFile()), "settings.json")
}
func loadScanFolders(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var settings struct {
		Folders []string `json:"scanFolders"`
	}
	err = json.Unmarshal(data, &settings)
	return settings.Folders, err
}
func saveScanFolders(path string, folders []string) error {
	data, err := json.MarshalIndent(struct {
		Folders []string `json:"scanFolders"`
	}{folders}, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "settings-*.json")
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
func (a *client) openScanSettings() {
	a.settings = scanSettingsState{Open: true, Folders: append([]string(nil), a.scanFolders...)}
}
func (a *client) appendScanFolders(paths []string) {
	seen := map[string]bool{}
	for _, path := range a.settings.Folders {
		seen[path] = true
	}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if path == "~" {
			home, err := os.UserHomeDir()
			if err != nil {
				a.settings.Error = err.Error()
				continue
			}
			path = home
		} else if strings.HasPrefix(path, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				a.settings.Error = err.Error()
				continue
			}
			path = filepath.Join(home, path[2:])
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			a.settings.Error = err.Error()
			continue
		}
		path = filepath.Clean(abs)
		// Avoid filesystem I/O here: directory validation and canonical deduplication
		// happen in the background scanner, including paths currently unavailable.
		if !seen[path] {
			seen[path] = true
			a.settings.Folders = append(a.settings.Folders, path)
		}
	}
	a.settings.Path = ""
}
func (a *client) scanSettingsPanel(c *ui.Context) {
	if !a.settings.Open {
		return
	}
	ui.Modal(c.Key("scan-settings"), &a.settings.Open, func() {
		ui.Column(c).Width(920).Height(600).Gap(0).Background(ideaPanel).Children(func() {
			ui.Text(c, "Settings").Height(36).Padding(8, 12).FontSize(14).Bold()
			ui.Row(c).Grow(1).MinHeight(0).Gap(1).Background(ideaBorder).Children(func() {
				ui.Column(c).Width(205).FillHeight().Padding(8, 0).Gap(2).Background(ideaEditor).Children(func() {
					ui.Text(c, "Version Control").Padding(6, 12).FontSize(12).Bold()
					ui.Text(c, "Git").Padding(6, 28).FillWidth().Background(ideaSelection).TextColor(c.Theme().AccentText).FontSize(12)
				})
				ui.Column(c).Grow(1).MinWidth(0).FillHeight().Padding(16).Gap(12).Background(ideaPanel).Children(func() {
					ui.Text(c, "Version Control  ›  Git").FontSize(15).Bold()
					ui.Text(c, "Scan Roots").FontSize(12).Bold()
					ui.Text(c, "Add Git repositories or parent directories to manage.").FontSize(12).TextColor(c.Theme().TextMuted)
					ui.Row(c).Gap(6).Children(func() {
						ui.TextInput(c.Key("scan-folder-path"), &a.settings.Path).Grow(1).MinWidth(0).Placeholder("Git repository or parent directory path").Label("Scan root path").Disabled(a.busy)
						ui.Button(c, "Add").Label("Add Directory").Disabled(a.busy || strings.TrimSpace(a.settings.Path) == "").OnClick(func() { a.appendScanFolders([]string{a.settings.Path}) })
						ui.Button(c, "Browse…").Label("Choose Directory…").Disabled(a.busy).OnClick(func() {
							paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "Select Scan Roots (multiple selection)", Directory: true, Multiple: true})
							if err != nil {
								a.settings.Error = err.Error()
							} else {
								a.appendScanFolders(paths)
							}
						})
					})
					ui.Column(c).Grow(1).MinHeight(0).Gap(1).Padding(1).Background(ideaBorder).Children(func() {
						ui.Text(c, "Directory").Height(28).Padding(6, 8).FillWidth().Background(ideaToolbar).FontSize(11).Bold()
						ui.Scroll(c).Grow(1).FillWidth().Padding(2).Gap(1).Background(ideaEditor).Children(func() {
							if len(a.settings.Folders) == 0 {
								ui.Text(c, "No scan roots configured").Padding(12).TextColor(c.Theme().TextMuted)
							}
							for i, path := range a.settings.Folders {
								ui.Row(c.Key(path)).Height(30).Padding(2, 6).Gap(8).AlignItems(ui.Center).Children(func() {
									ui.Text(c, path).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").Tooltip(path).FontSize(12)
									ui.Button(c, "−").Width(24).Height(24).Label("Remove scan root " + path).Tooltip("Remove Scan Root").Disabled(a.busy).OnClick(func() { a.settings.Folders = append(a.settings.Folders[:i], a.settings.Folders[i+1:]...) })
								})
							}
						})
					})
					ui.Text(c, "Scan depth: 3 levels. Stop at Git repositories. Directory symlinks are not followed.").FontSize(11).TextColor(c.Theme().TextMuted)
					ui.Text(c, "Repositories outside these roots are removed from the list. Files on disk are preserved.").FontSize(11).TextColor(c.Theme().TextMuted)
					if a.settings.Error != "" {
						ui.Text(c, a.settings.Error).FontSize(12).TextColor(statusOrange)
					}
				})
			})
			ui.Row(c).Height(48).Padding(10, 12).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Textf(c, "%d scan roots", len(a.settings.Folders)).FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Spacer(c)
				ui.Button(c, "Cancel").Width(80).Disabled(a.busy).OnClick(func() { a.settings.Open = false })
				ui.Button(c, "Apply").Width(80).Disabled(a.busy).Tooltip("Save and rescan repositories").OnClick(func() { a.scanFoldersJobOptions(a.settings.Folders, true, false) })
				ui.PrimaryButton(c, "OK").Width(80).Disabled(a.busy).Tooltip("Save and rescan repositories").OnClick(func() { a.scanFoldersJob(a.settings.Folders, true) })
			})
		})
	})
}
