package db

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// PostgreSQLOptions defines options for PostgreSQL database.
type PostgreSQLOptions struct {
	Addr                  string
	Username              string
	Password              string
	Database              string
	MaxIdleConnections    int
	MaxOpenConnections    int
	MaxConnectionLifeTime time.Duration
	// +optional
	Logger logger.Interface
	// +optional
	// DisableDefaultTransaction 关闭 gorm 默认的写事务包装（gorm.Config.SkipDefaultTransaction）。
	// 默认 false，保持 gorm 的默认语义（每个写操作包在事务中）。
	// 仅当调用方明确接受"单条写语句在非事务上下文中执行"时才建议开启，可获得一定的性能收益。
	DisableDefaultTransaction bool
}

// DSN return DSN from PostgreSQLOptions.
func (o *PostgreSQLOptions) DSN() string {
	splited := strings.Split(o.Addr, ":")
	host, port := splited[0], "5432"
	if len(splited) > 1 {
		port = splited[1]
	}

	return fmt.Sprintf(`user=%s password=%s host=%s port=%s dbname=%s sslmode=disable TimeZone=Asia/Shanghai`,
		o.Username,
		o.Password,
		host,
		port,
		o.Database,
	)
}

// NewPostgreSQL create a new gorm db instance with the given options.
func NewPostgreSQL(opts *PostgreSQLOptions) (*gorm.DB, error) {
	// Set default values to ensure all fields in opts are available.
	setPostgreSQLDefaults(opts)

	db, err := gorm.Open(postgres.Open(opts.DSN()), &gorm.Config{
		// PrepareStmt executes the given query in cached statement.
		// This can improve performance.
		PrepareStmt: true,
		Logger:      opts.Logger,
		// 默认保持 gorm 语义（写操作包事务）；仅在显式开启时关闭。
		SkipDefaultTransaction: opts.DisableDefaultTransaction,
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// SetMaxOpenConns sets the maximum number of open connections to the database.
	sqlDB.SetMaxOpenConns(opts.MaxOpenConnections)

	// SetConnMaxLifetime sets the maximum amount of time a connection may be reused.
	sqlDB.SetConnMaxLifetime(opts.MaxConnectionLifeTime)

	// SetMaxIdleConns sets the maximum number of connections in the idle connection pool.
	sqlDB.SetMaxIdleConns(opts.MaxIdleConnections)

	return db, nil
}

// setPostgreSQLDefaults set available default values for some fields.
func setPostgreSQLDefaults(opts *PostgreSQLOptions) {
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:5432"
	}
	if opts.MaxIdleConnections == 0 {
		opts.MaxIdleConnections = 100
	}
	if opts.MaxOpenConnections == 0 {
		opts.MaxOpenConnections = 100
	}
	if opts.MaxConnectionLifeTime == 0 {
		opts.MaxConnectionLifeTime = time.Duration(10) * time.Second
	}
	if opts.Logger == nil {
		// 默认使用参数化的 logger，避免把 SQL 参数（PII/密码哈希）插值进日志。
		opts.Logger = defaultGormLogger()
	}
}
