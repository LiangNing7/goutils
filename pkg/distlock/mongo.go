package distlock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/LiangNing7/goutils/pkg/logger"
)

// mongoCollection is the subset of *mongo.Collection used by MongoLocker. It
// exists so the locker can be unit tested with a fake.
type mongoCollection interface {
	UpdateOne(
		ctx context.Context,
		filter interface{},
		update interface{},
		opts ...*options.UpdateOptions,
	) (*mongo.UpdateResult, error)

	DeleteOne(
		ctx context.Context,
		filter interface{},
		opts ...*options.DeleteOptions,
	) (*mongo.DeleteResult, error)
}

// mongoLockCollection is the collection used to store locks, kept for
// compatibility with the previous implementation.
const mongoLockCollection = "locks"

// MongoLocker provides a distributed locking mechanism using MongoDB.
//
// Design:
//   - The lock document is identified by its _id, which is the lock name
//     itself. That gives uniqueness for free, without the locker having to
//     create a unique index (the previous implementation relied on a "name"
//     field with no index at all, which is why two concurrent processes could
//     both "acquire" the lock).
//   - Acquisition is a SINGLE atomic FindOneAndUpdate-shaped operation
//     (UpdateOne with upsert): "match a document that is either expired or has
//     no token yet, and set it to my token". Whether we won is derived from
//     MatchedCount/UpsertedCount, never from a read-then-write dance.
//   - If the filter does not match because somebody holds a live lock, the
//     upsert turns into an insert of {_id: <name>} and Mongo answers with a
//     duplicate key error, which is translated into ErrLockAlreadyHeld.
//   - The expiration check compares server-stored expiredAt values against the
//     client clock, so leases are only as accurate as the clock skew between
//     the participating hosts. Keep NTP on.
type MongoLocker struct {
	client      *mongo.Client
	collection  mongoCollection
	lockName    string
	lockTimeout time.Duration
	ownerID     string
	logger      logger.Logger

	st lockerState

	disconnect func(ctx context.Context) error
}

// Ensure MongoLocker implements the Locker and Closer interfaces.
var (
	_ Locker = (*MongoLocker)(nil)
	_ Closer = (*MongoLocker)(nil)
)

// newMongoLocker wires a MongoLocker around an already resolved client and
// collection. It exists so tests can inject a fake collection.
func newMongoLocker(
	client *mongo.Client,
	coll mongoCollection,
	disconnect func(ctx context.Context) error,
	o *Options,
) *MongoLocker {
	locker := &MongoLocker{
		client:      client,
		collection:  coll,
		lockName:    o.lockName,
		lockTimeout: o.lockTimeout,
		ownerID:     o.ownerID,
		logger:      o.logger,
		disconnect:  disconnect,
	}

	locker.logger.Info("MongoLocker initialized", "lockName", locker.lockName, "ownerID", locker.ownerID)
	return locker
}

// NewMongoLocker creates a new MongoLocker instance and connects to the given
// MongoDB URI.
//
// The connection is owned by the locker: Close() disconnects it.
func NewMongoLocker(mongoURI string, dbName string, opts ...Option) (*MongoLocker, error) {
	o := ApplyOptions(opts...)
	if err := o.validateErr(); err != nil {
		return nil, err
	}

	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, err
	}

	locker := newMongoLocker(
		client,
		client.Database(dbName).Collection(mongoLockCollection),
		client.Disconnect,
		o,
	)
	return locker, nil
}

// Lock attempts to acquire the distributed lock.
//
// It is a single atomic operation: either the document is stolen from an expired
// owner (MatchedCount == 1), or it is created because it did not exist
// (UpsertedCount == 1), or the lock is held and we lose (duplicate key).
func (l *MongoLocker) Lock(ctx context.Context) error {
	defer l.st.lockOp()()

	if l.st.isHeld() {
		return fmt.Errorf("%w (mongo name=%s)", ErrLockAlreadyHeld, l.lockName)
	}

	now := time.Now()
	expiredAt := now.Add(l.lockTimeout)
	token := newToken()

	// The $or makes this a "steal if expired, or if it was never claimed"
	// filter. Note that $setOnInsert must NOT touch the same fields as $set:
	// MongoDB rejects an update that assigns the same path twice ("would create
	// a conflict"), which is what the previous implementation did.
	filter := bson.M{
		"_id": l.lockName,
		"$or": bson.A{
			bson.M{"expiredAt": bson.M{"$lte": now}},
			bson.M{"token": bson.M{"$exists": false}},
		},
	}
	update := bson.M{
		"$set": bson.M{
			"name":      l.lockName,
			"ownerID":   l.ownerID,
			"token":     token,
			"expiredAt": expiredAt,
		},
	}

	result, err := l.collection.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// Somebody holds a live lock, so the upsert degenerated into an
			// insert of {_id: <name>} and Mongo refused it. That is exactly the
			// "lock is taken" outcome.
			l.logger.Warn("lock is already held", "name", l.lockName)
			return fmt.Errorf("%w (mongo name=%s)", ErrLockAlreadyHeld, l.lockName)
		}
		l.logger.Error("failed to acquire lock", "name", l.lockName, "error", err)
		return fmt.Errorf("distlock/mongo: update: %w", err)
	}
	if result.MatchedCount == 0 && result.UpsertedCount == 0 {
		// Defensive: should not happen for an upsert that neither matched nor
		// inserted, but we must never report a false success.
		return fmt.Errorf("%w (mongo name=%s)", ErrLockAlreadyHeld, l.lockName)
	}

	l.st.markHeld(token, ctx, l.lockTimeout/2, func(rctx context.Context) {
		if err := l.renewWithToken(rctx, token); err != nil {
			l.logger.Error("failed to renew lock", "name", l.lockName, "error", err)
		}
	})

	l.logger.Info("Lock acquired", "name", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Unlock removes the lock document, but only if it still carries our token.
func (l *MongoLocker) Unlock(ctx context.Context) error {
	defer l.st.lockOp()()

	token, held := l.st.clearHeld()
	if !held {
		return fmt.Errorf("%w (mongo name=%s)", ErrLockNotHeld, l.lockName)
	}

	result, err := l.collection.DeleteOne(ctx, bson.M{"_id": l.lockName, "token": token})
	if err != nil {
		l.logger.Error("failed to release lock", "name", l.lockName, "error", err)
		return fmt.Errorf("distlock/mongo: delete: %w", err)
	}
	if result.DeletedCount == 0 {
		l.logger.Warn("lock is not ours any more, leaving it untouched", "name", l.lockName)
		return fmt.Errorf("%w (mongo name=%s: lease expired or taken over)", ErrLockNotHeld, l.lockName)
	}

	l.logger.Info("Lock released", "name", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Renew extends the lease, but only while the document still carries our token.
func (l *MongoLocker) Renew(ctx context.Context) error {
	defer l.st.lockOp()()

	token, held := l.st.heldToken()
	if !held {
		return fmt.Errorf("%w (mongo name=%s)", ErrLockNotHeld, l.lockName)
	}
	return l.renewWithToken(ctx, token)
}

// renewWithToken performs the conditional lease extension. It never takes the
// operation lock: it is called from Renew (which holds it) and from the renewal
// goroutine (which must not, see lockerState).
func (l *MongoLocker) renewWithToken(ctx context.Context, token string) error {
	expiredAt := time.Now().Add(l.lockTimeout)

	result, err := l.collection.UpdateOne(
		ctx,
		bson.M{"_id": l.lockName, "token": token},
		bson.M{"$set": bson.M{"expiredAt": expiredAt}},
	)
	if err != nil {
		return fmt.Errorf("distlock/mongo: renew: %w", err)
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("%w (mongo name=%s: lease expired or taken over)", ErrLockNotHeld, l.lockName)
	}
	return nil
}

// Close stops the renewal goroutine and disconnects the Mongo client this
// locker created. It is idempotent.
func (l *MongoLocker) Close() error {
	defer l.st.lockOp()()
	l.st.clearHeld()

	if l.disconnect == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.disconnect(ctx); err != nil && !errors.Is(err, mongo.ErrClientDisconnected) {
		return fmt.Errorf("distlock/mongo: disconnect: %w", err)
	}
	return nil
}
