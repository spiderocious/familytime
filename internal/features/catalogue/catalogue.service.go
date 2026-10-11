package catalogue

import (
	"familytime/backend/internal/engine"
	"familytime/backend/internal/lib/result"
	"familytime/backend/internal/shared/constants"
	"familytime/backend/internal/shared/messages"
)

// Games is the part of the engine registry the catalogue reads.
type Games interface {
	Games() []engine.Game
	Game(id string) (engine.Game, bool)
}

type Service struct {
	games Games
}

func NewService(games Games) *Service { return &Service{games: games} }

func (s *Service) List() result.Result[[]GameView] {
	views := []GameView{}
	for _, game := range s.games.Games() {
		views = append(views, toGameView(game))
	}
	return result.Ok(views)
}

func (s *Service) Options(gameID string) result.Result[GameOptionsView] {
	game, ok := s.games.Game(gameID)
	if !ok {
		return result.Fail[GameOptionsView](constants.CodeGameNotFound, messages.GamesNotFound, constants.StatusNotFound)
	}
	return result.Ok(GameOptionsView{Game: toGameView(game), Options: game.Options()})
}
