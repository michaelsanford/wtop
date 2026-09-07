package ui

import (
	"strings"

	"github.com/michaelsanford/wtop/internal/collector"
)

// ancestorWalkLimit bounds the climb up the PPID chain.  The visited set already
// stops a PID-reuse cycle; this is belt-and-braces against a pathological chain
// in a snapshot that is only ever ~128 entries deep.
const ancestorWalkLimit = 32

// searchQuery builds the web-search string for pid: the process name, prefixed
// with the nearest ancestor whose name differs.  A generic host binary on its own
// is a poor query — "msedgewebview2.exe" says nothing about which app is hosting
// it — while a repeated name (one Dropbox.exe under another) adds nothing, so an
// ancestor sharing the child's name is skipped rather than prepended.
//
// It reads the snapshot rather than the table row because the rendered name cell
// carries tree-drawing prefixes and the self marker.
func searchQuery(procs []collector.ProcSnapshot, pid int32) string {
	byPID := make(map[int32]collector.ProcSnapshot, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}

	self, ok := byPID[pid]
	if !ok || self.Name == "" {
		return ""
	}

	visited := map[int32]bool{self.PID: true}
	cur := self
	for i := 0; i < ancestorWalkLimit; i++ {
		parent, ok := byPID[cur.PPID]
		if !ok || visited[parent.PID] {
			break
		}
		visited[parent.PID] = true
		if parent.Name != "" && !strings.EqualFold(parent.Name, self.Name) {
			return parent.Name + " " + self.Name
		}
		cur = parent
	}
	return self.Name
}
