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
	eventCache *cache.LruCache[string, bool]
}

func New(c Client, h *events.EventHandler) *Orchestrator {
	cache, _ := cache.NewLruCache[string, bool](100)

	return &Orchestrator{
		client:     c,
		handler:    h,
		eventCache: cache,
	}
}

func (o *Orchestrator) Run(ctx context.Context) error {
	ch := make(chan struct{}, 1)
	defer close(ch)
	ch <- struct{}{}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Minute):
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
					_ = o.handler.Dispatch[OverdueTask](OverdueTask{
						Task: t,
						Remove: func() {
							o.eventCache.Delete(ctx, t.ID)
						},
					})
					o.eventCache.Put(ctx, t.ID, true)
				}
			}
		}
	}
}
