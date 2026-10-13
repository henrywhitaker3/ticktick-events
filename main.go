package main

import (
	"log/slog"
	"os"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/henrywhitaker3/ticktick-events/internal/config"
	"github.com/henrywhitaker3/ticktick-events/internal/metrics"
	"github.com/henrywhitaker3/ticktick-events/internal/orchestrator"
	"github.com/henrywhitaker3/ticktick-events/internal/runtime"
	"github.com/henrywhitaker3/ticktick-events/internal/task"
)

var (
	version = "dev"
)

func main() {
	ctx, cancel, conf := config.Setup(version)
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
	if err := orchestrator.Regster(host, orchestrator.OrchestratorOpts{
		Service:  host.Service(),
		Interval: conf.CheckInterval,
		TickTick: ticktick,
	}); err != nil {
		slog.Error("register orchestrator", "error", err)
		os.Exit(1)
	}
	go func() {
		if err := metrics.New(8080).Run(ctx); err != nil {
			slog.Error("run metrics server", "error", err)
			os.Exit(1)
		}
	}()

	if err := host.Run(ctx); err != nil {
		slog.Error("start actor runtime", "error", err)
		os.Exit(1)
	}
}
