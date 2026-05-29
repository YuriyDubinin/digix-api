package remoteinfo

import (
	"context"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/YuriyDubinin/dijex-api/internal/geo"
)

// geoResolver — узкий контракт резолва страны по IP (реализуется *geo.Resolver).
// nil допустим — host.country* просто не заполняются.
type geoResolver interface {
	Lookup(ip string) (geo.CountryInfo, bool)
}

// Collector собирает RemoteSystemInfo через открытый SSH-клиент. Сам клиент
// не открывает и не закрывает — это ответственность вызывающего кода
// (service-слоя), у которого уже есть Connector.
type Collector struct {
	geo geoResolver

	errMu sync.Mutex
}

func NewCollector(geoRes geoResolver) *Collector {
	return &Collector{geo: geoRes}
}

// Collect параллельно собирает все секции через переданный *ssh.Client.
// Ошибки секций пишутся в RemoteSystemInfo.Errors, остальные секции отдаются
// «как есть» — endpoint никогда не падает по причине одной плохой секции.
func (c *Collector) Collect(ctx context.Context, client *ssh.Client, conn ConnectionInfo) *RemoteSystemInfo {
	started := time.Now()
	out := &RemoteSystemInfo{
		CollectedAt: started.UTC(),
		Connection:  conn,
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		hi, err := c.collectHost(ctx, client)
		if err != nil {
			c.addErr(out, "host", err.Error())
		}
		out.Host = hi
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ci, err := c.collectCPU(ctx, client)
		if err != nil {
			c.addErr(out, "cpu", err.Error())
		}
		out.CPU = ci
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		mi, err := c.collectMemory(ctx, client)
		if err != nil {
			c.addErr(out, "memory", err.Error())
		}
		out.Memory = mi
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		di, err := c.collectDisks(ctx, client)
		if err != nil {
			c.addErr(out, "disks", err.Error())
		}
		out.Disks = di
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ni, err := c.collectNetwork(ctx, client)
		if err != nil {
			c.addErr(out, "network", err.Error())
		}
		out.Network = ni
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		out.Docker = c.collectDocker(ctx, client)
	}()

	wg.Wait()

	out.CollectionDurationMS = time.Since(started).Milliseconds()
	return out
}

func (c *Collector) addErr(out *RemoteSystemInfo, section, message string) {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	out.Errors = append(out.Errors, SectionError{Section: section, Message: message})
}
