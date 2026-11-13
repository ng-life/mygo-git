package main

import "github.com/egoist/mygo/ui"

var (
	ideaPanel     = ui.Hex("#3c3f41")
	ideaEditor    = ui.Hex("#2b2b2b")
	ideaToolbar   = ui.Hex("#3c3f41")
	ideaBorder    = ui.Hex("#323232")
	ideaSelection = ui.Hex("#214283")
	ideaLink      = ui.Hex("#589df6")
	ideaAdded     = ui.Hex("#294436")
	ideaDeleted   = ui.Hex("#493333")
)

// One immutable theme keeps native controls, menus, and custom panes in sync.
var ideaTheme = func() *ui.Theme {
	t := *ui.DarkTheme()
	t.Background = ideaEditor
	t.Surface = ideaPanel
	t.SurfaceHover = ui.Hex("#45494a")
	t.SurfacePressed = ui.Hex("#4b5052")
	t.Border = ui.Hex("#515658")
	t.Text = ui.Hex("#bbbbbb")
	t.TextMuted = ui.Hex("#909395")
	t.Accent = ui.Hex("#365880")
	t.AccentHover = ui.Hex("#416b98")
	t.AccentPressed = ideaSelection
	t.InactiveAccent = ui.Hex("#465461")
	t.AccentText = ui.Hex("#e4e8ed")
	t.Selection = ui.Hex("#214283")
	t.Focus = ui.Hex("#5e8cbd")
	t.FocusRingWidth = 1
	t.FocusRingOffset = -1
	t.Inverse = ui.Hex("#45494a")
	t.InverseText = t.Text
	t.Scrollbar = ui.Hex("#626668")
	t.ScrollbarWidth = 7
	t.Radius = 2
	t.Spacing = 3
	t.FontSize = 12
	return &t
}()

func diffRowColor(c *ui.Context, added bool) ui.Color {
	if c.Theme().Dark {
		if added {
			return ideaAdded
		}
		return ideaDeleted
	}
	if added {
		return ui.RGB(224, 247, 232)
	}
	return ui.RGB(255, 232, 232)
}
func diffCodeColor(c *ui.Context) ui.Color {
	if c.Theme().Dark {
		return c.Theme().Text
	}
	return ui.RGB(28, 38, 45)
}
