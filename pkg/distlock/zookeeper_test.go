package distlock

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-zookeeper/zk"
)

// fakeZK emulates the parts of a ZooKeeper connection the locker uses, including
// path validation, ErrNodeExists on an existing node, ErrNoNode on a missing
// parent, and the version CAS of Delete.
type fakeZK struct {
	mu       sync.Mutex
	values   map[string][]byte
	flags    map[string]int32
	versions map[string]int32
	closes   int
}

func newFakeZK() *fakeZK {
	return &fakeZK{
		values:   make(map[string][]byte),
		flags:    make(map[string]int32),
		versions: make(map[string]int32),
	}
}

func (f *fakeZK) Create(path string, data []byte, flags int32, _ []zk.ACL) (string, error) {
	if !strings.HasPrefix(path, "/") {
		// Exactly what ZooKeeper answers for a relative path.
		return "", zk.ErrInvalidPath
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.values[path]; exists {
		return "", zk.ErrNodeExists
	}
	if parent := path[:strings.LastIndex(path, "/")]; parent != "" && parent != "/" {
		if _, ok := f.values[parent]; !ok {
			return "", zk.ErrNoNode
		}
	}
	f.values[path] = append([]byte(nil), data...)
	f.flags[path] = flags
	f.versions[path] = 0
	return path, nil
}

func (f *fakeZK) Get(path string) ([]byte, *zk.Stat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.values[path]
	if !ok {
		return nil, nil, zk.ErrNoNode
	}
	stat := &zk.Stat{Version: f.versions[path]}
	if f.flags[path]&zk.FlagEphemeral != 0 {
		stat.EphemeralOwner = 1
	}
	return append([]byte(nil), data...), stat, nil
}

func (f *fakeZK) Delete(path string, version int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	current, ok := f.versions[path]
	if !ok {
		return zk.ErrNoNode
	}
	if version != -1 && version != current {
		return zk.ErrBadVersion
	}
	delete(f.values, path)
	delete(f.flags, path)
	delete(f.versions, path)
	return nil
}

func (f *fakeZK) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
}

// nodeFlags reports the flags a node was created with, so tests can assert the
// node really is ephemeral.
func (f *fakeZK) nodeFlags(path string) (int32, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	flags, ok := f.flags[path]
	return flags, ok
}

// existingPaths returns every node that currently exists.
func (f *fakeZK) existingPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	paths := make([]string, 0, len(f.values))
	for path := range f.values {
		paths = append(paths, path)
	}
	return paths
}

const zkTestPath = "/unit-test-lock"

func zookeeperHarness(t *testing.T) *lockerHarness {
	t.Helper()
	fake := newFakeZK()
	return &lockerHarness{
		newLocker: func() testLocker {
			o := ApplyOptions(WithLockName(zkTestPath), WithLockTimeout(10*time.Second))
			return newZookeeperLocker(fake, o)
		},
		// Losing the session makes ZooKeeper drop the ephemeral node.
		expireLease: func() {
			for _, path := range fake.existingPaths() {
				_ = fake.Delete(path, -1)
			}
		},
		shared: true,
	}
}

// TestZookeeperPathIsNormalizedAtConstruction pins the "relative path" bug: the
// default lock name has no leading slash, and ZooKeeper rejects such a path.
func TestZookeeperPathIsNormalizedAtConstruction(t *testing.T) {
	for input, want := range map[string]string{
		"onex-distributed-lock":  "/onex-distributed-lock",
		"/onex-distributed-lock": "/onex-distributed-lock",
		"onex/foo":               "/onex/foo",
		"/onex/foo/":             "/onex/foo",
		"":                       "/" + DefaultLockName,
		"/":                      "/" + DefaultLockName,
	} {
		if got := normalizeZKPath(input); got != want {
			t.Fatalf("normalizeZKPath(%q) = %q, want %q", input, got, want)
		}
	}

	l := newZKWithDefaultOptions(t)
	if !strings.HasPrefix(l.lockPath, "/") {
		t.Fatalf("default lock path %q is not absolute", l.lockPath)
	}
	if !strings.HasPrefix(l.lockPath, "/"+DefaultLockName) {
		t.Fatalf("default lock path %q does not start with the lock name", l.lockPath)
	}
}

// newZKWithDefaultOptions builds a ZookeeperLocker over a fake connection with
// the default options, so path normalisation can be observed.
func newZKWithDefaultOptions(t *testing.T) *ZookeeperLocker {
	t.Helper()
	fake := newFakeZK()
	t.Cleanup(fake.Close)
	return newZookeeperLocker(fake, ApplyOptions())
}

// TestZookeeperNodeIsEphemeralAndOwnerIndependent pins the two ZooKeeper bugs:
// the node used to be persistent (so a crash leaked the lock forever) and its
// path embedded the ownerID (so two owners never collided and there was no mutual
// exclusion at all).
func TestZookeeperNodeIsEphemeralAndOwnerIndependent(t *testing.T) {
	fake := newFakeZK()
	ctx := context.Background()
	a := newZookeeperLocker(fake, ApplyOptions(WithLockName(zkTestPath), WithLockTimeout(10*time.Second)))
	b := newZookeeperLocker(fake, ApplyOptions(WithLockName(zkTestPath), WithLockTimeout(10*time.Second)))

	if err := a.Lock(ctx); err != nil {
		t.Fatalf("a.Lock() = %v, want nil", err)
	}

	flags, ok := fake.nodeFlags(zkTestPath)
	if !ok {
		t.Fatal("lock node was not created at the shared lock path")
	}
	if flags&zk.FlagEphemeral == 0 {
		t.Fatalf("lock node flags = %d, want the ephemeral flag (%d) so a crashed holder releases it", flags, zk.FlagEphemeral)
	}

	if err := b.Lock(ctx); !isErrLockAlreadyHeld(err) {
		t.Fatalf("b.Lock() = %v, want ErrLockAlreadyHeld (both owners must contend for the same node)", err)
	}
	if paths := fake.existingPaths(); len(paths) != 1 {
		t.Fatalf("lock nodes = %v, want exactly one shared node", paths)
	}
}

// TestZookeeperUnlockRemovesOnlyItsOwnNode asserts the read-then-version-CAS
// delete protects a successor's node.
func TestZookeeperUnlockRemovesOnlyItsOwnNode(t *testing.T) {
	fake := newFakeZK()
	ctx := context.Background()
	a := newZookeeperLocker(fake, ApplyOptions(WithLockName(zkTestPath), WithLockTimeout(10*time.Second)))
	b := newZookeeperLocker(fake, ApplyOptions(WithLockName(zkTestPath), WithLockTimeout(10*time.Second)))

	if err := a.Lock(ctx); err != nil {
		t.Fatalf("a.Lock() = %v, want nil", err)
	}
	// Session loss drops a's node; b takes the lock.
	if err := fake.Delete(zkTestPath, -1); err != nil {
		t.Fatalf("dropping a's node: %v", err)
	}
	if err := b.Lock(ctx); err != nil {
		t.Fatalf("b.Lock() = %v, want nil", err)
	}
	bData, _, err := fake.Get(zkTestPath)
	if err != nil {
		t.Fatalf("reading b's node: %v", err)
	}

	if err := a.Unlock(ctx); !isErrLockNotHeld(err) {
		t.Fatalf("a.Unlock() = %v, want ErrLockNotHeld", err)
	}

	data, _, err := fake.Get(zkTestPath)
	if err != nil {
		t.Fatalf("b's node disappeared after a.Unlock(): %v", err)
	}
	if string(data) != string(bData) {
		t.Fatalf("b's node was rewritten: got %q, want %q", data, bData)
	}
}

// TestZookeeperUnlockUsesVersionCas asserts the delete really uses the version
// returned by Get: a node replaced in between must not be deleted.
func TestZookeeperUnlockUsesVersionCas(t *testing.T) {
	fake := newFakeZK()
	ctx := context.Background()
	l := newZookeeperLocker(fake, ApplyOptions(WithLockName(zkTestPath), WithLockTimeout(10*time.Second)))

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	// Simulate the node being recreated by somebody else with a newer version.
	fake.mu.Lock()
	fake.versions[zkTestPath] = 7
	fake.values[zkTestPath] = []byte("foreign")
	fake.mu.Unlock()

	if err := l.Unlock(ctx); !isErrLockNotHeld(err) {
		t.Fatalf("Unlock() = %v, want ErrLockNotHeld", err)
	}
	if got := fake.existingPaths(); len(got) != 1 {
		t.Fatalf("nodes after Unlock() = %v, want the foreign node untouched", got)
	}
}

// TestZookeeperRenewDoesNotPretendToRenew pins the honest Renew: the old version
// logged "Lock renewed" and returned nil even when it owned nothing.
func TestZookeeperRenewDoesNotPretendToRenew(t *testing.T) {
	fake := newFakeZK()
	log := &countingLogger{}
	ctx := context.Background()
	l := newZookeeperLocker(fake, ApplyOptions(
		WithLockName(zkTestPath),
		WithLockTimeout(10*time.Second),
		WithLogger(log),
	))

	if err := l.Renew(ctx); !isErrLockNotHeld(err) {
		t.Fatalf("Renew() without holding the lock = %v, want ErrLockNotHeld", err)
	}

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	if err := l.Renew(ctx); err != nil {
		t.Fatalf("Renew() while holding the lock = %v, want nil", err)
	}
	if got := log.count("Lock renewed"); got != 0 {
		t.Fatalf("Renew() logged %d fake %q messages; an ephemeral node is not renewed", got, "Lock renewed")
	}

	// With the node gone, Renew must report the truth.
	if err := fake.Delete(zkTestPath, -1); err != nil {
		t.Fatalf("dropping the node: %v", err)
	}
	if err := l.Renew(ctx); !isErrLockNotHeld(err) {
		t.Fatalf("Renew() after the node disappeared = %v, want ErrLockNotHeld", err)
	}
}

// TestZookeeperCreatesMissingParents asserts a nested lock path works instead of
// failing with ErrNoNode.
func TestZookeeperCreatesMissingParents(t *testing.T) {
	fake := newFakeZK()
	ctx := context.Background()
	l := newZookeeperLocker(fake, ApplyOptions(WithLockName("/onex/locks/distlock"), WithLockTimeout(10*time.Second)))

	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock() with a nested path = %v, want nil", err)
	}
	if _, ok := fake.nodeFlags("/onex/locks/distlock"); !ok {
		t.Fatalf("lock node missing; existing nodes: %v", fake.existingPaths())
	}
	if err := l.Unlock(ctx); err != nil {
		t.Fatalf("Unlock() = %v, want nil", err)
	}
}

// TestZookeeperCloseClosesTheConnection asserts Close actually releases the
// connection (and therefore the session that holds the ephemeral node).
func TestZookeeperCloseClosesTheConnection(t *testing.T) {
	fake := newFakeZK()
	l := newZookeeperLocker(fake, ApplyOptions(WithLockName(zkTestPath), WithLockTimeout(10*time.Second)))
	if err := l.Lock(context.Background()); err != nil {
		t.Fatalf("Lock() = %v, want nil", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	fake.mu.Lock()
	closes := fake.closes
	fake.mu.Unlock()
	if closes != 1 {
		t.Fatalf("connection closes = %d, want 1", closes)
	}
}
