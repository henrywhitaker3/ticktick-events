package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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

func jsonResponse(request *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}
