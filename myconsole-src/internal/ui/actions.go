package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/myconsole/internal/fsx"
	"github.com/offsideai/myconsole/internal/icloud"
)

// mutating actions are serialized through m.op: only one filesystem
// operation may run at a time, enforced here once rather than per-action.
var mutating = map[Action]bool{
	ActPaste: true, ActRename: true, ActTrash: true, ActDuplicate: true,
	ActNewFolder: true, ActNewFile: true, ActCompress: true, ActUndo: true,
	ActEvict: true,
}

// dispatch executes an action from a key press or menu selection.
func (m *Model) dispatch(act Action) tea.Cmd {
	if mutating[act] && m.op != nil {
		return m.note(levelWarn, "another operation is running — please wait")
	}
	switch act {

	// --- navigation ---------------------------------------------------
	case ActUp:
		return m.moveCursor(-1)
	case ActDown:
		return m.moveCursor(1)
	case ActTop:
		m.cursor = 0
		m.clampCursor()
		return m.followCursor()
	case ActBottom:
		m.cursor = len(m.view) - 1
		m.clampCursor()
		return m.followCursor()
	case ActPageUp:
		return m.moveCursor(-m.listHeight())
	case ActPageDown:
		return m.moveCursor(m.listHeight())
	case ActOpen:
		return m.openAction()
	case ActOpenRight:
		return m.smartRightAction()
	case ActSwapPane:
		return m.swapPaneAction()
	case ActClosePane:
		return m.togglePanelAction()
	case ActPaneNarrow:
		return m.resizePaneAction(-0.05)
	case ActPaneWiden:
		return m.resizePaneAction(+0.05)
	case ActOpenApp:
		e := m.currentEntry()
		if e == nil {
			return nil
		}
		if e.IsDir {
			return m.navigateTo(e.Path, "")
		}
		return m.openInApp(*e)
	case ActParent:
		parent := filepath.Dir(m.cwd)
		if parent == m.cwd {
			return nil
		}
		return m.navigateTo(parent, m.cwd)
	case ActBack:
		if len(m.histBack) == 0 {
			return m.note(levelInfo, "no history")
		}
		// Stacks are only mutated in applyNav, once the load succeeds.
		dest := m.histBack[len(m.histBack)-1]
		m.pendingNav = navIntent{kind: navBack, from: m.cwd, to: dest}
		m.loading = true
		return loadDirCmd(dest, m.cwd)
	case ActForward:
		if len(m.histFwd) == 0 {
			return m.note(levelInfo, "no forward history")
		}
		dest := m.histFwd[len(m.histFwd)-1]
		m.pendingNav = navIntent{kind: navForward, from: m.cwd, to: dest}
		m.loading = true
		return loadDirCmd(dest, m.cwd)
	case ActHome:
		return m.navigateTo(m.home, "")
	case ActRoot:
		return m.navigateTo("/", "")
	case ActICloud:
		root := icloud.DriveRoot()
		if _, err := os.Stat(root); err != nil {
			return m.note(levelWarn, "iCloud Drive not found: "+errHint(err))
		}
		return m.navigateTo(root, "")
	case ActGotoPath:
		im := newInputModal("Go to path", "/some/where or ~/…", "")
		im.CompletePath = true
		im.OnSubmit = func(m *Model, val string) tea.Cmd {
			p := val
			if strings.HasPrefix(p, "~") {
				p = m.home + p[1:]
			}
			if !filepath.IsAbs(p) {
				p = filepath.Join(m.cwd, p)
			}
			fi, err := os.Stat(p)
			if err != nil {
				return m.note(levelErr, errHint(err))
			}
			if !fi.IsDir() {
				return m.navigateTo(filepath.Dir(p), p)
			}
			return m.navigateTo(p, "")
		}
		m.modal = im
		return textinput.Blink
	case ActRefresh:
		return m.reload()
	case ActHidden:
		m.showHidden = !m.showHidden
		m.rebuildView("")
		return m.followCursor()
	case ActSort:
		m.modal = &SortModal{}
		return nil
	case ActFilter:
		m.filtering = true
		m.filterInput.Focus()
		return textinput.Blink
	case ActFuzzyFind:
		m.modal = newFindModal()
		return textinput.Blink

	// --- selection ------------------------------------------------------
	case ActSelect:
		if e := m.currentEntry(); e != nil {
			if m.selected[e.Path] {
				delete(m.selected, e.Path)
			} else {
				m.selected[e.Path] = true
			}
		}
		return m.moveCursor(1)
	case ActRangeSel:
		if m.anchor >= 0 {
			m.commitRange()
		} else {
			m.anchor = m.cursor
		}
		return nil
	case ActSelectAll:
		for _, ei := range m.view {
			m.selected[m.entries[ei].Path] = true
		}
		return nil
	case ActClearSelect:
		m.selected = map[string]bool{}
		m.anchor = -1
		return nil

	// --- clipboard / file ops --------------------------------------------
	case ActCopy:
		paths := m.selectionPaths()
		if len(paths) == 0 {
			return nil
		}
		m.clip = clipboard{paths: paths, cut: false}
		return m.note(levelInfo, strconv.Itoa(len(paths))+" item(s) copied")
	case ActCut:
		paths := m.selectionPaths()
		if len(paths) == 0 {
			return nil
		}
		m.clip = clipboard{paths: paths, cut: true}
		return m.note(levelInfo, strconv.Itoa(len(paths))+" item(s) cut")
	case ActPaste:
		return m.pasteAction()
	case ActRename:
		return m.renameAction()
	case ActTrash:
		return m.trashAction()
	case ActDuplicate:
		return m.duplicateAction()
	case ActNewFolder:
		return m.newEntryAction(true)
	case ActNewFile:
		return m.newEntryAction(false)
	case ActOpenWith:
		return m.openWithAction()
	case ActQuickLook:
		return m.quickLookAction()
	case ActGetInfo:
		return m.getInfoAction()
	case ActCompress:
		return m.compressAction()
	case ActReveal:
		if e := m.currentEntry(); e != nil {
			return execCmd("reveal", "open", "-R", e.Path)
		}
		return execCmd("reveal", "open", "-R", m.cwd)
	case ActTerminal:
		return execCmd("terminal", "open", "-a", "Terminal", m.cwd)
	case ActCopyPath:
		paths := m.selectionPaths()
		if len(paths) == 0 {
			paths = []string{m.cwd}
		}
		return tea.Batch(
			pbcopyCmd(strings.Join(paths, "\n")),
			m.note(levelOK, "path copied to clipboard"),
		)
	case ActUndo:
		return m.undoAction()

	// --- iCloud -----------------------------------------------------------
	case ActDownload:
		return m.downloadAction()
	case ActEvict:
		return m.evictAction()
	case ActQueue:
		m.setQsnap(m.queue.Snapshot())
		m.modal = &QueueModal{}
		return nil
	case ActSummary:
		cwd := m.cwd
		return func() tea.Msg {
			sum, capped, err := icloud.Summarize(cwd, 200000)
			return summaryMsg{root: cwd, sum: sum, capped: capped, err: err}
		}

	// --- app ---------------------------------------------------------------
	case ActPreview:
		return m.togglePanelAction()
	case ActHints:
		m.hintsOn = !m.hintsOn
		m.clampCursor()
		return nil
	case ActHelp:
		m.modal = &HelpModal{}
		return nil
	case ActMenu:
		m.menu = menuState{open: true, mi: 0, ii: firstItem(menus[0].items)}
		return nil
	case ActQuit:
		if m.queue != nil && m.queue.HasActive() {
			m.modal = &ConfirmModal{
				Title:    "Downloads in progress",
				Body:     []string{"Active downloads continue in fileproviderd,", "but progress tracking will stop."},
				YesLabel: "quit",
				Yes:      func(*Model) tea.Cmd { return tea.Quit },
			}
			return nil
		}
		return tea.Quit
	}
	return nil
}

// --- open ------------------------------------------------------------------

// openAction handles enter: directories are entered; files either get
// revealed in Finder or opened in their app, per config.
func (m *Model) openAction() tea.Cmd {
	e := m.currentEntry()
	if e == nil {
		return nil
	}
	if e.IsDir {
		if m.focusRight {
			// The contents panel navigates like a classic listing.
			return m.navigateTo(e.Path, "")
		}
		return m.toggleExpandAction(*e)
	}
	return m.openFileAction(*e)
}

// toggleExpandAction expands or collapses a folder in place,
// Windows-Explorer style. Children load asynchronously on first expand
// and are kept fresh by the same-directory reload path.
func (m *Model) toggleExpandAction(e fsx.Entry) tea.Cmd {
	if m.expanded[e.Path] {
		delete(m.expanded, e.Path)
		m.rebuildView(e.Path)
		return m.followCursor()
	}
	m.expanded[e.Path] = true
	if _, ok := m.childCache[e.Path]; ok {
		m.rebuildView(e.Path)
		return m.followCursor()
	}
	return loadChildrenCmd(e.Path)
}

// openFileAction applies the configured enter-on-file behavior.
func (m *Model) openFileAction(e fsx.Entry) tea.Cmd {
	switch m.cfg.General.EnterOpensFile {
	case "app":
		return m.openInApp(e)
	case "editor":
		return m.openInEditor(e)
	default: // "reveal" — reads no contents, safe even on ☁ files
		return execCmd("reveal", "open", "-R", e.Path)
	}
}

// openInEditor loads a local text file into the embedded editor. Evicted
// files are refused (editing would download them); the read command
// enforces size and binary limits.
func (m *Model) openInEditor(e fsx.Entry) tea.Cmd {
	if e.Dataless {
		return m.note(levelWarn, sanitize(e.Name)+" is cloud-only — press d to download it first")
	}
	return readForEditorCmd(e.Path)
}

// openInApp launches the file in its default application, confirming
// first when that would trigger an iCloud download.
func (m *Model) openInApp(e fsx.Entry) tea.Cmd {
	if e.Dataless {
		path := e.Path
		m.modal = &ConfirmModal{
			Title:    "Cloud-only file",
			Body:     []string{sanitize(e.Name) + " (" + humanBytes(e.Size) + ") is not downloaded.", "Queue it for download?"},
			YesLabel: "download",
			Yes: func(m *Model) tea.Cmd {
				return expandMarksCmd([]string{path})
			},
		}
		return nil
	}
	return execCmd("open", "open", e.Path)
}

func (m *Model) openWithAction() tea.Cmd {
	// Opening a cloud-only file with an app reads its contents and
	// silently downloads it — require an explicit download first.
	for _, e := range m.selectionEntries() {
		if e.Dataless {
			return m.note(levelWarn, sanitize(e.Name)+" is cloud-only — press d to download it first")
		}
	}
	paths := m.selectionPaths()
	if len(paths) == 0 {
		return nil
	}
	im := newInputModal("Open with application", "TextEdit, VLC, Visual Studio Code…", "")
	im.OnSubmit = func(m *Model, app string) tea.Cmd {
		args := append([]string{"-a", app}, paths...)
		return execCmd("open with "+app, "open", args...)
	}
	m.modal = im
	return textinput.Blink
}

func (m *Model) quickLookAction() tea.Cmd {
	entries := m.selectionEntries()
	var paths []string
	for _, e := range entries {
		if e.Dataless {
			return m.note(levelWarn, "Quick Look blocked: "+sanitize(e.Name)+" is cloud-only (press d to download)")
		}
		paths = append(paths, e.Path)
	}
	if len(paths) == 0 {
		return nil
	}
	return execCmd("Quick Look", "qlmanage", append([]string{"-p"}, paths...)...)
}

// --- paste -----------------------------------------------------------------

func (m *Model) pasteAction() tea.Cmd {
	if len(m.clip.paths) == 0 {
		return m.note(levelInfo, "clipboard empty — use c (copy) or x (cut) first")
	}
	dest := m.cwd

	var conflicts []string
	for _, src := range m.clip.paths {
		name := filepath.Base(src)
		dst := filepath.Join(dest, name)
		if dst == src {
			if !m.clip.cut {
				conflicts = append(conflicts, name) // copy onto itself → keep both
			}
			continue
		}
		if _, err := os.Lstat(dst); err == nil {
			conflicts = append(conflicts, name)
		}
	}

	next := func(m *Model) tea.Cmd {
		if len(conflicts) > 0 {
			m.modal = &CollisionModal{conflicts: conflicts}
			return nil
		}
		return m.runPaste(pasteKeepBoth)
	}

	// Copying a dataless file materializes it: warn before pulling bytes.
	if !m.clip.cut {
		var n int
		var bytes int64
		for _, src := range m.clip.paths {
			if fi, err := os.Lstat(src); err == nil && icloud.Dataless(fi) {
				n++
				bytes += fi.Size()
			}
		}
		if n > 0 {
			m.modal = &ConfirmModal{
				Title:    "Copy will download from iCloud",
				Body:     []string{fmt.Sprintf("%d source item(s), %s, are cloud-only and will be", n, humanBytes(bytes)), "downloaded while copying. Folders may contain more."},
				YesLabel: "copy anyway",
				Yes:      next,
			}
			return nil
		}
	}
	return next(m)
}

// runPaste executes the paste with the chosen conflict mode. In replace
// mode the pre-existing destination is moved to the Trash (not deleted),
// so a failed copy rolls it back and undo can restore it.
func (m *Model) runPaste(mode pasteMode) tea.Cmd {
	clip := m.clip
	dest := m.cwd
	bridge := m.bridge
	if clip.cut {
		m.clip = clipboard{}
	}
	prog := &fsx.Progress{}
	verb := "copied"
	if clip.cut {
		verb = "moved"
	}
	desc := fmt.Sprintf("%s %d item(s)", verb, len(clip.paths))
	m.op = &opState{desc: "pasting…", prog: prog}

	run := opCmd(desc, true, func() (*fsx.Undo, error) {
		type rec struct {
			from, to string
			copied   bool
			backup   string // Trash location of a replaced original
		}
		var recs []rec
		var firstErr error
		for _, src := range clip.paths {
			name := filepath.Base(src)
			dst := filepath.Join(dest, name)
			if dst == src && clip.cut {
				continue
			}
			backup := ""
			if _, err := os.Lstat(dst); err == nil {
				switch {
				case dst == src: // copy onto itself
					dst = filepath.Join(dest, fsx.KeepBothName(dest, name))
				case mode == pasteSkip:
					continue
				case mode == pasteKeepBoth:
					dst = filepath.Join(dest, fsx.KeepBothName(dest, name))
				case mode == pasteReplace:
					pb, err := bridge.Trash(dst)
					if err != nil {
						if firstErr == nil {
							firstErr = err
						}
						continue
					}
					backup = pb
				}
			}
			var err error
			if clip.cut {
				err = fsx.Move(src, dst, prog)
			} else {
				prog.AddTotal(fsx.TotalSize(src))
				prog.SetLabel(name)
				err = fsx.CopyPath(src, dst, prog)
			}
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				if backup != "" { // roll the replaced original back
					_ = os.Rename(backup, dst)
				}
				continue
			}
			recs = append(recs, rec{from: src, to: dst, copied: !clip.cut, backup: backup})
		}
		var undo *fsx.Undo
		if len(recs) > 0 {
			undo = &fsx.Undo{
				Desc: desc,
				Fn: func() error {
					var err error
					for i := len(recs) - 1; i >= 0; i-- {
						r := recs[i]
						if r.copied {
							if e := os.RemoveAll(r.to); e != nil && err == nil {
								err = e
							}
						} else {
							if e := fsx.Move(r.to, r.from, nil); e != nil && err == nil {
								err = e
							}
						}
						if r.backup != "" { // restore the replaced original
							if e := os.Rename(r.backup, r.to); e != nil && err == nil {
								err = e
							}
						}
					}
					return err
				},
			}
		}
		return undo, firstErr
	})
	return tea.Batch(run, opTickCmd())
}

// --- rename / new / duplicate / trash / compress -----------------------------

func validName(name string) error {
	if name == "" || name == "." || name == ".." {
		return errors.New("invalid name")
	}
	if strings.ContainsRune(name, '/') {
		return errors.New("name cannot contain /")
	}
	return nil
}

func (m *Model) renameAction() tea.Cmd {
	e := m.currentEntry()
	if e == nil {
		return nil
	}
	oldPath := e.Path
	dir := filepath.Dir(oldPath)
	im := newInputModal("Rename", "", e.Name)
	im.OnSubmit = func(m *Model, val string) tea.Cmd {
		if err := validName(val); err != nil {
			return m.note(levelErr, err.Error())
		}
		newPath := filepath.Join(dir, val)
		if newPath == oldPath {
			return nil
		}
		if _, err := os.Lstat(newPath); err == nil {
			return m.note(levelErr, "an item named "+sanitize(val)+" already exists")
		}
		m.op = &opState{desc: "renaming…"}
		run := opCmd("renamed to "+sanitize(val), true, func() (*fsx.Undo, error) {
			if err := os.Rename(oldPath, newPath); err != nil {
				return nil, err
			}
			return &fsx.Undo{Desc: "rename", Fn: func() error { return os.Rename(newPath, oldPath) }}, nil
		})
		return tea.Batch(run, opTickCmd())
	}
	m.modal = im
	return textinput.Blink
}

func (m *Model) newEntryAction(isDir bool) tea.Cmd {
	kind := "file"
	if isDir {
		kind = "folder"
	}
	dir := m.cwd
	im := newInputModal("New "+kind, "name", "")
	im.OnSubmit = func(m *Model, val string) tea.Cmd {
		if err := validName(val); err != nil {
			return m.note(levelErr, err.Error())
		}
		p := filepath.Join(dir, val)
		if _, err := os.Lstat(p); err == nil {
			return m.note(levelErr, sanitize(val)+" already exists")
		}
		m.op = &opState{desc: "creating…"}
		run := opCmd("created "+sanitize(val), true, func() (*fsx.Undo, error) {
			var err error
			if isDir {
				err = os.Mkdir(p, 0o755)
			} else {
				var f *os.File
				f, err = os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
				if err == nil {
					err = f.Close()
				}
			}
			if err != nil {
				return nil, err
			}
			return &fsx.Undo{Desc: "create " + kind, Fn: func() error { return os.Remove(p) }}, nil
		})
		return tea.Batch(run, opTickCmd())
	}
	m.modal = im
	return textinput.Blink
}

func (m *Model) duplicateAction() tea.Cmd {
	entries := m.selectionEntries()
	if len(entries) == 0 {
		return nil
	}
	prog := &fsx.Progress{}
	m.op = &opState{desc: "duplicating…", prog: prog}
	run := opCmd(fmt.Sprintf("duplicated %d item(s)", len(entries)), true, func() (*fsx.Undo, error) {
		var made []string
		var firstErr error
		for _, e := range entries {
			dir := filepath.Dir(e.Path)
			dst := filepath.Join(dir, fsx.DuplicateName(dir, e.Name))
			prog.AddTotal(fsx.TotalSize(e.Path))
			prog.SetLabel(e.Name)
			if err := fsx.CopyPath(e.Path, dst, prog); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			made = append(made, dst)
		}
		var undo *fsx.Undo
		if len(made) > 0 {
			undo = &fsx.Undo{Desc: "duplicate", Fn: func() error {
				var err error
				for _, p := range made {
					if e := os.RemoveAll(p); e != nil && err == nil {
						err = e
					}
				}
				return err
			}}
		}
		return undo, firstErr
	})
	return tea.Batch(run, opTickCmd())
}

func (m *Model) trashAction() tea.Cmd {
	paths := m.selectionPaths()
	if len(paths) == 0 {
		return nil
	}
	doIt := func(m *Model) tea.Cmd {
		bridge := m.bridge
		m.op = &opState{desc: "trashing…", prog: nil}
		run := opCmd(fmt.Sprintf("moved %d item(s) to Trash", len(paths)), true, func() (*fsx.Undo, error) {
			type rec struct{ orig, putback string }
			var recs []rec
			var firstErr error
			for _, p := range paths {
				pb, err := bridge.Trash(p)
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					continue
				}
				recs = append(recs, rec{orig: p, putback: pb})
			}
			var undo *fsx.Undo
			if len(recs) > 0 {
				undo = &fsx.Undo{Desc: "trash", Fn: func() error {
					var err error
					for i := len(recs) - 1; i >= 0; i-- {
						r := recs[i]
						if r.putback == "" {
							continue
						}
						if e := os.Rename(r.putback, r.orig); e != nil && err == nil {
							err = e
						}
					}
					return err
				}}
			}
			return undo, firstErr
		})
		return tea.Batch(run, opTickCmd())
	}
	if !m.cfg.General.ConfirmTrash {
		return doIt(m)
	}
	body := []string{}
	for i, p := range paths {
		if i == 4 && len(paths) > 5 {
			body = append(body, fmt.Sprintf("… and %d more", len(paths)-4))
			break
		}
		body = append(body, "• "+truncEnd(sanitize(filepath.Base(p)), 48))
	}
	m.modal = &ConfirmModal{
		Title:    fmt.Sprintf("Move %d item(s) to Trash?", len(paths)),
		Body:     body,
		YesLabel: "trash",
		Yes:      doIt,
	}
	return nil
}

func (m *Model) compressAction() tea.Cmd {
	entries := m.selectionEntries()
	if len(entries) == 0 {
		return nil
	}
	// ditto/zip read file contents, which materializes evicted iCloud
	// files — never do that silently (SPEC safety rails).
	return m.confirmIfDataless(entries, "Compress will download from iCloud", "compress anyway",
		func(m *Model) tea.Cmd {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				// Tree rows can be nested: zip by path relative to
				// the panel root, not by bare name.
				if rel, err := filepath.Rel(m.cwd, e.Path); err == nil {
					names = append(names, rel)
				} else {
					names = append(names, e.Name)
				}
			}
			destName := "Archive.zip"
			if len(entries) == 1 {
				destName = entries[0].Name + ".zip"
			}
			dir := m.cwd
			m.op = &opState{desc: "compressing…", prog: nil}
			run := opCmd("compressed to "+sanitize(destName), true, func() (*fsx.Undo, error) {
				if err := fsx.Zip(dir, names, destName); err != nil {
					return nil, err
				}
				p := filepath.Join(dir, destName)
				return &fsx.Undo{Desc: "compress", Fn: func() error { return os.Remove(p) }}, nil
			})
			return tea.Batch(run, opTickCmd())
		})
}

// confirmIfDataless counts cloud-only bytes in the selection (folders are
// scanned via their listings, bounded) and interposes a confirm modal when
// the operation would download them; otherwise it runs next immediately.
func (m *Model) confirmIfDataless(entries []fsx.Entry, title, yesLabel string, next func(*Model) tea.Cmd) tea.Cmd {
	var n int
	var bytes int64
	capped := false
	for _, e := range entries {
		switch {
		case e.IsDir && e.InICloud:
			files, c, _ := icloud.ExpandDataless(e.Path, 20000)
			capped = capped || c
			for _, f := range files {
				if fi, err := os.Lstat(f); err == nil {
					n++
					bytes += fi.Size()
				}
			}
		case e.Dataless:
			n++
			bytes += e.Size
		}
	}
	if n == 0 && !capped {
		return next(m)
	}
	body := []string{fmt.Sprintf("%d item(s), %s, are cloud-only and will be", n, humanBytes(bytes)),
		"downloaded to complete this operation."}
	if capped {
		body = append(body, "(large folder — scan capped, actual size may be higher)")
	}
	m.modal = &ConfirmModal{Title: title, Body: body, YesLabel: yesLabel, Yes: next}
	return nil
}

func (m *Model) undoAction() tea.Cmd {
	if m.undo == nil {
		return m.note(levelInfo, "nothing to undo")
	}
	u := m.undo
	m.undo = nil
	m.op = &opState{desc: "undoing…", prog: nil}
	run := opCmd("undid: "+u.Desc, true, func() (*fsx.Undo, error) {
		return nil, u.Fn()
	})
	return tea.Batch(run, opTickCmd())
}

// --- get info -----------------------------------------------------------------

func (m *Model) getInfoAction() tea.Cmd {
	e := m.currentEntry()
	if e == nil {
		return nil
	}
	lines := []string{
		"name:      " + sanitize(e.Name),
		"where:     " + sanitize(abbreviateHome(filepath.Dir(e.Path), m.home)),
		"kind:      " + e.Kind(),
		"size:      " + humanBytes(e.Size) + " logical, " + humanBytes(e.LocalBytes) + " on disk",
		"modified:  " + e.ModTime.Format("2006-01-02 15:04:05"),
		"perms:     " + e.Mode.String(),
	}
	if e.IsLink {
		lines = append(lines, "target:    "+sanitize(e.LinkTarget))
	}
	switch e.Status() {
	case icloud.StatusEvicted:
		lines = append(lines, "iCloud:    ☁ cloud-only (evicted)")
	case icloud.StatusLocal:
		lines = append(lines, "iCloud:    ✓ local & synced")
	default:
		lines = append(lines, "iCloud:    not in iCloud")
	}
	if e.IsDir {
		if des, err := os.ReadDir(e.Path); err == nil {
			lines = append(lines, fmt.Sprintf("contains:  %d items", len(des)))
		}
	}
	m.modal = &InfoModal{Title: "Get Info", Lines: lines}
	return nil
}

// --- iCloud download / evict -----------------------------------------------------

func expandMarksCmd(paths []string) tea.Cmd {
	return func() tea.Msg {
		var files []string
		var total int64
		capped := false
		for _, p := range paths {
			fi, err := os.Lstat(p)
			if err != nil {
				continue
			}
			if fi.IsDir() {
				fs_, c, _ := icloud.ExpandDataless(p, 200000)
				files = append(files, fs_...)
				capped = capped || c
				for _, f := range fs_ {
					if ffi, err := os.Lstat(f); err == nil {
						total += ffi.Size()
					}
				}
			} else if icloud.Dataless(fi) {
				files = append(files, p)
				total += fi.Size()
			}
		}
		return marksExpandedMsg{files: files, total: total, capped: capped}
	}
}

func (m *Model) downloadAction() tea.Cmd {
	paths := m.selectionPaths()
	var inCloud []string
	for _, p := range paths {
		if icloud.InICloud(p) {
			inCloud = append(inCloud, p)
		}
	}
	if len(inCloud) == 0 {
		return m.note(levelWarn, "selection is not inside iCloud Drive")
	}
	return tea.Batch(
		m.note(levelInfo, "scanning for cloud-only files…"),
		expandMarksCmd(inCloud),
	)
}

func (m *Model) evictAction() tea.Cmd {
	entries := m.selectionEntries()
	var targets []fsx.Entry
	for _, e := range entries {
		if e.InICloud && (e.IsDir || !e.Dataless) {
			targets = append(targets, e)
		}
	}
	if len(targets) == 0 {
		return m.note(levelWarn, "nothing evictable selected (must be local items in iCloud)")
	}
	bridge := m.bridge
	body := []string{"Local copies are removed; files stay in iCloud", "and show ☁ until downloaded again."}
	m.modal = &ConfirmModal{
		Title:    fmt.Sprintf("Evict %d item(s)?", len(targets)),
		Body:     body,
		YesLabel: "evict",
		Yes: func(m *Model) tea.Cmd {
			m.op = &opState{desc: "evicting…", prog: nil}
			run := opCmd(fmt.Sprintf("evicted %d item(s)", len(targets)), true, func() (*fsx.Undo, error) {
				var firstErr error
				for _, e := range targets {
					if err := bridge.Evict(e.Path); err != nil && firstErr == nil {
						firstErr = err
					}
				}
				return nil, firstErr
			})
			return tea.Batch(run, opTickCmd())
		},
	}
	return nil
}

// --- exec helpers ------------------------------------------------------------------

func execCmd(what, name string, args ...string) tea.Cmd {
	return func() tea.Msg {
		c := exec.Command(name, args...)
		c.Stdout = io.Discard
		c.Stderr = io.Discard
		c.Stdin = nil
		return execDoneMsg{what: what, err: c.Run()}
	}
}

func pbcopyCmd(text string) tea.Cmd {
	return func() tea.Msg {
		c := exec.Command("pbcopy")
		c.Stdin = strings.NewReader(text)
		return execDoneMsg{what: "clipboard", err: c.Run()}
	}
}
