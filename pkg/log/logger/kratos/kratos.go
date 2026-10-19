package kratos

import (
	krtlog "github.com/go-kratos/kratos/v2/log"

	"github.com/LiangNing7/goutils/pkg/log"
)

func NewLogger(id, name, version string) krtlog.Logger {
	// 使用 log.NewKratosLogger() 而不是 log.Default()：后者会在构造期就把
	// 全局 logger 捕获下来，log.Init 之后仍然在使用旧 logger.
	return krtlog.With(
		log.NewKratosLogger(),
		"ts", krtlog.DefaultTimestamp,
		"caller", krtlog.DefaultCaller,
		"service.id", id,
		"service.name", name,
		"service.version", version,
	)
}
