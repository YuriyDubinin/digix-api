package remotesystemd

import (
	"testing"

	"github.com/YuriyDubinin/dijex-api/internal/systemd"
)

func TestParseManager(t *testing.T) {
	ver := "systemd 252 (252.5-2ubuntu3)\n+PAM +AUDIT +SELINUX +APPARMOR +IMA"
	arch := "x86_64"
	m := parseManager(ver, arch)
	if m == nil {
		t.Fatalf("nil manager")
	}
	if m.Version != "252" {
		t.Errorf("version=%q", m.Version)
	}
	if m.Architecture != "x86_64" {
		t.Errorf("arch=%q", m.Architecture)
	}
}

func TestParseManager_Empty(t *testing.T) {
	if m := parseManager("", ""); m != nil {
		t.Errorf("expected nil for empty version")
	}
}

func TestParseListUnits(t *testing.T) {
	const sample = `accounts-daemon.service                 loaded active   running Accounts Service
cron.service                            loaded active   running Regular background program processing daemon
docker.service                          loaded active   running Docker Application Container Engine
some-broken.service                     loaded failed   failed  Some Broken Service
not-a-service.target                    loaded active   active  Not parsed
ssh.service                             loaded active   running OpenBSD Secure Shell server
`
	services := parseListUnits(sample)
	if len(services) != 5 {
		t.Fatalf("services=%d, want 5 (.target отфильтрован)", len(services))
	}
	if services[0].Name != "accounts-daemon.service" {
		t.Errorf("first=%q", services[0].Name)
	}
	if services[3].ActiveState != "failed" || services[3].SubState != "failed" {
		t.Errorf("failed wrong: %+v", services[3])
	}
	if services[2].Description != "Docker Application Container Engine" {
		t.Errorf("docker description=%q", services[2].Description)
	}
}

func TestEnrichFromShow(t *testing.T) {
	const sample = `Names=docker.service
Description=Docker Application Container Engine
LoadState=loaded
ActiveState=active
SubState=running
UnitFileState=enabled
Type=notify
MainPID=1042
Result=success
User=root
Group=docker
FragmentPath=/lib/systemd/system/docker.service
ActiveEnterTimestamp=Wed 2026-05-22 09:14:01 UTC
ExecMainStartTimestamp=Wed 2026-05-22 09:14:02 UTC
NRestarts=2
MemoryCurrent=52428800
MemoryPeak=104857600
CPUUsageNSec=15234567890
TasksCurrent=18
TasksMax=18446744073709551615
`
	services := []systemd.Service{
		{Name: "docker.service"},
		{Name: "cron.service"}, // в выводе show отсутствует — должен остаться нетронутым
	}
	enrichFromShow(sample, services)

	got := services[0]
	if got.MainPID != 1042 {
		t.Errorf("MainPID=%d", got.MainPID)
	}
	if !got.Enabled || got.UnitFileState != "enabled" {
		t.Errorf("enabled wrong: %+v", got)
	}
	if got.Type != "notify" || got.User != "root" || got.Group != "docker" {
		t.Errorf("type/user/group wrong: %+v", got)
	}
	if got.NRestarts != 2 {
		t.Errorf("nrestarts=%d", got.NRestarts)
	}
	if got.MemoryCurrentBytes != 52_428_800 || got.MemoryPeakBytes != 104_857_600 {
		t.Errorf("memory wrong: %+v", got)
	}
	if got.TasksMax != -1 { // MaxUint64 → -1
		t.Errorf("TasksMax=%d, want -1 (infinity)", got.TasksMax)
	}
	if got.ActiveEnterAt == nil || got.ExecMainStartAt == nil {
		t.Errorf("timestamps not parsed: active=%v exec=%v", got.ActiveEnterAt, got.ExecMainStartAt)
	}
	if got.UptimeSeconds <= 0 {
		t.Errorf("uptime not computed: %v", got.UptimeSeconds)
	}

	if services[1].MainPID != 0 || services[1].Type != "" {
		t.Errorf("cron should be untouched: %+v", services[1])
	}
}

func TestParseSystemdTimestamp(t *testing.T) {
	cases := []struct {
		in   string
		want bool // ожидаем непустой результат
	}{
		{"Wed 2026-05-22 09:14:01 UTC", true},
		{"2026-05-22 09:14:01 UTC", true},
		{"2026-05-22 09:14:01", true},
		{"", false},
		{"n/a", false},
		{"garbage", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := parseSystemdTimestamp(tc.in)
			if (got != nil) != tc.want {
				t.Errorf("got=%v, want non-nil=%v", got, tc.want)
			}
		})
	}
}

func TestUnsetMax(t *testing.T) {
	if got := unsetMax(0); got != 0 {
		t.Errorf("0 → %d", got)
	}
	if got := unsetMax(1234); got != 1234 {
		t.Errorf("1234 → %d", got)
	}
	if got := unsetMax(^uint64(0)); got != -1 {
		t.Errorf("MaxUint64 → %d, want -1", got)
	}
}
