package sysinfo

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/shirou/gopsutil/v4/host"
)

func (c *Collector) collectHost(ctx context.Context) (HostInfo, error) {
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		return HostInfo{}, fmt.Errorf("host info: %w", err)
	}

	tz, _ := time.Now().Zone()

	hostname, _ := os.Hostname()
	if info.Hostname == "" {
		info.Hostname = hostname
	}

	out := HostInfo{
		Hostname:             info.Hostname,
		FQDN:                 lookupFQDN(info.Hostname),
		OS:                   info.OS,
		Platform:             info.Platform,
		PlatformFamily:       info.PlatformFamily,
		PlatformVersion:      info.PlatformVersion,
		KernelVersion:        info.KernelVersion,
		KernelArch:           info.KernelArch,
		VirtualizationSystem: info.VirtualizationSystem,
		VirtualizationRole:   info.VirtualizationRole,
		BootTime:             time.Unix(int64(info.BootTime), 0).UTC(),
		UptimeSeconds:        info.Uptime,
		HostID:               info.HostID,
		Timezone:             tz,
	}
	return out, nil
}

// lookupFQDN — best-effort попытка получить полное доменное имя.
// На многих системах вернёт то же, что и hostname.
func lookupFQDN(hostname string) string {
	if hostname == "" {
		return ""
	}
	addrs, err := net.LookupHost(hostname)
	if err != nil || len(addrs) == 0 {
		return hostname
	}
	names, err := net.LookupAddr(addrs[0])
	if err != nil || len(names) == 0 {
		return hostname
	}
	// Срезаем хвостовую точку, которую возвращает резолвер для абсолютных имён.
	fqdn := names[0]
	if l := len(fqdn); l > 0 && fqdn[l-1] == '.' {
		fqdn = fqdn[:l-1]
	}
	return fqdn
}
