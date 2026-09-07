package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/henrywhitaker3/ticktick-events/internal/config"
	"github.com/henrywhitaker3/ticktick-events/internal/orchestrator"
	"github.com/henrywhitaker3/windowframe/v2/events"
	"github.com/redis/rueidis"
)

func main() {
	ctx, cancel, conf := config.Setup()
	defer cancel()

	redis, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{conf.RedisURL},
	})
	if err != nil {
		slog.Error("could not connect to redis", "error", err)
		os.Exit(1)
	}

	ticktick := client.New(conf.TickTickToken)
	pavlok := client.NewPavlokClient(conf.PavlokToken)

	handler := events.New(events.EventHandlerOptions{
		HandlerTimeout: time.Minute * 2,
	})
	_ = handler.Listen(
		orchestrator.HandleOverdueTask(
			ticktick,
			pavlok,
			redis,
			conf.InteractionWait,
			conf.QuietTimes,
		),
	)
	go handler.Run(ctx)
	defer handler.Flush()

	orch := orchestrator.New(orchestrator.OrchestratorOpts{
		TickTick: ticktick,
		Interval: conf.CheckInterval,
		Events:   handler,
	})
	if err := orch.Run(ctx); err != nil {
		slog.Error("failed to run orchestrator", "error", err)
	}
}
