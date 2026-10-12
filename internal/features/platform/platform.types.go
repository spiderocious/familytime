// Package platform is everything that exists because the game is hosted
// inside Lagos Life: players arrive already identified by a platform session,
// and duels are platform matches whose result we must report back.
package platform

import "familytime/backend/internal/features/solo"

// StatusView tells a client how the server is connected to the platform.
type StatusView struct {
	// True when real platform credentials are configured.
	Configured bool `json:"configured"`
	// True when the server is accepting stand-in "dev:" sessions instead. Only
	// ever true outside production.
	DevMode bool `json:"dev_mode"`
}

// SeatView is a place in a game, handed to a player the platform vouched for.
type SeatView struct {
	solo.StartedView
	// The platform mode the session came from: test, live or dev.
	Mode string `json:"mode"`
}
