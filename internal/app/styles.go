package app

import "charm.land/lipgloss/v2"

var (
	accent  = lipgloss.Color("#00665E") // Aston Martin F1 racing green
	accentL = lipgloss.Color("#00A19B") // lighter green, readable as text on dark terminals
	accent2 = lipgloss.Color("#CEDC00") // Aston Martin lime
	good    = lipgloss.Color("#87D75F")
	bad     = lipgloss.Color("#FF5F5F")
	muted   = lipgloss.Color("#808080")

	headerStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(muted)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent)
	clockStyle = lipgloss.NewStyle().Bold(true).Foreground(accent2)
	dimStyle   = lipgloss.NewStyle().Foreground(muted)
	helpStyle  = lipgloss.NewStyle().Foreground(muted).Padding(0, 1)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(1, 3)
	headingStyle  = lipgloss.NewStyle().Bold(true).Foreground(accentL)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(accent2)
	goodStyle     = lipgloss.NewStyle().Bold(true).Foreground(good)
	badStyle      = lipgloss.NewStyle().Bold(true).Foreground(bad)
)
