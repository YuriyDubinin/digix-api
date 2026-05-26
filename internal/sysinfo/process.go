package sysinfo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

func (c *Collector) collectProcess(ctx context.Context) (ProcessInfo, error) {
	pid := int32(os.Getpid())
	p, err := process.NewProcessWithContext(ctx, pid)
	if err != nil {
		return ProcessInfo{}, fmt.Errorf("process: %w", err)
	}

	out := ProcessInfo{PID: pid}

	if v, err := p.PpidWithContext(ctx); err == nil {
		out.PPID = v
	}
	if v, err := p.NameWithContext(ctx); err == nil {
		out.Name = v
	}
	if v, err := p.ExeWithContext(ctx); err == nil {
		out.Exe = v
	}
	if v, err := p.CmdlineSliceWithContext(ctx); err == nil {
		out.Cmdline = strings.Join(v, " ")
	}
	if v, err := p.CwdWithContext(ctx); err == nil {
		out.Cwd = v
	}
	if v, err := p.UsernameWithContext(ctx); err == nil {
		out.Username = v
	}
	if v, err := p.CreateTimeWithContext(ctx); err == nil {
		// CreateTime — unix epoch в миллисекундах.
		started := time.UnixMilli(v).UTC()
		out.StartedAt = started
		out.UptimeSeconds = time.Since(started).Seconds()
	}
	if mInfo, err := p.MemoryInfoWithContext(ctx); err == nil && mInfo != nil {
		out.MemoryRSSBytes = mInfo.RSS
		out.MemoryVMSBytes = mInfo.VMS
	}
	if v, err := p.MemoryPercentWithContext(ctx); err == nil {
		out.MemoryPercent = v
	}
	if v, err := p.CPUPercentWithContext(ctx); err == nil {
		out.CPUPercent = v
	}
	if v, err := p.NumThreadsWithContext(ctx); err == nil {
		out.NumThreads = v
	}
	if v, err := p.NumFDsWithContext(ctx); err == nil {
		out.NumFDs = v
	}
	if v, err := p.NiceWithContext(ctx); err == nil {
		out.Nice = v
	}

	return out, nil
}
