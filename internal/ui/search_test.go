package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/michaelsanford/wtop/internal/collector"
)

func TestSearchQuery(t *testing.T) {
	procs := []collector.ProcSnapshot{
		{PID: 1, PPID: 0, Name: "explorer.exe"},
		{PID: 2, PPID: 1, Name: "GoogleDriveFS.exe"},
		{PID: 3, PPID: 2, Name: "msedgewebview2.exe"},
		{PID: 4, PPID: 3, Name: "msedgewebview2.exe"},
		{PID: 5, PPID: 1, Name: "Dropbox.exe"},
		{PID: 6, PPID: 5, Name: "dropbox.exe"},
		{PID: 7, PPID: 999, Name: "orphan.exe"},
		{PID: 8, PPID: 9, Name: "cyclic-a.exe"},
		{PID: 9, PPID: 8, Name: "cyclic-a.exe"},
		{PID: 10, PPID: 1, Name: ""},
	}

	tests := []struct {
		name string
		pid  int32
		want string
	}{
		{"child of a differently named parent", 3, "GoogleDriveFS.exe msedgewebview2.exe"},
		{"climbs past same-named ancestors", 4, "GoogleDriveFS.exe msedgewebview2.exe"},
		{"root process searches its own name", 1, "explorer.exe"},
		{"a differently-cased parent counts as the same name", 6, "explorer.exe dropbox.exe"},
		{"differently named parent is prepended", 5, "explorer.exe Dropbox.exe"},
		{"parent missing from the snapshot", 7, "orphan.exe"},
		{"a PPID cycle terminates", 8, "cyclic-a.exe"},
		{"unknown PID yields nothing", 42, ""},
		{"unnamed process yields nothing", 10, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := searchQuery(procs, tc.pid); got != tc.want {
				t.Errorf("searchQuery(%d) = %q, want %q", tc.pid, got, tc.want)
			}
		})
	}
}

// A same-named ancestor must be skipped, not merely ignored, so "Dropbox.exe
// under Dropbox.exe" never becomes a doubled query.
func TestSearchQuery_SkipsSameNamedAncestors(t *testing.T) {
	procs := []collector.ProcSnapshot{
		{PID: 1, PPID: 0, Name: "chrome.exe"},
		{PID: 2, PPID: 1, Name: "chrome.exe"},
		{PID: 3, PPID: 2, Name: "chrome.exe"},
	}
	if got := searchQuery(procs, 3); got != "chrome.exe" {
		t.Errorf("searchQuery = %q, want %q", got, "chrome.exe")
	}
}

func TestSearchQuery_Empty(t *testing.T) {
	if got := searchQuery(nil, 1); got != "" {
		t.Errorf("searchQuery on an empty snapshot = %q, want empty", got)
	}
}

// Pressing [?] must never panic, whatever the table holds.
func TestSearchKey_SurvivesAnEmptySnapshot(t *testing.T) {
	var empty collector.Snapshot
	m := New(fixedCollector{empty})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	mm := updated.(Model)
	updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if updated.(Model).lastErr != nil {
		t.Errorf("[?] with no rows set an error: %v", updated.(Model).lastErr)
	}
}

func TestSearchKey_DoesNotDisturbTheModel(t *testing.T) {
	m := New(fixedCollector{viewFixture()})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	before := updated.(Model)
	updated, _ = before.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	after := updated.(Model)
	if after.confirming {
		t.Error("[?] opened the kill confirmation")
	}
	if after.treeView != before.treeView || after.sortBy != before.sortBy {
		t.Error("[?] changed view state")
	}
}
