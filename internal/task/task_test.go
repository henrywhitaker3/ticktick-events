package task

import (
	"context"
	"testing"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/henrywhitaker3/ticktick-events/internal/config"
	"github.com/italypaleale/francis/actor"
	"github.com/stretchr/testify/require"
)

type mockTaskGetter struct {
	task      client.Task
	calls     int
	projectID string
	taskID    string
}

func (m *mockTaskGetter) GetTask(_ context.Context, projectID, taskID string) (client.Task, error) {
	m.calls++
	m.projectID = projectID
	m.taskID = taskID
	return m.task, nil
}

type mockStimulusSender struct {
	calls    int
	task     client.Task
	stimulus client.Stimulus
}

func (m *mockStimulusSender) Send(
	_ context.Context,
	task client.Task,
	stimulus client.Stimulus,
) error {
	m.calls++
	m.task = task
	m.stimulus = stimulus
	return nil
}

type mockActorClient struct {
	actor.Client[taskState]
	state          taskState
	deleted        bool
	halted         bool
	dispatchCalls  int
	dispatchMethod string
	dispatchInput  any
}

func (m *mockActorClient) GetState(context.Context) (taskState, error) {
	return m.state, nil
}

func (m *mockActorClient) SetState(_ context.Context, state taskState, _ *actor.SetStateOpts) error {
	m.state = state
	return nil
}

func (m *mockActorClient) DeleteState(context.Context) error {
	m.deleted = true
	return nil
}

func (m *mockActorClient) Halt() {
	m.halted = true
}

func (m *mockActorClient) Dispatch(
	_ context.Context,
	method string,
	input any,
	_ ...actor.JobOption,
) (string, bool, error) {
	m.dispatchCalls++
	m.dispatchMethod = method
	m.dispatchInput = input
	return "job-1", true, nil
}

type taskEnvelope struct {
	task client.Task
}

func (e taskEnvelope) Decode(into any) error {
	*into.(*client.Task) = e.task
	return nil
}

func TestOverdueDispatchesNotificationJob(t *testing.T) {
	ctx := t.Context()

	originalTask := client.Task{
		ID:        "task-1",
		ProjectID: "project-1",
		Title:     "Pay bill",
		Status:    client.TaskStatusNormal,
	}
	ticktick := &mockTaskGetter{task: originalTask}
	pavlok := &mockStimulusSender{}
	state := &mockActorClient{}
	task := &Task{
		client:   state,
		ticktick: ticktick,
		pavlok:   pavlok,
		quiet:    config.QuietTimes{},
	}

	_, err := task.Invoke(ctx, Overdue, taskEnvelope{task: originalTask})
	require.NoError(t, err)
	require.Zero(t, ticktick.calls)
	require.Zero(t, pavlok.calls)
	require.Equal(t, &originalTask, state.state.Task)
	require.Equal(t, 1, state.dispatchCalls)
	require.Equal(t, Overdue, state.dispatchMethod)
	require.Nil(t, state.dispatchInput)

	require.NoError(t, task.Job(ctx, Overdue, nil))
	require.Equal(t, 1, ticktick.calls)
	require.Equal(t, originalTask.ProjectID, ticktick.projectID)
	require.Equal(t, originalTask.ID, ticktick.taskID)
	require.Equal(t, 1, pavlok.calls)
	require.Equal(t, originalTask.ID, pavlok.task.ID)
	require.Equal(t, client.Zap, pavlok.stimulus)
	require.Equal(t, &originalTask, state.state.Task)
	require.NotNil(t, state.state.LastNotifiedAt)
	require.Equal(t, 1, state.state.NotificationCounter)

	_, err = task.Invoke(ctx, Overdue, taskEnvelope{task: originalTask})
	require.NoError(t, err)
	require.NoError(t, task.Job(ctx, Overdue, nil))
	require.Equal(t, 2, state.dispatchCalls)
	require.Equal(t, 1, ticktick.calls)
	require.Equal(t, 1, pavlok.calls)
}

func TestOverdueDoesNotZapDuringQuietTime(t *testing.T) {
	ctx := t.Context()

	originalTask := client.Task{
		ID:        "task-quiet-time",
		ProjectID: "project-1",
		Title:     "Pay bill",
		Status:    client.TaskStatusNormal,
	}
	ticktick := &mockTaskGetter{task: originalTask}
	pavlok := &mockStimulusSender{}
	state := &mockActorClient{}
	task := &Task{
		client:   state,
		ticktick: ticktick,
		pavlok:   pavlok,
		quiet:    quietTimesAround(time.Now()),
	}

	_, err := task.Invoke(ctx, Overdue, taskEnvelope{task: originalTask})
	require.NoError(t, err)
	require.NoError(t, task.Job(ctx, Overdue, nil))
	require.Equal(t, 1, state.dispatchCalls)
	require.Equal(t, 1, ticktick.calls)
	require.Zero(t, pavlok.calls)
	require.Equal(t, &originalTask, state.state.Task)
	require.Nil(t, state.state.LastNotifiedAt)
	require.Zero(t, state.state.NotificationCounter)
}

func TestOverdueJobDoesNotNotifyWithinTenMinutesOfLastNotification(t *testing.T) {
	ctx := t.Context()

	originalTask := client.Task{
		ID:        "task-recently-notified",
		ProjectID: "project-1",
		Title:     "Pay bill",
		Status:    client.TaskStatusNormal,
	}
	lastNotifiedAt := time.Now().Add(-9 * time.Minute)
	ticktick := &mockTaskGetter{task: originalTask}
	pavlok := &mockStimulusSender{}
	state := &mockActorClient{state: taskState{
		Task:                &originalTask,
		LastNotifiedAt:      &lastNotifiedAt,
		NotificationCounter: 1,
	}}
	task := &Task{
		client:   state,
		ticktick: ticktick,
		pavlok:   pavlok,
		quiet:    config.QuietTimes{},
	}

	require.NoError(t, task.Job(ctx, Overdue, nil))
	require.Zero(t, ticktick.calls)
	require.Zero(t, pavlok.calls)
	require.Equal(t, 1, state.state.NotificationCounter)
	require.Equal(t, &lastNotifiedAt, state.state.LastNotifiedAt)
}

func quietTimesAround(c time.Time) config.QuietTimes {
	start := c.Add(-time.Minute)
	end := c.Add(time.Minute)
	quietTime := config.TimeRange{
		Start: time.Date(
			0,
			time.January,
			1,
			start.Hour(),
			start.Minute(),
			start.Second(),
			start.Nanosecond(),
			time.UTC,
		),
		End: time.Date(
			0,
			time.January,
			1,
			end.Hour(),
			end.Minute(),
			end.Second(),
			end.Nanosecond(),
			time.UTC,
		),
	}

	return config.QuietTimes{
		Monday:    quietTime,
		Tuesday:   quietTime,
		Wednesday: quietTime,
		Thursday:  quietTime,
		Friday:    quietTime,
		Saturday:  quietTime,
		Sunday:    quietTime,
	}
}

var _ TaskGetter = (*mockTaskGetter)(nil)
var _ StimulusSender = (*mockStimulusSender)(nil)
