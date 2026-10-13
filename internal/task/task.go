// Package task
package task

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/henrywhitaker3/ticktick-events/internal/config"
	"github.com/henrywhitaker3/windowframe/v2/log"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/host/local"
)

const (
	Type = "task"
)

type TaskGetter interface {
	GetTask(ctx context.Context, projectID, taskID string) (client.Task, error)
}

type StimulusSender interface {
	Send(ctx context.Context, task client.Task, stimulus client.Stimulus) error
}

type Task struct {
	client           actor.Client[taskState]
	ticktick         TaskGetter
	pavlok           StimulusSender
	sleep            time.Duration
	quiet            config.QuietTimes
	maxNotifications int
}

type taskState struct {
	Task                *client.Task
	LastNotifiedAt      *time.Time
	NotificationCounter int
	Completed           bool
	Onboarded           bool
}

type Opts struct {
	Ticktick         TaskGetter
	Pavlok           StimulusSender
	Sleep            time.Duration
	Quiet            config.QuietTimes
	MaxNotifications int
}

func New(opts Opts) actor.Factory {
	return func(actorID string, service *actor.Service) actor.Actor {
		return &Task{
			client:           actor.NewActorClient[taskState](Type, actorID, service),
			ticktick:         opts.Ticktick,
			pavlok:           opts.Pavlok,
			sleep:            opts.Sleep,
			quiet:            opts.Quiet,
			maxNotifications: opts.MaxNotifications,
		}
	}
}

func Register(host *local.Host, opts Opts) error {
	return host.RegisterActor(Type, New(opts))
}

const (
	Overdue = "overdue"
)

func (t *Task) Invoke(ctx context.Context, method string, data actor.Envelope) (any, error) {
	switch method {
	case Overdue:
		return t.overdue(ctx, data)
	default:
		return nil, fmt.Errorf("unknown invoke method %s", method)
	}
}

func (t *Task) overdue(ctx context.Context, data actor.Envelope) (any, error) {
	state, err := t.client.GetState(ctx)
	if err != nil {
		return nil, fmt.Errorf("get task state: %w", err)
	}
	if state.Onboarded {
		return state, nil
	}

	var task client.Task
	if err := data.Decode(&task); err != nil {
		return nil, fmt.Errorf("decode task data: %w", err)
	}
	if err := t.client.SetAlarm(ctx, Overdue, actor.AlarmProperties{
		DueTime:  time.Now().Add(t.sleep),
		Interval: "PT10M",
	}); err != nil {
		return nil, fmt.Errorf("setup alert alarm: %w", err)
	}
	state.Task = &task
	state.Onboarded = true
	if err := t.client.SetState(ctx, state, nil); err != nil {
		return nil, fmt.Errorf("set task state: %w", err)
	}
	return state, nil
}

func (t *Task) Alarm(ctx context.Context, method string, data actor.Envelope) error {
	switch method {
	case Overdue:
		return t.processOverdueJob(ctx)
	default:
		return fmt.Errorf("unknown job method %s", method)
	}
}

func (t *Task) processOverdueJob(ctx context.Context) error {
	state, err := t.client.GetState(ctx)
	if err != nil {
		return fmt.Errorf("get task state: %w", err)
	}

	if state.Task == nil {
		return fmt.Errorf("cannot process nil task")
	}
	ctx = log.WithLogAttr(ctx, "state", state)
	slog.InfoContext(ctx, "handling overdue task job")

	if state.Completed {
		return nil
	}

	task, err := t.ticktick.GetTask(ctx, state.Task.ProjectID, state.Task.ID)
	if err != nil {
		return fmt.Errorf("refresh task data: %w", err)
	}

	if task.Status > 0 || task.CompletedTime != nil || task.DueDate != state.Task.DueDate ||
		state.NotificationCounter >= t.maxNotifications {
		slog.DebugContext(ctx, "task completed")
		state.Completed = true
		if err := t.client.SetState(ctx, state, nil); err != nil {
			return fmt.Errorf("store task state: %w", err)
		}
		if err := t.client.DeleteAlarm(ctx, Overdue); err != nil {
			return fmt.Errorf("delete overdue alarm: %w", err)
		}
		return nil
	}

	if t.quiet.In(time.Now()) {
		slog.DebugContext(ctx, "skipping zap due to quiet times")
		return nil
	}

	slog.InfoContext(ctx, "sending zap")
	if err := t.pavlok.Send(ctx, task, client.Zap); err != nil {
		return fmt.Errorf("send zap: %w", err)
	}

	state.NotificationCounter++
	now := time.Now()
	state.LastNotifiedAt = &now
	state.Task = &task

	if err := t.client.SetState(ctx, state, nil); err != nil {
		return fmt.Errorf("store task state: %w", err)
	}

	return nil
}
