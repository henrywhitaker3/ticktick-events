package main

import (
	"log/slog"
	"os"

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
	task.Register(host, task.Opts{
		Ticktick: ticktick,
		Pavlok:   pavlok,
		Sleep:    conf.InteractionWait,
		Quiet:    conf.QuietTimes,
	})

	orch := orchestrator.New(orchestrator.OrchestratorOpts{
		TickTick: ticktick,
		Interval: conf.CheckInterval,
		Service:  host.Service(),
	})
	if err := orch.Run(ctx); err != nil {
		slog.Error("failed to run orchestrator", "error", err)
	}
}
