package app

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	triviaGame      = "trivia"
	triviaRound     = 10
	triviaTimeLimit = 15 * time.Second
)

// Question is one trivia question. Answer indexes Choices.
type Question struct {
	Q       string   `json:"q"`
	Choices []string `json:"choices"`
	Answer  int      `json:"answer"`
}

// LoadQuestions reads the question bank from a JSON file in fsys.
func LoadQuestions(fsys fs.FS, path string) ([]Question, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, err
	}
	var qs []Question
	if err := json.Unmarshal(data, &qs); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i, q := range qs {
		if len(q.Choices) < 2 || q.Answer < 0 || q.Answer >= len(q.Choices) {
			return nil, fmt.Errorf("%s: question %d is malformed", path, i)
		}
	}
	return qs, nil
}

// points awards a base amount plus a bonus for answering quickly.
func points(correct bool, left time.Duration) int {
	if !correct {
		return 0
	}
	return 50 + 10*int(left.Round(time.Second)/time.Second)
}

type triviaState int

const (
	triviaAsking triviaState = iota
	triviaReveal
	triviaDone
)

type scoreSavedMsg struct{ err error }

type triviaScreen struct {
	deps *Deps
	sess *Session

	questions []Question
	idx       int
	cursor    int
	deadline  time.Time
	left      time.Duration
	picked    int // -1 when time ran out
	state     triviaState

	score   int
	correct int
	saveMsg string
}

func newTrivia(deps *Deps, sess *Session) *triviaScreen {
	return &triviaScreen{deps: deps, sess: sess}
}

// round picks n random questions with shuffled choices.
func round(bank []Question, n int) []Question {
	out := make([]Question, 0, n)
	for _, i := range rand.Perm(len(bank))[:min(n, len(bank))] {
		q := bank[i]
		order := rand.Perm(len(q.Choices))
		choices := make([]string, len(q.Choices))
		answer := 0
		for dst, src := range order {
			choices[dst] = q.Choices[src]
			if src == q.Answer {
				answer = dst
			}
		}
		out = append(out, Question{Q: q.Q, Choices: choices, Answer: answer})
	}
	return out
}

func (s *triviaScreen) Init() tea.Cmd {
	s.questions = round(s.deps.Questions, triviaRound)
	s.idx, s.score, s.correct, s.saveMsg = 0, 0, 0, ""
	s.ask()
	return nil
}

func (s *triviaScreen) ask() {
	s.state = triviaAsking
	s.cursor = 0
	s.deadline = time.Now().Add(triviaTimeLimit)
	s.left = triviaTimeLimit
}

func (s *triviaScreen) answer(choice int) {
	q := s.questions[s.idx]
	s.picked = choice
	ok := choice == q.Answer
	if ok {
		s.correct++
	}
	s.score += points(ok, s.left)
	s.state = triviaReveal
}

func (s *triviaScreen) next() tea.Cmd {
	s.idx++
	if s.idx < len(s.questions) {
		s.ask()
		return nil
	}
	s.state = triviaDone
	if s.sess.Guest || s.sess.Fingerprint == "" {
		s.saveMsg = "Guests don't make the leaderboard. Connect with an SSH key to save scores."
		return nil
	}
	s.saveMsg = "Saving score…"
	st, fp, score := s.deps.Store, s.sess.Fingerprint, s.score
	return func() tea.Msg { return scoreSavedMsg{st.AddScore(fp, triviaGame, score)} }
}

func (s *triviaScreen) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tickMsg:
		if s.state == triviaAsking {
			s.left = max(time.Until(s.deadline), 0)
			if s.left == 0 {
				s.answer(-1)
			}
		}
	case scoreSavedMsg:
		if msg.err != nil {
			s.saveMsg = "Could not save score: " + msg.err.Error()
		} else {
			s.saveMsg = "Score saved to the leaderboard."
		}
	case tea.KeyPressMsg:
		k := msg.String()
		if k == "esc" || k == "q" {
			return back
		}
		switch s.state {
		case triviaAsking:
			n := len(s.questions[s.idx].Choices)
			switch k {
			case "up", "k":
				s.cursor = (s.cursor - 1 + n) % n
			case "down", "j":
				s.cursor = (s.cursor + 1) % n
			case "enter", "space":
				s.answer(s.cursor)
			default:
				if i := choiceIndex(k); i >= 0 && i < n {
					s.answer(i)
				}
			}
		case triviaReveal:
			if k == "enter" || k == "space" {
				return s.next()
			}
		case triviaDone:
			if k == "r" {
				return s.Init()
			}
		}
	}
	return nil
}

// choiceIndex maps "1".."9" and "a".."i" to a choice index, or -1.
func choiceIndex(k string) int {
	if len(k) != 1 {
		return -1
	}
	switch c := k[0]; {
	case c >= '1' && c <= '9':
		return int(c - '1')
	case c >= 'a' && c <= 'i':
		return int(c - 'a')
	}
	return -1
}

func (s *triviaScreen) View(width, height int) string {
	w := min(64, width-2)
	if len(s.questions) == 0 {
		return boxStyle.Width(w).Render("No trivia questions loaded.")
	}
	var b strings.Builder
	if s.state == triviaDone {
		b.WriteString(headingStyle.Render("Round over!") + "\n\n")
		fmt.Fprintf(&b, "Correct: %d / %d\n", s.correct, len(s.questions))
		fmt.Fprintf(&b, "Score:   %s\n\n", selectedStyle.Render(fmt.Sprint(s.score)))
		b.WriteString(dimStyle.Render(s.saveMsg))
		return boxStyle.Width(w).Render(b.String())
	}

	q := s.questions[s.idx]
	timer := fmt.Sprintf("⏱ %2ds", int(s.left.Round(time.Second)/time.Second))
	if s.left <= 5*time.Second {
		timer = badStyle.Render(timer)
	} else {
		timer = selectedStyle.Render(timer)
	}
	fmt.Fprintf(&b, "%s   %s   %s\n\n",
		headingStyle.Render(fmt.Sprintf("Question %d/%d", s.idx+1, len(s.questions))),
		dimStyle.Render(fmt.Sprintf("score %d", s.score)), timer)
	b.WriteString(q.Q + "\n\n")

	for i, c := range q.Choices {
		label := fmt.Sprintf("%c) %s", 'a'+i, c)
		switch {
		case s.state == triviaReveal && i == q.Answer:
			b.WriteString(goodStyle.Render("✔ " + label))
		case s.state == triviaReveal && i == s.picked:
			b.WriteString(badStyle.Render("✘ " + label))
		case s.state == triviaAsking && i == s.cursor:
			b.WriteString(selectedStyle.Render("▸ " + label))
		default:
			b.WriteString("  " + label)
		}
		b.WriteString("\n")
	}

	if s.state == triviaReveal {
		b.WriteString("\n")
		switch {
		case s.picked == -1:
			b.WriteString(badStyle.Render("Time's up!"))
		case s.picked == q.Answer:
			b.WriteString(goodStyle.Render(fmt.Sprintf("Correct! +%d", points(true, s.left))))
		default:
			b.WriteString(badStyle.Render("Nope."))
		}
	}
	return boxStyle.Width(w).Render(strings.TrimRight(b.String(), "\n"))
}

func (s *triviaScreen) Help() string {
	switch s.state {
	case triviaReveal:
		return "enter next question · esc menu"
	case triviaDone:
		return "r play again · esc menu"
	}
	return "↑/↓ move · enter answer · a-d / 1-4 quick answer · esc menu"
}
