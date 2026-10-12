package wallet

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repo struct {
	wallets   *mongo.Collection
	purchases *mongo.Collection
}

func NewRepo(db *mongo.Database) *Repo {
	return &Repo{wallets: db.Collection(walletsCollection), purchases: db.Collection(purchasesCollection)}
}

func (r *Repo) Balance(ctx context.Context, playerID string) (int, error) {
	var wallet Wallet
	err := r.wallets.FindOne(ctx, bson.D{{Key: "_id", Value: playerID}}).Decode(&wallet)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, nil
	}
	return wallet.Gems, err
}

// add moves a balance by delta and returns the new one, creating the wallet
// on a player's first credit.
func (r *Repo) add(ctx context.Context, filter bson.D, delta int, upsert bool) (int, error) {
	update := bson.D{
		{Key: "$inc", Value: bson.D{{Key: "gems", Value: delta}}},
		{Key: "$set", Value: bson.D{{Key: "updated_at", Value: time.Now().UTC()}}},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After).SetUpsert(upsert)
	var wallet Wallet
	err := r.wallets.FindOneAndUpdate(ctx, filter, update, opts).Decode(&wallet)
	return wallet.Gems, err
}

func (r *Repo) Credit(ctx context.Context, purchase Purchase) (bool, int, error) {
	// The purchase id is the document id, so a second insert of the same
	// purchase fails on the unique key. That failure IS the "already credited"
	// answer, and it holds across restarts and concurrent requests.
	if _, err := r.purchases.InsertOne(ctx, purchase); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			balance, balanceErr := r.Balance(ctx, purchase.PlayerID)
			return false, balance, balanceErr
		}
		return false, 0, err
	}
	balance, err := r.add(ctx, bson.D{{Key: "_id", Value: purchase.PlayerID}}, purchase.Gems, true)
	return err == nil, balance, err
}

func (r *Repo) Spend(ctx context.Context, playerID string, gems int) (bool, int, error) {
	// The balance check and the deduction are one atomic update, so two taps
	// at once cannot both spend the last gems.
	filter := bson.D{{Key: "_id", Value: playerID}, {Key: "gems", Value: bson.D{{Key: "$gte", Value: gems}}}}
	balance, err := r.add(ctx, filter, -gems, false)
	if errors.Is(err, mongo.ErrNoDocuments) {
		current, balanceErr := r.Balance(ctx, playerID)
		return false, current, balanceErr
	}
	return err == nil, balance, err
}

func (r *Repo) Refund(ctx context.Context, playerID string, gems int) (int, error) {
	return r.add(ctx, bson.D{{Key: "_id", Value: playerID}}, gems, true)
}
