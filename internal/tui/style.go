package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Environment prompt colors (§18). ANSI base colors downsample gracefully on
// limited terminals (e.g. the legacy Windows console).
var envStyles = map[string]lipgloss.Style{
	"dev":   lipgloss.NewStyle().Foreground(lipgloss.Color("2")), // green
	"test":  lipgloss.NewStyle().Foreground(lipgloss.Color("3")), // yellow
	"stage": lipgloss.NewStyle().Foreground(lipgloss.Color("3")), // yellow
	"prod":  lipgloss.NewStyle().Foreground(lipgloss.Color("1")), // red
}

// unknownEnvStyle is used for any unrecognized or empty environment.
var unknownEnvStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // gray

// promptStyleFor returns the style for an environment label.
func promptStyleFor(env string) lipgloss.Style {
	if s, ok := envStyles[env]; ok {
		return s
	}
	return unknownEnvStyle
}

// Help and table styles (§12). Foreground-only colors stay theme-safe (they ride
// on the terminal's own palette); the alternating stripe is the one place we set
// a background, so its shade is chosen from the detected terminal background.
var (
	helpTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")) // section headers, magenta
	helpCmdStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4")) // command names, blue
	helpArgStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))            // argument syntax, gray
	helpDescStyle  = lipgloss.NewStyle()                                            // descriptions, default fg
	helpFootStyle  = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("8"))

	// Column headers for the simple object tables (.server list, .list, etc.).
	tableHeadStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")) // cyan
	tableRuleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))            // separator rule, gray

	// mutedStyle dims secondary output such as a result's row-count summary.
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // gray
)

// stripeColor returns the subdued background for alternating rows: a hair darker
// than a dark terminal, a hair lighter than a light one, so the banding reads as
// a faint tint rather than a block. 256-color values downsample gracefully.
func stripeColor(dark bool) color.Color {
	if dark {
		return lipgloss.Color("236") // near-black gray
	}
	return lipgloss.Color("254") // near-white gray
}
