//go:build !linux

package systemd

import (
	"context"
	"runtime"
)

// gather на не-Linux платформах — заглушка. systemd существует только на Linux,
// поэтому здесь честно сообщаем о недоступности (используется на dev-машинах
// macOS/Windows). go-systemd в эту сборку не подтягивается.
func (c *Collector) gather(_ context.Context, out *ServicesInfo) {
	out.Available = false
	out.Reason = "systemd is only available on Linux (current OS: " + runtime.GOOS + ")"
}
