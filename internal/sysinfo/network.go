package sysinfo

import (
	"context"
	"fmt"
	"strings"

	gnet "github.com/shirou/gopsutil/v4/net"
)

func (c *Collector) collectNetwork(ctx context.Context) (NetworkInfo, []SectionError) {
	var sectionErrs []SectionError
	out := NetworkInfo{}

	ifaces, err := gnet.InterfacesWithContext(ctx)
	if err != nil {
		sectionErrs = append(sectionErrs, SectionError{
			Section: "network.interfaces",
			Message: err.Error(),
		})
	}
	for _, iface := range ifaces {
		row := NetInterface{
			Name:         iface.Name,
			HardwareAddr: iface.HardwareAddr,
			MTU:          iface.MTU,
			Flags:        iface.Flags,
		}
		for _, addr := range iface.Addrs {
			row.Addresses = append(row.Addresses, NetAddr{
				Addr:   addr.Addr,
				Family: addrFamily(addr.Addr),
			})
		}
		out.Interfaces = append(out.Interfaces, row)
	}

	if io, err := gnet.IOCountersWithContext(ctx, true); err == nil {
		for _, v := range io {
			out.IOCounters = append(out.IOCounters, NetIOCounters{
				Name:        v.Name,
				BytesSent:   v.BytesSent,
				BytesRecv:   v.BytesRecv,
				PacketsSent: v.PacketsSent,
				PacketsRecv: v.PacketsRecv,
				ErrIn:       v.Errin,
				ErrOut:      v.Errout,
				DropIn:      v.Dropin,
				DropOut:     v.Dropout,
			})
		}
	} else {
		sectionErrs = append(sectionErrs, SectionError{
			Section: "network.io_counters",
			Message: err.Error(),
		})
	}

	// Connections — может быть медленным или требовать прав на некоторых ОС.
	// Считаем только количество, не выгружаем все строки.
	if conns, err := gnet.ConnectionsWithContext(ctx, "all"); err == nil {
		out.ConnectionsCount = len(conns)
	} else {
		sectionErrs = append(sectionErrs, SectionError{
			Section: "network.connections_count",
			Message: fmt.Sprintf("listing connections: %v", err),
		})
	}

	return out, sectionErrs
}

// addrFamily определяет, IPv4 или IPv6 в строке.
// gopsutil возвращает строки вида "192.168.1.1/24" или "fe80::1/64".
func addrFamily(addr string) string {
	if strings.Contains(addr, ":") {
		return "ipv6"
	}
	return "ipv4"
}
