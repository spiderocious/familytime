// Package constants holds the stable, machine-readable identities shared by
// every feature. Never an inline string at a callsite.
package constants

import "net/http"

// ErrorCode is the client contract: clients switch on these and on nothing
// else. Renaming one is a breaking change.
type ErrorCode string

const (
	// Request issues
	CodeValidation    ErrorCode = "validation_error"
	CodeMalformedJSON ErrorCode = "malformed_json"
	CodeRateLimited   ErrorCode = "rate_limited"

	// Resources
	CodeNotFound     ErrorCode = "not_found"
	CodeGameNotFound ErrorCode = "game_not_found"

	// Rooms
	CodeRoomNotFound ErrorCode = "room_not_found"
	CodeRoomClosed   ErrorCode = "room_closed"
	CodeRoomFull     ErrorCode = "room_full"
	CodeRoomPrivate  ErrorCode = "room_private"
	CodeNameTaken    ErrorCode = "name_taken"
	CodeInvalidToken ErrorCode = "invalid_token"
	CodeNotHost      ErrorCode = "not_host"

	// Game lifecycle
	CodeNotEnoughPlayers   ErrorCode = "not_enough_players"
	CodeNoQuestions        ErrorCode = "no_questions"
	CodeGameAlreadyRunning ErrorCode = "game_already_running"
	CodeNoActiveGame       ErrorCode = "no_active_game"
	CodeInvalidAction      ErrorCode = "invalid_action"

	// The host platform (Lagos Life)
	CodePlatformUnavailable ErrorCode = "platform_unavailable"
	CodeSessionInvalid      ErrorCode = "session_invalid"
	CodeMatchNotReady       ErrorCode = "match_not_ready"
	CodeNotInMatch          ErrorCode = "not_in_match"

	// Purchases and gems
	CodePurchaseNotFound ErrorCode = "purchase_not_found"
	CodePurchaseRefunded ErrorCode = "purchase_refunded"
	CodeNotEnoughGems    ErrorCode = "not_enough_gems"
	CodeBadSignature     ErrorCode = "bad_signature"

	// Server
	CodeInternal ErrorCode = "internal"
)

// Severity bands answer one question: should this page someone? They are coarse
// by design and can never identify an error. Clients must not branch on these.
type Severity int

const (
	SeverityBodyValidation       Severity = 10
	SeveritySuspiciousValidation Severity = 20
	SeverityAuth                 Severity = 30
	SeverityForbidden            Severity = 40
	SeverityNotFound             Severity = 50
	SeverityConflict             Severity = 60
	SeverityBusinessRule         Severity = 70
	SeverityRateLimited          Severity = 80
	SeverityUpstream             Severity = 90
	SeverityServerFault          Severity = 100
)

var severityByCode = map[ErrorCode]Severity{
	CodeValidation:    SeverityBodyValidation,
	CodeMalformedJSON: SeverityBodyValidation,
	CodeRateLimited:   SeverityRateLimited,

	CodeNotFound:     SeverityNotFound,
	CodeGameNotFound: SeverityNotFound,
	CodeRoomNotFound: SeverityNotFound,

	CodeRoomClosed: SeverityConflict,
	CodeRoomFull:   SeverityConflict,
	CodeNameTaken:  SeverityConflict,

	CodeInvalidToken:   SeverityAuth,
	CodeSessionInvalid: SeverityAuth,
	CodeRoomPrivate:    SeverityForbidden,
	CodeNotInMatch:     SeverityForbidden,
	CodeMatchNotReady:  SeverityConflict,
	// The platform being down is an upstream fault, not the player's.
	CodePlatformUnavailable: SeverityUpstream,
	CodeNotHost:             SeverityForbidden,

	CodePurchaseNotFound: SeverityNotFound,
	CodePurchaseRefunded: SeverityConflict,
	CodeNotEnoughGems:    SeverityBusinessRule,
	// Nobody but the platform should be calling the webhook at all.
	CodeBadSignature: SeveritySuspiciousValidation,

	CodeNotEnoughPlayers:   SeverityBusinessRule,
	CodeNoQuestions:        SeverityBusinessRule,
	CodeGameAlreadyRunning: SeverityConflict,
	CodeNoActiveGame:       SeverityConflict,
	// A well-behaved client never sends a malformed game action.
	CodeInvalidAction: SeveritySuspiciousValidation,

	CodeInternal: SeverityServerFault,
}

func SeverityFor(code ErrorCode) Severity {
	if s, ok := severityByCode[code]; ok {
		return s
	}
	return SeverityServerFault
}

// HTTP statuses, named so no handler writes a bare number.
const (
	StatusOK            = http.StatusOK
	StatusCreated       = http.StatusCreated
	StatusNoContent     = http.StatusNoContent
	StatusBadRequest    = http.StatusBadRequest
	StatusUnauthorized  = http.StatusUnauthorized
	StatusForbidden     = http.StatusForbidden
	StatusNotFound      = http.StatusNotFound
	StatusConflict      = http.StatusConflict
	StatusGone          = http.StatusGone
	StatusUnprocessable = http.StatusUnprocessableEntity
	StatusTooMany       = http.StatusTooManyRequests
	StatusInternal      = http.StatusInternalServerError
	StatusBadGateway    = http.StatusBadGateway
	StatusUnavailable   = http.StatusServiceUnavailable
)
