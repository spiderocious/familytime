package solo

import (
	"encoding/json"

	"familytime/backend/internal/engine"
	"familytime/backend/internal/games/quiz"
	"familytime/backend/internal/lib/result"
	"familytime/backend/internal/shared/constants"
	"familytime/backend/internal/shared/messages"
)

// Registry is the part of the engine a solo start drives.
type Registry interface {
	Game(id string) (engine.Game, bool)
	Create(game engine.Game, config engine.Config) *engine.Room
}

type Service struct {
	registry Registry
}

func NewService(registry Registry) *Service { return &Service{registry: registry} }

// Start opens a private room for one already-identified player and makes them
// its owner. The caller is responsible for knowing who the player is: a name
// they typed (standalone) or an identity the platform vouched for.
func (s *Service) Start(name, avatar string, rawConfig json.RawMessage) result.Result[StartedView] {
	game, ok := s.registry.Game(quiz.GameID)
	if !ok {
		return result.Fail[StartedView](constants.CodeGameNotFound, messages.GamesNotFound, constants.StatusNotFound)
	}
	chosen, fieldErrors := game.ParseConfig(rawConfig)
	if len(fieldErrors) > 0 {
		return result.Invalid[StartedView](fieldErrors)
	}
	// The player picks difficulty, topics and the clock. The shape of the run
	// is fixed, so every solo score is comparable with every other.
	config := quiz.SoloConfig(chosen)
	if failure := game.CanStart(config); failure != nil {
		return result.FailWith[StartedView](failure)
	}

	room := s.registry.Create(game, config)
	room.MakePrivate()
	player, failure := room.Seat(name, avatar)
	if failure != nil {
		room.Close()
		return result.FailWith[StartedView](failure)
	}
	room.SetOwner(player.ID)

	return result.Ok(StartedView{Code: room.Code(), PlayerID: player.ID, PlayerToken: player.Token, Name: player.Name, Avatar: player.Avatar})
}
