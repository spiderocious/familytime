// Package solo starts one-player games: a private room that the player both
// owns and plays in, with no host screen.
package solo

// StartedView is what a client needs to open its socket and play.
type StartedView struct {
	Code        string `json:"code"`
	PlayerID    string `json:"player_id"`
	PlayerToken string `json:"player_token"`
	Name        string `json:"name"`
	Avatar      string `json:"avatar"`
}
