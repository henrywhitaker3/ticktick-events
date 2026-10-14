// Package orchestrator
package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/henrywhitaker3/ticktick-events/internal/habit"
	"github.com/henrywhitaker3/ticktick-events/internal/task"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/cronjob"
	"github.com/italypaleale/francis/host/local"
)

type Client interface {
	GetOverdueTasks(ctx context.Context) ([]client.Task, error)
	GetHabits(ctx context.Context) ([]client.Habit, error)
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
		service:  opts.Service,
		interval: opts.Interval,
	}
}

func Regster(host *local.Host, opts OrchestratorOpts) error {
	orch := New(opts)

	overdue, err := newOverdueTasksCronjob(orch)
	if err != nil {
		return fmt.Errorf("build overdue tasks cronjob: %w", err)
	}
	if err := host.RegisterBuiltInActor(overdue); err != nil {
		return fmt.Errorf("register overdue tasks cronjob: %w", err)
	}

	habits, err := newGetHabitsCronjob(orch)
	if err != nil {
		return fmt.Errorf("build habits cronjob: %w", err)
	}
	if err := host.RegisterBuiltInActor(habits); err != nil {
		return fmt.Errorf("register habits cronjob: %w", err)
	}

	return nil
}

func newOverdueTasksCronjob(orch *Orchestrator) (*cronjob.CronJob, error) {
	return cronjob.New(
		"overdue-tasks",
		cronjob.WithInterval(orch.interval),
		cronjob.WithImmediate(),
		cronjob.WithLogger(slog.With("component", "overdue-tasks")),
		cronjob.WithJob(orch.overdueTasks),
	)
}

func newGetHabitsCronjob(orch *Orchestrator) (*cronjob.CronJob, error) {
	return cronjob.New(
		"habits",
		cronjob.WithInterval(orch.interval),
		cronjob.WithImmediate(),
		cronjob.WithLogger(slog.With("component", "habits")),
		cronjob.WithJob(orch.getHabits),
	)
}

func (o *Orchestrator) getHabits(ctx context.Context) error {
	habits, err := o.client.GetHabits(ctx)
	if err != nil {
		return fmt.Errorf("get habits: %w", err)
	}
	slog.Debug("get habits", "habits", habits)
	for _, h := range habits {
		if _, err := o.service.Invoke(ctx, habit.Type, h.ID, habit.Onboard, h); err != nil {
			return fmt.Errorf("invoke habit: %w", err)
		}
	}
	return nil
}

func (o *Orchestrator) overdueTasks(ctx context.Context) error {
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
