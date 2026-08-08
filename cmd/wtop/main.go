package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/michaelsanford/wtop/internal/collector"
	"github.com/michaelsanford/wtop/internal/ui"
)

// Embed the Windows VERSIONINFO block and application manifest from
// winres/winres.json.  The generated rsrc_windows_<arch>.syso files are
// gitignored and picked up implicitly by `go build` via their GOARCH filename
// suffix; releases regenerate them with the tag's version numbers.  A binary
// built without running this carries no resource block, which is one of the
// signals that got v1.2.1 flagged by Defender's ML heuristics (see #20).
//
//go:generate go run github.com/tc-hib/go-winres@v0.3.3 make --arch amd64,arm64 --out rsrc

func main() {
	coll := collector.New()
	model := ui.New(coll)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "wtop: %v\n", err)
		os.Exit(1)
	}
}
