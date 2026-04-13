package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Repository interface {
	GetAllTags(ctx context.Context) ([]bson.M, error)
	GetTagByID(ctx context.Context, id primitive.ObjectID) (bson.M, error)
	GetTagByName(ctx context.Context, name string) (bson.M, error)
	CreateTag(ctx context.Context, payload bson.M) (bson.M, error)
	UpdateTag(ctx context.Context, id primitive.ObjectID, payload bson.M) (bson.M, error)
	SearchTags(ctx context.Context, query string, fields []string, limit int64) ([]bson.M, error)
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
}

type MongoRepository struct {
	client     *mongo.Client
	collection *mongo.Collection
}

func NewMongoRepository(ctx context.Context, mongoURL string, collectionName string) (*MongoRepository, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURL))
	if err != nil {
		return nil, fmt.Errorf("connect mongodb: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		return nil, fmt.Errorf("ping mongodb: %w", err)
	}

	dbName := databaseNameFromURL(mongoURL)
	if dbName == "" {
		dbName = "saymon"
	}

	if strings.TrimSpace(collectionName) == "" {
		collectionName = "tags"
	}

	return &MongoRepository{
		client:     client,
		collection: client.Database(dbName).Collection(collectionName),
	}, nil
}

func (r *MongoRepository) GetAllTags(ctx context.Context) ([]bson.M, error) {
	cursor, err := r.collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []bson.M
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (r *MongoRepository) GetTagByID(ctx context.Context, id primitive.ObjectID) (bson.M, error) {
	var doc bson.M
	if err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (r *MongoRepository) GetTagByName(ctx context.Context, name string) (bson.M, error) {
	var doc bson.M
	if err := r.collection.FindOne(ctx, bson.M{"name": name}).Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (r *MongoRepository) CreateTag(ctx context.Context, payload bson.M) (bson.M, error) {
	result, err := r.collection.InsertOne(ctx, payload)
	if err != nil {
		return nil, err
	}

	oid, ok := result.InsertedID.(primitive.ObjectID)
	if !ok {
		return nil, fmt.Errorf("inserted id is not ObjectID")
	}
	return r.GetTagByID(ctx, oid)
}

func (r *MongoRepository) UpdateTag(ctx context.Context, id primitive.ObjectID, payload bson.M) (bson.M, error) {
	updateDoc := bson.M{"$set": payload}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updated bson.M
	if err := r.collection.FindOneAndUpdate(ctx, bson.M{"_id": id}, updateDoc, opts).Decode(&updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *MongoRepository) SearchTags(ctx context.Context, query string, fields []string, limit int64) ([]bson.M, error) {
	searchFilter := make([]bson.M, 0, len(fields))
	for _, field := range fields {
		searchFilter = append(searchFilter, bson.M{field: bson.M{
			"$regex":   query,
			"$options": "i",
		}})
	}

	filter := bson.M{"$or": searchFilter}
	findOpts := options.Find().SetLimit(limit)

	cursor, err := r.collection.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []bson.M
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (r *MongoRepository) Close(ctx context.Context) error {
	return r.client.Disconnect(ctx)
}

func (r *MongoRepository) Ping(ctx context.Context) error {
	return r.client.Ping(ctx, nil)
}

func databaseNameFromURL(url string) string {
	afterSlash := url[strings.LastIndex(url, "/")+1:]
	if afterSlash == "" {
		return ""
	}

	db := afterSlash
	if idx := strings.Index(db, "?"); idx >= 0 {
		db = db[:idx]
	}

	if idx := strings.Index(db, "&"); idx >= 0 {
		db = db[:idx]
	}

	return db
}
