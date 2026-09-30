package app

import "testing"

func newTestSnake() *snakeScreen {
	s := newSnake(&Deps{}, &Session{Guest: true})
	s.Init()
	return s
}

func TestSnakeEatsAndGrows(t *testing.T) {
	s := newTestSnake()
	head := s.body[0]
	s.food = point{head.x + 1, head.y}
	if !s.step() {
		t.Fatal("snake died eating")
	}
	if s.apples != 1 || len(s.body) != 4 || s.score() != 10 {
		t.Fatalf("apples=%d len=%d score=%d", s.apples, len(s.body), s.score())
	}
}

func TestSnakeDiesAtWall(t *testing.T) {
	s := newTestSnake()
	s.food = point{0, 0}
	for range snakeCols {
		if !s.step() {
			return
		}
	}
	t.Fatal("snake went through the wall")
}

func TestSnakeIgnoresReverse(t *testing.T) {
	s := newTestSnake()
	s.turn(dirLeft)
	if s.next != dirRight {
		t.Fatal("reversal accepted")
	}
	s.turn(dirUp)
	s.turn(dirLeft) // still a reversal: the snake hasn't stepped up yet
	if s.next != dirUp {
		t.Fatalf("next = %v", s.next)
	}
}

func TestSnakeCanChaseTail(t *testing.T) {
	s := newTestSnake()
	// A 2x2 loop: the head moves into the cell the tail is leaving.
	s.body = []point{{1, 1}, {1, 2}, {2, 2}, {2, 1}}
	s.dir, s.next = dirUp, dirRight
	s.food = point{10, 10}
	if !s.step() {
		t.Fatal("moving into the vacating tail cell should be allowed")
	}
}
