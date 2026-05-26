// Package clientinfo извлекает из HTTP-запроса метаданные клиента:
// IP, User-Agent, тип устройства, ОС, браузер. Использует простую
// эвристику на основе подстрок (без внешней зависимости на UA-парсер).
// Корректность — best-effort: для редких/новых UA результаты могут быть
// неполными. Это допустимо: данные нужны для аудита, не для авторизации.
package clientinfo

import (
	"net"
	"net/http"
	"regexp"
	"strings"
)

// ClientInfo — то, что мы можем достать из *http.Request. Поля совпадают
// с полями в service.ClientInfo (тот шаг — отдельный, чтобы transport
// не зависел от service-DTO напрямую).
type ClientInfo struct {
	IPAddress      string
	UserAgent      string
	DeviceType     string
	DeviceName     string
	OS             string
	OSVersion      string
	Browser        string
	BrowserVersion string
	AppVersion     string
}

// Extract собирает данные клиента из запроса. Ничего не падает — что не
// удалось распарсить, остаётся пустой строкой.
func Extract(r *http.Request) ClientInfo {
	ua := r.Header.Get("User-Agent")
	dt, osName, osVer, br, brVer := parseUserAgent(ua)
	return ClientInfo{
		IPAddress:      extractIP(r),
		UserAgent:      ua,
		DeviceType:     dt,
		DeviceName:     strings.TrimSpace(r.Header.Get("X-Device-Name")),
		OS:             osName,
		OSVersion:      osVer,
		Browser:        br,
		BrowserVersion: brVer,
		AppVersion:     strings.TrimSpace(r.Header.Get("X-App-Version")),
	}
}

// extractIP достаёт IP клиента. Приоритет:
//  1. X-Forwarded-For (берём ЛЕВЫЙ адрес — это исходный клиент, остальные —
//     прокси-цепочка). Доверять можно только если за вашим reverse proxy.
//  2. r.RemoteAddr — формат "ip:port", очищаем порт.
//
// Если ни то ни другое не парсится — возвращаем пустую строку,
// репозиторий запишет NULL.
func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Левый IP = клиент, остальные = промежуточные proxy.
		first := strings.TrimSpace(strings.Split(xff, ",")[0])
		if first != "" && net.ParseIP(first) != nil {
			return first
		}
	}
	if r.RemoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// На некоторых runtime'ах RemoteAddr может прийти без порта.
		if net.ParseIP(r.RemoteAddr) != nil {
			return r.RemoteAddr
		}
		return ""
	}
	if net.ParseIP(host) == nil {
		return ""
	}
	return host
}

// ─────────────────────────── Парсер User-Agent ───────────────────────────
// Простая эвристика. Цель — корректно идентифицировать массовые случаи
// (десктопы Chrome/Firefox/Safari/Edge, мобильные iOS/Android, простые
// клиенты типа curl/Postman). Для экзотики результат будет неполным —
// поля заполнятся пустыми строками, ENUM device_type упадёт в UNKNOWN.

var (
	// Версии браузеров — захватываем числовую часть после имени.
	chromeVerRe  = regexp.MustCompile(`Chrome/([0-9.]+)`)
	firefoxVerRe = regexp.MustCompile(`Firefox/([0-9.]+)`)
	safariVerRe  = regexp.MustCompile(`Version/([0-9.]+)`)
	edgeVerRe    = regexp.MustCompile(`Edg/([0-9.]+)`)
	operaVerRe   = regexp.MustCompile(`OPR/([0-9.]+)`)

	// Версии ОС
	windowsVerRe = regexp.MustCompile(`Windows NT ([0-9.]+)`)
	macVerRe     = regexp.MustCompile(`Mac OS X ([0-9_]+)`)
	iosVerRe     = regexp.MustCompile(`OS ([0-9_]+) like Mac OS X`)
	androidVerRe = regexp.MustCompile(`Android ([0-9.]+)`)
)

func parseUserAgent(ua string) (deviceType, osName, osVersion, browser, browserVersion string) {
	if ua == "" {
		return "UNKNOWN", "", "", "", ""
	}
	lower := strings.ToLower(ua)

	// 1) Браузер — порядок важен (Edge содержит "Chrome" в UA, Opera тоже).
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
		browserVersion = match(edgeVerRe, ua)
	case strings.Contains(ua, "OPR/"):
		browser = "Opera"
		browserVersion = match(operaVerRe, ua)
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
		browserVersion = match(firefoxVerRe, ua)
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
		browserVersion = match(chromeVerRe, ua)
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
		browserVersion = match(safariVerRe, ua)
	case strings.Contains(lower, "curl/"):
		browser = "curl"
	case strings.Contains(lower, "postman"):
		browser = "Postman"
	case strings.Contains(lower, "go-http-client"):
		browser = "Go HTTP client"
	}

	// 2) ОС
	switch {
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPod"):
		osName = "iOS"
		osVersion = strings.ReplaceAll(match(iosVerRe, ua), "_", ".")
	case strings.Contains(ua, "iPad"):
		osName = "iPadOS"
		osVersion = strings.ReplaceAll(match(iosVerRe, ua), "_", ".")
	case strings.Contains(ua, "Android"):
		osName = "Android"
		osVersion = match(androidVerRe, ua)
	case strings.Contains(ua, "Windows"):
		osName = "Windows"
		osVersion = match(windowsVerRe, ua)
	case strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh"):
		osName = "macOS"
		osVersion = strings.ReplaceAll(match(macVerRe, ua), "_", ".")
	case strings.Contains(lower, "linux"):
		osName = "Linux"
	}

	// 3) Тип устройства — выводим из ОС/UA, не из браузера.
	switch {
	case strings.Contains(ua, "iPad") || strings.Contains(lower, "tablet"):
		deviceType = "TABLET"
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "Android") || strings.Contains(lower, "mobi"):
		deviceType = "MOBILE"
	case osName == "Windows" || osName == "macOS" || osName == "Linux":
		// Десктопная ОС, доступ через браузер — WEB; если без браузера (curl и т.п.) — DESKTOP/API.
		if browser != "" && browser != "curl" && browser != "Postman" && browser != "Go HTTP client" {
			deviceType = "WEB"
		} else if browser == "curl" || browser == "Postman" || browser == "Go HTTP client" {
			deviceType = "API"
		} else {
			deviceType = "DESKTOP"
		}
	default:
		deviceType = "UNKNOWN"
	}

	return
}

func match(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
