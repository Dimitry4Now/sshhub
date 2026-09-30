package app

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"sshhub/assets"
)

func TestQuestionBank(t *testing.T) {
	qs, err := LoadQuestions(assets.FS, "questions.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) < triviaRound {
		t.Fatalf("only %d questions, a round needs %d", len(qs), triviaRound)
	}
	seen := map[string]bool{}
	for _, q := range qs {
		if seen[q.Q] {
			t.Errorf("duplicate question %q", q.Q)
		}
		seen[q.Q] = true
		if len(q.Choices) > 4 {
			t.Errorf("%q has %d choices; keys only cover a-d", q.Q, len(q.Choices))
		}
		choices := map[string]bool{}
		for _, c := range q.Choices {
			if strings.TrimSpace(c) == "" || choices[c] {
				t.Errorf("%q has an empty or duplicate choice %q", q.Q, c)
			}
			choices[c] = true
		}
	}
}

func TestMemes(t *testing.T) {
	memes, err := LoadMemes(assets.FS, "memes")
	if err != nil {
		t.Fatal(err)
	}
	if len(memes) == 0 {
		t.Fatal("no memes")
	}
	for _, m := range memes {
		// Keep art viewable in an 80x30 terminal inside the gallery frame.
		if w := lipgloss.Width(m.Art); w > 70 {
			t.Errorf("%q is %d columns wide, max 70", m.Title, w)
		}
		if h := lipgloss.Height(m.Art); h > 20 {
			t.Errorf("%q is %d lines tall, max 20", m.Title, h)
		}
	}
}
