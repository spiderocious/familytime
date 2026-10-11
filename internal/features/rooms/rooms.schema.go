package rooms

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"familytime/backend/internal/shared/messages"
)

// Request validation. Each schema reports every invalid field at once; the
// policy is the same here and in the error middleware.

type CreateRoomBody struct {
	GameID string          `json:"game_id"`
	Config json.RawMessage `json:"config"`
}

func (b *CreateRoomBody) Validate() map[string][]string {
	fieldErrors := map[string][]string{}
	b.GameID = strings.TrimSpace(b.GameID)
	if b.GameID == "" {
		fieldErrors["game_id"] = []string{messages.Resolve(messages.FieldGameRequired)}
	}
	return fieldErrors
}

type JoinRoomBody struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

func (b *JoinRoomBody) Validate() map[string][]string {
	fieldErrors := map[string][]string{}

	// Collapse runs of whitespace: "  Mum   " and "Mum" are the same name.
	b.Name = strings.Join(strings.Fields(b.Name), " ")
	if b.Name == "" {
		fieldErrors["name"] = []string{messages.Resolve(messages.FieldNameRequired)}
	} else if utf8.RuneCountInString(b.Name) > maxNameLength {
		fieldErrors["name"] = []string{messages.Resolve(messages.FieldNameTooLong)}
	}

	if b.Avatar == "" {
		b.Avatar = Avatars[0]
	} else if !slices.Contains(Avatars, b.Avatar) {
		fieldErrors["avatar"] = []string{messages.Resolve(messages.FieldAvatarUnknown)}
	}
	return fieldErrors
}
