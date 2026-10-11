// Package db owns the MongoDB connection.
package db

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

type Database struct {
	client *mongo.Client
	DB     *mongo.Database
}

// Connect fails fast: an instance that accepts traffic it cannot serve fails
// every request instead of failing to start.
func Connect(ctx context.Context, uri, name string) (*Database, error) {
	opts := options.Client().
		ApplyURI(uri).
		SetServerSelectionTimeout(5 * time.Second).
		SetMaxPoolSize(20)

	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		return nil, err
	}

	return &Database{client: client, DB: client.Database(name)}, nil
}

func (d *Database) Ping(ctx context.Context) bool {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return d.client.Ping(pingCtx, readpref.Primary()) == nil
}

func (d *Database) Close(ctx context.Context) error {
	return d.client.Disconnect(ctx)
}
