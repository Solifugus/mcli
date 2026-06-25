package tui

import "charm.land/lipgloss/v2"

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
