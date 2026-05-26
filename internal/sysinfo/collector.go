package sysinfo

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Collector собирает данные о машине. Создаётся один раз в main.go,
// передаётся в HTTP-хендлер. Thread-safe.
type Collector struct {
	app  AppMeta
	pool *pgxpool.Pool

	// errMu защищает добавление в SystemInfo.Errors из параллельных горутин.
	errMu sync.Mutex
}

func NewCollector(app AppMeta, pool *pgxpool.Pool) *Collector {
	return &Collector{
		app:  app,
		pool: pool,
	}
}

// Collect собирает все секции. Большинство категорий бежит параллельно —
// один медленный пинг БД не должен задерживать сбор CPU/Disks/Network.
//
// Любая ошибка категории пишется в SystemInfo.Errors и не валит остальные.
// Эндпоинт остаётся живучим: если что-то одно не сработало — отдадим, что собрали.
func (c *Collector) Collect(ctx context.Context) *SystemInfo {
	startedAt := time.Now()
	out := &SystemInfo{
		CollectedAt: startedAt.UTC(),
	}

	// App и Go runtime — мгновенные, без I/O — собираем синхронно.
	out.App = c.collectApp(ctx)
	out.GoRuntime = c.collectGoRuntime(ctx)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		hi, err := c.collectHost(ctx)
		if err != nil {
			c.addErr(out, "host", err.Error())
		}
		out.Host = hi
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ci, err := c.collectCPU(ctx)
		if err != nil {
			c.addErr(out, "cpu", err.Error())
		}
		out.CPU = ci
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		mi, err := c.collectMemory(ctx)
		if err != nil {
			c.addErr(out, "memory", err.Error())
		}
		out.Memory = mi
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		di, errs := c.collectDisks(ctx)
		out.Disks = di
		for _, e := range errs {
			c.addErr(out, e.Section, e.Message)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ni, errs := c.collectNetwork(ctx)
		out.Network = ni
		for _, e := range errs {
			c.addErr(out, e.Section, e.Message)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		pi, err := c.collectProcess(ctx)
		if err != nil {
			c.addErr(out, "process", err.Error())
		}
		out.Process = pi
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		out.Database = c.collectDatabase(ctx)
	}()

	wg.Wait()

	out.CollectionDurationMS = time.Since(startedAt).Milliseconds()
	return out
}

func (c *Collector) addErr(out *SystemInfo, section, message string) {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	out.Errors = append(out.Errors, SectionError{Section: section, Message: message})
}
