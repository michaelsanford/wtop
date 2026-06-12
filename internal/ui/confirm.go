package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

type confirmState struct {
	pid  int32
	name string
}

func renderConfirmOverlay(s confirmState, termW, termH int) string {
	body := fmt.Sprintf(
		"%s\n%s\n\n%s",
		styleConfirmTitle.Render("Kill process?"),
		styleMuted.Render(fmt.Sprintf("%s (PID %d)", s.name, s.pid)),
		"[y] confirm   [n / Esc] cancel",
	)
	box := styleConfirmBox.Render(body)
	return lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, box)
}
