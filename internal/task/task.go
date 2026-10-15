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
	"github.com/sosodev/duration"
)

const (
	Type = "task"
)

type TaskGetter interface {
	GetTask(ctx context.Context, projectID, taskID string) (client.Task, error)
}

type StimulusSender interface {
	Send(ctx context.Context, task string, stimulus client.Stimulus) error
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
	LastAlarmAt         time.Time
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
	cleanup, err := newCleanupTasksCronjob(host)
	if err != nil {
		return fmt.Errorf("create cleanup cronjob: %w", err)
	}
	if err := host.RegisterBuiltInActor(cleanup); err != nil {
		return fmt.Errorf("register cleanup cronjob: %w", err)
	}
	return host.RegisterActor(Type, New(opts))
}

const (
	Overdue = "overdue"
	Stop    = "stop"

	notifyInterval   = time.Minute * 10
	alarmGracePeriod = notifyInterval + (time.Minute * 5)
)

func (t *Task) Invoke(ctx context.Context, method string, data actor.Envelope) (any, error) {
	switch method {
	case Overdue:
		return t.overdue(ctx, data)
	case Stop:
		t.client.Halt()
		return nil, t.client.DeleteState(ctx)
	default:
		return nil, fmt.Errorf("unknown invoke method %s", method)
	}
}

func (t *Task) overdue(ctx context.Context, data actor.Envelope) (any, error) {
	state, err := t.client.GetState(ctx)
	if err != nil {
		return nil, fmt.Errorf("get task state: %w", err)
	}
	var task client.Task
	if err := data.Decode(&task); err != nil {
		return nil, fmt.Errorf("decode task data: %w", err)
	}

	if state.Task != nil && state.Onboarded && state.Task.DueDate.Equal(task.DueDate) &&
		!state.LastAlarmAt.Before(time.Now().Add(-alarmGracePeriod)) {
		return state, nil
	}

	if err := t.client.SetAlarm(ctx, Overdue, actor.AlarmProperties{
		DueTime:  time.Now().Add(t.sleep),
		Interval: duration.FromTimeDuration(notifyInterval).String(),
	}); err != nil {
		return nil, fmt.Errorf("setup alert alarm: %w", err)
	}

	dueChanged := state.Task == nil || !state.Task.DueDate.Equal(task.DueDate)
	if dueChanged {
		state.NotificationCounter = 0
		state.Completed = false
	}

	state.Task = &task
	state.Onboarded = true
	state.LastAlarmAt = time.Now()
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

	state.LastAlarmAt = time.Now()
	if err := t.client.SetState(ctx, state, nil); err != nil {
		return fmt.Errorf("store task state: %w", err)
	}

	if state.Completed {
		// If it has completed, then the alarm was not cleared up,
		// so try that again.
		slog.DebugContext(ctx, "task already completed")
		if err := t.client.DeleteAlarm(ctx, Overdue); err != nil {
			slog.ErrorContext(ctx, "cleanup completed task alarm", "error", err)
		}
		return nil
	}

	task, err := t.ticktick.GetTask(ctx, state.Task.ProjectID, state.Task.ID)
	if err != nil {
		return fmt.Errorf("refresh task data: %w", err)
	}

	if !task.DueDate.Equal(state.Task.DueDate) {
		slog.InfoContext(ctx, "task due date change, deleting alarm")
		return t.client.DeleteAlarm(ctx, Overdue)
	}

	if task.Status > 0 || task.CompletedTime != nil ||
		state.NotificationCounter >= t.maxNotifications {
		slog.DebugContext(ctx, "task completed")
		state.Completed = true
		state.Task = &task
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
	pctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()
	if err := t.pavlok.Send(
		pctx,
		fmt.Sprintf("Overdue task: %s", task.Title),
		client.Zap,
	); err != nil {
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
