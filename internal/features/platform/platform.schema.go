package platform

import (
	"encoding/json"
	"strings"

	"familytime/backend/internal/shared/messages"
)

// SessionBody carries the platform session token. It is the only credential a
// platform player has, so every platform endpoint takes one.
type SessionBody struct {
	Session string          `json:"session"`
	Config  json.RawMessage `json:"config"`
}

func (b *SessionBody) Validate() map[string][]string {
	fieldErrors := map[string][]string{}
	b.Session = strings.TrimSpace(b.Session)
	if b.Session == "" {
		fieldErrors["session"] = []string{messages.Resolve(messages.FieldSessionMissing)}
	}
	return fieldErrors
}
