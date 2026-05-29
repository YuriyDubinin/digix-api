package sysinfo

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
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

	publicIP := c.resolvePublicIP(ctx)
	countryCode, country := c.resolveCountry(publicIP)

	out := HostInfo{
		Hostname:             info.Hostname,
		FQDN:                 lookupFQDN(info.Hostname),
		PrimaryIP:            outboundIP(),
		PublicIP:             publicIP,
		CountryCode:          countryCode,
		Country:              country,
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

// outboundIP определяет основной исходящий IPv4 машины: «фейковый» UDP-dial
// не шлёт пакетов, но заставляет ОС выбрать интерфейс/адрес для маршрута наружу.
// В bridge-контейнере вернёт IP контейнера (172.x), на голом железе — реальный.
func outboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

// resolvePublicIP возвращает публичный (внешний) IP сервера.
// Приоритет:
//  1. env HOST_PUBLIC_IP (через AppMeta.PublicIP) — мгновенно, без сети;
//  2. закэшированный результат прошлого внешнего lookup'а;
//  3. одноразовый best-effort внешний запрос (с коротким таймаутом).
func (c *Collector) resolvePublicIP(ctx context.Context) string {
	if c.app.PublicIP != "" {
		return c.app.PublicIP
	}

	c.ipMu.Lock()
	if c.publicResolved {
		ip := c.publicIP
		c.ipMu.Unlock()
		return ip
	}
	c.ipMu.Unlock()

	ip := fetchPublicIP(ctx)
	if ip != "" {
		c.ipMu.Lock()
		c.publicIP = ip
		c.publicResolved = true
		c.ipMu.Unlock()
	}
	return ip
}

// resolveCountry — резолв страны по уже определённому публичному IP через
// встроенную mmdb-базу (пакет internal/geo). Полностью локально, без сетевых
// вызовов. Пустые результаты при отсутствии резолвера, пустом IP или приватных
// диапазонах — не ошибка.
func (c *Collector) resolveCountry(publicIP string) (code, name string) {
	if c.geo == nil || publicIP == "" {
		return "", ""
	}
	ci, ok := c.geo.Lookup(publicIP)
	if !ok {
		return "", ""
	}
	return ci.Code, ci.Name
}

// fetchPublicIP — best-effort внешний запрос публичного IP. Жёсткий таймаут,
// чтобы не подвешивать сбор. Пустая строка при любой ошибке.
func fetchPublicIP(ctx context.Context) string {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return ""
	}
	ip := strings.TrimSpace(string(body))
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}
