// Package engine is the game-agnostic core: rooms, the runtime that drives a
// game inside a room, and the contract every game implements.
//
// The one rule: a game owns no clocks, no sockets, no persistence. It declares
// intent as Effects and the room executes them. That keeps a game testable with
// a fake clock and identical however it is transported.
//
// Calls only go down: transport → room → game. The engine imports no feature
// and no transport; both are injected.
package engine

import (
	"encoding/json"

	"familytime/backend/internal/lib/result"
)

// EpochMs is an absolute time in milliseconds. Deadlines are always absolute,
// never durations, so every device counts down to the same instant.
type EpochMs = int64

type PlayerRef struct {
	ID     string
	Name   string
	Avatar string
}

// RosterEntry is a player plus whether they currently hold a live connection.
type RosterEntry struct {
	Player    PlayerRef
	Connected bool
}

type AudienceKind string

const (
	AudienceHost   AudienceKind = "host"
	AudiencePlayer AudienceKind = "player"
)

// Audience is who a view is projected for. The same state renders differently
// per audience, and secrets (the right answer) are withheld server-side.
type Audience struct {
	Kind     AudienceKind
	PlayerID string
}

type EffectKind string

const (
	EffectBroadcast  EffectKind = "broadcast"
	EffectToPlayer   EffectKind = "to_player"
	EffectStartTimer EffectKind = "start_timer"
	EffectClearTimer EffectKind = "clear_timer"
	EffectGameEnded  EffectKind = "game_ended"
)

// Effect is the only way a game touches the world.
type Effect struct {
	Kind     EffectKind
	PlayerID string  // EffectToPlayer
	Key      string  // timers
	FireAt   EpochMs // EffectStartTimer — an absolute deadline
}

func Broadcast() Effect               { return Effect{Kind: EffectBroadcast} }
func ToPlayer(playerID string) Effect { return Effect{Kind: EffectToPlayer, PlayerID: playerID} }
func ClearTimer(key string) Effect    { return Effect{Kind: EffectClearTimer, Key: key} }
func GameEnded() Effect               { return Effect{Kind: EffectGameEnded} }
func StartTimer(key string, fireAt EpochMs) Effect {
	return Effect{Kind: EffectStartTimer, Key: key, FireAt: fireAt}
}

// Fact is one public, read-only line about a game's settings, shown in the lobby.
type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Config is a game's validated settings. It marshals to JSON as itself.
type Config interface {
	Facts() []Fact
}

// View is a game's projection for one audience. It marshals to JSON as itself.
type View interface {
	GameID() string
}

// Options is what a game's setup screen needs to offer choices. It marshals to
// JSON as itself.
type Options interface {
	GameID() string
}

// Action is a message from a client for the running game. The game validates
// the payload; the engine treats it as opaque.
type Action struct {
	Type    string
	Payload json.RawMessage
}

type ActionCtx struct {
	Role AudienceKind
	// IsHost is true for the host screen AND for a player who owns the room
	// (a solo game has no host screen: the one player runs it). Role stays
	// "player" for an owner, because an owner still answers questions.
	IsHost bool
	Actor  PlayerRef // zero for the host
	Now    EpochMs
}

type InitInput struct {
	Config Config
	Roster []RosterEntry
	Seed   int64
	Now    EpochMs
}

// SummaryPlayer is one player's final line, in a shape every game can fill.
type SummaryPlayer struct {
	PlayerID    string
	Name        string
	Avatar      string
	Rank        int
	Score       int
	Correct     int
	Answered    int
	AvgAnswerMs int
	BestStreak  int
}

type Summary struct {
	TotalQuestions int
	RoundsPlayed   int
	Players        []SummaryPlayer
}

type Manifest struct {
	ID         string
	Title      string
	Tagline    string
	Emoji      string
	MinPlayers int
	MaxPlayers int
}

// Game is the static side of a game: identity, settings, and the factory.
type Game interface {
	Manifest() Manifest
	DefaultConfig() Config
	Options() Options
	// ParseConfig validates raw settings and applies defaults. Field errors are
	// keyed by field name, ready for the error envelope.
	ParseConfig(raw json.RawMessage) (Config, map[string][]string)
	// CanStart is the last check before a game begins (is there content?).
	CanStart(config Config) *result.Failure
	New(input InitInput) (Instance, []Effect)
}

// Instance is one running game. Every method is called with the room's lock
// held, so an instance needs no locking of its own. `now` always comes in as
// an argument; an instance never reads the clock.
type Instance interface {
	// OnAction returns a failure only for a malformed or forbidden action. A
	// legal-but-late action (answering twice) is a silent no-op.
	OnAction(action Action, ctx ActionCtx) ([]Effect, *result.Failure)
	OnTimer(key string, now EpochMs) []Effect
	OnRoster(roster []RosterEntry, now EpochMs) []Effect
	View(audience Audience) View
	IsOver() bool
	Summary() Summary
}
