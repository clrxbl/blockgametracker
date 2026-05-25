package queue

import (
	"context"
	"time"

	"github.com/charmbracelet/log"

	"mcstatus-exporter/internal/config"
	"mcstatus-exporter/internal/ping"
)

type Producer struct {
	Source        string
	Interval      time.Duration
	DrainPerTick  int
	Store         *config.Store
	Client        *Client
	Spool         *Spool
}

// Run blocks until ctx is cancelled. Each tick pings all enabled servers,
// pushes the batch, and (on success) drains up to DrainPerTick spooled
// batches in age order. On push failure the batch is written to the spool
// so it survives restarts.
func (p *Producer) Run(ctx context.Context) {
	if existing, err := p.Spool.List(); err == nil && len(existing) > 0 {
		log.Warn("spool contains batches from previous run", "count", len(existing))
	}

	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()

	p.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Producer) tick(ctx context.Context) {
	results := ping.QueryAll(p.Store.Snapshot())
	if len(results) == 0 {
		log.Warn("queue tick produced no ping results, skipping push")
		return
	}

	payload := EncodeBatch(p.Source, results)

	if err := p.Client.Push(ctx, payload); err != nil {
		log.Warn("queue push failed, spooling batch", "err", err.Error(), "pings", len(results))
		if path, werr := p.Spool.Write(payload); werr != nil {
			log.Error("failed to spool batch", "err", werr.Error())
		} else {
			log.Info("spooled batch", "path", path)
		}
		return
	}
	log.Info("pushed batch", "pings", len(results))

	p.drain(ctx)
}

func (p *Producer) drain(ctx context.Context) {
	paths, err := p.Spool.List()
	if err != nil {
		log.Error("failed to list spool", "err", err.Error())
		return
	}
	if len(paths) == 0 {
		return
	}

	limit := p.DrainPerTick
	if limit <= 0 || limit > len(paths) {
		limit = len(paths)
	}

	for _, path := range paths[:limit] {
		payload, err := p.Spool.Read(path)
		if err != nil {
			log.Error("failed to read spooled batch", "path", path, "err", err.Error())
			return
		}
		if err := p.Client.Push(ctx, payload); err != nil {
			log.Warn("drain push failed, stopping drain", "err", err.Error())
			return
		}
		if err := p.Spool.Delete(path); err != nil {
			log.Error("failed to delete spooled batch", "path", path, "err", err.Error())
			return
		}
		log.Info("drained spooled batch", "path", path)
	}
}
