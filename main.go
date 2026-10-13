package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/henrywhitaker3/ticktick-events/internal/config"
	"github.com/henrywhitaker3/ticktick-events/internal/orchestrator"
	"github.com/henrywhitaker3/ticktick-events/internal/runtime"
	"github.com/henrywhitaker3/ticktick-events/internal/task"
)

func main() {
	ctx, cancel, conf := config.Setup()
	defer cancel()

	host, err := runtime.NewHost(conf.Runtime)
	if err != nil {
		slog.Error("init actor runtime", "error", err)
		os.Exit(1)
	}
	ticktick := client.New(string(conf.TickTickToken))
	pavlok := client.NewPavlokClient(string(conf.PavlokToken))
	if err := task.Register(host, task.Opts{
		Ticktick:         ticktick,
		Pavlok:           pavlok,
		Sleep:            conf.InteractionWait,
		Quiet:            conf.QuietTimes,
		MaxNotifications: conf.MaxNotifications,
	}); err != nil {
		slog.Error("register task actor", "error", err)
		os.Exit(1)
	}
	go func() {
		if err := host.Run(ctx); err != nil {
			slog.Error("start actor runtime", "error", err)
			os.Exit(1)
		}
	}()
	waitCtx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()
	select {
	case <-waitCtx.Done():
		slog.Error("wait for runtime to be ready", "error", waitCtx.Err())
		os.Exit(1)
	case <-host.Ready():
	}

	orch := orchestrator.New(orchestrator.OrchestratorOpts{
		TickTick: ticktick,
		Interval: conf.CheckInterval,
		Service:  host.Service(),
	})
	if err := orch.Run(ctx); err != nil {
		slog.Error("failed to run orchestrator", "error", err)
	}
}
