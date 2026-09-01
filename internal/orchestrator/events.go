package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/henrywhitaker3/windowframe/v2/events"
	"github.com/redis/rueidis"
)

type OverdueTask struct {
	Task   client.Task
	Remove func()
}

func HandleOverdueTask(
	ticktick *client.TickTickClient,
	pavlok *client.PavlokClient,
	redis rueidis.Client,
	sleep time.Duration,
) events.Listener[OverdueTask] {

	return func(ctx context.Context, event OverdueTask) error {
		slog.Debug("processing overdue task", "task", event.Task)

		if exists, err := checkIfTaskHasBeenProcessed(ctx, redis, event.Task); err != nil {
			return fmt.Errorf("could not check if task has beeen notified: %w", err)
		} else if exists {
			slog.Info("not sending duplicate notification", "task", event.Task)
			return nil
		}

		// Wait for interaction to set it to done
		slog.Debug("waiting for task interaction", "task", event.Task)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}

		task, err := ticktick.GetTask(ctx, event.Task.ProjectID, event.Task.ID)
		if err != nil {
			return fmt.Errorf("could not refresh task: %w", err)
		}

		if task.Status > 0 {
			slog.Debug("task completed", "task", task)
			return nil
		}

		slog.Info("sending zap", "task", task)
		zctx, cancel := context.WithTimeout(ctx, time.Second*3)
		defer cancel()
		if err := pavlok.Send(zctx, event.Task, client.Zap); err != nil {
			event.Remove()
			return fmt.Errorf("send zap: %w", err)
		}

		return storeNotifiedTask(ctx, redis, event.Task)
	}
}

func checkIfTaskHasBeenProcessed(
	ctx context.Context,
	c rueidis.Client,
	task client.Task,
) (bool, error) {
	if task.ID == "" {
		return false, fmt.Errorf("check processed task: task ID is empty")
	}

	cmd := c.B().Exists().Key(fmt.Sprintf("ticktick:%s", task.ID)).Build()
	exists, err := c.Do(ctx, cmd).AsInt64()
	if err != nil {
		return false, fmt.Errorf("check processed task in redis: %w", err)
	}

	return exists > 0, nil
}

func storeNotifiedTask(ctx context.Context, c rueidis.Client, task client.Task) error {
	tomorrow := time.Now().Add(time.Hour * 24 * 30)

	cmd := c.B().
		Set().
		Key(fmt.Sprintf("ticktick:%s", task.ID)).
		Value("notified").
		Exat(tomorrow).
		Build()
	if err := c.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("could not store task in redis: %w", err)
	}
	return nil
}
