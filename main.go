package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/henrywhitaker3/ticktick-events/internal/orchestrator"
	"github.com/henrywhitaker3/windowframe/v2/config"
	"github.com/henrywhitaker3/windowframe/v2/events"
	"github.com/redis/rueidis"
	"github.com/spf13/pflag"
)

func main() {
	ctx, cancel, conf := setup()
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
	handler.Listen(orchestrator.HandleOverdueTask(ticktick, pavlok, redis))
	go handler.Run(ctx)
	defer handler.Flush()

	orch := orchestrator.New(ticktick, handler)
	if err := orch.Run(ctx); err != nil {
		slog.Error("failed to run orchestrator", "error", err)
	}
}

func setup() (context.Context, context.CancelFunc, *Config) {
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	set := setupFlags()
	if err := set.Parse(os.Args[1:]); err != nil {
		fmt.Println(err)
		os.Exit(2)
	}

	conf, err := parseConfig(set)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level: logLevel(conf.LogLevel),
		},
	)))
	slog.Debug("loaded config", "config", conf)

	return ctx, cancel, conf
}

type Config struct {
	TickTickToken string `env:"TICKTICK_TOKEN"`
	PavlokToken   string `env:"PAVLOK_TOKEN"`

	LogLevel string `flag:"log-level"`

	RedisURL string `flag:"redis-url"`
}

func parseConfig(set *pflag.FlagSet) (*Config, error) {
	conf, err := config.NewParser[Config]().WithExtractors(
		config.NewEnvExtractor[Config](),
		config.NewPFlagExtractor[Config](set),
	).Parse()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return &conf, nil
}

func setupFlags() *pflag.FlagSet {
	set := pflag.NewFlagSet("flags", pflag.ContinueOnError)
	set.String("log-level", "info", "The level to log at")
	set.String("redis-url", "127.0.0.1:6379", "The redis url to connect to")
	return set
}

func logLevel(level string) slog.Level {
	switch level {
	case "error":
		return slog.LevelError
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "info":
		fallthrough
	default:
		return slog.LevelInfo
	}
}
