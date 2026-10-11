package engine

import (
	"context"
	"strings"
	"sync"
	"time"

	"familytime/backend/internal/lib/ids"
)

const (
	roomCodeLength = 5
	// A room nobody is connected to is removed after this long.
	roomIdleMs  = 3 * 60 * 60 * 1000
	sweepPeriod = 5 * time.Minute
)

type RegistryOptions struct {
	Now        func() EpochMs
	JoinURL    func(code string) string
	OnFinished func(FinishedGame)
}

// Registry holds the live rooms and the games that can be played in them.
// Rooms live in memory: a family game night does not outlive the process, and
// keeping them here is what makes every move instant.
type Registry struct {
	mu    sync.RWMutex
	rooms map[string]*Room
	games []Game
	opts  RegistryOptions
}

func NewRegistry(opts RegistryOptions) *Registry {
	if opts.Now == nil {
		opts.Now = func() EpochMs { return time.Now().UnixMilli() }
	}
	if opts.JoinURL == nil {
		opts.JoinURL = func(code string) string { return "/join/" + code }
	}
	return &Registry{rooms: map[string]*Room{}, opts: opts}
}

// RegisterGame adds a game. Order of registration is the order hosts see.
func (g *Registry) RegisterGame(game Game) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.games = append(g.games, game)
}

func (g *Registry) Games() []Game {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Game, len(g.games))
	copy(out, g.games)
	return out
}

// Game looks a game up by id. An unknown id simply misses.
func (g *Registry) Game(id string) (Game, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, game := range g.games {
		if game.Manifest().ID == id {
			return game, true
		}
	}
	return nil, false
}

func (g *Registry) Create(game Game, config Config) *Room {
	g.mu.Lock()
	defer g.mu.Unlock()

	code := ids.RoomCode(roomCodeLength)
	for g.rooms[code] != nil {
		code = ids.RoomCode(roomCodeLength)
	}
	room := newRoom(code, game, config, deps{
		now:        g.opts.Now,
		joinURL:    g.opts.JoinURL,
		onFinished: g.opts.OnFinished,
		onClosed:   g.remove,
	})
	g.rooms[code] = room
	return room
}

// Get is case-insensitive: codes are typed on phones.
func (g *Registry) Get(code string) (*Room, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	room, ok := g.rooms[strings.ToUpper(strings.TrimSpace(code))]
	return room, ok
}

func (g *Registry) Count() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.rooms)
}

func (g *Registry) remove(code string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.rooms, code)
}

// StartSweeper removes abandoned rooms until the context ends.
func (g *Registry) StartSweeper(ctx context.Context) {
	ticker := time.NewTicker(sweepPeriod)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				g.sweep()
			}
		}
	}()
}

func (g *Registry) sweep() {
	now := g.opts.Now()
	g.mu.RLock()
	stale := []*Room{}
	for _, room := range g.rooms {
		last, connected := room.idleSince()
		if !connected && now-last > roomIdleMs {
			stale = append(stale, room)
		}
	}
	g.mu.RUnlock()

	for _, room := range stale {
		room.mu.Lock()
		room.close()
		room.mu.Unlock()
	}
}
