// Package health serves the probes. They are infrastructure, not API surface,
// so they are unversioned.
package health

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"familytime/backend/internal/lib/response"
)

type Pinger interface {
	Ping(ctx context.Context) bool
}

type BankInfo interface {
	Total() int
	LoadedAt() time.Time
}

type liveView struct {
	Status string `json:"status"`
}

type readyView struct {
	Status          string `json:"status"`
	Database        bool   `json:"database"`
	QuestionsCached int    `json:"questions_cached"`
	CacheLoadedAt   string `json:"cache_loaded_at"`
}

const (
	statusOK       = "ok"
	statusDegraded = "degraded"
)

func Register(app *gin.Engine, db Pinger, bank BankInfo) {
	app.GET("/health", func(c *gin.Context) {
		response.OK(c, liveView{Status: statusOK})
	})

	app.GET("/health/ready", func(c *gin.Context) {
		view := readyView{
			Status:          statusOK,
			Database:        db.Ping(c.Request.Context()),
			QuestionsCached: bank.Total(),
			CacheLoadedAt:   bank.LoadedAt().UTC().Format(time.RFC3339),
		}
		if !view.Database || view.QuestionsCached == 0 {
			view.Status = statusDegraded
		}
		response.OK(c, view)
	})
}
