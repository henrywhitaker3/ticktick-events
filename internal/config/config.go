// Package config
package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/henrywhitaker3/windowframe/v2/config"
	"github.com/spf13/pflag"
)

func Setup() (context.Context, context.CancelFunc, *Config) {
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

type TimeRange struct {
	Start time.Time
	End   time.Time
}

// UnmarshalText parses a time range in the form "HH:mm:ss-HH:mm:ss".
func (t *TimeRange) UnmarshalText(text []byte) error {
	start, end, ok := strings.Cut(string(text), "-")
	if !ok || start == "" || end == "" {
		return fmt.Errorf("invalid time range %q: expected HH:mm:ss-HH:mm:ss", text)
	}

	startTime, err := time.Parse(time.TimeOnly, start)
	if err != nil {
		return fmt.Errorf("parse start time: %w", err)
	}
	endTime, err := time.Parse(time.TimeOnly, end)
	if err != nil {
		return fmt.Errorf("parse end time: %w", err)
	}

	t.Start = startTime
	t.End = endTime
	return nil
}

func (t TimeRange) Empty() bool {
	return t.Start.IsZero() || t.End.IsZero()
}

// In returns true if the given time is within the range
func (t TimeRange) In(c time.Time) bool {
	if t.Empty() {
		return false
	}

	current := time.Date(
		0,
		time.January,
		1,
		c.Hour(),
		c.Minute(),
		c.Second(),
		c.Nanosecond(),
		time.UTC,
	)
	if t.Start.Before(t.End) || t.Start.Equal(t.End) {
		return !current.Before(t.Start) && !current.After(t.End)
	}

	// A range such as 22:00:00-06:00:00 spans midnight.
	return !current.Before(t.Start) || !current.After(t.End)
}

type QuietTimes struct {
	Monday    TimeRange `env:"MONDAY"`
	Tuesday   TimeRange `env:"TUESDAY"`
	Wednesday TimeRange `env:"WEDNESDAY"`
	Thursday  TimeRange `env:"THURSDAY"`
	Friday    TimeRange `env:"FRIDAY"`
	Saturday  TimeRange `env:"SATURDAY"`
	Sunday    TimeRange `env:"SUNDAY"`
}

// In returns true if the given time is within the current day's range
func (q QuietTimes) In(c time.Time) bool {
	switch c.Weekday() {
	case time.Monday:
		return q.Monday.In(c)
	case time.Tuesday:
		return q.Tuesday.In(c)
	case time.Wednesday:
		return q.Wednesday.In(c)
	case time.Thursday:
		return q.Thursday.In(c)
	case time.Friday:
		return q.Friday.In(c)
	case time.Saturday:
		return q.Saturday.In(c)
	case time.Sunday:
		return q.Sunday.In(c)
	default:
		return false
	}
}

type Config struct {
	TickTickToken string `env:"TICKTICK_TOKEN"`
	PavlokToken   string `env:"PAVLOK_TOKEN"`

	LogLevel string `flag:"log-level"`

	RedisURL string `flag:"redis-url"`

	CheckInterval   time.Duration `flag:"check-interval"`
	InteractionWait time.Duration `flag:"interaction-wait"`

	QuietTimes QuietTimes `env:",prefix=QUIET_TIMES_"`
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
	set.Duration(
		"check-interval",
		time.Minute,
		"The amount of time to wait before retrieving overdue tasks",
	)
	set.Duration(
		"interaction-wait",
		time.Minute,
		"The amount of time to wait in the event handler before sending a zap. Gives time for the user to mark the task complete after it is due",
	)
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
