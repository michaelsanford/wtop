package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/michaelsanford/wtop/internal/collector"
	"github.com/michaelsanford/wtop/internal/ui"
)

func main() {
	coll := collector.New()
	model := ui.New(coll)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "wtop: %v\n", err)
		os.Exit(1)
	}
}
