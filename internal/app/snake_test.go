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
	s.turn(dirRight) // already going right
	if len(s.turns) != 0 {
		t.Fatalf("turns = %v", s.turns)
	}
	s.turn(dirUp)
	s.turn(dirDown) // reverses the queued up
	if len(s.turns) != 1 || s.turns[0] != dirUp {
		t.Fatalf("turns = %v", s.turns)
	}
}

func TestSnakeQueuesQuickTurns(t *testing.T) {
	s := newTestSnake()
	s.food = point{0, 0}
	head := s.body[0]
	// Up then left within one step: a U-turn over two steps.
	s.turn(dirUp)
	s.turn(dirLeft)
	s.step()
	s.step()
	if want := (point{head.x - 1, head.y - 1}); s.body[0] != want || s.dir != dirLeft {
		t.Fatalf("head = %v dir = %v, want %v going left", s.body[0], s.dir, want)
	}

	for _, d := range []point{dirUp, dirRight, dirDown, dirLeft, dirUp} {
		s.turn(d)
	}
	if len(s.turns) != maxTurns {
		t.Fatalf("queued %d turns, want %d", len(s.turns), maxTurns)
	}
}

func TestSnakeCanChaseTail(t *testing.T) {
	s := newTestSnake()
	// A 2x2 loop: the head moves into the cell the tail is leaving.
	s.body = []point{{1, 1}, {1, 2}, {2, 2}, {2, 1}}
	s.dir, s.turns = dirUp, []point{dirRight}
	s.food = point{10, 10}
	if !s.step() {
		t.Fatal("moving into the vacating tail cell should be allowed")
	}
}
