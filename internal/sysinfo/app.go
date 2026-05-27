package sysinfo

import (
	"context"
	"time"
)

// AppMeta — статичные данные о приложении, передаются в коллектор при инициализации.
// Все они доступны только из контекста main.go, поэтому собираются не runtime'ом,
// а извне.
type AppMeta struct {
	Name      string
	Env       string
	Version   string
	StartedAt time.Time
	HTTPPort  string
	// PublicIP — явный публичный IP сервера (env HOST_PUBLIC_IP). Если задан,
	// используется как host.public_ip без внешнего lookup'а. Рекомендуется в
	// контейнерах, где автоопределение даёт IP контейнера, а не хоста.
	PublicIP string
}

func (c *Collector) collectApp(_ context.Context) AppInfo {
	return AppInfo{
		Name:          c.app.Name,
		Env:           c.app.Env,
		Version:       c.app.Version,
		StartedAt:     c.app.StartedAt,
		UptimeSeconds: time.Since(c.app.StartedAt).Seconds(),
		HTTPPort:      c.app.HTTPPort,
	}
}
