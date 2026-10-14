package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/ticktickapi"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestGetOverdueTasks(t *testing.T) {
	client, err := newClient("test-token", "https://ticktick.test", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", request.Method)
			}
			if request.URL.Path != "/open/v1/task/undone" {
				t.Errorf("path = %q, want /open/v1/task/undone", request.URL.Path)
			}
			if got := request.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("Authorization = %q, want Bearer test-token", got)
			}
			if got := request.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}

			var body struct {
				StartDate string `json:"startDate"`
				EndDate   string `json:"endDate"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode request body: %v", err)
			}
			start, startErr := parseTickTickTime(body.StartDate)
			end, endErr := parseTickTickTime(body.EndDate)
			if startErr != nil || endErr != nil {
				t.Errorf("date range = %#v, parse errors: %v, %v", body, startErr, endErr)
			} else if duration := end.Sub(start); duration != undoneTaskWindow {
				t.Errorf("date range = %v, want %v", duration, undoneTaskWindow)
			}

			return jsonResponse(request, `[
  {"id":"overdue","title":"Overdue","content":"Pay bill","dueDate":"2020-01-02T03:04:05+0000","snoozeDate":"2020-01-01T03:04:05+0000","status":0},
  {"id":"future","title":"Future","dueDate":"2999-01-02T03:04:05+0000","status":0},
  {"id":"undated","title":"Undated","status":0},
  {"id":"completed","title":"Completed","dueDate":"2020-01-02T03:04:05+0000","status":2}
]`), nil
		}),
	})
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	tasks, err := client.GetOverdueTasks(context.Background())
	if err != nil {
		t.Fatalf("GetOverdueTasks() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("GetOverdueTasks() returned %d tasks, want 1", len(tasks))
	}

	task := tasks[0]
	if task.ID != "overdue" || task.Title != "Overdue" || task.Content != "Pay bill" {
		t.Errorf("GetOverdueTasks() task = %#v", task)
	}
	if !task.DueDate.Equal(time.Date(2020, time.January, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("DueDate = %v", task.DueDate)
	}
	if task.SnoozeDate == nil ||
		!task.SnoozeDate.Equal(time.Date(2020, time.January, 1, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("SnoozeDate = %v", task.SnoozeDate)
	}
}

func TestGetTask(t *testing.T) {
	client, err := newClient("test-token", "https://ticktick.test", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodGet {
				t.Errorf("method = %q, want GET", request.Method)
			}
			if request.URL.Path != "/open/v1/project/project-1/task/task-1" {
				t.Errorf("path = %q", request.URL.Path)
			}

			return jsonResponse(request, `{
  "id":"task-1", "projectId":"project-1", "title":"Task", "content":"Content", "desc":"Description",
  "startDate":"2020-01-01T03:04:05+0000", "dueDate":"2020-01-02T03:04:05+0000",
  "snoozeDate":"2020-01-03T03:04:05+0000", "completedTime":"2020-01-04T03:04:05+0000",
  "status":2, "kind":"CHECKLIST"
}`), nil
		}),
	})
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	task, err := client.GetTask(context.Background(), "project-1", "task-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.ProjectID != "project-1" || task.Description != "Description" ||
		task.Kind != "CHECKLIST" || task.Status != 2 {
		t.Errorf("GetTask() task = %#v", task)
	}
	if task.SnoozeDate == nil ||
		!task.SnoozeDate.Equal(time.Date(2020, time.January, 3, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("SnoozeDate = %v", task.SnoozeDate)
	}
}

func TestGetHabits(t *testing.T) {
	client, err := newClient("test-token", "https://ticktick.test", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if got := request.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("Authorization = %q, want Bearer test-token", got)
			}

			switch request.URL.Path {
			case "/open/v1/habit":
				if request.Method != http.MethodGet {
					t.Errorf("method = %q, want GET", request.Method)
				}
				return jsonResponse(request, `[
  {"id":"habit-1","name":"Read","status":0,"repeatRule":"RRULE:FREQ=DAILY;INTERVAL=1","reminders":["TRIGGER:P0DT9H0M0S"],"targetStartDate":20261001},
  {"id":"habit-2","name":"Exercise","status":2,"repeatRule":"RRULE:FREQ=DAILY;INTERVAL=1","reminders":["09:30"]},
  {"id":"habit-3","name":"No reminder","status":0}
]`), nil
			case "/open/v1/habit/checkins":
				if got := request.URL.Query().Get("habitIds"); got != "habit-1,habit-2" {
					t.Errorf("habitIds = %q, want habit-1,habit-2", got)
				}
				if request.URL.Query().Get("from") == "" || request.URL.Query().Get("to") == "" {
					t.Errorf("check-in range = %q, want both from and to", request.URL.RawQuery)
				}
				return jsonResponse(request, `[
  {"habitId":"habit-1","checkins":[{"stamp":20261012},{"stamp":20261013,"time":"2026-10-13T09:00:00+0000"}]}
]`), nil
			default:
				t.Errorf("path = %q", request.URL.Path)
				return nil, nil
			}

			return nil, nil
		}),
	})
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	habits, err := client.GetHabits(context.Background())
	if err != nil {
		t.Fatalf("GetHabits() error = %v", err)
	}
	if len(habits) != 2 {
		t.Fatalf("GetHabits() returned %d habits, want 2", len(habits))
	}

	first := habits[0]
	if first.ID != "habit-1" || first.Name != "Read" || first.Status != 0 {
		t.Errorf("GetHabits() first habit = %#v", first)
	}
	if first.LastCheckin == nil ||
		!first.LastCheckin.Equal(time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("LastCheckin = %v", first.LastCheckin)
	}
	if first.NextCheckin.In(time.Local).Hour() != 9 || first.NextCheckin.In(time.Local).Minute() != 0 {
		t.Errorf("NextCheckin = %v, want a 09:00 reminder", first.NextCheckin)
	}

	second := habits[1]
	if second.LastCheckin != nil {
		t.Errorf("GetHabits() absent check-ins = %#v", second)
	}
	if second.NextCheckin.In(time.Local).Hour() != 9 || second.NextCheckin.In(time.Local).Minute() != 30 {
		t.Errorf("NextCheckin = %v, want a 09:30 reminder", second.NextCheckin)
	}
}

func TestGetHabitRejectsHabitWithoutReminder(t *testing.T) {
	client, err := newClient("test-token", "https://ticktick.test", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/open/v1/habit/habit-1" {
				t.Errorf("path = %q, want /open/v1/habit/habit-1", request.URL.Path)
			}
			return jsonResponse(request, `{"id":"habit-1","name":"No reminder","status":0}`), nil
		}),
	})
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	_, err = client.GetHabit(context.Background(), "habit-1")
	if err == nil || !strings.Contains(err.Error(), "no reminder configured") {
		t.Fatalf("GetHabit() error = %v, want no reminder configured", err)
	}
}

func TestGetHabit(t *testing.T) {
	client, err := newClient("test-token", "https://ticktick.test", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case "/open/v1/habit/habit-1":
				if request.Method != http.MethodGet {
					t.Errorf("method = %q, want GET", request.Method)
				}
				if got := request.Header.Get("Authorization"); got != "Bearer test-token" {
					t.Errorf("Authorization = %q, want Bearer test-token", got)
				}
				return jsonResponse(request, `{
  "id":"habit-1","name":"Read","status":0,"repeatRule":"RRULE:FREQ=DAILY;INTERVAL=1","reminders":["09:30"],"targetStartDate":20261001
}`), nil
			case "/open/v1/habit/checkins":
				if got := request.URL.Query().Get("habitIds"); got != "habit-1" {
					t.Errorf("habitIds = %q, want habit-1", got)
				}
				return jsonResponse(request, `[
  {"habitId":"habit-1","checkins":[{"stamp":20261013,"time":"2026-10-13T09:00:00+0000"}]}
]`), nil
			default:
				t.Errorf("path = %q", request.URL.Path)
				return nil, nil
			}
		}),
	})
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	habit, err := client.GetHabit(context.Background(), "habit-1")
	if err != nil {
		t.Fatalf("GetHabit() error = %v", err)
	}
	if habit.ID != "habit-1" || habit.Name != "Read" || habit.Status != 0 {
		t.Errorf("GetHabit() habit = %#v", habit)
	}
	if habit.LastCheckin == nil ||
		!habit.LastCheckin.Equal(time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("LastCheckin = %v", habit.LastCheckin)
	}
	if habit.NextCheckin.In(time.Local).Hour() != 9 || habit.NextCheckin.In(time.Local).Minute() != 30 {
		t.Errorf("NextCheckin = %v, want a 09:30 reminder", habit.NextCheckin)
	}
}

func TestHabitCheckinDateRangeIncludesToday(t *testing.T) {
	location, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load Europe/London location: %v", err)
	}

	from, to := habitCheckinDateRange(
		time.Date(2026, time.October, 14, 12, 1, 0, 0, location),
	)
	if from != 20260930 || to != 20261015 {
		t.Errorf("habitCheckinDateRange() = (%d, %d), want (20260930, 20261015)", from, to)
	}
}

func TestNextHabitCheckin(t *testing.T) {
	now := time.Date(2026, time.October, 14, 15, 0, 0, 0, time.UTC)
	dailyRule := "RRULE:FREQ=DAILY;INTERVAL=1"
	weeklyRule := "RRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR"
	start := 20261001
	nineAMReminder := []string{"TRIGGER:P0DT9H0M0S"}
	halfPastNineReminder := []string{"09:30"}

	testCases := []struct {
		name  string
		habit ticktickapi.Habit
		want  time.Time
	}{
		{
			name:  "daily",
			habit: ticktickapi.Habit{Id: "daily", RepeatRule: &dailyRule, Reminders: &nineAMReminder, TargetStartDate: &start},
			want:  time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC),
		},
		{
			name:  "weekly days",
			habit: ticktickapi.Habit{Id: "weekly", RepeatRule: &weeklyRule, Reminders: &halfPastNineReminder, TargetStartDate: &start},
			want:  time.Date(2026, time.October, 16, 9, 30, 0, 0, time.UTC),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := nextHabitCheckin(testCase.habit, now)
			if err != nil {
				t.Fatalf("nextHabitCheckin() error = %v", err)
			}
			if !got.Equal(testCase.want) {
				t.Errorf("nextHabitCheckin() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestNextHabitCheckinConvertsLocalReminderToUTC(t *testing.T) {
	location, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load Europe/London location: %v", err)
	}
	now := time.Date(2026, time.October, 14, 11, 35, 0, 0, location)
	rule := "RRULE:FREQ=DAILY;INTERVAL=1"
	reminders := []string{"11:40"}
	start := 20261001

	got, err := nextHabitCheckin(ticktickapi.Habit{
		Id:              "habit-1",
		RepeatRule:      &rule,
		Reminders:       &reminders,
		TargetStartDate: &start,
	}, now)
	if err != nil {
		t.Fatalf("nextHabitCheckin() error = %v", err)
	}
	want := time.Date(2026, time.October, 14, 10, 40, 0, 0, time.UTC)
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("nextHabitCheckin() = %v (%v), want %v (UTC)", got, got.Location(), want)
	}
}

func TestParseTickTickTime(t *testing.T) {
	got, err := parseTickTickTime("2026-03-04T23:58:20.000+0000")
	if err != nil {
		t.Fatalf("parseTickTickTime() error = %v", err)
	}
	want := time.Date(2026, time.March, 4, 23, 58, 20, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("parseTickTickTime() = %v, want %v", got, want)
	}
}

func TestTickTickClientEmitsHTTPSpan(t *testing.T) {
	recorder := installSpanRecorder(t)
	client, err := newClient("test-token", "https://ticktick.test", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return jsonResponse(request, `{"id":"task-1","title":"Task","status":0}`), nil
		}),
	})
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	if _, err := client.GetTask(context.Background(), "project-1", "task-1"); err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}

	assertClientSpan(t, recorder.Ended(), "GET /open/v1/project/project-1/task/task-1")
}

func TestPavlokClientEmitsHTTPSpan(t *testing.T) {
	recorder := installSpanRecorder(t)
	client := newPavlokClient("test-token", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return jsonResponse(request, `{}`), nil
		}),
	})

	if err := client.Send(context.Background(), "Task", Zap); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	assertClientSpan(t, recorder.Ended(), "POST /api/v5/stimulus/send")
}

func installSpanRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
	})
	return recorder
}

func assertClientSpan(t *testing.T, spans []sdktrace.ReadOnlySpan, wantName string) {
	t.Helper()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := spans[0].Name(); got != wantName {
		t.Errorf("span name = %q, want %q", got, wantName)
	}
	if got := spans[0].SpanKind(); got != trace.SpanKindClient {
		t.Errorf("span kind = %v, want %v", got, trace.SpanKindClient)
	}
}

func jsonResponse(request *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}
