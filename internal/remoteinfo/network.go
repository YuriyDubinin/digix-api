package remoteinfo

import (
	"context"
	"encoding/json"
	"strings"

	"golang.org/x/crypto/ssh"
)

// collectNetwork собирает интерфейсы (через `ip -j addr`, JSON-формат iproute2),
// IO-счётчики (`/proc/net/dev`) и общее число открытых соединений (`ss -s`).
func (c *Collector) collectNetwork(ctx context.Context, client *ssh.Client) (NetworkInfo, error) {
	// printf '\n===<NAME>===\n' с ведущим \n — на случай, если предыдущая
	// команда (например, ip -j addr на старых iproute2) выдаёт вывод без
	// trailing newline; без ведущего \n маркер «приклеится» к данным.
	const script = `printf '===IP_ADDR===\n'
ip -j addr 2>/dev/null
printf '\n===NET_DEV===\n'
cat /proc/net/dev 2>/dev/null
printf '\n===SS===\n'
ss -s 2>/dev/null || ( cat /proc/net/sockstat 2>/dev/null; cat /proc/net/sockstat6 2>/dev/null )
printf '\n===END===\n'
`
	out, err := runOutput(ctx, client, script)
	if err != nil && out == "" {
		return NetworkInfo{}, err
	}
	blocks := splitByMarkers(out, []string{
		"===IP_ADDR===", "===NET_DEV===", "===SS===", "===END===",
	})

	interfaces := parseIPAddrJSON(blocks["===IP_ADDR==="])
	ioCounters := parseNetDev(blocks["===NET_DEV==="])
	conns := parseConnCount(blocks["===SS==="])

	return NetworkInfo{
		Interfaces:       interfaces,
		IOCounters:       ioCounters,
		ConnectionsCount: conns,
	}, nil
}

// parseIPAddrJSON разбирает вывод `ip -j addr` (iproute2). Формат стабилен
// от версии 4.10+ (2017 г.) — мы поддерживаем поля: ifname, mtu, flags,
// address (MAC), addr_info[].{family, local, prefixlen}.
func parseIPAddrJSON(text string) []NetInterface {
	if text == "" {
		return nil
	}
	var raw []struct {
		IfName    string   `json:"ifname"`
		MTU       int      `json:"mtu"`
		Flags     []string `json:"flags"`
		Address   string   `json:"address"`
		AddrInfo  []struct {
			Family    string `json:"family"`     // "inet" | "inet6"
			Local     string `json:"local"`
			PrefixLen int    `json:"prefixlen"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil
	}
	out := make([]NetInterface, 0, len(raw))
	for _, r := range raw {
		iface := NetInterface{
			Name:         r.IfName,
			HardwareAddr: r.Address,
			MTU:          r.MTU,
			Flags:        r.Flags,
		}
		for _, a := range r.AddrInfo {
			fam := "ipv4"
			if a.Family == "inet6" {
				fam = "ipv6"
			}
			iface.Addresses = append(iface.Addresses, NetAddr{Addr: a.Local, Family: fam})
		}
		out = append(out, iface)
	}
	return out
}

// parseNetDev разбирает /proc/net/dev. Формат:
//
//	Inter-|   Receive                                                |  Transmit
//	 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
//	    lo: 12345    100    0    0    ...                              12345    100    ...
func parseNetDev(text string) []NetIOCounters {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= 2 {
		return nil
	}
	out := make([]NetIOCounters, 0, len(lines)-2)
	for _, line := range lines[2:] {
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:i])
		f := strings.Fields(line[i+1:])
		if len(f) < 16 {
			continue
		}
		out = append(out, NetIOCounters{
			Name:        name,
			BytesRecv:   atou64Safe(f[0]),
			PacketsRecv: atou64Safe(f[1]),
			ErrIn:       atou64Safe(f[2]),
			DropIn:      atou64Safe(f[3]),
			BytesSent:   atou64Safe(f[8]),
			PacketsSent: atou64Safe(f[9]),
			ErrOut:      atou64Safe(f[10]),
			DropOut:     atou64Safe(f[11]),
		})
	}
	return out
}

// parseConnCount извлекает «total» из вывода `ss -s`:
//
//	Total: 240 (kernel 0)
//	TCP:   25 (estab 5, closed 17, orphaned 0, synrecv 0, timewait 17/0), ports 0
//	...
//
// Если ss отсутствует, используем сумму TCP/UDP из /proc/net/sockstat
// (там есть строки вида "TCP: inuse 5 orphan 0 tw 17 alloc 12 mem 1").
func parseConnCount(text string) int {
	if text == "" {
		return 0
	}
	// Сначала пробуем «ss -s» формат — это самый информативный источник.
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Total:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				return atoiSafe(f[1])
			}
		}
	}
	// Fallback: суммируем TCP/UDP inuse из sockstat.
	total := 0
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		// "TCP: inuse 5 orphan 0 ..." → f[0]=TCP:, f[1]=inuse, f[2]=N
		if len(f) >= 3 && (f[0] == "TCP:" || f[0] == "TCP6:" || f[0] == "UDP:" || f[0] == "UDP6:") && f[1] == "inuse" {
			total += atoiSafe(f[2])
		}
	}
	return total
}
