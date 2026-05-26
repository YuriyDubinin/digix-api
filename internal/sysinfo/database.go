package sysinfo

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// collectDatabase читает состояние pgxpool + лёгкие server-side метаданные.
// Все запросы идут с собственным таймаутом, чтобы /api/system не зависал
// при проблемах с БД.
func (c *Collector) collectDatabase(ctx context.Context) DatabaseInfo {
	out := DatabaseInfo{}
	if c.pool == nil {
		return out
	}

	// Pool stats — мгновенно, не требует БД.
	stat := c.pool.Stat()
	out.Pool = DBPoolStats{
		MaxConns:             stat.MaxConns(),
		TotalConns:           stat.TotalConns(),
		IdleConns:            stat.IdleConns(),
		AcquiredConns:        stat.AcquiredConns(),
		ConstructingConns:    stat.ConstructingConns(),
		AcquireCount:         stat.AcquireCount(),
		AcquireDurationNs:    stat.AcquireDuration().Nanoseconds(),
		EmptyAcquireCount:    stat.EmptyAcquireCount(),
		CanceledAcquireCount: stat.CanceledAcquireCount(),
	}

	// Ping с коротким дедлайном.
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := c.pool.Ping(pingCtx); err != nil {
		out.Reachable = false
		return out
	}
	out.Reachable = true
	out.PingLatencyMS = float64(time.Since(start).Microseconds()) / 1000.0

	// Server-side метаданные — несколько лёгких запросов, тоже под таймаутом.
	queryCtx, cancel2 := context.WithTimeout(ctx, 3*time.Second)
	defer cancel2()

	_ = c.pool.QueryRow(queryCtx, `SELECT version()`).Scan(&out.Version)
	_ = c.pool.QueryRow(queryCtx, `SELECT current_database()`).Scan(&out.Server.CurrentDatabase)
	_ = c.pool.QueryRow(queryCtx, `SELECT pg_database_size(current_database())`).Scan(&out.Server.DatabaseSizeBytes)
	_ = c.pool.QueryRow(queryCtx, `SELECT count(*) FROM pg_stat_activity WHERE state IS NOT NULL`).Scan(&out.Server.ActiveConnections)
	_ = c.pool.QueryRow(queryCtx, `SHOW max_connections`).Scan(&out.Server.MaxConnections)

	var serverStart time.Time
	if err := c.pool.QueryRow(queryCtx, `SELECT pg_postmaster_start_time()`).Scan(&serverStart); err == nil {
		out.Server.ServerStartedAt = serverStart.UTC().Format(time.RFC3339)
	}

	return out
}

// Гарантируем, что *pgxpool.Pool используется через ожидаемый набор методов
// (компиляция упадёт, если контракт сломается из-за обновления pgx).
var _ pingableDB = (*pgxpool.Pool)(nil)

type pingableDB interface {
	Ping(ctx context.Context) error
}
