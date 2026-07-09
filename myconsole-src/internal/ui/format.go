package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

// humanBytes renders n like Finder: "247.4 MB", "2.9 GB", "612 bytes".
func humanBytes(n int64) string {
	switch {
	case n < 0:
		return "--"
	case n == 1:
		return "1 byte"
	case n < 1000:
		return fmt.Sprintf("%d bytes", n)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	v := float64(n)
	for _, u := range units {
		v /= 1000
		if v < 1000 {
			if v < 10 {
				return fmt.Sprintf("%.2f %s", v, u)
			}
			if v < 100 {
				return fmt.Sprintf("%.1f %s", v, u)
			}
			return fmt.Sprintf("%.0f %s", v, u)
		}
	}
	return fmt.Sprintf("%.1f EB", v/1000)
}

// shortBytes is a compact form for tight columns: "247M", "2.9G".
func shortBytes(n int64) string {
	if n < 0 {
		return "--"
	}
	if n < 1000 {
		return fmt.Sprintf("%d B", n)
	}
	units := "KMGTP"
	v := float64(n)
	for i := 0; i < len(units); i++ {
		v /= 1000
		if v < 1000 {
			if v < 10 {
				return fmt.Sprintf("%.1f %c", v, units[i])
			}
			return fmt.Sprintf("%.0f %c", v, units[i])
		}
	}
	return "big"
}

// humanTime formats like ls -l: recent files get time, old ones the year.
func humanTime(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "--"
	}
	if now.Sub(t) < 365*24*time.Hour && t.Before(now.Add(24*time.Hour)) {
		return t.Format("Jan _2 15:04")
	}
	return t.Format("Jan _2  2006")
}

// sanitize replaces control characters and normalizes exotic spaces so a
// filename cannot corrupt the display.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f:
			b.WriteRune('�')
		case r == ' ' || r == ' ' || r == ' ':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// truncMiddle shortens s to width cells with a middle ellipsis (Finder style).
func truncMiddle(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	left := (width - 1) / 2
	right := width - 1 - left
	return runewidth.Truncate(s, left, "") + "…" + truncLeft(s, right)
}

// truncLeft keeps the rightmost cells of s.
func truncLeft(s string, width int) string {
	if runewidth.StringWidth(s) <= width {
		return s
	}
	runes := []rune(s)
	w := 0
	i := len(runes)
	for i > 0 {
		rw := runewidth.RuneWidth(runes[i-1])
		if w+rw > width {
			break
		}
		w += rw
		i--
	}
	return string(runes[i:])
}

// truncEnd truncates at the end with a trailing ellipsis.
func truncEnd(s string, width int) string {
	if runewidth.StringWidth(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return runewidth.Truncate(s, width-1, "") + "…"
}

// pad right-pads s with spaces to exactly width cells (truncating if needed).
func pad(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w > width {
		return truncEnd(s, width)
	}
	return s + strings.Repeat(" ", width-w)
}

// padLeft left-pads s to width cells.
func padLeft(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w > width {
		return truncEnd(s, width)
	}
	return strings.Repeat(" ", width-w) + s
}

// progressBar renders a filled/empty bar of exactly width cells.
func progressBar(frac float64, width int) string {
	if width <= 0 {
		return ""
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(frac*float64(width) + 0.5)
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// abbreviateHome shows ~ for the home directory prefix.
func abbreviateHome(path, home string) string {
	if home != "" && strings.HasPrefix(path, home) {
		rest := path[len(home):]
		if rest == "" {
			return "~"
		}
		if strings.HasPrefix(rest, "/") {
			return "~" + rest
		}
	}
	return path
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
