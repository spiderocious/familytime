package rooms

import (
	"encoding/json"

	"familytime/backend/internal/engine"
	"familytime/backend/internal/lib/result"
	"familytime/backend/internal/shared/constants"
	"familytime/backend/internal/shared/messages"
)

// Registry is the part of the engine the rooms service drives.
type Registry interface {
	Game(id string) (engine.Game, bool)
	Create(game engine.Game, config engine.Config) *engine.Room
	Get(code string) (*engine.Room, bool)
}

// Service holds room business logic. It returns a Result and never sees the
// HTTP request.
type Service struct {
	registry Registry
	joinURL  func(code string) string
}

func NewService(registry Registry, joinURL func(code string) string) *Service {
	return &Service{registry: registry, joinURL: joinURL}
}

func roomNotFound[T any]() result.Result[T] {
	return result.Fail[T](constants.CodeRoomNotFound, messages.RoomsNotFound, constants.StatusNotFound)
}

func (s *Service) Create(gameID string, rawConfig json.RawMessage) result.Result[CreatedRoomView] {
	game, ok := s.registry.Game(gameID)
	if !ok {
		return result.Fail[CreatedRoomView](constants.CodeGameNotFound, messages.GamesNotFound, constants.StatusNotFound)
	}

	config, fieldErrors := game.ParseConfig(rawConfig)
	if len(fieldErrors) > 0 {
		return result.Invalid[CreatedRoomView](fieldErrors)
	}

	room := s.registry.Create(game, config)
	return result.Ok(CreatedRoomView{
		Code:      room.Code(),
		HostToken: room.HostToken(),
		JoinURL:   s.joinURL(room.Code()),
		GameID:    gameID,
	})
}

func (s *Service) Info(code string) result.Result[RoomInfoView] {
	room, ok := s.registry.Get(code)
	if !ok {
		return roomNotFound[RoomInfoView]()
	}
	return result.Ok(toInfoView(room.Info()))
}

func (s *Service) Join(code, name, avatar string) result.Result[JoinedView] {
	room, ok := s.registry.Get(code)
	if !ok {
		return roomNotFound[JoinedView]()
	}

	player, failure := room.AddPlayer(name, avatar)
	if failure != nil {
		return result.FailWith[JoinedView](failure)
	}

	return result.Ok(JoinedView{
		Code:        room.Code(),
		PlayerID:    player.ID,
		PlayerToken: player.Token,
		Name:        player.Name,
		Avatar:      player.Avatar,
	})
}
