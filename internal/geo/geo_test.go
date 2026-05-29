package geo

import "testing"

func TestResolver_KnownPublicIPs(t *testing.T) {
	r, err := NewResolver()
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	defer r.Close()

	cases := []struct {
		name    string
		ip      string
		wantCC  string
		wantHit bool
	}{
		// Стабильные anycast/DNS-IP крупных провайдеров — отлично подходят для
		// smoke-теста; страна привязки в DB-IP Lite не меняется годами.
		{"google_dns_v4", "8.8.8.8", "US", true},
		{"google_dns_v6", "2001:4860:4860::8888", "", true}, // anycast IPv6 — код страны плавает по базам, проверяем только сам факт hit'а
		{"cloudflare_dns", "1.1.1.1", "AU", true},           // CF DNS приписан к Австралии в большинстве GeoIP-баз
		{"yandex_dns", "77.88.8.8", "RU", true},

		{"empty", "", "", false},
		{"garbage", "not-an-ip", "", false},
		{"loopback_v4", "127.0.0.1", "", false},
		{"loopback_v6", "::1", "", false},
		{"private_10", "10.0.0.1", "", false},
		{"private_192", "192.168.1.1", "", false},
		{"cgnat", "100.64.0.1", "", false},
		{"link_local", "169.254.1.1", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := r.Lookup(tc.ip)
			if ok != tc.wantHit {
				t.Fatalf("ok=%v, want %v (got=%+v)", ok, tc.wantHit, got)
			}
			if !tc.wantHit {
				return
			}
			// wantCC=="" значит «hit достаточен, точный код не важен» (anycast и т.п.).
			if tc.wantCC != "" && got.Code != tc.wantCC {
				t.Errorf("code=%q, want %q", got.Code, tc.wantCC)
			}
			if got.Code == "" {
				t.Errorf("code is empty for %s", tc.ip)
			}
			if got.Name == "" {
				t.Errorf("name is empty for %s", tc.ip)
			}
		})
	}
}
