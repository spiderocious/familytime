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

// ProductView is a gem pack on sale. Its price is the platform's to show.
type ProductView struct {
	ID   string `json:"id"`
	Gems int    `json:"gems"`
}

type WalletView struct {
	Gems     int           `json:"gems"`
	Products []ProductView `json:"products"`
}

// ClaimView is the balance after a purchase was settled.
type ClaimView struct {
	Gems int `json:"gems"`
	// False when this purchase had already been turned into gems.
	Credited bool `json:"credited"`
	Added    int  `json:"added"`
}

// RevealView is the right answer a player paid to see, and what it cost.
type RevealView struct {
	AnswerIdx int `json:"answer_idx"`
	Cost      int `json:"cost"`
	Gems      int `json:"gems"`
}
