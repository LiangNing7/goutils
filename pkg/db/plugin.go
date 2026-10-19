package db

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

const (
	callBackBeforeName = "core:before"
	callBackAfterName  = "core:after"
	startTime          = "_start_time"
)

// callbackRegistrar 抽象 gorm 内部的 *callback，便于统一处理 Register 的返回值.
type callbackRegistrar interface {
	Register(name string, fn func(*gorm.DB)) error
}

// TracePlugin defines gorm plugin used to trace sql.
type TracePlugin struct{}

// Name returns the name of trace plugin.
func (op *TracePlugin) Name() string {
	return "tracePlugin"
}

// Initialize initialize the trace plugin.
//
// 所有 Register 的错误都会被收集并通过返回值暴露（同时记录到 klog），
// 不再像旧实现那样直接丢弃，避免注册失败时插件静默不可用.
func (op *TracePlugin) Initialize(db *gorm.DB) (err error) {
	var errs []error
	reg := func(r callbackRegistrar, name string, fn func(*gorm.DB)) {
		if e := r.Register(name, fn); e != nil {
			wrapped := fmt.Errorf("tracePlugin: register callback %q: %w", name, e)
			klog.Errorf("%v", wrapped)
			errs = append(errs, wrapped)
		}
	}

	// 开始前
	reg(db.Callback().Create().Before("gorm:before_create"), callBackBeforeName, before)
	reg(db.Callback().Query().Before("gorm:query"), callBackBeforeName, before)
	reg(db.Callback().Delete().Before("gorm:before_delete"), callBackBeforeName, before)
	reg(db.Callback().Update().Before("gorm:setup_reflect_value"), callBackBeforeName, before)
	reg(db.Callback().Row().Before("gorm:row"), callBackBeforeName, before)
	reg(db.Callback().Raw().Before("gorm:raw"), callBackBeforeName, before)

	// 结束后
	reg(db.Callback().Create().After("gorm:after_create"), callBackAfterName, after)
	reg(db.Callback().Query().After("gorm:after_query"), callBackAfterName, after)
	reg(db.Callback().Delete().After("gorm:after_delete"), callBackAfterName, after)
	reg(db.Callback().Update().After("gorm:after_update"), callBackAfterName, after)
	reg(db.Callback().Row().After("gorm:row"), callBackAfterName, after)
	reg(db.Callback().Raw().After("gorm:raw"), callBackAfterName, after)

	if len(errs) > 0 {
		err = errors.Join(errs...)
	}
	return err
}

var _ gorm.Plugin = &TracePlugin{}

func before(db *gorm.DB) {
	db.InstanceSet(startTime, time.Now())
}

func after(db *gorm.DB) {
	_ts, isExist := db.InstanceGet(startTime)
	if !isExist {
		return
	}

	ts, ok := _ts.(time.Time)
	if !ok {
		return
	}
	klog.V(4).Infof("sql cost time: %fs", time.Since(ts).Seconds())
}
