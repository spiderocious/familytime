package questions

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// Loader is the part of the repo the bank needs. Narrow, so tests fake it.
type Loader interface {
	FindAll(ctx context.Context) ([]Question, error)
}

// Bank is the whole question bank held in memory, loaded once at boot.
//
// Starting a game must not wait on the database: a host presses Start with the
// family watching, and a slow query there is the worst possible moment for
// one. So every question is read up front and indexed by category and
// difficulty, and dealing a deck is slice work — no I/O at all.
//
// The snapshot is immutable. A reload swaps in a new one, and any deck already
// dealt keeps reading the snapshot it was built from.
type Bank struct {
	mu       sync.RWMutex
	snapshot *snapshot
}

type snapshot struct {
	all      []Question
	index    map[string]map[Difficulty][]int
	stats    []CategoryStats
	loadedAt time.Time
}

func NewBank() *Bank {
	return &Bank{snapshot: buildSnapshot(nil)}
}

// Load reads every question and replaces the snapshot.
func (b *Bank) Load(ctx context.Context, loader Loader) error {
	all, err := loader.FindAll(ctx)
	if err != nil {
		return err
	}
	b.Replace(all)
	return nil
}

// Replace swaps the snapshot. Also the seam tests use to supply fixtures.
func (b *Bank) Replace(all []Question) {
	next := buildSnapshot(all)
	b.mu.Lock()
	b.snapshot = next
	b.mu.Unlock()
}

func (b *Bank) current() *snapshot {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.snapshot
}

func buildSnapshot(all []Question) *snapshot {
	s := &snapshot{
		all:      all,
		index:    map[string]map[Difficulty][]int{},
		loadedAt: time.Now(),
	}
	for i, q := range all {
		byDifficulty, ok := s.index[q.Category]
		if !ok {
			byDifficulty = map[Difficulty][]int{}
			s.index[q.Category] = byDifficulty
		}
		byDifficulty[q.Difficulty] = append(byDifficulty[q.Difficulty], i)
	}
	// Precomputed: the catalogue endpoint serves this without counting anything.
	for _, category := range Categories {
		byDifficulty := s.index[category.Slug]
		stat := CategoryStats{
			Category: category,
			Easy:     len(byDifficulty[DifficultyEasy]),
			Medium:   len(byDifficulty[DifficultyMedium]),
			Hard:     len(byDifficulty[DifficultyHard]),
		}
		stat.Total = stat.Easy + stat.Medium + stat.Hard
		s.stats = append(s.stats, stat)
	}
	return s
}

func (b *Bank) Total() int { return len(b.current().all) }

func (b *Bank) LoadedAt() time.Time { return b.current().loadedAt }

// Stats returns every category with its counts, in registry order.
func (b *Bank) Stats() []CategoryStats {
	stats := b.current().stats
	out := make([]CategoryStats, len(stats))
	copy(out, stats)
	return out
}

func (s *snapshot) match(filter Filter) []int {
	categories := filter.Categories
	if len(categories) == 0 {
		for _, c := range Categories {
			categories = append(categories, c.Slug)
		}
	}
	matched := []int{}
	for _, slug := range categories {
		byDifficulty := s.index[slug]
		if filter.Difficulty != "" {
			matched = append(matched, byDifficulty[filter.Difficulty]...)
			continue
		}
		for _, d := range Difficulties {
			matched = append(matched, byDifficulty[d]...)
		}
	}
	return matched
}

// Count is how many questions match a filter.
func (b *Bank) Count(filter Filter) int {
	return len(b.current().match(filter))
}

// piles groups a filter's matches by category.
func (s *snapshot) piles(filter Filter) [][]int {
	categories := filter.Categories
	if len(categories) == 0 {
		for _, c := range Categories {
			categories = append(categories, c.Slug)
		}
	}
	piles := [][]int{}
	for _, slug := range categories {
		pile := s.match(Filter{Categories: []string{slug}, Difficulty: filter.Difficulty})
		if len(pile) > 0 {
			piles = append(piles, pile)
		}
	}
	return piles
}

// NewDeck deals a shuffled deck for a filter. The seed makes the order
// reproducible, which is what lets a game be replayed in a test.
func (b *Bank) NewDeck(filter Filter, seed int64) *Deck {
	s := b.current()
	deck := &Deck{
		source: s.all,
		piles:  s.piles(filter),
		rng:    rand.New(rand.NewSource(seed)),
		last:   -1,
	}
	deck.pos = make([]int, len(deck.piles))
	for i := range deck.piles {
		deck.shuffle(i)
	}
	return deck
}

// Deck hands out questions one category at a time.
//
// Each draw first picks a CATEGORY at random, then the next question from that
// category's own shuffled pile. Picking the category first is what keeps a
// mixed game mixed: categories differ in size by orders of magnitude (tens of
// thousands of generated sums beside a hundred history questions), and dealing
// from one big shuffled pile would make a "bit of everything" game almost
// entirely sums.
//
// Within a category no question repeats until every match has been seen; the
// pile then reshuffles and carries on, which is what makes an endless game
// possible on a finite bank.
type Deck struct {
	source []Question
	piles  [][]int
	pos    []int
	rng    *rand.Rand
	last   int
}

func (d *Deck) shuffle(pile int) {
	order := d.piles[pile]
	d.rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	d.pos[pile] = 0
}

func (d *Deck) Size() int {
	total := 0
	for _, pile := range d.piles {
		total += len(pile)
	}
	return total
}

// Draw returns the next question, or false when the deck holds none at all.
func (d *Deck) Draw() (Question, bool) {
	if len(d.piles) == 0 {
		return Question{}, false
	}
	pile := d.rng.Intn(len(d.piles))
	order := d.piles[pile]

	if d.pos[pile] >= len(order) {
		d.shuffle(pile)
		// A reshuffle must not deal the question just asked straight back.
		if len(order) > 1 && order[0] == d.last {
			order[0], order[1] = order[1], order[0]
		}
	}
	idx := order[d.pos[pile]]
	d.pos[pile]++
	d.last = idx
	return d.source[idx], true
}
