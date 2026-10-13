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
	"github.com/henrywhitaker3/windowframe/v2/log"
)

func Setup() (context.Context, context.CancelFunc, *Config) {
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	conf, err := parseConfig()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	log.Setup(log.SlogLevel(conf.LogLevel))
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

type Runtime struct {
	PostgresURL config.RedactedString `env:"DB_URL"`
	PSK         config.RedactedString `env:"PSK"`
	Host        string                `env:"HOST"`
	Port        int                   `env:"PORT, default=7571"`
}

type Config struct {
	TickTickToken config.RedactedString `env:"TICKTICK_TOKEN"`
	PavlokToken   config.RedactedString `env:"PAVLOK_TOKEN"`

	Runtime Runtime `env:",prefix=RUNTIME_"`

	LogLevel string `env:"LOG_LEVEL, default=info"`

	MaxNotifications int           `env:"MAX_NOTIFICATIONS, default=5"`
	CheckInterval    time.Duration `env:"CHECK_INTERVAL, default=1m"`
	InteractionWait  time.Duration `env:"INTERACTION_WAIT, default=1m"`

	QuietTimes QuietTimes `env:",prefix=QUIET_TIMES_"`
}

func parseConfig() (*Config, error) {
	conf, err := config.NewParser[Config]().WithExtractors(
		config.NewEnvExtractor[Config](),
	).Parse()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return &conf, nil
}
