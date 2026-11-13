package main

import (
	"encoding/json"
	"fmt"
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
		ui.Column(c).Width(720).MaxHeight(620).Gap(12).Children(func() {
			ui.Text(c, "设置 · 扫描文件夹").FontSize(18).Bold()
			ui.Text(c, "可以添加 Git 仓库本身或父文件夹。所选文件夹为第 0 层，最多向下扫描 3 层；找到仓库后停止深入，不扫描其内部仓库。").FontSize(12)
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c.Key("scan-folder-path"), &a.settings.Path).Grow(1).MinWidth(0).Placeholder("Git 项目或父文件夹路径").Label("扫描文件夹路径").Disabled(a.busy)
				ui.Button(c, "添加文件夹").Disabled(a.busy || strings.TrimSpace(a.settings.Path) == "").OnClick(func() { a.appendScanFolders([]string{a.settings.Path}) })
				ui.Button(c, "选择文件夹…").Disabled(a.busy).OnClick(func() {
					paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "选择扫描文件夹（可多选）", Directory: true, Multiple: true})
					if err != nil {
						a.settings.Error = err.Error()
					} else {
						a.appendScanFolders(paths)
					}
				})
			})
			ui.Scroll(c).MaxHeight(300).Gap(6).Children(func() {
				if len(a.settings.Folders) == 0 {
					ui.Text(c, "尚未添加扫描文件夹").TextColor(c.Theme().TextMuted)
				}
				for i, path := range a.settings.Folders {
					ui.Row(c.Key(path)).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, path).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").Tooltip(path)
						ui.Button(c, "移除").Label("移除扫描文件夹 " + path).Disabled(a.busy).OnClick(func() { a.settings.Folders = append(a.settings.Folders[:i], a.settings.Folders[i+1:]...) })
					})
				}
			})
			if a.settings.Error != "" {
				ui.Text(c, a.settings.Error).FontSize(12)
			}
			ui.Text(c, "保存后立即扫描，启动时自动扫描。移除扫描文件夹后，已添加的仓库仍保留。扫描不会跟随子目录符号链接。").FontSize(11).TextColor(c.Theme().TextMuted)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "取消").Disabled(a.busy).OnClick(func() { a.settings.Open = false })
				ui.PrimaryButton(c, fmt.Sprintf("保存并扫描 (%d)", len(a.settings.Folders))).Label("保存并扫描").Disabled(a.busy).OnClick(func() { a.scanFoldersJob(a.settings.Folders, true) })
			})
		})
	})
}
