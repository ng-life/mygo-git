package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

type fileIconSpec struct {
	Kind, Badge string
	Color       ui.Color
}

func fileType(path string) fileIconSpec {
	name := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(name))
	spec := func(kind, badge, color string) fileIconSpec { return fileIconSpec{kind, badge, ui.Hex(color)} }
	switch {
	case name == "go.mod" || name == "go.sum" || name == "go.work" || name == "go.work.sum" || ext == ".go":
		return spec("Go", "Go", "#61afc3")
	case name == "pom.xml" || name == "mvnw" || name == "mvnw.cmd" || strings.HasSuffix(name, ".gradle") || strings.HasSuffix(name, ".gradle.kts") || ext == ".java" || ext == ".class" || ext == ".jar" || ext == ".war":
		return spec("Java", "J", "#cc975e")
	case ext == ".py" || ext == ".pyi" || ext == ".pyw" || ext == ".pyc" || ext == ".pyd" || ext == ".ipynb" || name == "pyproject.toml" || name == "uv.lock" || name == "poetry.lock" || name == "pipfile" || name == "pipfile.lock" || name == "setup.cfg" || name == "tox.ini" || name == ".python-version" || strings.HasPrefix(name, "requirements") && (ext == ".txt" || ext == ".in"):
		return spec("Python", "Py", "#d0b75e")
	case ext == ".ts" || ext == ".tsx" || ext == ".mts" || ext == ".cts" || strings.HasPrefix(name, "tsconfig") && ext == ".json":
		return spec("TypeScript", "TS", "#75a5db")
	case ext == ".js" || ext == ".jsx" || ext == ".mjs" || ext == ".cjs" || name == "package.json" || name == "package-lock.json" || name == "pnpm-lock.yaml" || name == "yarn.lock" || name == "bun.lock" || name == "bun.lockb" || name == ".npmrc" || name == ".nvmrc" || strings.HasPrefix(name, "jsconfig") || strings.Contains(name, "eslint") || strings.Contains(name, "prettier") || strings.HasPrefix(name, ".babel"):
		return spec("JavaScript", "JS", "#c9b85e")
	case ext == ".md" || ext == ".markdown" || ext == ".mdown" || ext == ".mkd" || ext == ".mdx" || name == "readme" || name == "changelog":
		return spec("Markdown", "M↓", "#8fa9c7")
	case ext == ".html" || ext == ".htm" || ext == ".xhtml":
		return spec("HTML", "<>", "#ce8b67")
	case ext == ".css" || ext == ".scss" || ext == ".sass" || ext == ".less":
		return spec("Stylesheet", "#", "#a68bbd")
	case strings.HasPrefix(name, "dockerfile") || strings.HasPrefix(name, "compose.") || strings.HasPrefix(name, "docker-compose."):
		return spec("Docker", "D", "#74a2cb")
	case strings.HasPrefix(name, ".git") || name == ".gitignore":
		return spec("Git", "git", "#c18870")
	case ext == ".json" || ext == ".jsonc":
		return spec("JSON", "{}", "#c7b46c")
	case ext == ".xml" || ext == ".xsd" || ext == ".xsl":
		return spec("XML", "<>", "#b6a56f")
	case ext == ".yaml" || ext == ".yml" || ext == ".toml" || ext == ".ini" || ext == ".conf" || ext == ".properties" || name == ".editorconfig" || name == ".env" || strings.HasPrefix(name, ".env."):
		return spec("Configuration", "cfg", "#8fa5a7")
	case ext == ".sh" || ext == ".bash" || ext == ".zsh" || ext == ".ps1" || ext == ".bat" || ext == ".cmd" || name == "makefile":
		return spec("Script", ">_", "#94ad80")
	case ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".svg" || ext == ".webp" || ext == ".ico":
		return spec("Image", "img", "#a69ab8")
	}
	return spec("File", "", "#909da6")
}
func fileTypeIcon(c *ui.Context, path string) {
	spec := fileType(path)
	ui.Box(c).Width(16).Height(16).Shrink(0).Label(spec.Kind + " icon").Tooltip(spec.Kind).Children(func() {
		ui.Icon(c, workingFileIcon).Width(16).Height(16).TextColor(spec.Color)
		if spec.Badge != "" {
			ui.Text(c, spec.Badge).Absolute().Left(0).Top(5).Width(16).Height(10).Font("monospace").FontSize(8).Bold().TextAlign(ui.Center).Background(ideaPanel).TextColor(spec.Color).Unselectable()
		}
	})
}
func fileTreeItem(c *ui.Context, label, path string) ui.Element {
	return ui.TreeItem(c, "", nil, nil).Gap(4).Label(label).Children(func() {
		fileTypeIcon(c, path)
		ui.Text(c, label).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
	})
}

func folderTypeIcon(c *ui.Context) {
	ui.Icon(c, workingFolderIcon).Width(16).Height(16).Shrink(0).TextColor(ui.Hex("#9ba9b1")).Label("Folder icon").Tooltip("Directory")
}
func folderTreeItem(c *ui.Context, label string, open *bool, children func()) ui.Element {
	return ui.TreeItem(c, "", open, func() {
		// Match the 16px icon plus 4px gap at the next tree level.
		ui.Column(c).PaddingStart(4).Children(children)
	}).Gap(4).Label(label).Children(func() {
		folderTypeIcon(c)
		ui.Text(c, label).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
	})
}

var referenceBranchIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><g fill="none" stroke="currentColor" stroke-width="1.4"><circle cx="4" cy="3" r="1.7"/><circle cx="12" cy="3" r="1.7"/><circle cx="4" cy="13" r="1.7"/><path d="M4 5v6M12 5c0 4-8 0-8 5"/></g></svg>`))

func referenceTreeItem(c *ui.Context, label string, open *bool, children func()) ui.Element {
	if open != nil && children != nil {
		return folderTreeItem(c, label, open, children)
	}
	return ui.TreeItem(c, "", nil, nil).Gap(4).Label(label).Children(func() {
		ui.Icon(c, referenceBranchIcon).Width(16).Height(16).Shrink(0).TextColor(statusBlue).Label("Ref icon")
		ui.Text(c, label).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
	})
}
