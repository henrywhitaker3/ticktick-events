package task

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/henrywhitaker3/windowframe/v2/log"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/cronjob"
	"github.com/italypaleale/francis/host/local"
)

func newCleanupTasksCronjob(host *local.Host) (*cronjob.CronJob, error) {
	return cronjob.New(
		"cleanup-tasks",
		cronjob.WithInterval(time.Hour),
		cronjob.WithImmediate(),
		cronjob.WithLogger(slog.With("component", "cleanup")),
		cronjob.WithJob(cleanupTasks(host)),
	)
}

type taskStateStore interface {
	ListStates(context.Context, string, *actor.ListStatesOpts) (actor.StateList, error)
	Invoke(
		ctx context.Context,
		actorType string,
		actorID string,
		method string,
		data any,
		optsFn ...actor.InvokeOption,
	) (actor.Envelope, error)
}

func cleanupTasks(host taskStateStore) func(context.Context) error {
	return func(ctx context.Context) error {
		hasMore := true
		cursor := ""

		for hasMore {
			actors, err := host.ListStates(ctx, Type, &actor.ListStatesOpts{
				IncludeData: true,
				After:       cursor,
				Limit:       25,
			})
			if err != nil {
				return fmt.Errorf("list task actors: %w", err)
			}

			for _, a := range actors.States {
				if err := handleActor(ctx, host, a.ActorID, a.Data); err != nil {
					slog.Error("delete actor state", "error", err)
				}
			}

			cursor = actors.AfterID()
			hasMore = actors.HasMore
		}

		return nil
	}
}

func handleActor(
	ctx context.Context,
	host taskStateStore,
	actorID string,
	data actor.Envelope,
) error {
	var state taskState
	if err := data.Decode(&state); err != nil {
		return fmt.Errorf("decode task actor state: %w", err)
	}
	ctx = log.WithLogAttr(ctx, "state", state)
	slog.DebugContext(ctx, "checking if task can be forgotten")

	if !state.Completed {
		return nil
	}

	slog.DebugContext(ctx, "deleting actor state")

	if _, err := host.Invoke(ctx, Type, actorID, Stop, nil); err != nil {
		return fmt.Errorf("invoke task stop: %w", err)
	}

	return nil
}
