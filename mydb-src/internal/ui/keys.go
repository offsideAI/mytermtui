package ui

// Action names every keyboard-reachable command. Config key overrides
// refer to these names in the [keys] table.
type Action string

const (
	ActUp       Action = "up"
	ActDown     Action = "down"
	ActOpen     Action = "open" // expand/collapse (connecting first if needed)
	ActExpand   Action = "expand"
	ActCollapse Action = "collapse"
	ActTop      Action = "top"
	ActBottom   Action = "bottom"
	ActPageUp   Action = "page_up"
	ActPageDown Action = "page_down"
	ActFilter   Action = "filter"
	ActRefresh  Action = "refresh"

	ActConnect      Action = "connect"
	ActDisconnect   Action = "disconnect"
	ActNewConn      Action = "new_connection"
	ActEditConn     Action = "edit_connection"
	ActDeleteConn   Action = "delete_connection"
	ActRevealSecret Action = "reveal_password"

	ActRunQuery Action = "run_query"
	ActHistory  Action = "query_history"

	ActSwapPane   Action = "swap_pane"
	ActTabNext    Action = "tab_next"
	ActTabPrev    Action = "tab_prev"
	ActPanel      Action = "toggle_panel"
	ActPaneNarrow Action = "pane_narrower"
	ActPaneWiden  Action = "pane_wider"
	ActHints      Action = "toggle_hints"
	ActHelp       Action = "help"
	ActMenu       Action = "menu"
	ActQuit       Action = "quit"
)

// defaultBindings: action → keys. Multiple keys may map to one action;
// config overrides replace the whole list for that action.
var defaultBindings = map[Action][]string{
	ActUp:       {"up", "k"},
	ActDown:     {"down", "j"},
	ActOpen:     {"enter"},
	ActExpand:   {"right", "l"},
	ActCollapse: {"left", "h", "backspace"},
	ActTop:      {"g"},
	ActBottom:   {"G"},
	ActPageUp:   {"pgup", "ctrl+b"},
	ActPageDown: {"pgdown", "ctrl+f"},
	ActFilter:   {"f"},
	ActRefresh:  {"ctrl+r"},

	ActConnect:      {"c"},
	ActDisconnect:   {"ctrl+c"},
	ActNewConn:      {"B"},
	ActEditConn:     {"E"},
	ActDeleteConn:   {"X"},
	ActRevealSecret: {"p"},

	ActRunQuery: {"f5"},
	ActHistory:  {"ctrl+h"},

	ActSwapPane:   {"tab"},
	ActTabNext:    {"]"},
	ActTabPrev:    {"["},
	ActPanel:      {"f3", "P"},
	ActPaneNarrow: {"<"},
	ActPaneWiden:  {">"},
	ActHints:      {"H"},
	ActHelp:       {"?", "f1"},
	ActMenu:       {"m", "f10"},
	ActQuit:       {"ctrl+q"},
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
	{"Navigate", []Action{ActUp, ActDown, ActOpen, ActExpand, ActCollapse, ActTop, ActBottom, ActPageUp, ActPageDown, ActFilter, ActRefresh}},
	{"Connections", []Action{ActConnect, ActDisconnect, ActNewConn, ActEditConn, ActDeleteConn, ActRevealSecret}},
	{"SQL", []Action{ActRunQuery, ActHistory}},
	{"Workspace", []Action{ActSwapPane, ActTabNext, ActTabPrev, ActPanel, ActPaneNarrow, ActPaneWiden}},
	{"App", []Action{ActHints, ActHelp, ActMenu, ActQuit}},
}

var actionHelp = map[Action]string{
	ActUp:       "move up",
	ActDown:     "move down",
	ActOpen:     "expand/collapse (connects first)",
	ActExpand:   "expand node",
	ActCollapse: "collapse / jump to parent",
	ActTop:      "first row",
	ActBottom:   "last row",
	ActPageUp:   "page up",
	ActPageDown: "page down",
	ActFilter:   "filter the tree",
	ActRefresh:  "reload connections & schemas",

	ActConnect:      "connect",
	ActDisconnect:   "disconnect",
	ActNewConn:      "new connection…",
	ActEditConn:     "edit connection…",
	ActDeleteConn:   "delete connection…",
	ActRevealSecret: "reveal saved password (10s)",

	ActRunQuery: "run the SQL buffer (also ctrl+r / :w in the tab)",
	ActHistory:  "query history…",

	ActSwapPane:   "focus workspace / tree",
	ActTabNext:    "next workspace tab",
	ActTabPrev:    "previous workspace tab",
	ActPanel:      "toggle workspace panel",
	ActPaneNarrow: "shrink tree pane",
	ActPaneWiden:  "widen tree pane",
	ActHints:      "toggle shortcut bar",
	ActHelp:       "help",
	ActMenu:       "open menus",
	ActQuit:       "quit",
}
