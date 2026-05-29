package remoteinfo

import (
	"testing"
)

func TestParseCPUInfo(t *testing.T) {
	const sample = `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6
model		: 142
model name	: Intel(R) Core(TM) i7-8650U CPU @ 1.90GHz
stepping	: 10
cpu MHz		: 2110.000
cache size	: 8192 KB
core id		: 0
flags		: fpu vme sse sse2 avx avx2

processor	: 1
vendor_id	: GenuineIntel
cpu family	: 6
model		: 142
model name	: Intel(R) Core(TM) i7-8650U CPU @ 1.90GHz
stepping	: 10
cpu MHz		: 2300.000
cache size	: 8192 KB
core id		: 0
flags		: fpu vme sse sse2 avx avx2

processor	: 2
vendor_id	: GenuineIntel
cpu family	: 6
model		: 142
model name	: Intel(R) Core(TM) i7-8650U CPU @ 1.90GHz
stepping	: 10
cpu MHz		: 2200.000
cache size	: 8192 KB
core id		: 1
flags		: fpu vme sse sse2 avx avx2
`
	got := parseCPUInfo(sample)
	if got.ModelName != "Intel(R) Core(TM) i7-8650U CPU @ 1.90GHz" {
		t.Errorf("ModelName=%q", got.ModelName)
	}
	if got.Vendor != "GenuineIntel" {
		t.Errorf("Vendor=%q", got.Vendor)
	}
	if got.Family != "6" {
		t.Errorf("Family=%q", got.Family)
	}
	if got.Stepping != 10 {
		t.Errorf("Stepping=%d", got.Stepping)
	}
	if got.MHz < 2100 || got.MHz > 2200 {
		t.Errorf("MHz=%v", got.MHz)
	}
	if got.CacheSizeKB != 8192 {
		t.Errorf("CacheSizeKB=%d", got.CacheSizeKB)
	}
	if got.LogicalCores != 3 {
		t.Errorf("LogicalCores=%d, want 3", got.LogicalCores)
	}
	if got.PhysicalCores != 2 {
		t.Errorf("PhysicalCores=%d, want 2 (unique core ids 0,1)", got.PhysicalCores)
	}
	if len(got.Flags) == 0 {
		t.Errorf("Flags is empty")
	}
}

func TestParseMeminfo(t *testing.T) {
	const sample = `MemTotal:       16384000 kB
MemFree:         1024000 kB
MemAvailable:    8192000 kB
Buffers:          512000 kB
Cached:          4096000 kB
Shmem:            256000 kB
Slab:             128000 kB
SwapTotal:       2097152 kB
SwapFree:        2000000 kB
NotANumber:      garbage
`
	kv := parseMeminfo(sample)
	if kv["MemTotal"] != 16384000*1024 {
		t.Errorf("MemTotal=%d", kv["MemTotal"])
	}
	if kv["MemAvailable"] != 8192000*1024 {
		t.Errorf("MemAvailable=%d", kv["MemAvailable"])
	}
	if kv["SwapTotal"] != 2097152*1024 {
		t.Errorf("SwapTotal=%d", kv["SwapTotal"])
	}
}

func TestParseProcStat_DeltaPercent(t *testing.T) {
	const stat1 = `cpu  100 0 50 850 0 0 0 0 0 0
cpu0 50 0 25 425 0 0 0 0 0 0
cpu1 50 0 25 425 0 0 0 0 0 0
intr 12345
`
	const stat2 = `cpu  200 0 100 900 0 0 0 0 0 0
cpu0 100 0 50 450 0 0 0 0 0 0
cpu1 100 0 50 450 0 0 0 0 0 0
intr 23456
`
	t1, _ := parseProcStat(stat1)
	t2, perCore2 := parseProcStat(stat2)
	got := cpuDeltaPercent(t1, t2)
	// total delta = 1200-1000 = 200, idle delta = 900-850 = 50 → busy = (200-50)/200 = 75%
	if got < 74 || got > 76 {
		t.Errorf("usage=%.2f, want ~75%%", got)
	}
	if len(perCore2) != 2 {
		t.Errorf("perCore=%d, want 2", len(perCore2))
	}
}

func TestParseDF(t *testing.T) {
	const dfBytes = `Filesystem     Type     1B-blocks       Used   Available Capacity Mounted on
/dev/sda1      ext4   500000000000 100000000000 400000000000      20% /
tmpfs          tmpfs    8000000000           0   8000000000       0% /dev/shm
/dev/sda2      ext4   100000000000  50000000000  50000000000      50% /var
`
	const dfInodes = `Filesystem     Type     Inodes IUsed   IFree IUse% Mounted on
/dev/sda1      ext4   1000000 100000 900000   10% /
tmpfs          tmpfs   500000      0 500000    0% /dev/shm
/dev/sda2      ext4    500000  50000 450000   10% /var
`
	const mounts = `/dev/sda1 / ext4 rw,relatime 0 0
tmpfs /dev/shm tmpfs rw,nosuid,nodev 0 0
/dev/sda2 /var ext4 rw,relatime,noexec 0 0
`
	got := parseDF(dfBytes, dfInodes, mounts)
	if len(got) != 2 {
		t.Fatalf("partitions=%d, want 2 (tmpfs отфильтрован)", len(got))
	}
	root := got[0]
	if root.Mountpoint != "/" || root.Device != "/dev/sda1" || root.Fstype != "ext4" {
		t.Errorf("root partition wrong: %+v", root)
	}
	if root.TotalBytes != 500_000_000_000 {
		t.Errorf("root.TotalBytes=%d", root.TotalBytes)
	}
	if root.UsedPercent < 19 || root.UsedPercent > 21 {
		t.Errorf("root.UsedPercent=%v, want ~20", root.UsedPercent)
	}
	if root.InodesTotal != 1_000_000 {
		t.Errorf("root.InodesTotal=%d", root.InodesTotal)
	}
	if root.Opts != "rw,relatime" {
		t.Errorf("root.Opts=%q", root.Opts)
	}
}

func TestParseIPAddrJSON(t *testing.T) {
	const sample = `[
  {
    "ifindex": 1,
    "ifname": "lo",
    "flags": ["LOOPBACK","UP","LOWER_UP"],
    "mtu": 65536,
    "address": "00:00:00:00:00:00",
    "addr_info": [
      {"family":"inet","local":"127.0.0.1","prefixlen":8},
      {"family":"inet6","local":"::1","prefixlen":128}
    ]
  },
  {
    "ifindex": 2,
    "ifname": "eth0",
    "flags": ["BROADCAST","MULTICAST","UP","LOWER_UP"],
    "mtu": 1500,
    "address": "02:42:ac:11:00:02",
    "addr_info": [
      {"family":"inet","local":"172.17.0.2","prefixlen":16}
    ]
  }
]`
	got := parseIPAddrJSON(sample)
	if len(got) != 2 {
		t.Fatalf("interfaces=%d, want 2", len(got))
	}
	if got[1].Name != "eth0" || got[1].MTU != 1500 {
		t.Errorf("eth0 wrong: %+v", got[1])
	}
	if len(got[1].Addresses) != 1 || got[1].Addresses[0].Addr != "172.17.0.2" || got[1].Addresses[0].Family != "ipv4" {
		t.Errorf("eth0 addresses wrong: %+v", got[1].Addresses)
	}
	if got[0].Addresses[1].Family != "ipv6" {
		t.Errorf("lo ipv6 family wrong: %+v", got[0].Addresses[1])
	}
}

func TestParseConnCount(t *testing.T) {
	const ssOut = `Total: 240 (kernel 0)
TCP:   25 (estab 5, closed 17)
Transport Total     IP        IPv6
*	    0         -         -
RAW	    0         0         0
UDP	    8         5         3
TCP	    8         5         3
`
	if got := parseConnCount(ssOut); got != 240 {
		t.Errorf("conn count=%d, want 240", got)
	}

	const sockstatOut = `sockets: used 250
TCP: inuse 25 orphan 0 tw 17 alloc 30 mem 1
UDP: inuse 5 mem 0
`
	if got := parseConnCount(sockstatOut); got != 30 {
		t.Errorf("conn count fallback=%d, want 30 (25 TCP + 5 UDP)", got)
	}
}

func TestSplitByMarkers(t *testing.T) {
	const sample = `===A===
line1
line2
===B===
line3
===C===
===END===
`
	got := splitByMarkers(sample, []string{"===A===", "===B===", "===C===", "===END==="})
	if got["===A==="] != "line1\nline2" {
		t.Errorf("A=%q", got["===A==="])
	}
	if got["===B==="] != "line3" {
		t.Errorf("B=%q", got["===B==="])
	}
	if got["===C==="] != "" {
		t.Errorf("C=%q (want empty)", got["===C==="])
	}
}

// TestSplitByMarkers_TrailingMarker — регресс на баг с «приклеенным» маркером:
// предыдущая команда (например, `curl https://api.ipify.org`) не вывела
// trailing newline, и следующий printf маркера оказался в той же строке.
// Парсер должен корректно отделить маркер от данных.
func TestSplitByMarkers_TrailingMarker(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		wantA     string
		wantEnd   bool // должен ли быть распознан ===END===
		wantValue string
	}{
		{
			name:      "stuck_end_to_value",
			text:      "===A===\n5.45.207.140===END===\n",
			wantA:     "5.45.207.140",
			wantEnd:   true,
			wantValue: "",
		},
		{
			name:      "stuck_end_to_empty_block",
			text:      "===A===\n===END===\n",
			wantA:     "",
			wantEnd:   true,
			wantValue: "",
		},
		{
			name:      "normal_with_newline",
			text:      "===A===\n5.45.207.140\n===END===\n",
			wantA:     "5.45.207.140",
			wantEnd:   true,
			wantValue: "",
		},
		{
			name:      "json_with_no_trailing_newline",
			text:      `===A===` + "\n" + `[{"x":1}]===END===` + "\n",
			wantA:     `[{"x":1}]`,
			wantEnd:   true,
			wantValue: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitByMarkers(tc.text, []string{"===A===", "===END==="})
			if got["===A==="] != tc.wantA {
				t.Errorf("A=%q, want %q", got["===A==="], tc.wantA)
			}
			_, hasEnd := got["===END==="]
			if hasEnd != tc.wantEnd {
				t.Errorf("END present=%v, want %v", hasEnd, tc.wantEnd)
			}
		})
	}
}

// TestSplitByMarkers_PublicIP_Realistic — реальный сценарий бага: curl вернул
// IP без trailing newline, и маркер ===END=== приклеился. Парсер должен
// корректно извлечь чистый IP, а не "5.45.207.140===END===".
func TestSplitByMarkers_PublicIP_Realistic(t *testing.T) {
	// Симулируем фрагмент выхода скрипта host.go ДО фикса (без ведущего \n
	// у ===END===, как было раньше). Парсер всё равно должен справиться
	// благодаря splitTrailingMarker.
	const text = "===PUBLIC_IP===\n5.45.207.140===END===\n"
	got := splitByMarkers(text, []string{"===PUBLIC_IP===", "===END==="})
	if got["===PUBLIC_IP==="] != "5.45.207.140" {
		t.Errorf("PUBLIC_IP=%q, want %q", got["===PUBLIC_IP==="], "5.45.207.140")
	}
	if _, ok := got["===END==="]; !ok {
		t.Errorf("===END=== not recognized as a marker")
	}
	// Главное — IP пригоден для net.ParseIP / geo.Lookup:
	publicIP := firstLine(got["===PUBLIC_IP==="])
	if publicIP != "5.45.207.140" {
		t.Errorf("firstLine of public_ip=%q, want clean IP", publicIP)
	}
}

func TestIsPartition(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"sda", false},
		{"sda1", true},
		{"sdb15", true},
		{"vda", false},
		{"vda1", true},
		{"nvme0n1", false},
		{"nvme0n1p1", true},
		{"mmcblk0", false},
		{"mmcblk0p1", true},
	}
	for _, tc := range cases {
		if got := isPartition(tc.name); got != tc.want {
			t.Errorf("isPartition(%q)=%v, want %v", tc.name, got, tc.want)
		}
	}
}
