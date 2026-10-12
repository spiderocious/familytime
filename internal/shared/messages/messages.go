// Package messages is the registry every human-readable response string
// resolves from. Copy is reviewable in one place; nothing is written inline.
package messages

import "familytime/backend/internal/shared/constants"

type Key string

const (
	CommonValidation    Key = "common.validation_error"
	CommonMalformedJSON Key = "common.malformed_json"
	CommonNotFound      Key = "common.not_found"
	CommonInternal      Key = "common.internal"
	CommonRateLimited   Key = "common.rate_limited"

	GamesNotFound Key = "games.not_found"

	RoomsNotFound Key = "rooms.not_found"
	RoomsClosed   Key = "rooms.closed"
	RoomsFull     Key = "rooms.full"
	RoomsPrivate  Key = "rooms.private"

	PlatformUnavailable Key = "platform.unavailable"
	PlatformSession     Key = "platform.session_invalid"
	PlatformMatchWait   Key = "platform.match_not_ready"
	PlatformNotInMatch  Key = "platform.not_in_match"
	FieldSessionMissing Key = "field.session_required"
	RoomsNameTaken      Key = "rooms.name_taken"
	RoomsInvalidToken   Key = "rooms.invalid_token"
	RoomsNotHost        Key = "rooms.not_host"

	PlayNotEnoughPlayers Key = "play.not_enough_players"
	PlayNoQuestions      Key = "play.no_questions"
	PlayAlreadyRunning   Key = "play.already_running"
	PlayNoActiveGame     Key = "play.no_active_game"
	PlayInvalidAction    Key = "play.invalid_action"

	// Field-level validation copy.
	FieldNameRequired      Key = "field.name_required"
	FieldNameTooLong       Key = "field.name_too_long"
	FieldAvatarUnknown     Key = "field.avatar_unknown"
	FieldGameRequired      Key = "field.game_required"
	FieldDifficultyUnknown Key = "field.difficulty_unknown"
	FieldCategoryUnknown   Key = "field.category_unknown"
	FieldRoundsRange       Key = "field.rounds_range"
	FieldPerRoundRange     Key = "field.per_round_range"
	FieldSecondsRange      Key = "field.seconds_range"
	FieldPacingUnknown     Key = "field.pacing_unknown"
	FieldConfigMalformed   Key = "field.config_malformed"
)

var catalog = map[Key]string{
	CommonValidation:    "Some of the details you sent are not valid.",
	CommonMalformedJSON: "That request could not be read. Check the JSON and try again.",
	CommonNotFound:      "We could not find that.",
	CommonInternal:      "Something went wrong on our side. Please try again.",
	CommonRateLimited:   "Slow down a little — too many messages at once.",

	GamesNotFound: "That game does not exist.",

	RoomsNotFound: "No game was found with that code. Check the code on the big screen.",
	RoomsClosed:   "That game has ended.",
	RoomsFull:     "That game is full.",
	RoomsPrivate:  "That game is not open for joining.",

	PlatformUnavailable: "We could not reach Lagos Life just now. Please try again in a moment.",
	PlatformSession:     "Your Lagos Life session has expired. Close the game and open it again.",
	PlatformMatchWait:   "That match has not started yet.",
	PlatformNotInMatch:  "You are not one of the players in that match.",
	FieldSessionMissing: "A session is required.",
	RoomsNameTaken:      "Someone in this game already has that name. Pick another.",
	RoomsInvalidToken:   "Your place in this game could not be confirmed. Join again.",
	RoomsNotHost:        "Only the host can do that.",

	PlayNotEnoughPlayers: "At least one player needs to join before the game can start.",
	PlayNoQuestions:      "There are no questions for those settings. Try more categories or another difficulty.",
	PlayAlreadyRunning:   "A game is already running in this room.",
	PlayNoActiveGame:     "There is no game running right now.",
	PlayInvalidAction:    "That move is not allowed right now.",

	FieldNameRequired:      "Enter a name.",
	FieldNameTooLong:       "Keep the name to 16 characters or fewer.",
	FieldAvatarUnknown:     "Pick one of the avatars shown.",
	FieldGameRequired:      "Choose a game.",
	FieldDifficultyUnknown: "Choose easy, medium, hard or mixed.",
	FieldCategoryUnknown:   "One of those categories does not exist.",
	FieldRoundsRange:       "Rounds must be between 1 and 20.",
	FieldPerRoundRange:     "Questions per round must be between 3 and 30.",
	FieldSecondsRange:      "Seconds per question must be between 5 and 60.",
	FieldPacingUnknown:     "Choose automatic or host-paced.",
	FieldConfigMalformed:   "Those game settings could not be read.",
}

var defaultByCode = map[constants.ErrorCode]Key{
	constants.CodeValidation:          CommonValidation,
	constants.CodeMalformedJSON:       CommonMalformedJSON,
	constants.CodeRateLimited:         CommonRateLimited,
	constants.CodeNotFound:            CommonNotFound,
	constants.CodeGameNotFound:        GamesNotFound,
	constants.CodeRoomNotFound:        RoomsNotFound,
	constants.CodeRoomClosed:          RoomsClosed,
	constants.CodeRoomFull:            RoomsFull,
	constants.CodeRoomPrivate:         RoomsPrivate,
	constants.CodePlatformUnavailable: PlatformUnavailable,
	constants.CodeSessionInvalid:      PlatformSession,
	constants.CodeMatchNotReady:       PlatformMatchWait,
	constants.CodeNotInMatch:          PlatformNotInMatch,
	constants.CodeNameTaken:           RoomsNameTaken,
	constants.CodeInvalidToken:        RoomsInvalidToken,
	constants.CodeNotHost:             RoomsNotHost,
	constants.CodeNotEnoughPlayers:    PlayNotEnoughPlayers,
	constants.CodeNoQuestions:         PlayNoQuestions,
	constants.CodeGameAlreadyRunning:  PlayAlreadyRunning,
	constants.CodeNoActiveGame:        PlayNoActiveGame,
	constants.CodeInvalidAction:       PlayInvalidAction,
	constants.CodeInternal:            CommonInternal,
}

// Resolve returns the text for a key. An unknown key falls back to the generic
// internal message rather than leaking the key itself to a user.
func Resolve(key Key) string {
	if text, ok := catalog[key]; ok {
		return text
	}
	return catalog[CommonInternal]
}

// ForCode resolves the default message for an error identity.
func ForCode(code constants.ErrorCode) string {
	if key, ok := defaultByCode[code]; ok {
		return Resolve(key)
	}
	return catalog[CommonInternal]
}
