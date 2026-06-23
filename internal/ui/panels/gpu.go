package panels

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/michaelsanford/wtop/internal/collector"
)

// GPU renders the GPU panel for one snapshot. idx and total are used to show a
// "1/2" cycling indicator when multiple GPUs are present; pass 0, 0 when there
// is only one GPU or no GPUs.
func GPU(snap collector.GPUSnapshot, idx, total int, width, height int) string {
	inner := width - 4
	if inner < 10 {
		inner = 10
	}
	// Overhead: "%-10s [%s %3.0f%%]" = label(10) + " ["(2) + " NNN%]"(6) = 18
	barW := inner - 18
	if barW < 4 {
		barW = 4
	}

	var lines []string

	switch snap.Source {
	case collector.GPUSourceNone:
		lines = append(lines, lipgloss.NewStyle().Foreground(colorBorder).Render("[ loading… ]"))

	case collector.GPUSourceNvidiaSmi:
		lines = append(lines, gpuNameLine(snap.Name, idx, total, inner))

		purpleColor := lipgloss.Color("13")
		magentaColor := lipgloss.Color("5")

		utilBar := renderBar(snap.UtilPct, barW, purpleColor)
		lines = append(lines, fmt.Sprintf("%-10s [%s %3.0f%%]", "3D Util", utilBar, snap.UtilPct))

		memUtilBar := renderBar(snap.MemUtilPct, barW, magentaColor)
		lines = append(lines, fmt.Sprintf("%-10s [%s %3.0f%%]", "Mem Util", memUtilBar, snap.MemUtilPct))

		if snap.MemTotMiB > 0 {
			vramPct := float64(snap.MemUsedMiB) / float64(snap.MemTotMiB) * 100
			vramBar := renderBar(vramPct, barW, lipgloss.Color("12"))
			lines = append(lines, fmt.Sprintf("%-10s [%s %3.0f%%]", "VRAM", vramBar, vramPct))
			lines = append(lines, fmt.Sprintf("           %d / %d MiB", snap.MemUsedMiB, snap.MemTotMiB))
		}

		tempStr := "N/A"
		if snap.TempC > 0 {
			tempStr = fmt.Sprintf("%.0f°C", snap.TempC)
		}
		fanStr := "passive"
		if snap.FanPct > 0 {
			fanStr = fmt.Sprintf("%.0f%%", snap.FanPct)
		}
		lines = append(lines, fmt.Sprintf("  Temp %-8s  Fan %s", tempStr, fanStr))

		if snap.PowerLimitW > 0 {
			lines = append(lines, fmt.Sprintf("  Power %.0f / %.0f W  (%s)",
				snap.PowerDrawW, snap.PowerLimitW, snap.PState))
		}
		if snap.GfxClockMHz > 0 {
			lines = append(lines, fmt.Sprintf("  GfxClk %d MHz  MemClk %d MHz",
				snap.GfxClockMHz, snap.MemClockMHz))
		}

	case collector.GPUSourcePowerShell:
		lines = append(lines, gpuNameLine(snap.Name, idx, total, inner))

		purpleColor := lipgloss.Color("13")
		utilBar := renderBar(snap.UtilPct, barW, purpleColor)
		lines = append(lines, fmt.Sprintf("%-10s [%s %3.0f%%]", "3D Util", utilBar, snap.UtilPct))
		if snap.MemTotMiB > 0 {
			lines = append(lines, fmt.Sprintf("  VRAM %d MiB total", snap.MemTotMiB))
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(colorBorder).Render("(limited: Get-Counter)"))
	}

	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1).
		Width(width - 2).
		Height(height).
		Render(content)
}

// gpuNameLine returns the GPU name line, appending a dim "n/N" indicator when
// total > 1 so the user can see there are more GPUs to cycle through.
func gpuNameLine(name string, idx, total, inner int) string {
	indicator := ""
	if total > 1 {
		indicator = fmt.Sprintf(" %d/%d", idx+1, total)
	}
	label := name
	if label == "" {
		label = "GPU (integrated)"
	}
	maxName := inner - len(indicator)
	if len(label) > maxName {
		label = label[:maxName-3] + "..."
	}
	nameStr := lipgloss.NewStyle().Foreground(colorWhite).Render(label)
	if indicator == "" {
		return nameStr
	}
	indStr := lipgloss.NewStyle().Foreground(colorBorder).Render(indicator)
	return nameStr + indStr
}
