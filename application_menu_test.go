package main

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func findApplicationItem(items []*mygo.MenuItem, id string) *mygo.MenuItem {
	for _, item := range items {
		if item.ID == id {
			return item
		}
		if found := findApplicationItem(item.Submenu, id); found != nil {
			return found
		}
	}
	return nil
}
func chooseApplicationAction(t *testing.T, a *client, id string) {
	t.Helper()
	item := findApplicationItem(a.applicationMenuTemplate(runtime.GOOS), id)
	if item == nil || item.Disabled || item.Click == nil {
		t.Fatalf("native action unavailable: %s", id)
	}
	item.Click(item, nil)
}
func TestSystemMenuPlacementAndActions(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		t.Run(goos, func(t *testing.T) {
			a := testClient(nil)
			menus := a.applicationMenuTemplate(goos)
			parent := "File"
			if goos == "darwin" {
				parent = "MyGo Git"
			}
			for _, menu := range menus {
				if menu.Label == parent {
					item := findApplicationItem(menu.Submenu, "settings")
					if item == nil || item.Accelerator != "CmdOrCtrl+," {
						t.Fatal("settings missing from platform menu", menu)
					}
				}
				if menu.Label == "Help" && findApplicationItem(menu.Submenu, "shortcuts") == nil {
					t.Fatal("shortcut help missing")
				}
			}
			if item := findApplicationItem(menus, "review"); item == nil || !item.Disabled {
				t.Fatal("Review must be disabled without repositories")
			}
			chooseApplicationAction(t, a, "results")
			if !a.showOutput {
				t.Fatal("results action did not toggle output")
			}
			a.busy = true
			if !findApplicationItem(a.applicationMenuTemplate(goos), "settings").Disabled {
				t.Fatal("Settings must be disabled during writes")
			}
			a.applicationAction("settings")
			if a.settings.Open {
				t.Fatal("disabled action still opened settings")
			}
		})
	}
}
func TestSettingsShortcutAndNoWindowMenu(t *testing.T) {
	a := testClient(nil)
	tester := ui.NewTester(a.view, 1560, 940)
	if tester.HasText("Menu") || tester.HasText("Keyboard Shortcuts") || tester.HasText("Refresh All") {
		t.Fatal("system actions remain in window toolbar")
	}
	tester.Key(primaryShortcutModifier(), ui.KeyComma)
	if !a.settings.Open || !tester.HasText("Version Control  ›  Git") {
		t.Fatal("settings shortcut did not open settings")
	}
}
