package log

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	infoStr       = "%s[info] "
	warnStr       = "%s[warn] "
	errStr        = "%s[error] "
	traceStr      = "[%s][%.3fms] [rows:%v] %s"
	traceWarnStr  = "%s %s[%.3fms] [rows:%v] %s"
	traceErrStr   = "%s %s[%.3fms] [rows:%v] %s"
	slowThreshold = 200 * time.Millisecond
)

var levelM = map[string]gormlogger.LogLevel{
	"panic": gormlogger.Silent,
	"error": gormlogger.Error,
	"warn":  gormlogger.Warn,
	"info":  gormlogger.Info,
	// debug 是最详细的级别，缺失该键会让 log.level=debug 时
	// levelM[level] 取到零值(Silent)，SQL 日志被全部静默.
	"debug": gormlogger.Info,
}

func (l *zapLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	opts := *l.opts
	switch {
	case level <= gormlogger.Silent:
		opts.Level = "panic"
	case level <= gormlogger.Error:
		opts.Level = "error"
	case level <= gormlogger.Warn:
		opts.Level = "warn"
	case level <= gormlogger.Info:
		opts.Level = "info"
	default:
	}

	return NewLogger(&opts)
}

// ParamsFilter 让 gorm 交出参数化 SQL（保留 `?` 占位符），避免参数值被写进日志.
//
// gorm 只在 logger 实现了该接口时才会保留占位符；否则 gorm 会在
// callbacks.go 里无条件调用 Dialector.Explain 把参数插值进 SQL，再交给
// Trace，于是密码哈希、手机号等 PII 会直接出现在应用日志里.
// gorm 内置 logger 的 ParameterizedQueries=true 走的就是同样的实现方式.
func (l *zapLogger) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}

// gormLoggerAdapter 把 gorm 的日志调用延迟转发给"当时"的全局 logger.
//
// 它同时实现了 ParamsFilter，保证经由它记录的 SQL 不会带出参数值.
type gormLoggerAdapter struct {
	level gormlogger.LogLevel
}

// NewGormLogger 返回一个 gorm logger，它在每次记录日志时才解析当前的全局
// logger，因此即使在 log.Init 之前就被塞进 gorm.Config，仍能用上 Init 之后的
// 日志级别与输出路径.
func NewGormLogger() gormlogger.Interface {
	return &gormLoggerAdapter{level: gormlogger.Warn}
}

// current 解析当前的全局 logger 并套用本适配器持有的日志级别.
func (a *gormLoggerAdapter) current() gormlogger.Interface {
	return std.Load().LogMode(a.level)
}

// LogMode 返回套用了指定级别的新适配器，不修改自身.
func (a *gormLoggerAdapter) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	return &gormLoggerAdapter{level: level}
}

func (a *gormLoggerAdapter) Info(ctx context.Context, msg string, keyvals ...any) {
	a.current().Info(ctx, msg, keyvals...)
}

func (a *gormLoggerAdapter) Warn(ctx context.Context, msg string, keyvals ...any) {
	a.current().Warn(ctx, msg, keyvals...)
}

func (a *gormLoggerAdapter) Error(ctx context.Context, msg string, keyvals ...any) {
	a.current().Error(ctx, msg, keyvals...)
}

func (a *gormLoggerAdapter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	a.current().Trace(ctx, begin, fc, err)
}

// ParamsFilter 见 (*zapLogger).ParamsFilter.
func (a *gormLoggerAdapter) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}

// 编译期断言：适配器必须同时满足 gorm 的日志接口与参数过滤接口.
var (
	_ gormlogger.Interface = (*gormLoggerAdapter)(nil)
	_ gorm.ParamsFilter    = (*gormLoggerAdapter)(nil)
	_ gorm.ParamsFilter    = (*zapLogger)(nil)
)

func (l *zapLogger) Info(ctx context.Context, msg string, keyvals ...any) {
	l.z.Sugar().Infof(infoStr+msg, append([]any{fileWithLineNum()}, keyvals...)...)
}

func (l *zapLogger) Warn(ctx context.Context, msg string, keyvals ...any) {
	l.z.Sugar().Warnf(warnStr+msg, append([]any{fileWithLineNum()}, keyvals...)...)
}

func (l *zapLogger) Error(ctx context.Context, msg string, keyvals ...any) {
	l.z.Sugar().Errorf(errStr+msg, append([]any{fileWithLineNum()}, keyvals...)...)
}

func (l *zapLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if levelM[l.opts.Level] <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	switch {
	case err != nil && levelM[l.opts.Level] >= gormlogger.Error:
		sql, rows := fc()
		if rows == -1 {
			l.z.Sugar().Errorf(traceErrStr, fileWithLineNum(), err, float64(elapsed.Nanoseconds())/1e6, "-", sql)
		} else {
			l.z.Sugar().Errorf(traceErrStr, fileWithLineNum(), err, float64(elapsed.Nanoseconds())/1e6, rows, sql)
		}
	case elapsed > slowThreshold && slowThreshold != 0 && levelM[l.opts.Level] >= gormlogger.Warn:
		sql, rows := fc()
		slowLog := fmt.Sprintf("SLOW SQL >= %v", slowThreshold)
		if rows == -1 {
			l.z.Sugar().Warnf(traceWarnStr, fileWithLineNum(), slowLog, float64(elapsed.Nanoseconds())/1e6, "-", sql)
		} else {
			l.z.Sugar().Warnf(traceWarnStr, fileWithLineNum(), slowLog, float64(elapsed.Nanoseconds())/1e6, rows, sql)
		}
	case levelM[l.opts.Level] >= gormlogger.Info:
		sql, rows := fc()
		if rows == -1 {
			l.z.Sugar().Infof(traceStr, fileWithLineNum(), float64(elapsed.Nanoseconds())/1e6, "-", sql)
		} else {
			l.z.Sugar().Infof(traceStr, fileWithLineNum(), float64(elapsed.Nanoseconds())/1e6, rows, sql)
		}
	}
}

func fileWithLineNum() string {
	for i := 4; i < 15; i++ {
		_, file, line, ok := runtime.Caller(i)

		// if ok && (!strings.HasPrefix(file, gormSourceDir) || strings.HasSuffix(file, "_test.go")) {
		if ok && !strings.HasSuffix(file, "_test.go") {
			dir, f := filepath.Split(file)

			return filepath.Join(filepath.Base(dir), f) + ":" + strconv.FormatInt(int64(line), 10)
		}
	}

	return ""
}
