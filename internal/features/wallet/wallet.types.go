// Package wallet keeps each platform player's gems: bought in packs through
// Lagos Life, spent on answer reveals in a solo run.
package wallet

import (
	"context"
	"time"
)

const (
	walletsCollection   = "wallets"
	purchasesCollection = "purchases"
)

// Products is every pack on sale, by the product id set in the Lagos Life
// dashboard, and how many gems it grants. The price lives with the platform,
// never here.
var Products = map[string]int{
	"gems_10": 10,
}

// Wallet is one platform player's balance. The id is the platform's own
// per-game player id, so it is the same player every time they come back.
type Wallet struct {
	PlayerID  string    `bson:"_id"`
	Gems      int       `bson:"gems"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// Purchase records a platform purchase that has been turned into gems. Its id
// is the platform's purchase id, which is what makes crediting happen once.
type Purchase struct {
	ID        string    `bson:"_id"`
	PlayerID  string    `bson:"player_id"`
	ProductID string    `bson:"product_id"`
	Gems      int       `bson:"gems"`
	CreatedAt time.Time `bson:"created_at"`
}

// Store is where balances live. Every method is safe to call concurrently.
type Store interface {
	Balance(ctx context.Context, playerID string) (int, error)
	// Credit adds a purchase's gems exactly once. credited is false when the
	// purchase was already recorded; the balance is returned either way.
	Credit(ctx context.Context, purchase Purchase) (credited bool, balance int, err error)
	// Spend takes gems only if the balance covers them.
	Spend(ctx context.Context, playerID string, gems int) (ok bool, balance int, err error)
	// Refund returns gems taken for something that then could not be given.
	Refund(ctx context.Context, playerID string, gems int) (balance int, err error)
}
