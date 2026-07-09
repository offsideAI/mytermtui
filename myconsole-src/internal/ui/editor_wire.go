package ui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// editorMaxBytes caps files the embedded editor will open; the buffer is
// held whole in memory, so huge files are refused rather than risked.
const editorMaxBytes = 10 << 20 // 10 MB

type editorOpenMsg struct {
	path   string
	data   []byte
	err    error
	tooBig bool
	binary bool
}

type editorSavedMsg struct {
	path string
	err  error
}

// readForEditorCmd reads a (known-local) file for editing, off the update
// loop. Size and binary guards keep the editor to sane text files.
func readForEditorCmd(path string) tea.Cmd {
	return func() tea.Msg {
		fi, err := os.Lstat(path)
		if err != nil {
			return editorOpenMsg{path: path, err: err}
		}
		if fi.Size() > editorMaxBytes {
			return editorOpenMsg{path: path, tooBig: true}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return editorOpenMsg{path: path, err: err}
		}
		if isBinary(data) {
			return editorOpenMsg{path: path, binary: true}
		}
		return editorOpenMsg{path: path, data: data}
	}
}

// saveEditorCmd writes the editor buffer back, preserving the file's
// existing permission bits.
func saveEditorCmd(path string, content []byte) tea.Cmd {
	return func() tea.Msg {
		mode := os.FileMode(0o644)
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
		err := os.WriteFile(path, content, mode)
		return editorSavedMsg{path: path, err: err}
	}
}

// isBinary reports whether the first chunk contains a NUL byte.
func isBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}
