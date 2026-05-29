package remoteinfo

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"
)

// runOutput выполняет одну команду в новой сессии и возвращает её stdout
// (trim, без NUL'ов). Контекст не подключаем напрямую к сессии (golang.org/x/crypto/ssh
// не умеет ctx-cancel прямо в Run), но проверяем его до запуска — это даёт
// возможность быстро выйти на отменённом запросе.
func runOutput(ctx context.Context, client *ssh.Client, cmd string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.Output(cmd)
	if err != nil {
		// Возвращаем то, что успели получить (если что-то есть): часть утилит
		// пишут полезный вывод в stdout, но завершаются с ненулевым кодом.
		if len(out) == 0 {
			return "", err
		}
	}
	return strings.TrimSpace(strings.ReplaceAll(string(out), "\x00", "")), err
}

// runMustOutput — как runOutput, но возвращает ошибку как nil, если вывод
// непустой. Удобно для команд, у которых ненулевой exit code не означает
// отсутствие полезных данных (например, `df` ругается на недоступные ФС,
// но при этом выдаёт корректный отчёт по доступным).
func runMustOutput(ctx context.Context, client *ssh.Client, cmd string) (string, error) {
	out, err := runOutput(ctx, client, cmd)
	if out != "" {
		return out, nil
	}
	if err == nil {
		return "", errors.New("empty output")
	}
	return "", err
}
