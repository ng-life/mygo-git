package main

import (
	"runtime"

	"github.com/egoist/mygo"
)

func (a *client) applicationActionEnabled(action string) bool {
	if a.closed || a.shortcutModalOpen() {
		return false
	}
	switch action {
	case "close-editor":
		return len(a.editorTabs) > 0
	case "settings", "refresh-all":
		return !a.busy
	case "review":
		return len(a.repos) > 0 && !(a.tab == 2 && len(a.refSelection) > 2)
	}
	return true
}

// Native menu callbacks run on the main thread; Update also schedules a UI rebuild.
func (a *client) applicationAction(action string) {
	apply := func() {
		if !a.applicationActionEnabled(action) {
			return
		}
		switch action {
		case "close-editor":
			a.closeActiveEditor()
		case "settings":
			a.openScanSettings()
		case "refresh-all":
			a.refresh()
		case "review":
			a.openToolbarReview()
		case "shortcuts":
			a.shortcutHelpOpen = true
		case "results":
			a.showOutput = !a.showOutput
		case "repositories":
			a.switchTab(0)
		case "changes":
			a.switchTab(1)
		case "logs":
			a.switchTab(2)
		}
	}
	if a.win != nil {
		a.win.Update(apply)
	} else {
		apply()
	}
}

func (a *client) applicationMenuTemplate(goos string) []*mygo.MenuItem {
	item := func(id, label, shortcut string) *mygo.MenuItem {
		return &mygo.MenuItem{ID: id, Label: label, Accelerator: shortcut, Disabled: !a.applicationActionEnabled(id), Click: func(*mygo.MenuItem, *mygo.Window) { a.applicationAction(id) }}
	}
	settings := item("settings", "Settings…", "CmdOrCtrl+,")
	file := &mygo.MenuItem{Label: "File", Submenu: []*mygo.MenuItem{item("close-editor", "Close Tab", "CmdOrCtrl+W"), {Role: mygo.RoleClose, Label: "Close Window", Accelerator: "CmdOrCtrl+Shift+W"}}}
	menus := []*mygo.MenuItem{}
	if goos == "darwin" {
		menus = append(menus, &mygo.MenuItem{Label: "MyGo Git", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout, Label: "About MyGo Git"}, mygo.Separator(), settings, mygo.Separator(),
			{Role: mygo.RoleServices, Label: "Services"}, mygo.Separator(),
			{Role: mygo.RoleHide, Label: "Hide MyGo Git"}, {Role: mygo.RoleHideOthers, Label: "Hide Others"}, {Role: mygo.RoleUnhide, Label: "Show All"}, mygo.Separator(),
			{Role: mygo.RoleQuit, Label: "Quit MyGo Git"},
		}})
	} else {
		file.Submenu = append([]*mygo.MenuItem{settings, mygo.Separator()}, file.Submenu...)
		file.Submenu = append(file.Submenu, mygo.Separator(), &mygo.MenuItem{Role: mygo.RoleQuit, Label: "Exit"})
	}
	results := item("results", "Operation Results", "")
	results.Type, results.Checked = mygo.MenuItemCheckbox, a.showOutput
	menus = append(menus, file, &mygo.MenuItem{Role: mygo.RoleEditMenu, Label: "Edit"},
		&mygo.MenuItem{Label: "View", Submenu: []*mygo.MenuItem{
			item("repositories", "Repositories", "CmdOrCtrl+0"), item("changes", "Changes", "CmdOrCtrl+1"), item("logs", "Logs", "CmdOrCtrl+2"), mygo.Separator(), results,
		}},
		&mygo.MenuItem{Label: "Git", Submenu: []*mygo.MenuItem{item("review", "Review…", ""), item("refresh-all", "Refresh All Repositories", "CmdOrCtrl+Shift+R")}},
		&mygo.MenuItem{Role: mygo.RoleWindowMenu, Label: "Window"},
		&mygo.MenuItem{Role: mygo.RoleHelp, Label: "Help", Submenu: []*mygo.MenuItem{item("shortcuts", "Keyboard Shortcuts", "")}},
	)
	return menus
}

func (a *client) installApplicationMenu() {
	a.applicationMenu = mygo.NewMenu(a.applicationMenuTemplate(runtime.GOOS))
	mygo.App.SetMenu(a.applicationMenu)
}

// Change native menu state only when the underlying application state changes.
func (a *client) syncApplicationMenu() {
	if a.applicationMenu == nil {
		return
	}
	for _, id := range []string{"close-editor", "settings", "refresh-all", "review", "shortcuts", "results", "repositories", "changes", "logs"} {
		item := a.applicationMenu.ItemByID(id)
		enabled := a.applicationActionEnabled(id)
		if item.Disabled == enabled {
			item.SetEnabled(enabled)
		}
		if id == "results" && item.Checked != a.showOutput {
			item.SetChecked(a.showOutput)
		}
	}
}
