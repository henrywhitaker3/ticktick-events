// Package habit
package habit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/henrywhitaker3/windowframe/v2/log"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/host/local"
)

type HabitGetter interface {
	GetHabit(ctx context.Context, id string) (client.Habit, error)
}

type StimulusSender interface {
	Send(ctx context.Context, task string, stimulus client.Stimulus) error
}

type Habit struct {
	client   actor.Client[habitState]
	pavlok   StimulusSender
	ticktick HabitGetter
}

type habitState struct {
	Habit        *client.Habit
	Onboarded    bool
	NextAlarm    time.Time
	LastAlertFor *time.Time
}

type Opts struct {
	TickTick HabitGetter
	Pavlok   StimulusSender
}

const (
	Type    = "habit"
	Onboard = "onboard"
	Alert   = "alert"
)

func Register(host *local.Host, opts Opts) error {
	return host.RegisterActor(Type, New(opts))
}

func New(opts Opts) actor.Factory {
	return func(actorID string, service *actor.Service) actor.Actor {
		return &Habit{
			client:   actor.NewActorClient[habitState](Type, actorID, service),
			ticktick: opts.TickTick,
			pavlok:   opts.Pavlok,
		}
	}
}

func (h *Habit) Invoke(ctx context.Context, method string, data actor.Envelope) (any, error) {
	switch method {
	case Onboard:
		return h.onboardHabit(ctx, data)
	default:
		return nil, fmt.Errorf("unknown method %s", method)
	}
}

func (h *Habit) Alarm(ctx context.Context, method string, data actor.Envelope) error {
	switch method {
	case Alert:
		return h.processAlert(ctx)
	default:
		return fmt.Errorf("unknown alarm %s", method)
	}
}

func (h *Habit) processAlert(ctx context.Context) error {
	state, err := h.client.GetState(ctx)
	if err != nil {
		return fmt.Errorf("get habit state: %w", err)
	}
	if state.Habit == nil {
		return fmt.Errorf("nil habit in state")
	}

	habit, err := h.ticktick.GetHabit(ctx, state.Habit.ID)
	if err != nil {
		return fmt.Errorf("refresh habit: %w", err)
	}
	state.Habit = &habit
	if err := h.client.SetState(ctx, state, nil); err != nil {
		return fmt.Errorf("set refreshed habit state: %w", err)
	}

	ctx = log.WithLogAttr(ctx, "state", state)
	slog.DebugContext(ctx, "processing alert")

	if state.LastAlertFor != nil && state.LastAlertFor.Equal(state.NextAlarm) {
		slog.DebugContext(ctx, "already alerted")
		return nil
	}
	if checkedInForAlert(state.Habit.LastCheckin, state.NextAlarm, state.Habit.TimeZone) {
		slog.InfoContext(ctx, "habit already checked in")
		state.LastAlertFor = &state.NextAlarm
		if err := h.client.SetState(ctx, state, nil); err != nil {
			return fmt.Errorf("set checked-in habit state: %w", err)
		}
		return nil
	}

	slog.InfoContext(ctx, "sending zap for missed habit")
	pctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()
	if err := h.pavlok.Send(
		pctx,
		fmt.Sprintf("Overdue habit: %s", state.Habit.Name),
		client.Zap,
	); err != nil {
		return fmt.Errorf("send zap: %w", err)
	}

	state.LastAlertFor = &state.NextAlarm
	if err := h.client.SetState(ctx, state, nil); err != nil {
		return fmt.Errorf("set habit state: %w", err)
	}

	return nil
}

func checkedInForAlert(lastCheckin *time.Time, alertTime time.Time, timeZone string) bool {
	if lastCheckin == nil {
		return false
	}

	location, err := time.LoadLocation(timeZone)
	if err != nil {
		location = time.UTC
	}
	return startOfDay(*lastCheckin, location).Equal(startOfDay(alertTime, location))
}

func startOfDay(value time.Time, location *time.Location) time.Time {
	value = value.In(location)
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, location)
}

func (h *Habit) onboardHabit(ctx context.Context, data actor.Envelope) (any, error) {
	state, err := h.client.GetState(ctx)
	if err != nil {
		return nil, fmt.Errorf("get habit state: %w", err)
	}

	ctx = log.WithLogAttr(ctx, "state", state)

	var habit client.Habit
	if err := data.Decode(&habit); err != nil {
		return nil, fmt.Errorf("decode habit data: %w", err)
	}

	state.Habit = &habit
	// Next checkin doesn't match, re-create the alarm
	if !state.NextAlarm.Equal(habit.NextCheckin) {
		slog.DebugContext(ctx, "setup habit alert", "time", habit.NextCheckin)
		if err := h.client.SetAlarm(ctx, Alert, actor.AlarmProperties{
			DueTime: habit.NextCheckin.UTC(),
		}); err != nil {
			return nil, fmt.Errorf("set habit alarm: %w", err)
		}
		state.NextAlarm = habit.NextCheckin
	}
	if err := h.client.SetState(ctx, state, nil); err != nil {
		return nil, fmt.Errorf("set habit state: %w", err)
	}
	return state, nil
}
