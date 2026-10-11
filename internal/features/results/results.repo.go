package results

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repo struct {
	coll *mongo.Collection
}

func NewRepo(db *mongo.Database) *Repo {
	return &Repo{coll: db.Collection(collectionName)}
}

func (r *Repo) Insert(ctx context.Context, result GameResult) error {
	_, err := r.coll.InsertOne(ctx, result)
	return err
}

// ListBefore returns up to limit results older than the cursor, newest first.
// Ids sort by creation time, so the id is the cursor — no offset, and a game
// finishing mid-scroll cannot shift the page.
func (r *Repo) ListBefore(ctx context.Context, cursor string, limit int) ([]GameResult, error) {
	filter := bson.D{}
	if cursor != "" {
		filter = bson.D{{Key: "_id", Value: bson.D{{Key: "$lt", Value: cursor}}}}
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit))

	found, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	out := []GameResult{}
	if err := found.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
