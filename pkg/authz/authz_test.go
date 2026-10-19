package authz

import (
	"fmt"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newTestDB opens an isolated in-memory sqlite database for the casbin
// adapter. The name is unique per test so that the shared in-memory cache does
// not leak policies between tests.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	return db
}

// TestNewAuthzWithInvalidModelReturnsError asserts a malformed ACL model makes
// NewAuthz fail instead of returning an unusable authorizer. A nil *gorm.DB is
// passed on purpose: the model must be validated before any database access,
// otherwise the failure surfaces as an unrelated nil pointer panic.
func TestNewAuthzWithInvalidModelReturnsError(t *testing.T) {
	a, err := NewAuthz(nil, WithAclModel("this is not a valid casbin model"))

	assert.Error(t, err)
	assert.Nil(t, a)
}

// TestAuthorizeWithValidModel asserts the happy path still works.
func TestAuthorizeWithValidModel(t *testing.T) {
	a, err := NewAuthz(newTestDB(t))
	require.NoError(t, err)
	t.Cleanup(a.StopAutoLoadPolicy)

	_, err = a.AddPolicy("alice", "/data1", "read", "allow")
	require.NoError(t, err)

	ok, err := a.Authorize("alice", "/data1", "read")
	require.NoError(t, err)
	assert.True(t, ok)

	// A deny rule overrides the allow rule.
	_, err = a.AddPolicy("alice", "/data1", "read", "deny")
	require.NoError(t, err)

	ok, err = a.Authorize("alice", "/data1", "read")
	require.NoError(t, err)
	assert.False(t, ok)

	// keyMatch is part of the default model: /data* matches /data42.
	_, err = a.AddPolicy("bob", "/data*", "write", "allow")
	require.NoError(t, err)

	ok, err = a.Authorize("bob", "/data42", "write")
	require.NoError(t, err)
	assert.True(t, ok)
}

// TestDefaultModelIsDefaultAllow documents the semantics of the built-in ACL
// model: its policy effect is `!some(where (p.eft == deny))` (deny-override),
// so a request that matches no policy at all is ALLOWED. This test records the
// current behaviour, it does not endorse it. Changing the default model to a
// fail-closed one is a breaking behaviour change and is out of scope here.
func TestDefaultModelIsDefaultAllow(t *testing.T) {
	a, err := NewAuthz(newTestDB(t))
	require.NoError(t, err)
	t.Cleanup(a.StopAutoLoadPolicy)

	ok, err := a.Authorize("nobody", "/anything", "read")
	require.NoError(t, err)
	assert.True(t, ok, "built-in model allows requests matching no policy at all")
}

// TestAuthorizeConcurrently exercises concurrent authorizations: it is the
// evidence that the shared enforcer is safe for concurrent use. Run with -race.
func TestAuthorizeConcurrently(t *testing.T) {
	a, err := NewAuthz(newTestDB(t))
	require.NoError(t, err)
	t.Cleanup(a.StopAutoLoadPolicy)

	_, err = a.AddPolicy("alice", "/data1", "read", "allow")
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 16 {
				ok, err := a.Authorize("alice", "/data1", "read")
				assert.NoError(t, err)
				assert.True(t, ok)
			}
		}()
	}
	wg.Wait()
}
