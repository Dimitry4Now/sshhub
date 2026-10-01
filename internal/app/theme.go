package app

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme is a color scheme. Themes are inspired by the historic national
// racing colors and by motorsport color traditions in general; they are not
// affiliated with any team or series.
type Theme struct {
	Name string
	Desc string

	Primary   string // borders, title badge background
	BadgeText string // title badge text, readable on Primary
	Heading   string // headings, readable on a dark terminal
	Highlight string // clock, selection, cursor
	Good      string // correct answers, "in chat" dots
	Bad       string // wrong answers, errors, food
}

// Themes lists the available themes; the first one is the default.
var Themes = []Theme{
	{Name: "Racing Green", Desc: "British racing green with a lime flash",
		Primary: "#00665E", BadgeText: "#FFFFFF", Heading: "#00A19B", Highlight: "#CEDC00", Good: "#3DDC84", Bad: "#FF5F5F"},
	{Name: "Rosso Corsa", Desc: "Italian racing red with yellow",
		Primary: "#B3000C", BadgeText: "#FFFFFF", Heading: "#FF4040", Highlight: "#FFD500", Good: "#3DDC84", Bad: "#FF9E5E"},
	{Name: "Papaya", Desc: "papaya orange with sky blue",
		Primary: "#FF8000", BadgeText: "#1A1A1A", Heading: "#FF9E3D", Highlight: "#47C7FC", Good: "#3DDC84", Bad: "#FF5F87"},
	{Name: "Silver", Desc: "German racing silver with teal",
		Primary: "#6F7A82", BadgeText: "#FFFFFF", Heading: "#C4CCD2", Highlight: "#00D2BE", Good: "#3DDC84", Bad: "#FF5F5F"},
	{Name: "Midnight", Desc: "midnight navy with red and yellow",
		Primary: "#1E3A8A", BadgeText: "#FFFFFF", Heading: "#5B7FFF", Highlight: "#FFC906", Good: "#3DDC84", Bad: "#FF3B3B"},
	{Name: "Racing Blue", Desc: "French racing blue with pink",
		Primary: "#0055A4", BadgeText: "#FFFFFF", Heading: "#3C8DFF", Highlight: "#FF87BC", Good: "#3DDC84", Bad: "#FF5F5F"},
}

// themeIndex returns the index of the named theme, or 0 (the default).
func themeIndex(name string) int {
	for i, t := range Themes {
		if t.Name == name {
			return i
		}
	}
	return 0
}

// styles is a theme turned into ready-to-use Lip Gloss styles.
type styles struct {
	theme Theme
	muted color.Color

	header, title, clock, dim, help lipgloss.Style
	box, heading, selected          lipgloss.Style
	good, bad                       lipgloss.Style

	snakeHead, snakeBody, food, board lipgloss.Style

	logo []lipgloss.Style // one style per logo row
}

var themeStyles = func() []*styles {
	out := make([]*styles, len(Themes))
	for i, t := range Themes {
		out[i] = newStyles(t)
	}
	return out
}()

// theme returns the styles for a session's chosen theme.
func theme(sess *Session) *styles { return themeStyles[themeIndex(sess.Theme)] }

func newStyles(t Theme) *styles {
	c := lipgloss.Color
	muted := c("#808080")
	s := &styles{theme: t, muted: muted}

	s.header = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).BorderForeground(muted)
	s.title = lipgloss.NewStyle().Bold(true).Foreground(c(t.BadgeText)).Background(c(t.Primary))
	s.clock = lipgloss.NewStyle().Bold(true).Foreground(c(t.Highlight))
	s.dim = lipgloss.NewStyle().Foreground(muted)
	s.help = lipgloss.NewStyle().Foreground(muted).Padding(0, 1)

	s.box = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c(t.Primary)).Padding(1, 3)
	s.heading = lipgloss.NewStyle().Bold(true).Foreground(c(t.Heading))
	s.selected = lipgloss.NewStyle().Bold(true).Foreground(c(t.Highlight))
	s.good = lipgloss.NewStyle().Bold(true).Foreground(c(t.Good))
	s.bad = lipgloss.NewStyle().Bold(true).Foreground(c(t.Bad))

	s.snakeHead = lipgloss.NewStyle().Foreground(c(t.Highlight))
	s.snakeBody = lipgloss.NewStyle().Foreground(c(t.Heading))
	s.food = lipgloss.NewStyle().Foreground(c(t.Bad))
	s.board = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c(t.Primary)).PaddingRight(1)

	// The logo fades primary -> heading -> highlight over its rows.
	for _, hex := range gradient([]string{t.Primary, t.Heading, t.Highlight}, len(logo)) {
		s.logo = append(s.logo, lipgloss.NewStyle().Foreground(c(hex)))
	}
	return s
}

// gradient returns n colors evenly spread along the given color stops.
func gradient(stops []string, n int) []string {
	out := make([]string, n)
	for i := range n {
		pos := float64(i) / float64(n-1) * float64(len(stops)-1)
		seg := min(int(pos), len(stops)-2)
		out[i] = mix(stops[seg], stops[seg+1], pos-float64(seg))
	}
	return out
}

func mix(a, b string, f float64) string {
	var ar, ag, ab, br, bg, bb int
	fmt.Sscanf(a, "#%02x%02x%02x", &ar, &ag, &ab)
	fmt.Sscanf(b, "#%02x%02x%02x", &br, &bg, &bb)
	lerp := func(x, y int) int { return x + int(float64(y-x)*f+0.5) }
	return fmt.Sprintf("#%02X%02X%02X", lerp(ar, br), lerp(ag, bg), lerp(ab, bb))
}
