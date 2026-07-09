package ui

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/offsideai/myconsole/internal/fsx"
	"github.com/offsideai/myconsole/internal/icloud"
)

// schedulePreview requests preview content for the cursor entry when the
// panel is visible and the entry changed.
func (m *Model) schedulePreview() tea.Cmd {
	if !m.previewOn {
		return nil
	}
	e := m.currentEntry()
	if e == nil {
		m.prevPath = ""
		m.prevLines = nil
		return nil
	}
	if e.Path == m.prevPath {
		return nil
	}
	m.prevPath = e.Path
	m.prevLines = []string{"…"}
	entry := *e
	maxLines := m.listHeight()
	return func() tea.Msg {
		return previewMsg{path: entry.Path, lines: buildPreview(entry, maxLines)}
	}
}

const previewReadCap = 64 * 1024

// buildPreview produces the content lines for the preview panel. It runs
// with dataless-materialization disabled: previewing an evicted file must
// never trigger a multi-gigabyte download (spec §1.7).
func buildPreview(e fsx.Entry, maxLines int) []string {
	switch {
	case e.IsDir:
		return previewDir(e, maxLines)
	case e.Dataless:
		return []string{
			"☁ cloud-only (evicted)",
			"",
			fmt.Sprintf("%s in iCloud, %s local", humanBytes(e.Size), humanBytes(e.LocalBytes)),
			"",
			"press d to download",
		}
	case e.IsLink:
		lines := []string{"symlink → " + sanitize(e.LinkTarget)}
		if e.LinkBroken {
			lines = append(lines, "(broken)")
		}
		return lines
	}

	var data []byte
	var err error
	icloud.WithNoMaterialize(func() {
		f, oerr := os.Open(e.Path)
		if oerr != nil {
			err = oerr
			return
		}
		defer f.Close()
		buf := make([]byte, previewReadCap)
		n, _ := f.Read(buf)
		data = buf[:n]
	})
	if err != nil {
		return []string{"unreadable: " + err.Error()}
	}
	if len(data) == 0 {
		return []string{"(empty file)"}
	}
	if bytes.IndexByte(data[:min(len(data), 1024)], 0) >= 0 {
		return []string{"binary file — no text preview", "", "q Quick Look · o open"}
	}
	text := string(data)
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, sanitize(strings.ReplaceAll(line, "\t", "    ")))
		if len(out) >= maxLines {
			break
		}
	}
	return out
}

func previewDir(e fsx.Entry, maxLines int) []string {
	des, err := os.ReadDir(e.Path)
	if err != nil {
		return []string{"unreadable: " + errHint(err)}
	}
	dirs, files := 0, 0
	for _, de := range des {
		if de.IsDir() {
			dirs++
		} else {
			files++
		}
	}
	out := []string{
		fmt.Sprintf("%d folders, %d files", dirs, files),
		"modified " + e.ModTime.Format("2006-01-02 15:04"),
		"",
	}
	for i, de := range des {
		if i >= maxLines-3 {
			out = append(out, fmt.Sprintf("… %d more", len(des)-i))
			break
		}
		name := sanitize(de.Name())
		if de.IsDir() {
			name = "▸ " + name
		} else {
			name = "  " + name
		}
		out = append(out, name)
	}
	return out
}
