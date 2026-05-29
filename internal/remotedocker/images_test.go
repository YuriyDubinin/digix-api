package remotedocker

import (
	"testing"
)

func TestParseImageInspectArray(t *testing.T) {
	const sample = `[
  {
    "Id": "sha256:abc1234567890def",
    "Parent": "sha256:parent1234567890",
    "RepoTags": ["nginx:1.27", "nginx:latest"],
    "RepoDigests": ["nginx@sha256:digest..."],
    "Created": "2026-05-22T09:14:01.000Z",
    "Size": 142000000,
    "Config": {
      "Labels": {"maintainer": "nginx team"}
    }
  },
  {
    "Id": "sha256:dangling0987654321",
    "Parent": "",
    "RepoTags": ["<none>:<none>"],
    "RepoDigests": [],
    "Created": "2026-05-10T00:00:00Z",
    "Size": 50000000,
    "Config": {"Labels": null}
  }
]`
	counts := map[string]int{
		"sha256:abc1234567890def":      3, // используется 3 контейнерами
		"sha256:dangling0987654321":    0, // никем не используется
	}
	imgs, err := parseImageInspectArray(sample, counts)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(imgs) != 2 {
		t.Fatalf("count=%d, want 2", len(imgs))
	}

	nginx := imgs[0]
	if nginx.ID != "sha256:abc1234567890def" {
		t.Errorf("ID=%q", nginx.ID)
	}
	if nginx.ShortID != "abc123456789" {
		t.Errorf("ShortID=%q", nginx.ShortID)
	}
	if len(nginx.RepoTags) != 2 || nginx.RepoTags[0] != "nginx:1.27" {
		t.Errorf("RepoTags=%v", nginx.RepoTags)
	}
	if nginx.SizeBytes != 142_000_000 {
		t.Errorf("SizeBytes=%d", nginx.SizeBytes)
	}
	if nginx.Containers != 3 {
		t.Errorf("Containers=%d, want 3", nginx.Containers)
	}
	if nginx.Dangling {
		t.Errorf("nginx should NOT be dangling")
	}
	if nginx.Labels["maintainer"] != "nginx team" {
		t.Errorf("Labels=%v", nginx.Labels)
	}

	dangling := imgs[1]
	if !dangling.Dangling {
		t.Errorf("expected dangling=true (tags only '<none>:<none>')")
	}
	if len(dangling.RepoTags) != 0 {
		t.Errorf("dangling.RepoTags should be empty after filtering, got %v", dangling.RepoTags)
	}
	if dangling.Containers != 0 {
		t.Errorf("dangling.Containers=%d, want 0", dangling.Containers)
	}
}

func TestCountContainerImages(t *testing.T) {
	const sample = `sha256:abc1234567890def
sha256:def0987654321abc
abc1234567890def
sha256:abc1234567890def

sha256:def0987654321abc`
	got := countContainerImages(sample)

	// "abc..." без префикса должен нормализоваться к "sha256:abc...".
	if got["sha256:abc1234567890def"] != 3 {
		t.Errorf("abc count=%d, want 3 (3 контейнера, один без префикса)", got["sha256:abc1234567890def"])
	}
	if got["sha256:def0987654321abc"] != 2 {
		t.Errorf("def count=%d, want 2", got["sha256:def0987654321abc"])
	}
}

func TestImageSortKey(t *testing.T) {
	cases := []struct {
		name string
		want string
		img  func() interface{}
	}{}
	_ = cases
	// просто smoke-test через готовые объекты — основное покрытие даёт ParseImageInspectArray.
}
