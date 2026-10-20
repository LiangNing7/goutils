package distlock

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// errFakeMongo is returned by the fake collection for shapes it does not expect.
var errFakeMongo = errors.New("fake mongo: unsupported filter or update")

// fakeMongoCollection is an in-memory stand-in for the one collection the
// MongoLocker uses. It interprets exactly the filter and update shapes the
// locker emits (plus enough of MongoDB's semantics to be honest about them):
//
//   - `$or` of `{expiredAt: {$lte: t}}` and `{token: {$exists: false}}`;
//   - plain equality on `_id` and `token`;
//   - `$set`, and `$setOnInsert` on insert;
//   - assigning the same path in both `$set` and `$setOnInsert` fails the way
//     MongoDB fails ("would create a conflict"), which is what the previous
//     implementation did;
//   - a conflicting upsert on an existing `_id` fails with a duplicate key
//     error, again like MongoDB.
type fakeMongoCollection struct {
	mu   sync.Mutex
	docs map[string]bson.M
}

func newFakeMongoCollection() *fakeMongoCollection {
	return &fakeMongoCollection{docs: make(map[string]bson.M)}
}

func (f *fakeMongoCollection) countOf() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.docs)
}

func (f *fakeMongoCollection) tokenOf(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	token, _ := f.docs[id]["token"].(string)
	return token
}

func (f *fakeMongoCollection) seed(id string, doc bson.M) {
	f.mu.Lock()
	defer f.mu.Unlock()
	clone := bson.M{"_id": id}
	for k, v := range doc {
		clone[k] = v
	}
	f.docs[id] = clone
}

func (f *fakeMongoCollection) expire(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if doc, ok := f.docs[id]; ok {
		doc["expiredAt"] = time.Now().Add(-time.Second)
	}
}

func (f *fakeMongoCollection) UpdateOne(
	_ context.Context,
	filter interface{},
	update interface{},
	opts ...*options.UpdateOptions,
) (*mongo.UpdateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	filterDoc, ok := filter.(bson.M)
	if !ok {
		return nil, fmt.Errorf("%w: filter %T", errFakeMongo, filter)
	}
	id, ok := filterDoc["_id"].(string)
	if !ok {
		return nil, fmt.Errorf("%w: filter has no string _id", errFakeMongo)
	}

	set, setOnInsert, err := splitUpdate(update)
	if err != nil {
		return nil, err
	}

	upsert := false
	for _, o := range opts {
		if o != nil && o.Upsert != nil && *o.Upsert {
			upsert = true
		}
	}

	if doc, exists := f.docs[id]; exists && matchesMongoFilter(doc, filterDoc) {
		for k, v := range set {
			doc[k] = v
		}
		return &mongo.UpdateResult{MatchedCount: 1, ModifiedCount: 1}, nil
	}
	if !upsert {
		return &mongo.UpdateResult{}, nil
	}
	if _, exists := f.docs[id]; exists {
		return nil, mongo.WriteException{
			WriteErrors: mongo.WriteErrors{{
				Index:   0,
				Code:    11000,
				Message: "E11000 duplicate key error collection: test.locks index: _id_ dup key",
			}},
		}
	}

	doc := bson.M{"_id": id}
	for k, v := range setOnInsert {
		doc[k] = v
	}
	for k, v := range set {
		doc[k] = v
	}
	f.docs[id] = doc
	return &mongo.UpdateResult{UpsertedCount: 1, UpsertedID: id}, nil
}

func (f *fakeMongoCollection) DeleteOne(
	_ context.Context,
	filter interface{},
	_ ...*options.DeleteOptions,
) (*mongo.DeleteResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	filterDoc, ok := filter.(bson.M)
	if !ok {
		return nil, fmt.Errorf("%w: filter %T", errFakeMongo, filter)
	}
	id, ok := filterDoc["_id"].(string)
	if !ok {
		return nil, fmt.Errorf("%w: filter has no string _id", errFakeMongo)
	}
	doc, exists := f.docs[id]
	if !exists || !matchesMongoFilter(doc, filterDoc) {
		return &mongo.DeleteResult{DeletedCount: 0}, nil
	}
	delete(f.docs, id)
	return &mongo.DeleteResult{DeletedCount: 1}, nil
}

// splitUpdate separates $set from $setOnInsert, reproducing MongoDB's refusal to
// assign the same path twice.
func splitUpdate(update interface{}) (bson.M, bson.M, error) {
	updateDoc, ok := update.(bson.M)
	if !ok {
		return nil, nil, fmt.Errorf("%w: update %T", errFakeMongo, update)
	}
	set := bson.M{}
	setOnInsert := bson.M{}
	for op, raw := range updateDoc {
		fields, ok := raw.(bson.M)
		if !ok {
			return nil, nil, fmt.Errorf("%w: operator %s", errFakeMongo, op)
		}
		switch op {
		case "$set":
			for k, v := range fields {
				set[k] = v
			}
		case "$setOnInsert":
			for k, v := range fields {
				setOnInsert[k] = v
			}
		default:
			return nil, nil, fmt.Errorf("%w: operator %s", errFakeMongo, op)
		}
	}
	for k := range set {
		if _, both := setOnInsert[k]; both {
			return nil, nil, mongo.WriteException{
				WriteErrors: mongo.WriteErrors{{
					Code:    40,
					Message: fmt.Sprintf("Updating the path '%s' would create a conflict at '%s'", k, k),
				}},
			}
		}
	}
	return set, setOnInsert, nil
}

// matchesMongoFilter evaluates the filter shapes MongoLocker generates.
func matchesMongoFilter(doc bson.M, filter bson.M) bool {
	for key, cond := range filter {
		if key == "$or" {
			alternatives, ok := cond.(bson.A)
			if !ok {
				return false
			}
			matched := false
			for _, alt := range alternatives {
				altDoc, ok := alt.(bson.M)
				if ok && matchesMongoFilter(doc, altDoc) {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
			continue
		}
		if !matchesMongoCondition(doc, key, cond) {
			return false
		}
	}
	return true
}

func matchesMongoCondition(doc bson.M, key string, cond interface{}) bool {
	value, exists := doc[key]
	if operator, ok := cond.(bson.M); ok {
		for op, operand := range operator {
			switch op {
			case "$lte":
				limit, ok := operand.(time.Time)
				if !ok {
					return false
				}
				got, ok := value.(time.Time)
				if !ok || got.After(limit) {
					return false
				}
			case "$exists":
				want, _ := operand.(bool)
				if exists != want {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	if !exists {
		return false
	}
	got, ok := value.(string)
	want, ok2 := cond.(string)
	return ok && ok2 && got == want
}

const mongoTestName = "unit-test-lock"

func mongoHarness(t *testing.T) *lockerHarness {
	t.Helper()
	fake := newFakeMongoCollection()
	return &lockerHarness{
		newLocker: func() testLocker {
			o := ApplyOptions(WithLockName(mongoTestName), WithLockTimeout(10*time.Second))
			return newMongoLocker(nil, fake, nil, o)
		},
		expireLease: func() { fake.expire(mongoTestName) },
		shared:      true,
	}
}

// TestMongoFirstAcquisitionSucceeds pins the worst of the old bugs: the old
// implementation used $setOnInsert with upsert and treated MatchedCount == 0 as
// "already held", so the very first Lock always failed.
func TestMongoFirstAcquisitionSucceeds(t *testing.T) {
	fake := newFakeMongoCollection()
	o := ApplyOptions(WithLockName(mongoTestName), WithLockTimeout(10*time.Second))
	l := newMongoLocker(nil, fake, nil, o)
	ctx := context.Background()
	defer func() { _ = l.Close() }()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("first Lock() = %v, want nil", err)
	}
	if got := fake.countOf(); got != 1 {
		t.Fatalf("lock documents after first Lock() = %d, want 1", got)
	}
	if token := fake.tokenOf(mongoTestName); token == "" {
		t.Fatal("lock document has no token after Lock()")
	}
}

// TestMongoLiveLockIsNotOverwritten pins the second half of the old bug: the
// unconditional $set meant every subsequent Lock "succeeded" and stole the lock.
func TestMongoLiveLockIsNotOverwritten(t *testing.T) {
	fake := newFakeMongoCollection()
	fake.seed(mongoTestName, bson.M{
		"name":      mongoTestName,
		"token":     "foreign-token",
		"ownerID":   "foreign-owner",
		"expiredAt": time.Now().Add(time.Minute), // still live
	})

	o := ApplyOptions(WithLockName(mongoTestName), WithLockTimeout(10*time.Second))
	l := newMongoLocker(nil, fake, nil, o)
	ctx := context.Background()
	defer func() { _ = l.Close() }()

	if err := l.Lock(ctx); !isErrLockAlreadyHeld(err) {
		t.Fatalf("Lock() over a live foreign lock = %v, want ErrLockAlreadyHeld", err)
	}
	if token := fake.tokenOf(mongoTestName); token != "foreign-token" {
		t.Fatalf("token after a rejected Lock() = %q, want the foreign token untouched", token)
	}
}

// TestMongoUpdateNeverAssignsAPathTwice guards against reintroducing the
// "$set and $setOnInsert on the same field" update, which MongoDB rejects with
// "would create a conflict".
func TestMongoUpdateNeverAssignsAPathTwice(t *testing.T) {
	// The fake reports the conflict as a write error (code 40), so a Lock that
	// used both operators would fail here with something other than success.
	fake := newFakeMongoCollection()
	o := ApplyOptions(WithLockName(mongoTestName), WithLockTimeout(10*time.Second))
	l := newMongoLocker(nil, fake, nil, o)
	ctx := context.Background()
	defer func() { _ = l.Close() }()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil (a conflict error means $set/$setOnInsert overlap)", err)
	}
}

// TestMongoExpiredLockIsStolen proves takeover works: an expired document is
// replaced by the new owner in a single atomic update.
func TestMongoExpiredLockIsStolen(t *testing.T) {
	fake := newFakeMongoCollection()
	fake.seed(mongoTestName, bson.M{
		"name":      mongoTestName,
		"token":     "stale-token",
		"ownerID":   "stale-owner",
		"expiredAt": time.Now().Add(-time.Second), // expired
	})

	o := ApplyOptions(WithLockName(mongoTestName), WithLockTimeout(10*time.Second))
	l := newMongoLocker(nil, fake, nil, o)
	ctx := context.Background()
	defer func() { _ = l.Close() }()

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() over an expired lock = %v, want nil", err)
	}
	if token := fake.tokenOf(mongoTestName); token == "stale-token" || token == "" {
		t.Fatalf("token after stealing = %q, want a fresh token", token)
	}
	if got := fake.countOf(); got != 1 {
		t.Fatalf("lock documents = %d, want 1 (takeover must not insert a second row)", got)
	}
}

// TestMongoRenewOnlyTouchesOurDocument asserts a stale holder cannot extend a
// successor's lease.
func TestMongoRenewOnlyTouchesOurDocument(t *testing.T) {
	fake := newFakeMongoCollection()
	ctx := context.Background()
	a := newMongoLocker(nil, fake, nil, ApplyOptions(WithLockName(mongoTestName), WithLockTimeout(10*time.Second)))
	b := newMongoLocker(nil, fake, nil, ApplyOptions(WithLockName(mongoTestName), WithLockTimeout(10*time.Second)))

	if err := a.Lock(ctx); err != nil {
		t.Fatalf("a.Lock() = %v, want nil", err)
	}
	fake.expire(mongoTestName)
	if err := b.Lock(ctx); err != nil {
		t.Fatalf("b.Lock() = %v, want nil", err)
	}

	if err := a.Renew(ctx); !isErrLockNotHeld(err) {
		t.Fatalf("a.Renew() = %v, want ErrLockNotHeld", err)
	}
	if err := b.Renew(ctx); err != nil {
		t.Fatalf("b.Renew() = %v, want nil", err)
	}
}
