package main

import "github.com/egoist/mygo/ui"

type treeGesture struct{ Double bool }

// Press and release can cause separate builds. Retain the gesture in widget
// state, then execute application actions through deferred OnClick callbacks.
func treeContentAction(element ui.Element, choose, toggle func()) ui.Element {
	gesture := ui.Local(element, "tree-content-gesture", func() treeGesture { return treeGesture{} })
	element.HandleInput(func(event ui.InputEvent) bool {
		if event.Kind == ui.InputPointerDown && event.Button == 0 {
			gesture.Double = event.Clicks == 2
		}
		if event.Kind == ui.InputKeyDown {
			gesture.Double = false
		}
		if event.Kind == ui.InputPointerUp && event.Canceled {
			gesture.Double = false
		}
		return false
	})
	return element.OnClick(func() {
		double := gesture.Double
		gesture.Double = false
		if double && toggle != nil {
			toggle()
			return
		}
		if choose != nil {
			choose()
		}
	})
}
func (a *client) toggleRepositoryTree(repo Repository) {
	open := a.browserOpen("repository:" + repo.Path)
	*open = !*open
	if *open {
		a.loadRepositoryFiles(repo, false)
	}
}
