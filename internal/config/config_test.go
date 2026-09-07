package config

import (
	"testing"
	"time"

	"github.com/henrywhitaker3/windowframe/v2/config"
	"github.com/stretchr/testify/require"
)

func TestTimeRangeUnmarshalText(t *testing.T) {
	var timeRange TimeRange
	require.NoError(t, timeRange.UnmarshalText([]byte("09:30:00-17:00:00")))
	require.Equal(t, 9, timeRange.Start.Hour())
	require.Equal(t, 30, timeRange.Start.Minute())
	require.Equal(t, 17, timeRange.End.Hour())

	for _, value := range []string{"", "09:00:00", "09:00-17:00:00", "09:00:00-17:00"} {
		t.Run(value, func(t *testing.T) {
			var timeRange TimeRange
			require.Error(t, timeRange.UnmarshalText([]byte(value)))
		})
	}
}

func TestTimeRangeIn(t *testing.T) {
	tests := []struct {
		name   string
		range_ string
		time   string
		want   bool
	}{
		{name: "at start", range_: "09:00:00-17:00:00", time: "09:00:00", want: true},
		{name: "during range", range_: "09:00:00-17:00:00", time: "12:00:00", want: true},
		{name: "at end", range_: "09:00:00-17:00:00", time: "17:00:00", want: true},
		{name: "before range", range_: "09:00:00-17:00:00", time: "08:59:59", want: false},
		{
			name:   "crosses midnight before midnight",
			range_: "22:00:00-06:00:00",
			time:   "23:00:00",
			want:   true,
		},
		{
			name:   "crosses midnight after midnight",
			range_: "22:00:00-06:00:00",
			time:   "05:00:00",
			want:   true,
		},
		{
			name:   "crosses midnight outside",
			range_: "22:00:00-06:00:00",
			time:   "12:00:00",
			want:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var timeRange TimeRange
			require.NoError(t, timeRange.UnmarshalText([]byte(test.range_)))

			current, err := time.Parse(time.TimeOnly, test.time)
			require.NoError(t, err)
			require.Equal(t, test.want, timeRange.In(current))
		})
	}
}

func TestTimeRangeEmpty(t *testing.T) {
	require.True(t, (TimeRange{}).Empty())
	require.True(t, (TimeRange{Start: time.Now()}).Empty())
}

func TestQuietTimesIn(t *testing.T) {
	var weekdayQuietTime TimeRange
	require.NoError(t, weekdayQuietTime.UnmarshalText([]byte("09:00:00-17:00:00")))

	quietTimes := QuietTimes{
		Monday: weekdayQuietTime,
		Sunday: weekdayQuietTime,
	}

	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{
			name: "within monday range",
			at:   time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "outside monday range",
			at:   time.Date(2026, time.September, 7, 18, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "empty tuesday range returns false",
			at:   time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "within sunday range",
			at:   time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC),
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, quietTimes.In(test.at))
		})
	}
}

func TestConfigParsesQuietTimes(t *testing.T) {
	t.Setenv("QUIET_TIMES_MONDAY", "22:00:00-06:00:00")

	conf, err := config.NewParser[Config]().WithExtractors(
		config.NewEnvExtractor[Config](),
	).Parse()
	require.NoError(t, err)
	require.Equal(t, 22, conf.QuietTimes.Monday.Start.Hour())
	require.Equal(t, 6, conf.QuietTimes.Monday.End.Hour())
}
