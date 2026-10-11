// Package rooms is the HTTP surface for rooms: open one, look one up, take a
// seat. Everything after that happens over the realtime gateway.
package rooms

import "familytime/backend/internal/engine"

// Avatars a player may pick. The value is the emoji itself: it is what every
// screen renders, so there is no id-to-art mapping to drift.
var Avatars = []string{
	"🦁", "🐯", "🦊", "🐼", "🐵", "🦄", "🐸", "🐙",
	"🦉", "🐧", "🐢", "🦖", "🐝", "🦋", "🐳", "🦒",
	"🐘", "🦜", "🐨", "🐰",
}

const (
	maxNameLength = 16
)

// ---- wire types (snake_case) ----

type CreatedRoomView struct {
	Code      string `json:"code"`
	HostToken string `json:"host_token"`
	JoinURL   string `json:"join_url"`
	GameID    string `json:"game_id"`
}

type RoomInfoView struct {
	Code        string           `json:"code"`
	Phase       engine.RoomPhase `json:"phase"`
	GameID      string           `json:"game_id"`
	GameTitle   string           `json:"game_title"`
	PlayerCount int              `json:"player_count"`
	MaxPlayers  int              `json:"max_players"`
	Joinable    bool             `json:"joinable"`
	Avatars     []string         `json:"avatars"`
}

type JoinedView struct {
	Code        string `json:"code"`
	PlayerID    string `json:"player_id"`
	PlayerToken string `json:"player_token"`
	Name        string `json:"name"`
	Avatar      string `json:"avatar"`
}

func toInfoView(info engine.RoomInfo) RoomInfoView {
	return RoomInfoView{
		Code:        info.Code,
		Phase:       info.Phase,
		GameID:      info.GameID,
		GameTitle:   info.GameTitle,
		PlayerCount: info.PlayerCount,
		MaxPlayers:  info.MaxPlayers,
		Joinable:    info.Joinable,
		Avatars:     Avatars,
	}
}
