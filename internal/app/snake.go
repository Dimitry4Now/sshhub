package app

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	snakeGame  = "snake"
	snakeCols  = 24
	snakeRows  = 14
	snakeStart = 140 * time.Millisecond
	snakeFloor = 60 * time.Millisecond
)

type point struct{ x, y int }

var (
	dirUp    = point{0, -1}
	dirDown  = point{0, 1}
	dirLeft  = point{-1, 0}
	dirRight = point{1, 0}
)

// snakeTickMsg drives one game step. gen discards ticks from earlier games.
type snakeTickMsg struct{ gen int }

type snakeScreen struct {
	deps *Deps
	sess *Session

	body    []point // body[0] is the head
	dir     point   // direction of the last step
	next    point   // direction for the next step
	food    point
	apples  int
	speed   time.Duration
	gen     int
	paused  bool
	over    bool
	saveMsg string
}

func newSnake(deps *Deps, sess *Session) *snakeScreen {
	return &snakeScreen{deps: deps, sess: sess}
}

func (s *snakeScreen) Init() tea.Cmd {
	cx, cy := snakeCols/2, snakeRows/2
	s.body = []point{{cx, cy}, {cx - 1, cy}, {cx - 2, cy}}
	s.dir, s.next = dirRight, dirRight
	s.apples, s.speed = 0, snakeStart
	s.paused, s.over, s.saveMsg = false, false, ""
	s.placeFood()
	s.gen++
	return s.tick()
}

func (s *snakeScreen) tick() tea.Cmd {
	gen := s.gen
	return tea.Tick(s.speed, func(time.Time) tea.Msg { return snakeTickMsg{gen} })
}

func (s *snakeScreen) score() int { return s.apples * 10 }

func (s *snakeScreen) placeFood() {
	occupied := map[point]bool{}
	for _, p := range s.body {
		occupied[p] = true
	}
	free := make([]point, 0, snakeCols*snakeRows-len(s.body))
	for y := range snakeRows {
		for x := range snakeCols {
			if p := (point{x, y}); !occupied[p] {
				free = append(free, p)
			}
		}
	}
	if len(free) > 0 {
		s.food = free[rand.IntN(len(free))]
	}
}

// step advances the game one cell. It returns false when the snake dies.
func (s *snakeScreen) step() bool {
	s.dir = s.next
	head := point{s.body[0].x + s.dir.x, s.body[0].y + s.dir.y}
	if head.x < 0 || head.x >= snakeCols || head.y < 0 || head.y >= snakeRows {
		return false
	}
	grow := head == s.food
	// The tail moves away this step unless we grow, so it doesn't count as a hit.
	body := s.body
	if !grow {
		body = body[:len(body)-1]
	}
	for _, p := range body {
		if p == head {
			return false
		}
	}
	s.body = append([]point{head}, body...)
	if grow {
		s.apples++
		s.speed = max(s.speed-5*time.Millisecond, snakeFloor)
		s.placeFood()
	}
	return true
}

func (s *snakeScreen) gameOver() tea.Cmd {
	s.over = true
	if s.sess.Guest || s.sess.Fingerprint == "" {
		s.saveMsg = "Guests don't make the leaderboard."
		return nil
	}
	if s.apples == 0 {
		return nil
	}
	s.saveMsg = "Saving score…"
	st, fp, score := s.deps.Store, s.sess.Fingerprint, s.score()
	return func() tea.Msg { return scoreSavedMsg{st.AddScore(fp, snakeGame, score)} }
}

func (s *snakeScreen) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case snakeTickMsg:
		if msg.gen != s.gen || s.over || s.paused {
			return nil
		}
		if !s.step() {
			return s.gameOver()
		}
		return s.tick()
	case scoreSavedMsg:
		if msg.err != nil {
			s.saveMsg = "Could not save score: " + msg.err.Error()
		} else {
			s.saveMsg = "Score saved to the leaderboard."
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q":
			s.gen++ // stop the running game loop
			return back
		case "up", "w", "k":
			s.turn(dirUp)
		case "down", "s", "j":
			s.turn(dirDown)
		case "left", "a", "h":
			s.turn(dirLeft)
		case "right", "d", "l":
			s.turn(dirRight)
		case "p", "space":
			if !s.over {
				s.paused = !s.paused
				if !s.paused {
					return s.tick()
				}
			}
		case "r":
			if s.over {
				return s.Init()
			}
		}
	}
	return nil
}

// turn queues a direction change, ignoring reversals into the snake's neck.
func (s *snakeScreen) turn(d point) {
	if d.x != -s.dir.x || d.y != -s.dir.y {
		s.next = d
	}
}

func (s *snakeScreen) View(width, height int) string {
	t := theme(s.sess)
	cells := map[point]string{s.food: t.food.Render(" ●")}
	for i, p := range s.body {
		if i == 0 {
			cells[p] = t.snakeHead.Render("██")
		} else {
			cells[p] = t.snakeBody.Render("▓▓")
		}
	}
	var grid strings.Builder
	for y := range snakeRows {
		for x := range snakeCols {
			if c, ok := cells[point{x, y}]; ok {
				grid.WriteString(c)
			} else {
				grid.WriteString(t.dim.Render(" ·"))
			}
		}
		if y < snakeRows-1 {
			grid.WriteString("\n")
		}
	}

	status := t.heading.Render("Snake") + "   " + t.selected.Render(fmt.Sprintf("score %d", s.score()))
	switch {
	case s.over:
		status += "   " + t.bad.Render("game over")
	case s.paused:
		status += "   " + t.dim.Render("paused")
	}
	out := lipgloss.JoinVertical(lipgloss.Center, status, "", t.board.Render(grid.String()))
	if s.over && s.saveMsg != "" {
		out = lipgloss.JoinVertical(lipgloss.Center, out, "", t.dim.Render(s.saveMsg))
	}
	return out
}

func (s *snakeScreen) Help() string {
	if s.over {
		return "r play again · esc menu"
	}
	return "arrows / wasd / hjkl steer · p pause · esc menu"
}
