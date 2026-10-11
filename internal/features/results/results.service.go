package results

import (
	"context"
	"encoding/json"
	"time"

	"familytime/backend/internal/engine"
	"familytime/backend/internal/lib/ids"
	"familytime/backend/internal/lib/logger"
	"familytime/backend/internal/lib/result"
)

type Store interface {
	Insert(ctx context.Context, result GameResult) error
	ListBefore(ctx context.Context, cursor string, limit int) ([]GameResult, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service { return &Service{store: store} }

// Record is the engine's finished-game hook. It runs after the podium is
// already on screen, so a slow or failed write costs the players nothing — it
// is logged and dropped rather than retried into the game.
func (s *Service) Record(finished engine.FinishedGame) {
	settings, err := json.Marshal(finished.Config)
	if err != nil {
		settings = []byte("{}")
	}

	record := GameResult{
		ID:             ids.New(ids.Result),
		RoomCode:       finished.RoomCode,
		GameID:         finished.GameID,
		InstanceID:     finished.InstanceID,
		Settings:       string(settings),
		TotalQuestions: finished.Summary.TotalQuestions,
		RoundsPlayed:   finished.Summary.RoundsPlayed,
		Players:        []PlayerRow{},
		StartedAt:      time.UnixMilli(finished.StartedAt),
		EndedAt:        time.UnixMilli(finished.EndedAt),
	}
	for _, p := range finished.Summary.Players {
		record.Players = append(record.Players, PlayerRow{
			PlayerID: p.PlayerID, Name: p.Name, Avatar: p.Avatar, Rank: p.Rank, Score: p.Score,
			Correct: p.Correct, Answered: p.Answered, AvgAnswerMs: p.AvgAnswerMs, BestStreak: p.BestStreak,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.store.Insert(ctx, record); err != nil {
		logger.Error("could not record finished game", "room_code", finished.RoomCode, "error", err.Error())
		return
	}
	logger.Info("finished game recorded", "result_id", record.ID, "room_code", finished.RoomCode, "players", len(record.Players))
}

// List returns one page, newest first. A real storage failure is returned as
// an error: that is the unexpected, not a domain outcome.
func (s *Service) List(ctx context.Context, cursor string, limit int) (result.Result[Page], error) {
	if limit <= 0 {
		limit = defaultPageSize
	}
	limit = min(limit, maxPageSize)

	// One extra row answers "is there more?" without a second query.
	rows, err := s.store.ListBefore(ctx, cursor, limit+1)
	if err != nil {
		return result.Result[Page]{}, err
	}

	page := Page{Items: []GameResultView{}}
	if len(rows) > limit {
		page.HasMore = true
		rows = rows[:limit]
		next := rows[len(rows)-1].ID
		page.NextCursor = &next
	}
	for _, row := range rows {
		page.Items = append(page.Items, toView(row))
	}
	return result.Ok(page), nil
}
