package wallet

import (
	"context"
	"sync"
)

// Memory is a Store that forgets on restart. It backs tests, and a server
// started without a database-backed store.
type Memory struct {
	mu        sync.Mutex
	gems      map[string]int
	purchases map[string]bool
}

func NewMemory() *Memory {
	return &Memory{gems: map[string]int{}, purchases: map[string]bool{}}
}

func (m *Memory) Balance(_ context.Context, playerID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gems[playerID], nil
}

func (m *Memory) Credit(_ context.Context, purchase Purchase) (bool, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.purchases[purchase.ID] {
		return false, m.gems[purchase.PlayerID], nil
	}
	m.purchases[purchase.ID] = true
	m.gems[purchase.PlayerID] += purchase.Gems
	return true, m.gems[purchase.PlayerID], nil
}

func (m *Memory) Spend(_ context.Context, playerID string, gems int) (bool, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gems[playerID] < gems {
		return false, m.gems[playerID], nil
	}
	m.gems[playerID] -= gems
	return true, m.gems[playerID], nil
}

func (m *Memory) Refund(_ context.Context, playerID string, gems int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gems[playerID] += gems
	return m.gems[playerID], nil
}
