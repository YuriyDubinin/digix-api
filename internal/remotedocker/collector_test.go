package remotedocker

import (
	"testing"
	"time"
)

func TestParseEngine(t *testing.T) {
	verRaw := `{"Version":"27.1.1","ApiVersion":"1.46","GitCommit":"abc123","GoVersion":"go1.22.5","KernelVersion":"5.15.0-105-generic","Os":"linux","Arch":"amd64"}`
	infoRaw := `{"ID":"NODE:HASH","Containers":5,"ContainersRunning":3,"ContainersPaused":0,"ContainersStopped":2,"Images":12,"Driver":"overlay2","MemTotal":16777216000,"NCPU":4,"Name":"app01","OperatingSystem":"Ubuntu 22.04.4 LTS","OSType":"linux","Architecture":"x86_64","KernelVersion":"5.15.0-105-generic","CgroupVersion":"2"}`

	var errs []string
	e := parseEngine(verRaw, infoRaw, &errs)
	if e == nil {
		t.Fatalf("engine is nil")
	}
	if e.Version != "27.1.1" || e.APIVersion != "1.46" {
		t.Errorf("ver/api wrong: %+v", e)
	}
	if e.ContainersRunning != 3 || e.ContainersStopped != 2 || e.ImagesTotal != 12 {
		t.Errorf("counts wrong: %+v", e)
	}
	if e.StorageDriver != "overlay2" || e.CgroupVersion != "2" {
		t.Errorf("driver/cgroup wrong: %+v", e)
	}
	if e.NCPU != 4 || e.MemoryTotalBytes != 16_777_216_000 {
		t.Errorf("ncpu/mem wrong: %+v", e)
	}
	if e.Name != "app01" || e.OperatingSystem != "Ubuntu 22.04.4 LTS" {
		t.Errorf("name/os wrong: %+v", e)
	}
	if len(errs) != 0 {
		t.Errorf("unexpected errs: %v", errs)
	}
}

func TestParseEngine_EmptyBoth(t *testing.T) {
	var errs []string
	if e := parseEngine("", "", &errs); e != nil {
		t.Errorf("expected nil engine when both raws are empty")
	}
}

func TestParseInspectArray(t *testing.T) {
	const sample = `[
  {
    "Id": "abc1234567890def",
    "Created": "2026-05-29T10:00:00.000Z",
    "Path": "nginx",
    "Args": ["-g", "daemon off;"],
    "Name": "/my-nginx",
    "Image": "sha256:1234567890abcdef",
    "RestartCount": 0,
    "Platform": "linux",
    "LogPath": "/var/log/.../my-nginx.log",
    "SizeRw": 1024,
    "SizeRootFs": 142000000,
    "State": {
      "Status": "running",
      "Running": true,
      "Paused": false,
      "Restarting": false,
      "OOMKilled": false,
      "Dead": false,
      "Pid": 1234,
      "ExitCode": 0,
      "StartedAt": "2026-05-29T11:00:00.000Z",
      "FinishedAt": "0001-01-01T00:00:00Z"
    },
    "HostConfig": {
      "RestartPolicy": {"Name": "always"},
      "Memory": 524288000,
      "NanoCpus": 2000000000,
      "CpuShares": 1024,
      "NetworkMode": "bridge",
      "Privileged": false
    },
    "Config": {
      "User": "",
      "Env": ["PATH=/usr/local/sbin"],
      "Cmd": ["nginx","-g","daemon off;"],
      "Entrypoint": null,
      "Image": "nginx:1.27",
      "WorkingDir": "",
      "Labels": {"com.example.team":"core"}
    },
    "NetworkSettings": {
      "Networks": {
        "bridge": {"IPAddress":"172.17.0.2","Gateway":"172.17.0.1","MacAddress":"02:42:ac:11:00:02","NetworkID":"netid-long-hash-here-1234"}
      },
      "Ports": {
        "80/tcp": [{"HostIp":"0.0.0.0","HostPort":"8080"}],
        "443/tcp": null
      }
    },
    "Mounts": [
      {"Type":"volume","Name":"vol1","Source":"/var/lib/docker/volumes/vol1/_data","Destination":"/data","Mode":"rw","RW":true}
    ]
  }
]`
	containers, err := parseInspectArray(sample)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("count=%d", len(containers))
	}
	c := containers[0]
	if c.Name != "my-nginx" || c.ShortID != "abc123456789" {
		t.Errorf("name/short wrong: %+v", c)
	}
	if c.Image != "nginx:1.27" || c.ImageID != "sha256:1234567890abcdef" {
		t.Errorf("image fields wrong: %+v", c)
	}
	if !c.Running || c.PID != 1234 || c.ExitCode != 0 {
		t.Errorf("state wrong: %+v", c)
	}
	if c.RestartPolicy != "always" || c.NetworkMode != "bridge" {
		t.Errorf("hostconfig wrong: %+v", c)
	}
	if c.Limits.MemoryBytes != 524288000 || c.Limits.NanoCPUs != 2000000000 {
		t.Errorf("limits wrong: %+v", c.Limits)
	}
	if c.Command != "nginx -g daemon off;" {
		t.Errorf("command wrong: %q", c.Command)
	}
	if len(c.Ports) != 2 {
		t.Fatalf("ports=%d, want 2 (80 published + 443 exposed-only)", len(c.Ports))
	}
	// Сортировка по ключу — "443/tcp" идёт раньше "80/tcp" лексикографически.
	if c.Ports[0].PrivatePort != 443 || c.Ports[0].PublicPort != 0 {
		t.Errorf("ports[0] wrong (443 exposed-only): %+v", c.Ports[0])
	}
	if c.Ports[1].PrivatePort != 80 || c.Ports[1].PublicPort != 8080 || c.Ports[1].IP != "0.0.0.0" {
		t.Errorf("ports[1] wrong (80 published): %+v", c.Ports[1])
	}
	if len(c.Networks) != 1 || c.Networks[0].Name != "bridge" || c.Networks[0].IPAddress != "172.17.0.2" {
		t.Errorf("networks wrong: %+v", c.Networks)
	}
	if c.Networks[0].NetworkID != "netid-long-h" {
		t.Errorf("network shortID wrong: %q", c.Networks[0].NetworkID)
	}
	if len(c.Mounts) != 1 || c.Mounts[0].Type != "volume" {
		t.Errorf("mounts wrong: %+v", c.Mounts)
	}
	if c.SizeRwBytes != 1024 || c.SizeRootFsBytes != 142000000 {
		t.Errorf("sizes wrong: %+v", c)
	}
	if c.StartedAt == nil || c.FinishedAt != nil {
		t.Errorf("started/finished wrong: started=%v finished=%v", c.StartedAt, c.FinishedAt)
	}
}

func TestBuildStatusString(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		st   apiState
		want string
	}{
		{"created", apiState{Status: "created"}, "Created"},
		{"running_3h", apiState{Running: true, Status: "running", StartedAt: "2026-05-29T09:00:00Z"}, "Up 3 hours"},
		{"paused", apiState{Running: true, Paused: true, Status: "paused", StartedAt: "2026-05-29T09:00:00Z"}, "Up 3 hours (Paused)"},
		{"healthy", apiState{Running: true, Status: "running", StartedAt: "2026-05-29T09:00:00Z", Health: &apiHealth{Status: "healthy"}}, "Up 3 hours (healthy)"},
		{"restarting", apiState{Restarting: true, ExitCode: 2}, "Restarting (2)"},
		{"exited", apiState{Status: "exited", ExitCode: 0, FinishedAt: "2026-05-28T10:00:00Z"}, "Exited (0) 1 day ago"},
		{"dead", apiState{Status: "dead", Dead: true}, "Dead"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildStatusString(tc.st, now); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSplitByMarkers(t *testing.T) {
	const sample = `===WHICH===
/usr/bin/docker
===VERSION===
{"Version":"27.1.1"}
===INFO===
{}
===INSPECT===
[]
===END===
`
	blocks := splitByMarkers(sample, []string{"===WHICH===", "===VERSION===", "===INFO===", "===INSPECT===", "===END==="})
	if blocks["===WHICH==="] != "/usr/bin/docker" {
		t.Errorf("WHICH=%q", blocks["===WHICH==="])
	}
	if blocks["===VERSION==="] != `{"Version":"27.1.1"}` {
		t.Errorf("VERSION=%q", blocks["===VERSION==="])
	}
	if blocks["===INSPECT==="] != "[]" {
		t.Errorf("INSPECT=%q", blocks["===INSPECT==="])
	}
}
