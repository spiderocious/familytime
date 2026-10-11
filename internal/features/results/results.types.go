// Package results keeps a record of every finished game.
package results

import "time"

const collectionName = "game_results"

const (
	defaultPageSize = 20
	maxPageSize     = 50
)

// ---- stored ----

type PlayerRow struct {
	PlayerID    string `bson:"player_id"`
	Name        string `bson:"name"`
	Avatar      string `bson:"avatar"`
	Rank        int    `bson:"rank"`
	Score       int    `bson:"score"`
	Correct     int    `bson:"correct"`
	Answered    int    `bson:"answered"`
	AvgAnswerMs int    `bson:"avg_answer_ms"`
	BestStreak  int    `bson:"best_streak"`
}

type GameResult struct {
	ID             string      `bson:"_id"`
	RoomCode       string      `bson:"room_code"`
	GameID         string      `bson:"game_id"`
	InstanceID     string      `bson:"instance_id"`
	Settings       string      `bson:"settings"` // the config, as JSON
	TotalQuestions int         `bson:"total_questions"`
	RoundsPlayed   int         `bson:"rounds_played"`
	Players        []PlayerRow `bson:"players"`
	StartedAt      time.Time   `bson:"started_at"`
	EndedAt        time.Time   `bson:"ended_at"`
}

// ---- wire (snake_case) ----

type PlayerRowView struct {
	Name        string `json:"name"`
	Avatar      string `json:"avatar"`
	Rank        int    `json:"rank"`
	Score       int    `json:"score"`
	Correct     int    `json:"correct"`
	Answered    int    `json:"answered"`
	AvgAnswerMs int    `json:"avg_answer_ms"`
	BestStreak  int    `json:"best_streak"`
}

type GameResultView struct {
	ID             string          `json:"id"`
	RoomCode       string          `json:"room_code"`
	GameID         string          `json:"game_id"`
	TotalQuestions int             `json:"total_questions"`
	RoundsPlayed   int             `json:"rounds_played"`
	Players        []PlayerRowView `json:"players"`
	StartedAt      string          `json:"started_at"`
	EndedAt        string          `json:"ended_at"`
}

type Page struct {
	Items      []GameResultView
	NextCursor *string
	HasMore    bool
}

func toView(r GameResult) GameResultView {
	players := make([]PlayerRowView, 0, len(r.Players))
	for _, p := range r.Players {
		players = append(players, PlayerRowView{
			Name: p.Name, Avatar: p.Avatar, Rank: p.Rank, Score: p.Score,
			Correct: p.Correct, Answered: p.Answered, AvgAnswerMs: p.AvgAnswerMs, BestStreak: p.BestStreak,
		})
	}
	return GameResultView{
		ID:             r.ID,
		RoomCode:       r.RoomCode,
		GameID:         r.GameID,
		TotalQuestions: r.TotalQuestions,
		RoundsPlayed:   r.RoundsPlayed,
		Players:        players,
		StartedAt:      r.StartedAt.UTC().Format(time.RFC3339),
		EndedAt:        r.EndedAt.UTC().Format(time.RFC3339),
	}
}
