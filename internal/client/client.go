// Package client provides TickTick operations used by the application.
package client

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/ticktickapi"
	"github.com/teambition/rrule-go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	apiBaseURL         = "https://api.ticktick.com"
	undoneTaskWindow   = 14 * 24 * time.Hour
	habitCheckinWindow = 14 * 24 * time.Hour
	tickTickTimeLayout = "2006-01-02T15:04:05-0700"
	tickTickDateLayout = "20060102"

	TaskStatusNormal = 0
)

var tickTickReminderDuration = regexp.MustCompile(
	`^TRIGGER(?:;RELATED=(?:START|END))?:P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`,
)

type TickTickClient struct {
	api ticktickapi.ClientWithResponsesInterface
}

type Task struct {
	ID            string
	ProjectID     string
	Title         string
	Content       string
	Description   string
	DueDate       time.Time
	SnoozeDate    *time.Time
	CompletedTime *time.Time
	StartDate     *time.Time
	Status        int
	Kind          string
}

type ChecklistItem struct {
	ID            string
	Title         string
	Status        int
	CompletedTime *time.Time
	StartDate     *time.Time
	IsAllDay      bool
	SortOrder     int64
	TimeZone      string
}

type Habit struct {
	ID          string
	Name        string
	Status      int
	LastCheckin *time.Time
	NextCheckin time.Time
}

func New(token string) *TickTickClient {
	client, err := newClient(token, apiBaseURL, http.DefaultClient)
	if err != nil {
		panic(fmt.Sprintf("create TickTick client: %v", err))
	}
	return client
}

func newClient(token, baseURL string, httpClient *http.Client) (*TickTickClient, error) {
	api, err := ticktickapi.NewClientWithResponses(
		baseURL,
		ticktickapi.WithHTTPClient(newInstrumentedHTTPClient(httpClient, "ticktick HTTP request")),
		ticktickapi.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("create generated TickTick client: %w", err)
	}

	return &TickTickClient{api: api}, nil
}

// newInstrumentedHTTPClient returns a copy of httpClient whose transport emits
// an OpenTelemetry client span for every HTTP round trip. Copying leaves a
// caller-supplied client unchanged while preserving its configuration.
func newInstrumentedHTTPClient(httpClient *http.Client, spanName string) *http.Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	instrumented := *httpClient
	instrumented.Transport = otelhttp.NewTransport(
		httpClient.Transport,
		otelhttp.WithSpanNameFormatter(func(operation string, req *http.Request) string {
			if req == nil {
				return spanName
			}
			return fmt.Sprintf("%s %s", req.Method, req.URL.Path)
		}),
	)
	return &instrumented
}

// GetOverdueTasks retrieves undone tasks from the last 14 days and returns the
// subset with a due date before the current time. TickTick's undone-tasks
// endpoint does not accept a date range longer than 14 days.
func (c *TickTickClient) GetOverdueTasks(ctx context.Context) ([]Task, error) {
	now := time.Now()
	response, err := c.api.ListUndoneTasksWithResponse(
		ctx,
		ticktickapi.ListUndoneTasksJSONRequestBody{
			StartDate: formatTickTickTime(now.Add(-undoneTaskWindow)),
			EndDate:   formatTickTickTime(now),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list undone tasks: %w", err)
	}
	if err := validateUndoneTasksResponse(response); err != nil {
		return nil, err
	}

	overdue := make([]Task, 0, len(*response.JSON200))
	for _, task := range *response.JSON200 {
		if task.Status != TaskStatusNormal || task.DueDate == nil {
			continue
		}

		dueDate, err := parseTickTickTime(*task.DueDate)
		if err != nil {
			return nil, fmt.Errorf("parse due date for task %q: %w", task.Id, err)
		}
		if !dueDate.Before(now) {
			continue
		}

		mappedTask, err := taskFromAPI(task)
		if err != nil {
			return nil, fmt.Errorf("map task %q: %w", task.Id, err)
		}
		mappedTask.DueDate = dueDate
		overdue = append(overdue, mappedTask)
	}

	return overdue, nil
}

// GetTask retrieves a task by its project and task IDs.
func (c *TickTickClient) GetTask(ctx context.Context, projectID, taskID string) (Task, error) {
	response, err := c.api.GetTaskWithResponse(ctx, projectID, taskID)
	if err != nil {
		return Task{}, fmt.Errorf("get task: %w", err)
	}
	if response == nil {
		return Task{}, fmt.Errorf("get task: empty response")
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return Task{}, responseError("get task", response.StatusCode(), response.Body)
	}

	task, err := taskFromAPI(*response.JSON200)
	if err != nil {
		return Task{}, fmt.Errorf("map task %q: %w", response.JSON200.Id, err)
	}
	return task, nil
}

// GetHabits retrieves all habits available to the authenticated user.
func (c *TickTickClient) GetHabits(ctx context.Context) ([]Habit, error) {
	response, err := c.api.ListHabitsWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("list habits: %w", err)
	}
	if err := validateHabitsResponse(response); err != nil {
		return nil, err
	}
	remindedHabits := make([]ticktickapi.Habit, 0, len(*response.JSON200))
	for _, habit := range *response.JSON200 {
		if hasHabitReminder(habit) {
			remindedHabits = append(remindedHabits, habit)
		}
	}
	if len(remindedHabits) == 0 {
		return []Habit{}, nil
	}

	slog.DebugContext(ctx, "got raw habits", "habits", response.JSON200)

	now := time.Now()
	habitIDs := make([]string, 0, len(remindedHabits))
	for _, habit := range remindedHabits {
		habitIDs = append(habitIDs, habit.Id)
	}
	lastCheckins, err := c.getHabitCheckins(ctx, habitIDs, now)
	if err != nil {
		return nil, err
	}

	habits := make([]Habit, 0, len(remindedHabits))
	for _, source := range remindedHabits {
		habit, err := habitFromAPI(source, lastCheckins[source.Id], now)
		if err != nil {
			return nil, fmt.Errorf("map habit %q: %w", source.Id, err)
		}
		habits = append(habits, habit)
	}

	return habits, nil
}

// GetHabit retrieves a habit by ID.
func (c *TickTickClient) GetHabit(ctx context.Context, habitID string) (Habit, error) {
	response, err := c.api.GetHabitWithResponse(ctx, habitID)
	if err != nil {
		return Habit{}, fmt.Errorf("get habit: %w", err)
	}
	if err := validateHabitResponse(response); err != nil {
		return Habit{}, err
	}
	if !hasHabitReminder(*response.JSON200) {
		return Habit{}, fmt.Errorf("get habit %q: no reminder configured", response.JSON200.Id)
	}

	now := time.Now()
	lastCheckins, err := c.getHabitCheckins(ctx, []string{habitID}, now)
	if err != nil {
		return Habit{}, err
	}

	habit, err := habitFromAPI(*response.JSON200, lastCheckins[response.JSON200.Id], now)
	if err != nil {
		return Habit{}, fmt.Errorf("map habit %q: %w", response.JSON200.Id, err)
	}
	return habit, nil
}

func hasHabitReminder(habit ticktickapi.Habit) bool {
	return habit.Reminders != nil && len(*habit.Reminders) > 0
}

func (c *TickTickClient) getHabitCheckins(
	ctx context.Context,
	habitIDs []string,
	now time.Time,
) (map[string]time.Time, error) {
	response, err := c.api.GetHabitCheckinsWithResponse(ctx, &ticktickapi.GetHabitCheckinsParams{
		HabitIds: strings.Join(habitIDs, ","),
		From:     tickTickDateStamp(now.AddDate(0, 0, -14)),
		To:       tickTickDateStamp(now),
	})
	if err != nil {
		return nil, fmt.Errorf("get habit check-ins: %w", err)
	}
	if err := validateHabitCheckinsResponse(response); err != nil {
		return nil, err
	}

	latest := make(map[string]time.Time, len(*response.JSON200))
	for _, checkinDocument := range *response.JSON200 {
		for _, checkin := range checkinDocument.Checkins {
			checkinTime, err := habitCheckinTime(checkin)
			if err != nil {
				return nil, fmt.Errorf(
					"parse check-in for habit %q: %w",
					checkinDocument.HabitId,
					err,
				)
			}
			if previous, ok := latest[checkinDocument.HabitId]; !ok || checkinTime.After(previous) {
				latest[checkinDocument.HabitId] = checkinTime
			}
		}
	}
	return latest, nil
}

func validateUndoneTasksResponse(response *ticktickapi.ListUndoneTasksResponse) error {
	if response == nil {
		return fmt.Errorf("list undone tasks: empty response")
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return responseError("list undone tasks", response.StatusCode(), response.Body)
	}
	return nil
}

func validateHabitsResponse(response *ticktickapi.ListHabitsResponse) error {
	if response == nil {
		return fmt.Errorf("list habits: empty response")
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return responseError("list habits", response.StatusCode(), response.Body)
	}
	return nil
}

func validateHabitResponse(response *ticktickapi.GetHabitResponse) error {
	if response == nil {
		return fmt.Errorf("get habit: empty response")
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return responseError("get habit", response.StatusCode(), response.Body)
	}
	return nil
}

func validateHabitCheckinsResponse(response *ticktickapi.GetHabitCheckinsResponse) error {
	if response == nil {
		return fmt.Errorf("get habit check-ins: empty response")
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return responseError("get habit check-ins", response.StatusCode(), response.Body)
	}
	return nil
}

func responseError(operation string, statusCode int, body []byte) error {
	message := strings.TrimSpace(string(body))
	if message == "" {
		return fmt.Errorf("%s: unexpected HTTP status %d", operation, statusCode)
	}
	return fmt.Errorf("%s: unexpected HTTP status %d: %s", operation, statusCode, message)
}

func formatTickTickTime(value time.Time) string {
	return value.Format(tickTickTimeLayout)
}

func parseTickTickTime(value string) (time.Time, error) {
	parsed, err := time.Parse(tickTickTimeLayout, value)
	if err == nil {
		return parsed, nil
	}

	parsed, rfc3339Err := time.Parse(time.RFC3339, value)
	if rfc3339Err == nil {
		return parsed, nil
	}

	return time.Time{}, fmt.Errorf("expected TickTick timestamp: %q", value)
}

func parseTickTickTimePointer(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := parseTickTickTime(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func taskFromAPI(source ticktickapi.Task) (Task, error) {
	snoozeDate, err := parseTickTickTimePointer(source.SnoozeDate)
	if err != nil {
		return Task{}, fmt.Errorf("parse snooze date: %w", err)
	}
	completedTime, err := parseTickTickTimePointer(source.CompletedTime)
	if err != nil {
		return Task{}, fmt.Errorf("parse completed time: %w", err)
	}
	startDate, err := parseTickTickTimePointer(source.StartDate)
	if err != nil {
		return Task{}, fmt.Errorf("parse start date: %w", err)
	}
	dueDate, err := parseTickTickTimePointer(source.DueDate)
	if err != nil {
		return Task{}, fmt.Errorf("parse due date: %w", err)
	}

	return Task{
		ID:            source.Id,
		ProjectID:     stringValue(source.ProjectId),
		Title:         source.Title,
		Content:       stringValue(source.Content),
		Description:   stringValue(source.Desc),
		DueDate:       timeValue(dueDate),
		SnoozeDate:    snoozeDate,
		CompletedTime: completedTime,
		StartDate:     startDate,
		Status:        source.Status,
		Kind:          stringValue(source.Kind),
	}, nil
}

func habitFromAPI(source ticktickapi.Habit, lastCheckin, now time.Time) (Habit, error) {
	nextCheckin, err := nextHabitCheckin(source, now)
	if err != nil {
		return Habit{}, err
	}

	var lastCheckinPointer *time.Time
	if !lastCheckin.IsZero() {
		lastCheckinPointer = &lastCheckin
	}

	return Habit{
		ID:          source.Id,
		Name:        source.Name,
		Status:      source.Status,
		LastCheckin: lastCheckinPointer,
		NextCheckin: nextCheckin,
	}, nil
}

func habitCheckinTime(checkin ticktickapi.HabitCheckinData) (time.Time, error) {
	if checkin.Time != nil {
		return parseTickTickTime(*checkin.Time)
	}
	return parseTickTickDateStamp(checkin.Stamp)
}

func nextHabitCheckin(habit ticktickapi.Habit, now time.Time) (time.Time, error) {
	if habit.RepeatRule == nil || *habit.RepeatRule == "" {
		return time.Time{}, nil
	}

	rule, err := rrule.StrToRRule(*habit.RepeatRule)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse repeat rule for habit %q: %w", habit.Id, err)
	}
	rule.DTStart(habitRecurrenceStart(habit, now))

	occurrence := rule.After(startOfDay(now), true)
	reminders, err := habitReminderDurations(habit.Reminders)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse reminders for habit %q: %w", habit.Id, err)
	}
	if len(reminders) == 0 {
		return occurrence.UTC(), nil
	}

	for !occurrence.IsZero() {
		for _, reminder := range reminders {
			candidate := occurrence.Add(reminder)
			if !candidate.Before(now) {
				return candidate.UTC(), nil
			}
		}
		occurrence = rule.After(occurrence, false)
	}

	return time.Time{}, nil
}

func habitReminderDurations(reminders *[]string) ([]time.Duration, error) {
	if reminders == nil {
		return nil, nil
	}

	durations := make([]time.Duration, 0, len(*reminders))
	for _, reminder := range *reminders {
		duration, err := habitReminderDuration(reminder)
		if err != nil {
			return nil, err
		}
		durations = append(durations, duration)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	return durations, nil
}

func habitReminderDuration(reminder string) (time.Duration, error) {
	if clockTime, err := time.Parse("15:04", reminder); err == nil {
		return time.Duration(
			clockTime.Hour(),
		)*time.Hour + time.Duration(
			clockTime.Minute(),
		)*time.Minute, nil
	}

	parts := tickTickReminderDuration.FindStringSubmatch(reminder)
	if parts == nil {
		return 0, fmt.Errorf("unsupported reminder %q", reminder)
	}

	units := []time.Duration{24 * time.Hour, time.Hour, time.Minute, time.Second}
	var duration time.Duration
	for index, unit := range units {
		if parts[index+1] == "" {
			continue
		}
		amount, err := strconv.Atoi(parts[index+1])
		if err != nil {
			return 0, fmt.Errorf("parse reminder %q: %w", reminder, err)
		}
		duration += time.Duration(amount) * unit
	}
	return duration, nil
}

func habitRecurrenceStart(habit ticktickapi.Habit, now time.Time) time.Time {
	if habit.TargetStartDate != nil {
		if start, err := parseTickTickDateStampInLocation(*habit.TargetStartDate, now.Location()); err == nil {
			return start
		}
	}
	if habit.CreatedTime != nil {
		if created, err := parseTickTickTime(*habit.CreatedTime); err == nil {
			return startOfDay(created.In(now.Location()))
		}
	}
	return startOfDay(now)
}

func parseTickTickDateStamp(stamp int) (time.Time, error) {
	return parseTickTickDateStampInLocation(stamp, time.Local)
}

func parseTickTickDateStampInLocation(stamp int, location *time.Location) (time.Time, error) {
	value := strconv.Itoa(stamp)
	if len(value) != len(tickTickDateLayout) {
		return time.Time{}, fmt.Errorf("expected TickTick date stamp: %d", stamp)
	}
	parsed, err := time.ParseInLocation(tickTickDateLayout, value, location)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse TickTick date stamp %d: %w", stamp, err)
	}
	return parsed, nil
}

func tickTickDateStamp(value time.Time) int {
	stamp, err := strconv.Atoi(value.Format(tickTickDateLayout))
	if err != nil {
		panic(fmt.Sprintf("format TickTick date stamp: %v", err))
	}
	return stamp
}

func startOfDay(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
