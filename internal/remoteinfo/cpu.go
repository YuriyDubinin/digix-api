package remoteinfo

import (
	"context"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// cpuSampleInterval — задержка между двумя снимками /proc/stat, по которым
// считается %CPU. 300мс — тот же компромисс, что и в локальном sysinfo.
const cpuSampleInterval = 300 * time.Millisecond

// collectCPU читает /proc/cpuinfo, /proc/loadavg и два снимка /proc/stat для
// расчёта %CPU. Один сводный скрипт + sleep между снимками — два round-trip
// по SSH (для total и per-core).
func (c *Collector) collectCPU(ctx context.Context, client *ssh.Client) (CPUInfo, error) {
	// printf '\n===<NAME>===\n' с ведущим \n — единый защитный стиль на случай
	// команды без trailing newline (см. splitTrailingMarker).
	const script = `printf '===CPUINFO===\n'
cat /proc/cpuinfo 2>/dev/null
printf '\n===LOADAVG===\n'
cat /proc/loadavg 2>/dev/null
printf '\n===STAT1===\n'
cat /proc/stat 2>/dev/null
printf '\n===SLEEP===\n'
sleep 0.3
printf '\n===STAT2===\n'
cat /proc/stat 2>/dev/null
printf '\n===END===\n'
`
	out, err := runOutput(ctx, client, script)
	if err != nil && out == "" {
		return CPUInfo{}, err
	}
	blocks := splitByMarkers(out, []string{
		"===CPUINFO===", "===LOADAVG===", "===STAT1===", "===SLEEP===", "===STAT2===", "===END===",
	})

	info := parseCPUInfo(blocks["===CPUINFO==="])

	if la := strings.Fields(blocks["===LOADAVG==="]); len(la) >= 3 {
		info.LoadAvg1 = atofSafe(la[0])
		info.LoadAvg5 = atofSafe(la[1])
		info.LoadAvg15 = atofSafe(la[2])
	}

	total1, perCore1 := parseProcStat(blocks["===STAT1==="])
	total2, perCore2 := parseProcStat(blocks["===STAT2==="])
	info.UsagePercent = cpuDeltaPercent(total1, total2)
	if len(perCore1) == len(perCore2) {
		out := make([]float64, len(perCore1))
		for i := range perCore1 {
			out[i] = cpuDeltaPercent(perCore1[i], perCore2[i])
		}
		info.PerCoreUsagePercent = out
	}

	return info, nil
}

// parseCPUInfo разбирает /proc/cpuinfo (формат «ключ : значение», абзацы
// разделены пустой строкой = по одному абзацу на logical-CPU). Берём из
// первого абзаца modelName/vendor/family/model/stepping/MHz/cache/flags.
// Логические/физические ядра считаем по абзацам и уникальным "core id".
func parseCPUInfo(text string) CPUInfo {
	if text == "" {
		return CPUInfo{}
	}

	paragraphs := strings.Split(text, "\n\n")
	out := CPUInfo{}
	out.LogicalCores = len(paragraphs)

	uniqCoreIDs := map[string]struct{}{}
	for i, p := range paragraphs {
		kv := parseKV(p, ":")
		if cid, ok := kv["core id"]; ok {
			uniqCoreIDs[cid] = struct{}{}
		}
		if i != 0 {
			continue
		}
		out.ModelName = kv["model name"]
		out.Vendor = kv["vendor_id"]
		out.Family = kv["cpu family"]
		out.Model = kv["model"]
		out.Stepping = int32(atoiSafe(kv["stepping"]))
		out.MHz = atofSafe(kv["cpu MHz"])
		if cs := kv["cache size"]; cs != "" {
			out.CacheSizeKB = int32(atoiSafe(strings.TrimSuffix(cs, " KB")))
		}
		if flags := kv["flags"]; flags != "" {
			out.Flags = strings.Fields(flags)
		}
	}
	if n := len(uniqCoreIDs); n > 0 {
		out.PhysicalCores = n
	} else {
		out.PhysicalCores = out.LogicalCores
	}
	// Хвостовой пустой абзац после последнего \n\n — нормально, корректируем.
	if out.LogicalCores > 0 && strings.TrimSpace(paragraphs[len(paragraphs)-1]) == "" {
		out.LogicalCores--
	}
	return out
}

// cpuTotals — суммарные времена из строки /proc/stat (jiffies).
type cpuTotals struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
}

func (t cpuTotals) sum() uint64 {
	return t.user + t.nice + t.system + t.idle + t.iowait + t.irq + t.softirq + t.steal
}

func (t cpuTotals) idleAll() uint64 { return t.idle + t.iowait }

// parseProcStat возвращает суммарную статистику (строка "cpu  ...") и срез
// per-core ("cpu0", "cpu1", ...). Если файл пустой — возвращает zero-values.
func parseProcStat(text string) (cpuTotals, []cpuTotals) {
	var total cpuTotals
	var perCore []cpuTotals
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		ct := cpuTotals{
			user:   atou64Safe(f[1]),
			nice:   atou64Safe(f[2]),
			system: atou64Safe(f[3]),
			idle:   atou64Safe(f[4]),
		}
		if len(f) > 5 {
			ct.iowait = atou64Safe(f[5])
		}
		if len(f) > 6 {
			ct.irq = atou64Safe(f[6])
		}
		if len(f) > 7 {
			ct.softirq = atou64Safe(f[7])
		}
		if len(f) > 8 {
			ct.steal = atou64Safe(f[8])
		}
		if f[0] == "cpu" {
			total = ct
		} else {
			perCore = append(perCore, ct)
		}
	}
	return total, perCore
}

// cpuDeltaPercent считает %CPU между двумя снимками. Возвращает 0 при невалидных
// дельтах (например, оба снимка идентичны или счётчик откатился).
func cpuDeltaPercent(prev, cur cpuTotals) float64 {
	prevTotal := prev.sum()
	curTotal := cur.sum()
	if curTotal <= prevTotal {
		return 0
	}
	totalDelta := curTotal - prevTotal
	idleDelta := cur.idleAll() - prev.idleAll()
	if idleDelta > totalDelta {
		return 0
	}
	busy := float64(totalDelta-idleDelta) / float64(totalDelta) * 100.0
	if busy < 0 {
		busy = 0
	}
	if busy > 100 {
		busy = 100
	}
	return busy
}
