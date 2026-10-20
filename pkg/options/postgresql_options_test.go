package options

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

// TestPostgreSQLOptions_Validate_Required mirrors the MySQL behaviour: a
// partially configured instance must be rejected so that pkg/db never silently
// falls back to 127.0.0.1:5432.
func TestPostgreSQLOptions_Validate_Required(t *testing.T) {
	cases := []struct {
		name     string
		add      string
		user     string
		password string
		database string
		wantErrs int
	}{
		{name: "defaults are not usable", add: "127.0.0.1:5432", wantErrs: 2},
		{name: "missing database", add: "127.0.0.1:5432", user: "root", wantErrs: 1},
		{name: "missing username", add: "127.0.0.1:5432", database: "app", wantErrs: 1},
		{name: "disabled by empty addr", add: "", wantErrs: 0},
		{name: "complete", add: "127.0.0.1:5432", user: "postgres", database: "app", password: "pw", wantErrs: 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := &PostgreSQLOptions{Addr: c.add, Username: c.user, Password: c.password, Database: c.database}
			require.Len(t, o.Validate(), c.wantErrs)
		})
	}
}

func TestPostgreSQLOptions_DefaultsAreNotCredentials(t *testing.T) {
	o := NewPostgreSQLOptions()

	require.NotEqual(t, "onex(#)666", o.Password)
	require.Empty(t, o.Password)
	require.NotEqual(t, "onex", o.Username)
	require.NotEqual(t, "onex", o.Database)
	require.Empty(t, o.Username)
	require.Empty(t, o.Database)
}

// TestPostgreSQLOptions_AddFlags_DoesNotClobberMaxIdle reproduces the
// wrong-default bug in AddFlags.
func TestPostgreSQLOptions_AddFlags_DoesNotClobberMaxIdle(t *testing.T) {
	o := &PostgreSQLOptions{MaxIdleConnections: 5, MaxOpenConnections: 100}

	fs := pflag.NewFlagSet("postgresql", pflag.ContinueOnError)
	o.AddFlags(fs)

	require.Equal(t, 5, o.MaxIdleConnections)
	require.Equal(t, 100, o.MaxOpenConnections)
}

func TestPostgreSQLOptions_MaxConnectionLifeTime(t *testing.T) {
	o := NewPostgreSQLOptions()
	require.Equal(t, 10*time.Second, o.MaxConnectionLifeTime)
}

// TestPostgreSQLOptions_NewDB_RefusesDisabled covers the guard that stops pkg/db's
// setPostgreSQLDefaults from silently rewriting an empty Addr to 127.0.0.1:5432.
func TestPostgreSQLOptions_NewDB_RefusesDisabled(t *testing.T) {
	o := &PostgreSQLOptions{Addr: "", Username: "u", Database: "d"}

	_, err := o.NewDB()
	require.ErrorIs(t, err, ErrPostgreSQLDisabled)
}
