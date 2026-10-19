package options

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/pflag"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/LiangNing7/goutils/pkg/db"
	"github.com/LiangNing7/goutils/pkg/log"
)

var _ IOptions = (*PostgreSQLOptions)(nil)

// PostgreSQLOptions defines options for postgresql database.
type PostgreSQLOptions struct {
	Addr                  string        `json:"addr,omitempty" mapstructure:"addr"`
	Username              string        `json:"username,omitempty" mapstructure:"username"`
	Password              string        `json:"-" mapstructure:"password"`
	Database              string        `json:"database" mapstructure:"database"`
	MaxIdleConnections    int           `json:"max-idle-connections,omitempty" mapstructure:"max-idle-connections,omitempty"`
	MaxOpenConnections    int           `json:"max-open-connections,omitempty" mapstructure:"max-open-connections"`
	MaxConnectionLifeTime time.Duration `json:"max-connection-life-time,omitempty" mapstructure:"max-connection-life-time"`
	LogLevel              int           `json:"log-level" mapstructure:"log-level"`
}

// NewPostgreSQLOptions create a `zero` value instance.
//
// The credentials are intentionally left empty: no default password is shipped,
// it MUST be provided by the configuration file, a command line flag or an
// environment variable. See Validate for the resulting semantics.
func NewPostgreSQLOptions() *PostgreSQLOptions {
	return &PostgreSQLOptions{
		Addr:                  "127.0.0.1:5432",
		Username:              "",
		Password:              "",
		Database:              "",
		MaxIdleConnections:    100,
		MaxOpenConnections:    100,
		MaxConnectionLifeTime: time.Duration(10) * time.Second,
		LogLevel:              1, // Silent
	}
}

// Validate verifies flags passed to PostgreSQLOptions.
//
// Semantics: PostgreSQL is considered *disabled* when Addr is empty, matching
// the documented meaning of the --postgresql.addr flag. As soon as Addr is set,
// Username and Database become mandatory.
//
// This matters because pkg/db's setPostgreSQLDefaults silently rewrites an empty
// Addr to 127.0.0.1:5432, so an incomplete configuration would otherwise connect
// to a local server without reporting anything.
func (o *PostgreSQLOptions) Validate() []error {
	errs := []error{}

	if o.Addr == "" {
		// PostgreSQL is intentionally not enabled.
		return errs
	}

	if o.Username == "" {
		errs = append(errs, fmt.Errorf("--postgresql.username can not be empty when --postgresql.addr is set"))
	}

	if o.Database == "" {
		errs = append(errs, fmt.Errorf("--postgresql.database can not be empty when --postgresql.addr is set"))
	}

	return errs
}

// ErrPostgreSQLDisabled is returned by NewDB when no PostgreSQL host is configured.
var ErrPostgreSQLDisabled = errors.New("postgresql is disabled: --postgresql.addr is empty")

// AddFlags adds flags related to postgresql storage for a specific APIServer to the specified FlagSet.
func (o *PostgreSQLOptions) AddFlags(fs *pflag.FlagSet, prefixes ...string) {
	fs.StringVar(&o.Addr, join(prefixes...)+"postgresql.addr", o.Addr, ""+
		"PostgreSQL service address. If left blank, the following related postgresql options will be ignored.")
	fs.StringVar(&o.Username, join(prefixes...)+"postgresql.username", o.Username, "Username for access to postgresql service.")
	fs.StringVar(&o.Password, join(prefixes...)+"postgresql.password", o.Password, ""+
		"Password for access to postgresql, should be used pair with password.")
	fs.StringVar(&o.Database, join(prefixes...)+"postgresql.database", o.Database, ""+
		"Database name for the server to use.")
	fs.IntVar(&o.MaxIdleConnections, join(prefixes...)+"postgresql.max-idle-connections", o.MaxIdleConnections, ""+
		"Maximum idle connections allowed to connect to postgresql.")
	fs.IntVar(&o.MaxOpenConnections, join(prefixes...)+"postgresql.max-open-connections", o.MaxOpenConnections, ""+
		"Maximum open connections allowed to connect to postgresql.")
	fs.DurationVar(&o.MaxConnectionLifeTime, join(prefixes...)+"postgresql.max-connection-life-time", o.MaxConnectionLifeTime, ""+
		"Maximum connection life time allowed to connect to postgresql.")
	fs.IntVar(&o.LogLevel, join(prefixes...)+"postgresql.log-mode", o.LogLevel, ""+
		"Specify gorm log level.")
}

// NewDB create postgresql store with the given config.
func (o *PostgreSQLOptions) NewDB() (*gorm.DB, error) {
	if o.Addr == "" {
		// Refuse to build a DSN that pkg/db would silently rewrite to
		// 127.0.0.1:5432.
		return nil, ErrPostgreSQLDisabled
	}

	opts := &db.PostgreSQLOptions{
		Addr:                  o.Addr,
		Username:              o.Username,
		Password:              o.Password,
		Database:              o.Database,
		MaxIdleConnections:    o.MaxIdleConnections,
		MaxOpenConnections:    o.MaxOpenConnections,
		MaxConnectionLifeTime: o.MaxConnectionLifeTime,
		Logger:                log.NewGormLogger().LogMode(gormlogger.LogLevel(o.LogLevel)),
	}

	return db.NewPostgreSQL(opts)
}
