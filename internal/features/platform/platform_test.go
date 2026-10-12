package platform

import (
	"testing"

	"familytime/backend/internal/engine"
	"familytime/backend/internal/lib/vatar"
)

func TestDecideWinner(t *testing.T) {
	ids := map[string]string{"pl_a": "p_ada", "pl_b": "p_bem", "pl_c": "p_cy"}
	player := func(id string, score int) engine.SummaryPlayer {
		return engine.SummaryPlayer{PlayerID: id, Score: score}
	}

	cases := []struct {
		name    string
		players []engine.SummaryPlayer
		want    string
	}{
		{"clear winner", []engine.SummaryPlayer{player("pl_a", 900), player("pl_b", 400)}, "p_ada"},
		{"tie for first refunds everyone", []engine.SummaryPlayer{player("pl_a", 900), player("pl_b", 900), player("pl_c", 100)}, vatar.WinnerDraw},
		{"nobody scored is a draw, not a win", []engine.SummaryPlayer{player("pl_a", 0), player("pl_b", 0)}, vatar.WinnerDraw},
		{"a tie lower down does not matter", []engine.SummaryPlayer{player("pl_c", 900), player("pl_a", 300), player("pl_b", 300)}, "p_cy"},
		{"the only player to turn up wins", []engine.SummaryPlayer{player("pl_b", 0)}, "p_bem"},
		{"no players at all", nil, vatar.WinnerDraw},
		{"an unknown seat is never paid", []engine.SummaryPlayer{player("pl_x", 900), player("pl_a", 1)}, vatar.WinnerDraw},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideWinner(engine.Summary{Players: tc.players}, ids); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDisplayNameAndAvatar(t *testing.T) {
	if got := displayName("   "); got != "Player" {
		t.Fatalf("blank name = %q", got)
	}
	if got := displayName("A very long platform display name"); len([]rune(got)) != maxNameRunes {
		t.Fatalf("long name = %q", got)
	}
	if avatarFor("p_1") != avatarFor("p_1") {
		t.Fatal("a player must keep the same avatar")
	}
}
