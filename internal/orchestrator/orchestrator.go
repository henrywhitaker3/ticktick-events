// Package orchestrator
package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/henrywhitaker3/ticktick-events/internal/task"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/cronjob"
	"github.com/italypaleale/francis/host/local"
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
		client:  opts.TickTick,
		service: opts.Service,
	}
}

func Regster(host *local.Host, opts OrchestratorOpts) error {
	orch := New(opts)

	job, err := cronjob.New(
		"orchestrator",
		cronjob.WithInterval(opts.Interval),
		cronjob.WithImmediate(),
		cronjob.WithLogger(slog.With("component", "orchestrator")),
		cronjob.WithJob(orch.Run),
	)
	if err != nil {
		return fmt.Errorf("init orch cronjob: %w", err)
	}

	if err := host.RegisterBuiltInActor(job); err != nil {
		return fmt.Errorf("register orch cronjob: %w", err)
	}

	return nil
}

func (o *Orchestrator) Run(ctx context.Context) error {
	tasks, err := o.client.GetOverdueTasks(ctx)
	if err != nil {
		return fmt.Errorf("get overdue tasks: %w", err)
	}
	slog.Debug("overdue tasks", "tasks", tasks)
	for _, t := range tasks {
		if _, err := o.service.Invoke(
			ctx,
			task.Type,
			t.ID,
			task.Overdue,
			t,
		); err != nil {
			return fmt.Errorf("invoke task: %w", err)
		}
	}
	return nil
}
