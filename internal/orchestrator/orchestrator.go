// Package orchestrator
package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/henrywhitaker3/windowframe/v2/cache"
	"github.com/henrywhitaker3/windowframe/v2/events"
)

type Client interface {
	GetOverdueTasks(ctx context.Context) ([]client.Task, error)
}

type Orchestrator struct {
	client     Client
	handler    *events.EventHandler
	eventCache *cache.LruCacheExpiring[string, bool]
	interval   time.Duration
}

type OrchestratorOpts struct {
	TickTick *client.TickTickClient
	Interval time.Duration
	Events   *events.EventHandler
}

func New(opts OrchestratorOpts) *Orchestrator {
	cache, _ := cache.NewLruCacheExpiring[string, bool](100)

	return &Orchestrator{
		client:     opts.TickTick,
		handler:    opts.Events,
		eventCache: cache,
		interval:   opts.Interval,
	}
}

func (o *Orchestrator) Run(ctx context.Context) error {
	ch := make(chan struct{}, 1)
	defer close(ch)
	ch <- struct{}{}

	tick := time.NewTicker(o.interval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			ch <- struct{}{}
		case <-ch:
			tasks, err := o.client.GetOverdueTasks(ctx)
			if err != nil {
				slog.Error("get overdue tasks", "error", err)
			} else {
				slog.Debug("overdue tasks", "tasks", tasks)
				for _, t := range tasks {
					if _, ok := o.eventCache.Get(ctx, t.ID); ok {
						// Task already processed
						continue
					}
					res, _ := o.handler.DispatchChannel(OverdueTask{
						Task: t,
					})
					o.eventCache.Put(ctx, t.ID, true, time.Minute*10)
					// Remove it from the cache if the job fails
					// so it gets retried
					for range res {
						o.eventCache.Delete(ctx, t.ID)
						break
					}
				}
			}
		}
	}
}
