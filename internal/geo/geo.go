// Package geo резолвит страну по IP-адресу через встроенную базу DB-IP Lite
// (Creative Commons, формат MaxMind .mmdb). Файл базы вшит в бинарь через
// //go:embed, поэтому сетевые вызовы и внешние зависимости отсутствуют —
// lookup идёт из памяти за микросекунды.
//
// Используется одинаково для:
//   - публичного IP самого сервиса (sysinfo, эндпоинт /api/system/main);
//   - публичных IP удалённых серверов (sshclient.collectFacts → ServerFacts).
//
// Принципы:
//   - resolver создаётся один раз в main.go и передаётся в коллекторы;
//   - lookup всегда best-effort: невалидный/приватный IP, отсутствие записи
//     в базе → возвращается ok=false, без ошибок;
//   - публичные имена страны — на английском (поле en из mmdb); локализацию
//     делает фронтенд.
package geo

import (
	_ "embed"
	"errors"
	"net"

	"github.com/oschwald/maxminddb-golang"
)

// data/dbip-country-lite.mmdb — DB-IP Lite Country, лицензия CC-BY 4.0.
// Размер ~8 МБ, обновлять вручную раз в N месяцев (см. README).
//
//go:embed data/dbip-country-lite.mmdb
var mmdbBytes []byte

// CountryInfo — результат резолва. Code — ISO 3166-1 alpha-2 в верхнем
// регистре (RU, US, DE...); Name — английское название из базы.
type CountryInfo struct {
	Code string
	Name string
}

// Resolver — thread-safe потокобезопасный lookup по встроенной mmdb-базе.
type Resolver struct {
	db *maxminddb.Reader
}

// NewResolver открывает встроенную базу и валидирует её. Возвращает ошибку,
// если файл повреждён/не распознан — это бы означало битый embed на сборке.
func NewResolver() (*Resolver, error) {
	if len(mmdbBytes) == 0 {
		return nil, errors.New("geo: embedded mmdb is empty")
	}
	db, err := maxminddb.FromBytes(mmdbBytes)
	if err != nil {
		return nil, err
	}
	return &Resolver{db: db}, nil
}

// Close освобождает ресурсы базы. Безопасно вызывать дважды.
func (r *Resolver) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	err := r.db.Close()
	r.db = nil
	return err
}

// Lookup резолвит IP-адрес в страну. Если ip пустой, невалидный, приватный,
// loopback или не найден в базе — возвращает пустую CountryInfo и ok=false.
// Ошибки парсинга/чтения базы не пробрасываются (best-effort) — для вызывающего
// нет разницы между «не нашли» и «техническая ошибка lookup'а».
func (r *Resolver) Lookup(ip string) (CountryInfo, bool) {
	if r == nil || r.db == nil {
		return CountryInfo{}, false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return CountryInfo{}, false
	}
	// Приватные/loopback/link-local — заведомо нет в базе, не дёргаем lookup.
	if isNonRoutable(parsed) {
		return CountryInfo{}, false
	}

	var rec struct {
		Country struct {
			ISOCode string            `maxminddb:"iso_code"`
			Names   map[string]string `maxminddb:"names"`
		} `maxminddb:"country"`
	}
	if err := r.db.Lookup(parsed, &rec); err != nil {
		return CountryInfo{}, false
	}
	code := rec.Country.ISOCode
	if code == "" {
		return CountryInfo{}, false
	}
	name := rec.Country.Names["en"]
	return CountryInfo{Code: code, Name: name}, true
}

// isNonRoutable — IP, который не имеет смысла резолвить через geo-IP базу:
// loopback (127.0.0.0/8, ::1), приватные (10/8, 172.16/12, 192.168/16, fc00::/7),
// link-local (169.254/16, fe80::/10), multicast и unspecified.
func isNonRoutable(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	// Дополнительно — CGNAT (100.64.0.0/10), который IsPrivate() в Go не считает приватным.
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 100 && ip4[1]&0xc0 == 64 {
			return true
		}
	}
	return false
}

