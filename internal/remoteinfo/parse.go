package remoteinfo

import (
	"strconv"
	"strings"
)

// parseKV парсит `key=value` или `key: value` построчно. Значения — без
// окружающих кавычек и пробелов. Пустые строки и комментарии (#...) пропускаются.
func parseKV(text string, sep string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.Index(line, sep)
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+len(sep):])
		val = strings.Trim(val, `"'`)
		out[key] = val
	}
	return out
}

// atoiSafe — безопасный strconv.Atoi: пустая строка / мусор → 0.
func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// atou64Safe — безопасный strconv.ParseUint: пустая строка / мусор → 0.
func atou64Safe(s string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return n
}

// atofSafe — безопасный strconv.ParseFloat: пустая строка / мусор → 0.
func atofSafe(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

// fields — strings.Fields, но всегда возвращает не-nil слайс, чтобы было
// безопасно индексировать после проверки len.
func fields(line string) []string {
	return strings.Fields(line)
}
