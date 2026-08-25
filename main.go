package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/henrywhitaker3/windowframe/v2/config"
	"github.com/spf13/pflag"
)

func main() {
	ctx, cancel, conf := setup()
	defer cancel()
}

func setup() (context.Context, context.CancelFunc, *Config) {
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

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
	Token string `env:"TICKTICK_TOKEN"`

	LogLevel string `flag:"log-level"`
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
