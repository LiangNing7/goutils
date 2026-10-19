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

var _ IOptions = (*MySQLOptions)(nil)

// MySQLOptions defines options for mysql database.
type MySQLOptions struct {
	Addr                  string        `json:"addr,omitempty" mapstructure:"addr"`
	Username              string        `json:"username,omitempty" mapstructure:"username"`
	Password              string        `json:"-" mapstructure:"password"`
	Database              string        `json:"database" mapstructure:"database"`
	MaxIdleConnections    int           `json:"max-idle-connections,omitempty" mapstructure:"max-idle-connections,omitempty"`
	MaxOpenConnections    int           `json:"max-open-connections,omitempty" mapstructure:"max-open-connections"`
	MaxConnectionLifeTime time.Duration `json:"max-connection-life-time,omitempty" mapstructure:"max-connection-life-time"`
	LogLevel              int           `json:"log-level" mapstructure:"log-level"`
}

// NewMySQLOptions create a `zero` value instance.
//
// The credentials are intentionally left empty: no default password is shipped,
// it MUST be provided by the configuration file, a command line flag or an
// environment variable. Because Username/Database are empty too, the value
// returned here does not pass Validate() until it is fully configured - that is
// deliberate, see Validate for the rationale.
func NewMySQLOptions() *MySQLOptions {
	return &MySQLOptions{
		Addr:                  "127.0.0.1:3306",
		Username:              "",
		Password:              "",
		Database:              "",
		MaxIdleConnections:    100,
		MaxOpenConnections:    100,
		MaxConnectionLifeTime: time.Duration(10) * time.Second,
		LogLevel:              1, // Silent
	}
}

// Validate verifies flags passed to MySQLOptions.
//
// Semantics: MySQL is considered *disabled* when Addr is empty, which matches
// the documented meaning of the --mysql.host flag ("If left blank, the following
// related mysql options will be ignored"). As soon as Addr is set, Username and
// Database become mandatory.
//
// This matters because pkg/db's setMySQLDefaults silently rewrites an empty Addr
// to 127.0.0.1:3306, so an incomplete configuration would otherwise connect to a
// local server without reporting anything.
func (o *MySQLOptions) Validate() []error {
	errs := []error{}

	if o.Addr == "" {
		// MySQL is intentionally not enabled.
		return errs
	}

	if o.Username == "" {
		errs = append(errs, fmt.Errorf("--mysql.username can not be empty when --mysql.host is set"))
	}

	if o.Database == "" {
		errs = append(errs, fmt.Errorf("--mysql.database can not be empty when --mysql.host is set"))
	}

	return errs
}

// AddFlags adds flags related to mysql storage for a specific APIServer to the specified FlagSet.
func (o *MySQLOptions) AddFlags(fs *pflag.FlagSet, prefixes ...string) {
	fs.StringVar(&o.Addr, join(prefixes...)+"mysql.host", o.Addr, ""+
		"MySQL service host address. If left blank, the following related mysql options will be ignored.")
	// --mysql.addr is an alias of --mysql.host, kept in sync with the Addr field
	// name (and with the PostgreSQL options).
	fs.StringVar(&o.Addr, join(prefixes...)+"mysql.addr", o.Addr, "Alias of --mysql.host.")
	fs.StringVar(&o.Username, join(prefixes...)+"mysql.username", o.Username, "Username for access to mysql service.")
	fs.StringVar(&o.Password, join(prefixes...)+"mysql.password", o.Password, ""+
		"Password for access to mysql, should be used pair with password.")
	fs.StringVar(&o.Database, join(prefixes...)+"mysql.database", o.Database, ""+
		"Database name for the server to use.")
	fs.IntVar(&o.MaxIdleConnections, join(prefixes...)+"mysql.max-idle-connections", o.MaxIdleConnections, ""+
		"Maximum idle connections allowed to connect to mysql.")
	fs.IntVar(&o.MaxOpenConnections, join(prefixes...)+"mysql.max-open-connections", o.MaxOpenConnections, ""+
		"Maximum open connections allowed to connect to mysql.")
	fs.DurationVar(&o.MaxConnectionLifeTime, join(prefixes...)+"mysql.max-connection-life-time", o.MaxConnectionLifeTime, ""+
		"Maximum connection life time allowed to connect to mysql.")
	fs.IntVar(&o.LogLevel, join(prefixes...)+"mysql.log-mode", o.LogLevel, ""+
		"Specify gorm log level.")
}

// ErrMySQLDisabled is returned by NewDB when no MySQL host is configured.
var ErrMySQLDisabled = errors.New("mysql is disabled: --mysql.host is empty")

// DSN returns the DSN for these options.
//
// It exists for backwards compatibility only and is not used at runtime (NewDB
// goes through pkg/db). To avoid the two implementations drifting apart it
// delegates to db.MySQLOptions.DSN rather than duplicating the format string.
func (o *MySQLOptions) DSN() string {
	return (&db.MySQLOptions{
		Addr:     o.Addr,
		Username: o.Username,
		Password: o.Password,
		Database: o.Database,
	}).DSN()
}

// NewDB create mysql store with the given config.
func (o *MySQLOptions) NewDB() (*gorm.DB, error) {
	if o.Addr == "" {
		// Refuse to build a DSN that pkg/db would silently rewrite to
		// 127.0.0.1:3306.
		return nil, ErrMySQLDisabled
	}

	opts := &db.MySQLOptions{
		Addr:                  o.Addr,
		Username:              o.Username,
		Password:              o.Password,
		Database:              o.Database,
		MaxIdleConnections:    o.MaxIdleConnections,
		MaxOpenConnections:    o.MaxOpenConnections,
		MaxConnectionLifeTime: o.MaxConnectionLifeTime,
		Logger:                log.NewGormLogger().LogMode(gormlogger.LogLevel(o.LogLevel)),
	}

	return db.NewMySQL(opts)
}
