package questions

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Repo is data access only.
type Repo struct {
	coll *mongo.Collection
}

func NewRepo(db *mongo.Database) *Repo {
	return &Repo{coll: db.Collection(collectionName)}
}

func (r *Repo) EnsureIndexes(ctx context.Context) error {
	_, err := r.coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "category", Value: 1}, {Key: "difficulty", Value: 1}},
	})
	return err
}

func (r *Repo) FindAll(ctx context.Context) ([]Question, error) {
	cursor, err := r.coll.Find(ctx, bson.D{}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	out := []Question{}
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ReplaceAll makes the SEEDED part of the collection match the given set
// exactly: every question is upserted by id and any seeded question not in the
// set is removed. Idempotent, so the seed can be re-run after an edit without
// leaving orphans behind.
//
// Imported questions (source "import") are never deleted here. They arrive by
// mongoimport, not from the seed files, so "not in the seed files" says
// nothing about whether they should exist.
func (r *Repo) ReplaceAll(ctx context.Context, questions []Question) (upserted int64, removed int64, err error) {
	if len(questions) == 0 {
		return 0, 0, nil
	}

	models := make([]mongo.WriteModel, 0, len(questions))
	keep := make([]string, 0, len(questions))
	for _, q := range questions {
		keep = append(keep, q.ID)
		models = append(models, mongo.NewReplaceOneModel().
			SetFilter(bson.D{{Key: "_id", Value: q.ID}}).
			SetReplacement(q).
			SetUpsert(true))
	}

	res, err := r.coll.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	if err != nil {
		return 0, 0, err
	}

	del, err := r.coll.DeleteMany(ctx, bson.D{
		{Key: "_id", Value: bson.D{{Key: "$nin", Value: keep}}},
		{Key: "source", Value: bson.D{{Key: "$ne", Value: SourceImport}}},
	})
	if err != nil {
		return 0, 0, err
	}

	return res.UpsertedCount + res.MatchedCount, del.DeletedCount, nil
}
