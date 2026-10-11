// Package ids mints resource-prefixed identifiers. The prefix makes an id
// self-describing in a log line; clients treat ids as opaque.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type Resource string

const (
	Player   Resource = "pl"
	Game     Resource = "gi"
	Result   Resource = "res"
	Question Resource = "q"
	Request  Resource = "req"
	Conn     Resource = "cn"
)

// New returns "<prefix>_<ms timestamp hex><random hex>": sortable by creation
// time, so it can serve as a cursor.
func New(resource Resource) string {
	return fmt.Sprintf("%s_%011x%s", resource, time.Now().UnixMilli(), randomHex(6))
}

// Token is an unguessable secret: a seat in a room, or the host's control.
func Token() string { return randomHex(24) }

// Unambiguous alphabet: no 0/O, 1/I/L. Codes are read off a TV and typed on a phone.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func RoomCode(length int) string {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		panic(err) // crypto/rand failing means the process cannot continue safely
	}
	out := make([]byte, length)
	for i, b := range buf {
		out[i] = codeAlphabet[int(b)%len(codeAlphabet)]
	}
	return string(out)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}
