package app

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Meme is one piece of terminal art.
type Meme struct {
	Title string
	Art   string
}

// LoadMemes reads every .txt/.ans file in dir, sorted by filename.
// A leading "NN-" prefix orders files and is stripped from the title.
func LoadMemes(fsys fs.FS, dir string) ([]Meme, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var memes []Meme
	for _, e := range entries {
		ext := path.Ext(e.Name())
		if e.IsDir() || (ext != ".txt" && ext != ".ans") {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		title := strings.TrimSuffix(e.Name(), ext)
		if i := strings.IndexByte(title, '-'); i > 0 && strings.Trim(title[:i], "0123456789") == "" {
			title = title[i+1:]
		}
		memes = append(memes, Meme{
			Title: strings.ReplaceAll(title, "-", " "),
			Art:   strings.Trim(string(data), "\n"),
		})
	}
	return memes, nil
}

type memesScreen struct {
	memes []Meme
	idx   int
}

func newMemes(deps *Deps) *memesScreen { return &memesScreen{memes: deps.Memes} }

func (s *memesScreen) Init() tea.Cmd { return nil }

func (s *memesScreen) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	n := len(s.memes)
	switch key.String() {
	case "esc", "q":
		return back
	case "right", "l", "space", "n":
		if n > 0 {
			s.idx = (s.idx + 1) % n
		}
	case "left", "h", "p":
		if n > 0 {
			s.idx = (s.idx - 1 + n) % n
		}
	}
	return nil
}

func (s *memesScreen) View(width, height int) string {
	if len(s.memes) == 0 {
		return boxStyle.Render("The gallery is empty.")
	}
	m := s.memes[s.idx]
	title := headingStyle.Render(m.Title) + dimStyle.Render(fmt.Sprintf("  %d/%d", s.idx+1, len(s.memes)))
	// The frame takes 6 lines (border, padding, title, gap) and 8 columns.
	maxH := max(height-6, 1)
	art := lipgloss.NewStyle().MaxWidth(width - 8).MaxHeight(maxH).Render(m.Art)
	if lipgloss.Height(m.Art) > maxH {
		art = lipgloss.NewStyle().MaxWidth(width-8).MaxHeight(maxH-1).Render(m.Art) + "\n" +
			dimStyle.Render("↕ make the window taller to see it all")
	}
	return boxStyle.Render(lipgloss.JoinVertical(lipgloss.Center, title, "", art))
}

func (s *memesScreen) Help() string { return "←/→ browse · esc menu" }
