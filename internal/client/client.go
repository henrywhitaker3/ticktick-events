// Package client provides TickTick operations used by the application.
package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/ticktickapi"
)

const (
	apiBaseURL         = "https://api.ticktick.com"
	undoneTaskWindow   = 14 * 24 * time.Hour
	tickTickTimeLayout = "2006-01-02T15:04:05-0700"

	TaskStatusNormal = 0
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
		ticktickapi.WithHTTPClient(httpClient),
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

// GetOverdueTasks retrieves undone tasks from the last 14 days and returns the
// subset with a due date before the current time. TickTick's undone-tasks
// endpoint does not accept a date range longer than 14 days.
func (c *TickTickClient) GetOverdueTasks(ctx context.Context) ([]Task, error) {
	now := time.Now().UTC()
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

func validateUndoneTasksResponse(response *ticktickapi.ListUndoneTasksResponse) error {
	if response == nil {
		return fmt.Errorf("list undone tasks: empty response")
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return responseError("list undone tasks", response.StatusCode(), response.Body)
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

func checklistItemsFromAPI(source *[]ticktickapi.ChecklistItem) ([]ChecklistItem, error) {
	if source == nil {
		return nil, nil
	}

	items := make([]ChecklistItem, 0, len(*source))
	for _, item := range *source {
		completedTime, err := parseTickTickTimePointer(item.CompletedTime)
		if err != nil {
			return nil, fmt.Errorf(
				"parse checklist item %q completed time: %w",
				stringValue(item.Id),
				err,
			)
		}
		startDate, err := parseTickTickTimePointer(item.StartDate)
		if err != nil {
			return nil, fmt.Errorf(
				"parse checklist item %q start date: %w",
				stringValue(item.Id),
				err,
			)
		}

		items = append(items, ChecklistItem{
			ID:            stringValue(item.Id),
			Title:         stringValue(item.Title),
			Status:        intValue(item.Status),
			CompletedTime: completedTime,
			StartDate:     startDate,
			IsAllDay:      boolValue(item.IsAllDay),
			SortOrder:     int64Value(item.SortOrder),
			TimeZone:      stringValue(item.TimeZone),
		})
	}

	return items, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringSliceValue(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}

func boolValue(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
