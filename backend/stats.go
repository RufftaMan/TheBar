package main

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"
)

const (
	statsInterval = 3 * time.Second
	historyLen    = 100 // 5 minutes at 3s
)

// Sample is one point for one container: rates are per second.
type Sample struct {
	T      int64   `json:"t"`   // unix ms
	CPU    float64 `json:"cpu"` // percent of the whole machine (all cores = 100)
	Mem    uint64  `json:"mem"`
	Limit  uint64  `json:"limit"`
	Rx     float64 `json:"rx"`
	Tx     float64 `json:"tx"`
	BlkR   float64 `json:"br"`
	BlkW   float64 `json:"bw"`
	HasNet bool    `json:"net"`
}

type raw struct {
	t              time.Time
	cpu, sys       uint64
	rx, tx, br, bw uint64
}

type Stats struct {
	d   *Docker
	inv *Inventory
	hub *Hub

	mu      sync.RWMutex
	last    map[string]raw
	history map[string][]Sample
}

func NewStats(d *Docker, inv *Inventory, hub *Hub) *Stats {
	return &Stats{d: d, inv: inv, hub: hub, last: map[string]raw{}, history: map[string][]Sample{}}
}

// History returns a copy of all histories (for the initial page payload).
func (s *Stats) History() map[string][]Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]Sample, len(s.history))
	for id, h := range s.history {
		out[id] = append([]Sample(nil), h...)
	}
	return out
}

func (s *Stats) Run(ctx context.Context) {
	t := time.NewTicker(statsInterval)
	defer t.Stop()
	for {
		s.poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Stats) poll(ctx context.Context) {
	ids := s.inv.Running()
	ctx, cancel := context.WithTimeout(ctx, statsInterval)
	defer cancel()

	latest := map[string]Sample{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			st, err := s.d.Stats(ctx, id)
			if err != nil {
				return
			}
			if smp, ok := s.compute(id, st); ok {
				mu.Lock()
				latest[id] = smp
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()

	running := map[string]bool{}
	for _, id := range ids {
		running[id] = true
	}
	s.mu.Lock()
	for id, smp := range latest {
		h := append(s.history[id], smp)
		if len(h) > historyLen {
			h = h[len(h)-historyLen:]
		}
		s.history[id] = h
	}
	for id := range s.history { // drop stopped/removed containers
		if !running[id] {
			delete(s.history, id)
			delete(s.last, id)
		}
	}
	s.mu.Unlock()
	if len(latest) > 0 {
		s.hub.Broadcast("stats", latest)
	}
}

func (s *Stats) compute(id string, st *dStats) (Sample, bool) {
	now := time.Now()
	r := raw{t: now, cpu: st.CPUStats.CPUUsage.TotalUsage, sys: st.CPUStats.SystemUsage}
	hasNet := st.Networks != nil
	for _, n := range st.Networks {
		r.rx += n.RxBytes
		r.tx += n.TxBytes
	}
	for _, b := range st.BlkioStats.IOServiceBytesRecursive {
		switch strings.ToLower(b.Op) {
		case "read":
			r.br += b.Value
		case "write":
			r.bw += b.Value
		}
	}
	mem := st.MemoryStats.Usage
	// like the docker CLI: don't count reclaimable page cache
	if v, ok := st.MemoryStats.Stats["inactive_file"]; ok && v < mem {
		mem -= v
	} else if v, ok := st.MemoryStats.Stats["total_inactive_file"]; ok && v < mem {
		mem -= v
	}

	s.mu.Lock()
	prev, had := s.last[id]
	s.last[id] = r
	s.mu.Unlock()
	if !had {
		return Sample{}, false // need two readings for rates
	}
	dt := now.Sub(prev.t).Seconds()
	if dt <= 0 {
		return Sample{}, false
	}
	smp := Sample{T: now.UnixMilli(), Mem: mem, Limit: st.MemoryStats.Limit, HasNet: hasNet}
	if r.sys > prev.sys && r.cpu >= prev.cpu {
		// system usage counts all cores, so this is already a share of the whole machine
		smp.CPU = round3(float64(r.cpu-prev.cpu) / float64(r.sys-prev.sys) * 100)
	}
	rate := func(a, b uint64) float64 {
		if a < b {
			return 0 // counter reset (container restarted)
		}
		return math.Round(float64(a-b) / dt)
	}
	smp.Rx, smp.Tx = rate(r.rx, prev.rx), rate(r.tx, prev.tx)
	smp.BlkR, smp.BlkW = rate(r.br, prev.br), rate(r.bw, prev.bw)
	return smp, true
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
func round3(f float64) float64 { return math.Round(f*1000) / 1000 }

// RunDiskUsage refreshes writable-layer and volume sizes. This call is slow on
// big hosts, so it runs rarely.
func (s *Stats) RunDiskUsage(ctx context.Context) {
	time.Sleep(20 * time.Second)
	for {
		dctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		df, err := s.d.DiskUsage(dctx)
		cancel()
		if err == nil {
			rw := map[string]int64{}
			for _, c := range df.Containers {
				rw[c.ID] = c.SizeRw
			}
			vol := map[string]int64{}
			for _, v := range df.Volumes {
				if v.UsageData.Size > 0 {
					vol[v.Name] = v.UsageData.Size
				}
			}
			s.inv.Update(func(c *Container) bool {
				var vs int64
				for _, v := range c.Volumes {
					vs += vol[v]
				}
				changed := c.SizeRw != rw[c.ID] || c.VolSize != vs
				c.SizeRw, c.VolSize = rw[c.ID], vs
				return changed
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Minute):
		}
	}
}
