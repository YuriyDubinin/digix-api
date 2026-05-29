package remotesystemd

import (
	"context"
	"strings"

	"golang.org/x/crypto/ssh"
)

func runSSH(ctx context.Context, client *ssh.Client, cmd string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.Output(cmd)
	if err != nil && len(out) == 0 {
		return "", err
	}
	return strings.TrimSpace(strings.ReplaceAll(string(out), "\x00", "")), nil
}

// splitByMarkers режет текст на блоки. Помимо точного совпадения строки с
// маркером поддерживается «приклеенный» маркер: если строка ИМЕЕТ суффикс
// одного из маркеров (предыдущая команда не вывела trailing newline), префикс
// уходит в текущий блок, маркер открывает следующий.
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
		trim := strings.TrimSpace(ln)
		if _, ok := markerSet[trim]; ok {
			flush()
			current = trim
			continue
		}
		if marker, prefix := splitTrailingMarker(trim, markerSet); marker != "" {
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

// splitTrailingMarker возвращает (marker, prefix), если строка оканчивается
// одним из маркеров; иначе ("", "").
func splitTrailingMarker(s string, markerSet map[string]struct{}) (marker, prefix string) {
	for m := range markerSet {
		if len(s) > len(m) && strings.HasSuffix(s, m) {
			return m, strings.TrimSpace(s[:len(s)-len(m)])
		}
	}
	return "", ""
}
