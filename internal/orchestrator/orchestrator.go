// Package orchestrator
package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/henrywhitaker3/ticktick-events/internal/task"
	"github.com/italypaleale/francis/actor"
)

type Client interface {
	GetOverdueTasks(ctx context.Context) ([]client.Task, error)
}

type Orchestrator struct {
	client   Client
	interval time.Duration
	service  *actor.Service
}

type OrchestratorOpts struct {
	TickTick *client.TickTickClient
	Service  *actor.Service
	Interval time.Duration
}

func New(opts OrchestratorOpts) *Orchestrator {
	return &Orchestrator{
		client:   opts.TickTick,
		interval: opts.Interval,
		service:  opts.Service,
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
					if _, err := o.service.Invoke(
						ctx,
						task.Type,
						t.ID,
						task.Overdue,
						t,
					); err != nil {
						slog.ErrorContext(ctx, "invoke overdue", "task", t, "error", err)
					}
				}
			}
		}
	}
}
