package remoteinfo

import (
	"context"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// collectHost собирает host-секцию: hostname, FQDN, IP, OS, kernel, virt,
// boot/uptime, timezone, host_id и публичный IP с резолвом страны.
func (c *Collector) collectHost(ctx context.Context, client *ssh.Client) (HostInfo, error) {
	// Один сводный шелл-скрипт с разделителями между блоками — экономим
	// сетевые round-trips. Каждый блок начинается с "===<NAME>===" чтобы
	// можно было разделить вывод даже при пустых ответах.
	// printf '\n===<NAME>===\n' с ведущим \n — на случай, если предыдущая
	// команда (curl, ip route, ip -j ...) выдала вывод без trailing newline.
	// Без ведущего \n маркер «приклеился» бы к данным и не был распознан как
	// маркер (см. дополнительную защиту в splitByMarkers / splitTrailingMarker).
	const script = `printf '===HOSTNAME===\n'
hostname 2>/dev/null
printf '\n===FQDN===\n'
hostname --fqdn 2>/dev/null || hostname 2>/dev/null
printf '\n===UNAME===\n'
uname -s 2>/dev/null
uname -r 2>/dev/null
uname -m 2>/dev/null
printf '\n===OS_RELEASE===\n'
cat /etc/os-release 2>/dev/null
printf '\n===UPTIME===\n'
cat /proc/uptime 2>/dev/null
printf '\n===TIMEZONE===\n'
( timedatectl show --property=Timezone --value 2>/dev/null ) \
  || cat /etc/timezone 2>/dev/null \
  || ( readlink -f /etc/localtime 2>/dev/null | sed 's|.*/zoneinfo/||' )
printf '\n===MACHINE_ID===\n'
cat /etc/machine-id 2>/dev/null
printf '\n===VIRT===\n'
systemd-detect-virt 2>/dev/null || echo none
printf '\n===PRIMARY_IP===\n'
( ip route get 8.8.8.8 2>/dev/null | awk '{for(i=1;i<=NF;i++)if($i=="src"){print $(i+1);exit}}' ) \
  || ( hostname -I 2>/dev/null | awk '{print $1}' )
printf '\n===PUBLIC_IP===\n'
curl -fsS --max-time 2 https://api.ipify.org 2>/dev/null \
  || dig +short +time=1 +tries=1 myip.opendns.com @resolver1.opendns.com 2>/dev/null \
  || echo ""
printf '\n===END===\n'
`
	out, err := runOutput(ctx, client, script)
	if err != nil && out == "" {
		return HostInfo{}, err
	}
	blocks := splitByMarkers(out, []string{
		"===HOSTNAME===", "===FQDN===", "===UNAME===", "===OS_RELEASE===",
		"===UPTIME===", "===TIMEZONE===", "===MACHINE_ID===", "===VIRT===",
		"===PRIMARY_IP===", "===PUBLIC_IP===", "===END===",
	})

	hostname := firstLine(blocks["===HOSTNAME==="])
	fqdn := firstLine(blocks["===FQDN==="])
	if fqdn == hostname {
		fqdn = ""
	}

	unameLines := strings.Split(blocks["===UNAME==="], "\n")
	osKernel := getLine(unameLines, 0) // "Linux"
	kernel := getLine(unameLines, 1)   // "5.15.0-..."
	arch := getLine(unameLines, 2)     // "x86_64"

	osRelease := parseKV(blocks["===OS_RELEASE==="], "=")
	platform := osRelease["ID"]                 // "ubuntu"
	platformFamily := osRelease["ID_LIKE"]      // "debian"
	platformVersion := osRelease["VERSION_ID"]  // "22.04"
	// PRETTY_NAME (fallback): "Ubuntu 22.04.4 LTS" — на случай если нет VERSION_ID.
	if platformVersion == "" {
		platformVersion = osRelease["PRETTY_NAME"]
	}

	uptimeSeconds := uint64(0)
	bootTime := time.Time{}
	if up := strings.Fields(blocks["===UPTIME==="]); len(up) > 0 {
		secs := atofSafe(up[0])
		uptimeSeconds = uint64(secs)
		bootTime = time.Now().UTC().Add(-time.Duration(secs * float64(time.Second)))
	}

	timezone := firstLine(blocks["===TIMEZONE==="])
	machineID := firstLine(blocks["===MACHINE_ID==="])

	virtSystem := firstLine(blocks["===VIRT==="])
	virtRole := ""
	if virtSystem != "" && virtSystem != "none" {
		virtRole = "guest"
	} else {
		virtSystem = ""
	}

	primaryIP := firstLine(blocks["===PRIMARY_IP==="])
	publicIP := firstLine(blocks["===PUBLIC_IP==="])

	// Country по public IP — локально через встроенную mmdb.
	countryCode, country := "", ""
	if c.geo != nil && publicIP != "" {
		if ci, ok := c.geo.Lookup(publicIP); ok {
			countryCode = ci.Code
			country = ci.Name
		}
	}

	return HostInfo{
		Hostname:             hostname,
		FQDN:                 fqdn,
		PrimaryIP:            primaryIP,
		PublicIP:             publicIP,
		CountryCode:          countryCode,
		Country:              country,
		OS:                   strings.ToLower(osKernel),
		Platform:             platform,
		PlatformFamily:       platformFamily,
		PlatformVersion:      platformVersion,
		KernelVersion:        kernel,
		KernelArch:           arch,
		VirtualizationSystem: virtSystem,
		VirtualizationRole:   virtRole,
		BootTime:             bootTime,
		UptimeSeconds:        uptimeSeconds,
		HostID:               machineID,
		Timezone:             timezone,
	}, nil
}

// splitByMarkers режет текст на блоки по упорядоченному списку маркеров.
// Маркер хранится как ключ; значение — это содержимое до следующего маркера.
//
// Парсер устойчив к «приклеенным» маркерам: если строка не равна маркеру
// целиком, но ИМЕЕТ суффикс одного из маркеров (например, "1.2.3.4===END==="
// — это происходит, когда предыдущая команда не вывела trailing newline,
// а следующий printf маркера выводит свой подряд), то парсер отделяет
// маркер от данных. Префикс попадает в текущий блок, маркер открывает следующий.
func splitByMarkers(text string, markers []string) map[string]string {
	out := make(map[string]string, len(markers))
	if text == "" {
		return out
	}
	markerSet := make(map[string]struct{}, len(markers))
	for _, m := range markers {
		markerSet[m] = struct{}{}
	}

	lines := strings.Split(text, "\n")
	current := ""
	var buf []string
	flush := func() {
		if current != "" {
			out[current] = strings.TrimSpace(strings.Join(buf, "\n"))
		}
		buf = buf[:0]
	}

	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)

		// 1) Полное совпадение строки с маркером — стандартный случай.
		if _, ok := markerSet[trimmed]; ok {
			flush()
			current = trimmed
			continue
		}

		// 2) Маркер «приклеился» к концу строки (нет newline между данными
		// и маркером). Отделяем префикс-данные от маркера.
		if marker, prefix := splitTrailingMarker(trimmed, markerSet); marker != "" {
			if prefix != "" {
				buf = append(buf, prefix)
			}
			flush()
			current = marker
			continue
		}

		buf = append(buf, ln)
	}
	flush()
	return out
}

// splitTrailingMarker проверяет, не оканчивается ли строка одним из маркеров.
// Если да — возвращает (marker, prefix) с уже trim'нутым префиксом. Иначе ("", "").
func splitTrailingMarker(s string, markerSet map[string]struct{}) (marker, prefix string) {
	for m := range markerSet {
		if len(s) > len(m) && strings.HasSuffix(s, m) {
			return m, strings.TrimSpace(s[:len(s)-len(m)])
		}
	}
	return "", ""
}

func firstLine(s string) string {
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func getLine(lines []string, i int) string {
	if i >= 0 && i < len(lines) {
		return strings.TrimSpace(lines[i])
	}
	return ""
}
