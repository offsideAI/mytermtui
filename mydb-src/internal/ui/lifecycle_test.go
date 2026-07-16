package ui

import (
	"strings"
	"testing"
)

// One active connection at a time (§3.2): connecting a new database
// disconnects the previous one and clears its derived state.
func TestConnectDisconnectsPrevious(t *testing.T) {
	m, _ := fixture(t)
	fakePGFixture(t, m, "pgc", "appdb")

	// Connect the sqlite connection first.
	m.cursor = indexOf2(t, m, "app")
	press(t, m, "enter")
	appID := m.currentNode().ConnID
	if m.state[appID] != connOpen {
		t.Fatal("precondition: app connected")
	}

	// Connecting pgc must close app and drop its subtree.
	m.cursor = indexOf2(t, m, "pgc")
	press(t, m, "c")
	pgID := m.currentNode().ConnID
	if m.state[pgID] != connOpen {
		t.Fatalf("pgc should be connected, state=%v", m.state[pgID])
	}
	if m.state[appID] != connClosed {
		t.Fatalf("app should have been auto-disconnected, state=%v", m.state[appID])
	}
	if contains(visible(m), "Tables (1)") {
		t.Fatalf("old connection's schema rows must be gone: %v", visible(m))
	}
	open := 0
	for range m.open {
		open++
	}
	if open != 1 {
		t.Fatalf("exactly one open connection expected, got %d", open)
	}
}

func TestStatusIndicator(t *testing.T) {
	m, _ := fixture(t)
	if !strings.Contains(m.View(), "● disconnected") {
		t.Fatal("frame should show the red disconnected indicator at launch")
	}
	press(t, m, "down", "enter") // connect app
	frame := m.View()
	if !strings.Contains(frame, "● app") || strings.Contains(frame, "● disconnected") {
		t.Fatal("frame should show the green connected indicator with the name")
	}
	press(t, m, "d")
	if !strings.Contains(m.View(), "● disconnected") {
		t.Fatal("indicator should flip back to disconnected after d")
	}
}
