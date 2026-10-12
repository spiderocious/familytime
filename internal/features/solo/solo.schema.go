package solo

import (
	"encoding/json"

	"familytime/backend/internal/features/rooms"
)

// StartBody is a standalone solo start: the same name and avatar a player
// gives when joining a party game, plus their choice of settings.
type StartBody struct {
	rooms.JoinRoomBody
	Config json.RawMessage `json:"config"`
}
