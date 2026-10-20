package options

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/LiangNing7/goutils/pkg/db"
)

// TestMySQLOptions_Validate_Required checks that a partially configured MySQL
// instance is rejected instead of silently falling back to 127.0.0.1:3306 in
// pkg/db.
func TestMySQLOptions_Validate_Required(t *testing.T) {
	cases := []struct {
		name     string
		add      string
		user     string
		password string
		database string
		wantErrs int
	}{
		{name: "defaults are not usable", add: "127.0.0.1:3306", wantErrs: 2},
		{name: "missing database", add: "127.0.0.1:3306", user: "root", wantErrs: 1},
		{name: "missing username", add: "127.0.0.1:3306", database: "app", wantErrs: 1},
		{name: "disabled by empty addr", add: "", wantErrs: 0},
		{name: "complete", add: "127.0.0.1:3306", user: "root", database: "app", wantErrs: 0},
		{name: "complete without password", add: "127.0.0.1:3306", user: "root", database: "app", password: "", wantErrs: 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := &MySQLOptions{Addr: c.add, Username: c.user, Password: c.password, Database: c.database}
			require.Len(t, o.Validate(), c.wantErrs)
		})
	}
}

// TestMySQLOptions_DefaultsAreNotCredentials asserts the shipped defaults no
// longer contain the leaked `onex` credentials.
func TestMySQLOptions_DefaultsAreNotCredentials(t *testing.T) {
	o := NewMySQLOptions()

	require.NotEqual(t, "onex(#)666", o.Password)
	require.Empty(t, o.Password)
	require.NotEqual(t, "onex", o.Username)
	require.NotEqual(t, "onex", o.Database)
	require.Empty(t, o.Username)
	require.Empty(t, o.Database)
}

// TestMySQLOptions_AddFlags_DoesNotClobberMaxIdle reproduces the
// `fs.IntVar(&o.MaxIdleConnections, ..., o.MaxOpenConnections, ...)` bug:
// pflag writes the default into the target pointer at registration time, so
// MaxIdleConnections was overwritten with MaxOpenConnections.
func TestMySQLOptions_AddFlags_DoesNotClobberMaxIdle(t *testing.T) {
	o := &MySQLOptions{MaxIdleConnections: 5, MaxOpenConnections: 100}

	fs := pflag.NewFlagSet("mysql", pflag.ContinueOnError)
	o.AddFlags(fs)

	require.Equal(t, 5, o.MaxIdleConnections)
	require.Equal(t, 100, o.MaxOpenConnections)
}

// TestMySQLOptions_DSN_MatchesDBLayer ensures the local DSN helper cannot drift
// away from the implementation used by pkg/db.
func TestMySQLOptions_DSN_MatchesDBLayer(t *testing.T) {
	o := &MySQLOptions{Addr: "db:3306", Username: "u", Password: "p", Database: "d"}

	want := (&db.MySQLOptions{
		Addr:     "db:3306",
		Username: "u",
		Password: "p",
		Database: "d",
	}).DSN()

	require.Equal(t, want, o.DSN(), "MySQLOptions.DSN must delegate to the pkg/db implementation")

	require.Contains(t, o.DSN(), "charset=utf8mb4")
}

// TestMySQLOptions_NewDB_RefusesDisabled covers the guard that stops pkg/db's
// setMySQLDefaults from silently rewriting an empty Addr to 127.0.0.1:3306.
func TestMySQLOptions_NewDB_RefusesDisabled(t *testing.T) {
	o := &MySQLOptions{Addr: "", Username: "u", Database: "d"}

	_, err := o.NewDB()
	require.ErrorIs(t, err, ErrMySQLDisabled)
}

func TestMySQLOptions_FlagsRegistered(t *testing.T) {
	o := NewMySQLOptions()
	fs := pflag.NewFlagSet("mysql", pflag.ContinueOnError)
	o.AddFlags(fs)

	for _, name := range []string{"mysql.host", "mysql.addr", "mysql.username", "mysql.password", "mysql.database"} {
		require.NotNil(t, fs.Lookup(name), "flag %s must be registered", name)
	}
}

func TestMySQLOptions_MaxConnectionLifeTime(t *testing.T) {
	o := NewMySQLOptions()
	require.Equal(t, 10*time.Second, o.MaxConnectionLifeTime)
}
