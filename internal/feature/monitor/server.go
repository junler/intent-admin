package monitor

import (
	"context"
	"runtime"
	"time"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

func (s *Service) registerServer() {
	capability.Register(capability.Capability[capability.Empty, map[string]any]{Meta: capability.Meta{Name: "monitor.server.info", Summary: "查询服务运行状态", Perm: "monitor:server:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/server"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, _ capability.Empty) (map[string]any, error) {
		memory, err := mem.VirtualMemoryWithContext(ctx)
		if err != nil {
			return nil, err
		}
		system, err := host.InfoWithContext(ctx)
		if err != nil {
			return nil, err
		}
		usage, err := disk.UsageWithContext(ctx, "/")
		if err != nil {
			return nil, err
		}
		cpus, err := cpu.InfoWithContext(ctx)
		if err != nil {
			return nil, err
		}
		percent, _ := cpu.PercentWithContext(ctx, 0, false)
		var gm runtime.MemStats
		runtime.ReadMemStats(&gm)
		return map[string]any{"cpu": map[string]any{"cores": runtime.NumCPU(), "percent": percent, "info": cpus}, "memory": memory, "system": system, "disk": usage, "go": map[string]any{"version": runtime.Version(), "goroutines": runtime.NumGoroutine(), "heapAlloc": gm.HeapAlloc, "heapSys": gm.HeapSys, "gcCount": gm.NumGC}, "sampledAt": time.Now()}, nil
	}})
}
