// Package app builds the configured HTTP application without listening, so a
// test can mount it directly instead of binding a port.
package app

import (
	"github.com/gin-gonic/gin"

	"familytime/backend/internal/engine"
	"familytime/backend/internal/env"
	"familytime/backend/internal/features/catalogue"
	"familytime/backend/internal/features/health"
	"familytime/backend/internal/features/questions"
	"familytime/backend/internal/features/realtime"
	"familytime/backend/internal/features/results"
	"familytime/backend/internal/features/rooms"
	"familytime/backend/internal/games/quiz"
	"familytime/backend/internal/shared/middleware"
)

type Deps struct {
	Env     env.Env
	DB      health.Pinger
	Bank    *questions.Bank
	Results *results.Service
}

type App struct {
	Engine   *gin.Engine
	Registry *engine.Registry
}

func JoinURL(appURL string) func(code string) string {
	return func(code string) string { return appURL + "/join/" + code }
}

// Build wires the application. MIDDLEWARE ORDER IS LOAD-BEARING; each step
// says what depends on it.
func Build(deps Deps) *App {
	if deps.Env.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	joinURL := JoinURL(deps.Env.AppURL)
	registry := engine.NewRegistry(engine.RegistryOptions{
		JoinURL:    joinURL,
		OnFinished: deps.Results.Record,
	})
	// Order of registration is the order hosts see the games in.
	registry.RegisterGame(quiz.NewGame(deps.Bank))

	origins := middleware.NewOriginPolicy(deps.Env.CORSOrigins, !deps.Env.IsProduction())

	router := gin.New()
	// One trusted hop, not an arbitrary chain a client could forge.
	_ = router.SetTrustedProxies(nil)

	// Seeds the request context. Must precede everything that logs.
	router.Use(middleware.RequestID())
	// After request-id, so its lines carry one.
	router.Use(middleware.HTTPLogger())
	// Before the routes, so a preflight never reaches a handler.
	router.Use(middleware.CORS(origins))
	// Wraps every route below: it renders whatever error a handler leaves
	// behind, and recovers a panic into the standard envelope.
	router.Use(middleware.ErrorHandler())

	// Probes first: they must answer even when something below is unhealthy.
	health.Register(router, deps.DB, deps.Bank)

	// Feature registration. The three mount non-overlapping prefixes under
	// /api/v1 (/games, /rooms, /results), so the order between them is not
	// load-bearing today.
	catalogue.Register(router, catalogue.NewService(registry))
	rooms.Register(router, rooms.NewService(registry, joinURL))
	results.Register(router, deps.Results)

	// The realtime gateway. Unversioned and outside /api/v1: it is a socket,
	// not a REST resource.
	realtime.Register(router, registry, origins)

	// Unmatched path → a 404 in the standard envelope, not gin's plain text.
	router.NoRoute(middleware.NotFound)

	return &App{Engine: router, Registry: registry}
}
