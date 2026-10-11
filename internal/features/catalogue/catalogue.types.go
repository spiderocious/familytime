// Package catalogue lists the games that can be played and what each one's
// setup screen can offer.
package catalogue

import (
	"familytime/backend/internal/engine"
)

type GameView struct {
	ID            string        `json:"id"`
	Title         string        `json:"title"`
	Tagline       string        `json:"tagline"`
	Emoji         string        `json:"emoji"`
	MinPlayers    int           `json:"min_players"`
	MaxPlayers    int           `json:"max_players"`
	DefaultConfig engine.Config `json:"default_config"`
}

type GameOptionsView struct {
	Game    GameView       `json:"game"`
	Options engine.Options `json:"options"`
}

func toGameView(game engine.Game) GameView {
	m := game.Manifest()
	return GameView{
		ID:            m.ID,
		Title:         m.Title,
		Tagline:       m.Tagline,
		Emoji:         m.Emoji,
		MinPlayers:    m.MinPlayers,
		MaxPlayers:    m.MaxPlayers,
		DefaultConfig: game.DefaultConfig(),
	}
}
