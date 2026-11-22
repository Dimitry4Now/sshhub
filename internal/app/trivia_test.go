package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

var testQuestions = []Question{
	{Q: "2 + 2?", Choices: []string{"3", "4", "5"}, Answer: 1},
	{Q: "Capital of France?", Choices: []string{"Paris", "Rome"}, Answer: 0},
	{Q: "Largest planet?", Choices: []string{"Mars", "Venus", "Jupiter", "Earth"}, Answer: 2},
}

func TestRoundShufflesButKeepsAnswers(t *testing.T) {
	for range 20 {
		qs := round(testQuestions, 2)
		if len(qs) != 2 || qs[0].Q == qs[1].Q {
			t.Fatalf("round = %+v", qs)
		}
		for _, q := range qs {
			for _, orig := range testQuestions {
				if orig.Q == q.Q && q.Choices[q.Answer] != orig.Choices[orig.Answer] {
					t.Fatalf("%q: answer moved to %q", q.Q, q.Choices[q.Answer])
				}
			}
		}
	}
	if qs := round(testQuestions, 10); len(qs) != len(testQuestions) {
		t.Fatalf("round longer than the bank: %d", len(qs))
	}
}

func TestPoints(t *testing.T) {
	if points(false, 15*time.Second) != 0 || points(true, 0) != 50 || points(true, 15*time.Second) != 200 {
		t.Fatal("unexpected points")
	}
}

// playRound answers the first question right at once, lets the second run
// out of time and gets the third wrong.
func playRound(t *testing.T, s *triviaScreen) tea.Cmd {
	t.Helper()
	q := s.questions[0]
	s.Update(key(rune('a' + q.Answer)))
	if s.state != triviaReveal || s.score != 200 || !strings.Contains(s.View(80, 24), "Correct! +200") {
		t.Fatalf("after a right answer: state=%v score=%d", s.state, s.score)
	}
	s.Update(key(tea.KeyEnter))

	s.deadline = time.Now().Add(-time.Second)
	s.Update(tickMsg(time.Now()))
	if s.state != triviaReveal || s.picked != -1 || !strings.Contains(s.View(80, 24), "Time's up") {
		t.Fatalf("after the timer ran out: state=%v picked=%d", s.state, s.picked)
	}
	s.Update(key(tea.KeyEnter))

	q = s.questions[2]
	s.Update(key(rune('1' + (q.Answer+1)%len(q.Choices))))
	if s.state != triviaReveal || s.score != 200 || !strings.Contains(s.View(80, 24), "Nope") {
		t.Fatalf("after a wrong answer: state=%v score=%d", s.state, s.score)
	}
	cmd := s.Update(key(tea.KeyEnter))
	if s.state != triviaDone || s.correct != 1 {
		t.Fatalf("round not over: state=%v correct=%d", s.state, s.correct)
	}
	return cmd
}

func TestTriviaRoundSavesScore(t *testing.T) {
	st := newTestStore(t)
	st.Register("SHA256:ada", "ada")
	s := newTrivia(&Deps{Store: st, Questions: testQuestions}, &Session{Fingerprint: "SHA256:ada", Nick: "ada"})
	s.Init()

	s.Update(result(playRound(t, s)))
	if !strings.Contains(s.saveMsg, "saved") {
		t.Fatalf("save message %q", s.saveMsg)
	}
	if top, _ := st.Top(triviaGame, 10); len(top) != 1 || top[0].Best != 200 {
		t.Fatalf("leaderboard %+v", top)
	}

	s.Update(key('r'))
	if s.state != triviaAsking || s.idx != 0 || s.score != 0 {
		t.Fatalf("r didn't start a new round: state=%v idx=%d score=%d", s.state, s.idx, s.score)
	}
}

func TestTriviaGuestScoreIsNotSaved(t *testing.T) {
	s := newTrivia(&Deps{Questions: testQuestions}, &Session{Nick: "bob", Guest: true})
	s.Init()
	if cmd := playRound(t, s); cmd != nil {
		t.Fatal("tried to save a guest's score")
	}
	if !strings.Contains(s.saveMsg, "Guests") {
		t.Fatalf("save message %q", s.saveMsg)
	}
}
