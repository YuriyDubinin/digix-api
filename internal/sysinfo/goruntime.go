package sysinfo

import (
	"context"
	"runtime"
	"runtime/debug"
	"time"
)

func (c *Collector) collectGoRuntime(_ context.Context) GoRuntimeInfo {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	gcPercent := debug.SetGCPercent(-1)
	debug.SetGCPercent(gcPercent) // вернуть назад — SetGCPercent одновременно читает и пишет.

	out := GoRuntimeInfo{
		Version:       runtime.Version(),
		Compiler:      runtime.Compiler,
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		GOROOT:        runtime.GOROOT(),
		GOMAXPROCS:    runtime.GOMAXPROCS(0),
		NumGoroutines: runtime.NumGoroutine(),
		NumCgoCalls:   runtime.NumCgoCall(),
		Memory: GoMemoryStats{
			AllocBytes:      ms.Alloc,
			TotalAllocBytes: ms.TotalAlloc,
			SysBytes:        ms.Sys,
			HeapAllocBytes:  ms.HeapAlloc,
			HeapSysBytes:    ms.HeapSys,
			HeapIdleBytes:   ms.HeapIdle,
			HeapInuseBytes:  ms.HeapInuse,
			HeapObjects:     ms.HeapObjects,
			StackInuseBytes: ms.StackInuse,
			StackSysBytes:   ms.StackSys,
			NextGCBytes:     ms.NextGC,
			Mallocs:         ms.Mallocs,
			Frees:           ms.Frees,
		},
		GC: GoGCStats{
			NumGC:        ms.NumGC,
			NumForcedGC:  ms.NumForcedGC,
			TotalPauseNs: ms.PauseTotalNs,
			CPUFraction:  ms.GCCPUFraction,
			GCPercent:    gcPercent,
		},
	}

	if ms.LastGC > 0 {
		out.GC.LastGCAt = time.Unix(0, int64(ms.LastGC)).UTC()
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		out.BuildInfo.MainModule = bi.Main.Path
		out.BuildInfo.MainVersion = bi.Main.Version
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				out.BuildInfo.VCSRevision = s.Value
			case "vcs.time":
				out.BuildInfo.VCSTime = s.Value
			case "vcs.modified":
				out.BuildInfo.VCSModified = s.Value == "true"
			}
		}
	}

	return out
}
