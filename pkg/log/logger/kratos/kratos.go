package kratos

import (
	krtlog "github.com/go-kratos/kratos/v2/log"

	"github.com/LiangNing7/goutils/pkg/log"
)

func NewLogger(id, name, version string) krtlog.Logger {
	return krtlog.With(
		log.Default(),
		"ts", krtlog.DefaultTimestamp,
		"caller", krtlog.DefaultCaller,
		"service.id", id,
		"service.name", name,
		"service.version", version,
	)
}
