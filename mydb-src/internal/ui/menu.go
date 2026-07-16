package ui

import tea "github.com/charmbracelet/bubbletea"

type menuItem struct {
	label  string
	action Action
	sep    bool
}

type menuDef struct {
	title string
	items []menuItem
}

func sep() menuItem { return menuItem{sep: true} }

var menus = []menuDef{
	{"File", []menuItem{
		{label: "New Connection…", action: ActNewConn},
		{label: "Edit Connection…", action: ActEditConn},
		{label: "Delete Connection…", action: ActDeleteConn},
		sep(),
		{label: "Quit", action: ActQuit},
	}},
	{"View", []menuItem{
		{label: "Focus Workspace / Tree", action: ActSwapPane},
		{label: "Next Workspace Tab", action: ActTabNext},
		{label: "Toggle Workspace Panel", action: ActPanel},
		sep(),
		{label: "Toggle Shortcut Bar", action: ActHints},
		{label: "Filter…", action: ActFilter},
		sep(),
		{label: "Refresh", action: ActRefresh},
	}},
	{"Database", []menuItem{
		{label: "Connect", action: ActConnect},
		{label: "Disconnect", action: ActDisconnect},
		sep(),
		{label: "Reveal Password (10s)", action: ActRevealSecret},
	}},
	{"Help", []menuItem{
		{label: "Keyboard Reference", action: ActHelp},
	}},
}

type menuState struct {
	open bool
	mi   int // menu index
	ii   int // item index
}

func (m *Model) updateMenu(key string) tea.Cmd {
	st := &m.menu
	items := menus[st.mi].items
	switch key {
	case "esc", "f10", "m", "ctrl+q":
		st.open = false
	case "left":
		st.mi = (st.mi + len(menus) - 1) % len(menus)
		st.ii = firstItem(menus[st.mi].items)
	case "right", "tab":
		st.mi = (st.mi + 1) % len(menus)
		st.ii = firstItem(menus[st.mi].items)
	case "up", "k":
		st.ii = stepItem(items, st.ii, -1)
	case "down", "j":
		st.ii = stepItem(items, st.ii, +1)
	case "enter", " ", "space":
		if st.ii >= 0 && st.ii < len(items) && !items[st.ii].sep {
			act := items[st.ii].action
			st.open = false
			return m.dispatch(act)
		}
	}
	return nil
}

func firstItem(items []menuItem) int {
	for i, it := range items {
		if !it.sep {
			return i
		}
	}
	return 0
}

func stepItem(items []menuItem, cur, dir int) int {
	i := cur
	for range items {
		i += dir
		if i < 0 {
			i = len(items) - 1
		}
		if i >= len(items) {
			i = 0
		}
		if !items[i].sep {
			return i
		}
	}
	return cur
}
