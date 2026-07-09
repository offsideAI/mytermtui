package ui

// Action names every keyboard-reachable command. Config key overrides
// refer to these names in the [keys] table.
type Action string

const (
	ActUp        Action = "up"
	ActDown      Action = "down"
	ActOpen      Action = "open"
	ActParent    Action = "parent"
	ActTop       Action = "top"
	ActBottom    Action = "bottom"
	ActPageUp    Action = "page_up"
	ActPageDown  Action = "page_down"
	ActBack      Action = "back"
	ActForward   Action = "forward"
	ActHome      Action = "home"
	ActRoot      Action = "root"
	ActICloud    Action = "icloud_root"
	ActGotoPath  Action = "goto_path"
	ActHidden    Action = "toggle_hidden"
	ActSort      Action = "sort_menu"
	ActFilter    Action = "filter"
	ActFuzzyFind Action = "fuzzy_find"
	ActRefresh   Action = "refresh"

	ActSelect      Action = "select"
	ActRangeSel    Action = "range_select"
	ActSelectAll   Action = "select_all"
	ActClearSelect Action = "clear_select"

	ActOpenApp    Action = "open_app"
	ActOpenRight  Action = "open_right"
	ActSwapPane   Action = "swap_pane"
	ActClosePane  Action = "close_pane"
	ActPaneNarrow Action = "pane_narrower"
	ActPaneWiden  Action = "pane_wider"
	ActCopy       Action = "copy"
	ActCut        Action = "cut"
	ActPaste      Action = "paste"
	ActRename     Action = "rename"
	ActTrash      Action = "trash"
	ActDuplicate  Action = "duplicate"
	ActNewFolder  Action = "new_folder"
	ActNewFile    Action = "new_file"
	ActOpenWith   Action = "open_with"
	ActQuickLook  Action = "quick_look"
	ActGetInfo    Action = "get_info"
	ActCompress   Action = "compress"
	ActReveal     Action = "reveal"
	ActTerminal   Action = "terminal"
	ActCopyPath   Action = "copy_path"
	ActUndo       Action = "undo"

	ActDownload Action = "download"
	ActEvict    Action = "evict"
	ActQueue    Action = "queue_manager"
	ActSummary  Action = "icloud_summary"

	ActPreview Action = "toggle_preview"
	ActHints   Action = "toggle_hints"
	ActHelp    Action = "help"
	ActMenu    Action = "menu"
	ActQuit    Action = "quit"
)

// defaultBindings: action → keys. Multiple keys may map to one action;
// config overrides replace the whole list for that action.
var defaultBindings = map[Action][]string{
	ActUp:         {"up", "k"},
	ActDown:       {"down", "j"},
	ActOpen:       {"enter"},
	ActOpenRight:  {"right", "l"},
	ActSwapPane:   {"tab"},
	ActClosePane:  {"ctrl+w"},
	ActPaneNarrow: {"<"},
	ActPaneWiden:  {">"},
	ActParent:     {"left", "h", "backspace"},
	ActTop:        {"g"},
	ActBottom:     {"G"},
	ActPageUp:     {"pgup", "ctrl+b"},
	ActPageDown:   {"pgdown", "ctrl+f"},
	ActBack:       {"[", "alt+left"},
	ActForward:    {"]", "alt+right"},
	ActHome:       {"~"},
	ActRoot:       {"/"},
	ActICloud:     {"i"},
	ActGotoPath:   {":"},
	ActHidden:     {"z"},
	ActSort:       {"s"},
	ActFilter:     {"f"},
	ActFuzzyFind:  {"F"},
	ActRefresh:    {"ctrl+r"},

	ActSelect:      {" ", "space"},
	ActRangeSel:    {"v"},
	ActSelectAll:   {"a"},
	ActClearSelect: {"A"},

	ActOpenApp:   {"o"},
	ActCopy:      {"c"},
	ActCut:       {"x"},
	ActPaste:     {"p"},
	ActRename:    {"r", "f2"},
	ActTrash:     {"D", "f8"},
	ActDuplicate: {"ctrl+d"},
	ActNewFolder: {"n", "f7"},
	ActNewFile:   {"N"},
	ActOpenWith:  {"O"},
	ActQuickLook: {"q"},
	ActGetInfo:   {"I"},
	ActCompress:  {"Z"},
	ActReveal:    {"R"},
	ActTerminal:  {"T"},
	ActCopyPath:  {"."},
	ActUndo:      {"u"},

	ActDownload: {"d"},
	ActEvict:    {"e"},
	ActQueue:    {"Q"},
	ActSummary:  {"S"},

	ActPreview: {"f3", "P"},
	ActHints:   {"H"},
	ActHelp:    {"?", "f1"},
	ActMenu:    {"m", "f10"},
	ActQuit:    {"ctrl+q", "ctrl+c"},
}

// Keymap resolves a pressed key to an action.
type Keymap struct {
	byKey    map[string]Action
	byAction map[Action][]string
}

// BuildKeymap merges config overrides over the defaults.
func BuildKeymap(overrides map[string][]string) Keymap {
	byAction := make(map[Action][]string, len(defaultBindings))
	for act, keys := range defaultBindings {
		byAction[act] = keys
	}
	for name, keys := range overrides {
		if _, known := defaultBindings[Action(name)]; known && len(keys) > 0 {
			byAction[Action(name)] = keys
		}
	}
	byKey := map[string]Action{}
	for act, keys := range byAction {
		for _, k := range keys {
			byKey[k] = act
		}
	}
	return Keymap{byKey: byKey, byAction: byAction}
}

// Lookup returns the action for a key press ("" if unbound).
func (km Keymap) Lookup(key string) (Action, bool) {
	a, ok := km.byKey[key]
	return a, ok
}

// KeyFor returns the primary key bound to an action, for menu labels.
func (km Keymap) KeyFor(act Action) string {
	keys := km.byAction[act]
	if len(keys) == 0 {
		return ""
	}
	k := keys[0]
	if k == " " {
		return "space"
	}
	return k
}

// helpSection groups actions for the help overlay.
type helpSection struct {
	Title   string
	Actions []Action
}

var helpSections = []helpSection{
	{"Navigate", []Action{ActUp, ActDown, ActOpen, ActOpenRight, ActSwapPane, ActClosePane, ActPaneNarrow, ActPaneWiden, ActParent, ActTop, ActBottom, ActPageUp, ActPageDown, ActBack, ActForward, ActHome, ActRoot, ActICloud, ActGotoPath, ActHidden, ActSort, ActFilter, ActFuzzyFind, ActRefresh}},
	{"Select", []Action{ActSelect, ActRangeSel, ActSelectAll, ActClearSelect}},
	{"Files", []Action{ActOpenApp, ActCopy, ActCut, ActPaste, ActRename, ActTrash, ActDuplicate, ActNewFolder, ActNewFile, ActOpenWith, ActQuickLook, ActGetInfo, ActCompress, ActReveal, ActTerminal, ActCopyPath, ActUndo}},
	{"iCloud", []Action{ActDownload, ActEvict, ActQueue, ActSummary}},
	{"App", []Action{ActPreview, ActHints, ActHelp, ActMenu, ActQuit}},
}

var actionHelp = map[Action]string{
	ActUp:         "move up",
	ActDown:       "move down",
	ActOpen:       "expand/collapse folder / reveal file",
	ActOpenRight:  "open folder in right panel",
	ActSwapPane:   "switch panel focus",
	ActClosePane:  "close right panel",
	ActPaneNarrow: "shrink left panel",
	ActPaneWiden:  "widen left panel",
	ActOpenApp:    "open in default app",
	ActParent:     "parent directory",
	ActTop:        "first entry",
	ActBottom:     "last entry",
	ActPageUp:     "page up",
	ActPageDown:   "page down",
	ActBack:       "history back",
	ActForward:    "history forward",
	ActHome:       "home directory",
	ActRoot:       "filesystem root",
	ActICloud:     "iCloud Drive root",
	ActGotoPath:   "go to path…",
	ActHidden:     "toggle hidden files",
	ActSort:       "sort options",
	ActFilter:     "filter this directory",
	ActFuzzyFind:  "fuzzy find recursively",
	ActRefresh:    "reload directory",

	ActSelect:      "toggle selection",
	ActRangeSel:    "range-select mode",
	ActSelectAll:   "select all",
	ActClearSelect: "clear selection",

	ActCopy:      "copy",
	ActCut:       "cut",
	ActPaste:     "paste",
	ActRename:    "rename",
	ActTrash:     "move to Trash",
	ActDuplicate: "duplicate",
	ActNewFolder: "new folder",
	ActNewFile:   "new file",
	ActOpenWith:  "open with…",
	ActQuickLook: "Quick Look",
	ActGetInfo:   "get info",
	ActCompress:  "compress to zip",
	ActReveal:    "reveal in Finder",
	ActTerminal:  "open Terminal here",
	ActCopyPath:  "copy path to clipboard",
	ActUndo:      "undo last operation",

	ActDownload: "download from iCloud",
	ActEvict:    "evict local copy",
	ActQueue:    "download queue",
	ActSummary:  "iCloud usage summary",

	ActPreview: "toggle preview panel",
	ActHints:   "toggle shortcut bar",
	ActHelp:    "help",
	ActMenu:    "open menus",
	ActQuit:    "quit",
}
