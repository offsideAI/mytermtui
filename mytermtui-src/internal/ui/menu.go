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
		{label: "Open / Reveal", action: ActOpen},
		{label: "Open in App", action: ActOpenApp},
		{label: "Open With…", action: ActOpenWith},
		{label: "Quick Look", action: ActQuickLook},
		sep(),
		{label: "New Folder…", action: ActNewFolder},
		{label: "New File…", action: ActNewFile},
		sep(),
		{label: "Get Info", action: ActGetInfo},
		{label: "Rename…", action: ActRename},
		{label: "Duplicate", action: ActDuplicate},
		{label: "Compress", action: ActCompress},
		sep(),
		{label: "Reveal in Finder", action: ActReveal},
		{label: "Open Terminal Here", action: ActTerminal},
		{label: "Copy Path", action: ActCopyPath},
		sep(),
		{label: "Move to Trash", action: ActTrash},
		sep(),
		{label: "Quit", action: ActQuit},
	}},
	{"Edit", []menuItem{
		{label: "Copy", action: ActCopy},
		{label: "Cut", action: ActCut},
		{label: "Paste", action: ActPaste},
		sep(),
		{label: "Select All", action: ActSelectAll},
		{label: "Clear Selection", action: ActClearSelect},
		{label: "Range Select", action: ActRangeSel},
		sep(),
		{label: "Undo", action: ActUndo},
	}},
	{"View", []menuItem{
		{label: "Toggle Hidden Files", action: ActHidden},
		{label: "Toggle Preview Panel", action: ActPreview},
		{label: "Toggle Shortcut Bar", action: ActHints},
		{label: "Sort…", action: ActSort},
		{label: "Filter…", action: ActFilter},
		sep(),
		{label: "Refresh", action: ActRefresh},
	}},
	{"Go", []menuItem{
		{label: "Parent Folder", action: ActParent},
		{label: "Back", action: ActBack},
		{label: "Forward", action: ActForward},
		sep(),
		{label: "Home", action: ActHome},
		{label: "Root /", action: ActRoot},
		{label: "iCloud Drive", action: ActICloud},
		sep(),
		{label: "Go to Path…", action: ActGotoPath},
		{label: "Fuzzy Find…", action: ActFuzzyFind},
	}},
	{"iCloud", []menuItem{
		{label: "Download (mark for sync)", action: ActDownload},
		{label: "Evict Local Copy", action: ActEvict},
		sep(),
		{label: "Download Queue…", action: ActQueue},
		{label: "Folder Summary", action: ActSummary},
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
