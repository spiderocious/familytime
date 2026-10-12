package engine

import "familytime/backend/internal/lib/response"

// The realtime protocol. Message names are named constants; a type not listed
// under the room-level names below is forwarded to the running game.

// Client → server, handled by the room itself.
const (
	ClientHostConfigure   = "host.configure"
	ClientHostStart       = "host.start"
	ClientHostPlayAgain   = "host.play_again"
	ClientHostBackToLobby = "host.back_to_lobby"
	ClientHostCloseRoom   = "host.close_room"
	ClientPlayerReact     = "player.react"
)

// Server → client.
const (
	ServerState    = "server.state"
	ServerError    = "server.error"
	ServerReaction = "server.reaction"
	ServerClosed   = "server.closed"
)

type RoomPhase string

const (
	PhaseLobby   RoomPhase = "lobby"
	PhasePlaying RoomPhase = "playing"
	PhaseClosed  RoomPhase = "closed"
)

// Envelope is every server → client message. Exactly one payload is set,
// matching Type.
type Envelope struct {
	Type     string              `json:"type"`
	State    *StatePayload       `json:"state,omitempty"`
	Error    *response.ErrorBody `json:"error,omitempty"`
	Reaction *ReactionPayload    `json:"reaction,omitempty"`
}

// StatePayload is the whole truth a client needs to render, sent on every
// change. Sending state rather than deltas is what makes a reconnect trivial:
// the first message after joining is always enough.
type StatePayload struct {
	// The server clock when this was sent. Clients measure their offset from it
	// so a countdown to an absolute deadline agrees across devices.
	Now  EpochMs  `json:"now"`
	Room RoomView `json:"room"`
	You  YouView  `json:"you"`
	Game View     `json:"game"`
}

type YouView struct {
	Role     AudienceKind `json:"role"`
	PlayerID string       `json:"player_id,omitempty"`
}

type RoomView struct {
	Code          string       `json:"code"`
	Phase         RoomPhase    `json:"phase"`
	GameID        string       `json:"game_id"`
	GameTitle     string       `json:"game_title"`
	JoinURL       string       `json:"join_url"`
	HostConnected bool         `json:"host_connected"`
	Players       []PlayerView `json:"players"`
	Config        Config       `json:"config"`
	Facts         []Fact       `json:"facts"`
	// Set only in a lobby whose players start the game themselves: who holds
	// the Start button, and how many must be seated before it works.
	StarterID  string `json:"starter_id,omitempty"`
	MinToStart int    `json:"min_to_start,omitempty"`
}

type PlayerView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Avatar    string `json:"avatar"`
	Connected bool   `json:"connected"`
}

type ReactionPayload struct {
	PlayerID string `json:"player_id"`
	Emoji    string `json:"emoji"`
}

// Reactions a phone may send to the big screen.
var Reactions = []string{"😂", "🔥", "👏", "😱", "🤔", "❤️"}

// RoomInfo is the public, unauthenticated summary of a room.
type RoomInfo struct {
	Code        string
	Phase       RoomPhase
	GameID      string
	GameTitle   string
	PlayerCount int
	MaxPlayers  int
	Joinable    bool
}
