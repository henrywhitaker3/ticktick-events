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
	task     string
	stimulus client.Stimulus
}

func (m *mockStimulusSender) Send(
	_ context.Context,
	task string,
	stimulus client.Stimulus,
) error {
	m.calls++
	m.task = task
	m.stimulus = stimulus
	return nil
}

type mockActorClient struct {
	actor.Client[taskState]
	state           taskState
	setAlarmCalls   int
	alarmName       string
	alarmProperties actor.AlarmProperties
	deletedAlarm    string
}

func (m *mockActorClient) GetState(context.Context) (taskState, error) {
	return m.state, nil
}

func (m *mockActorClient) SetState(_ context.Context, state taskState, _ *actor.SetStateOpts) error {
	m.state = state
	return nil
}

func (m *mockActorClient) SetAlarm(
	_ context.Context,
	name string,
	properties actor.AlarmProperties,
) error {
	m.setAlarmCalls++
	m.alarmName = name
	m.alarmProperties = properties
	return nil
}

func (m *mockActorClient) DeleteAlarm(_ context.Context, name string) error {
	m.deletedAlarm = name
	return nil
}

type taskEnvelope struct {
	task client.Task
}

func (e taskEnvelope) Decode(into any) error {
	*into.(*client.Task) = e.task
	return nil
}

func TestOverdueOnboardsTaskWithRecurringAlarm(t *testing.T) {
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
		client:           state,
		ticktick:         ticktick,
		pavlok:           pavlok,
		sleep:            time.Minute,
		quiet:            config.QuietTimes{},
		maxNotifications: 5,
	}

	before := time.Now()
	_, err := task.Invoke(ctx, Overdue, taskEnvelope{task: originalTask})
	require.NoError(t, err)
	require.Zero(t, ticktick.calls)
	require.Zero(t, pavlok.calls)
	require.Equal(t, &originalTask, state.state.Task)
	require.True(t, state.state.Onboarded)
	require.Equal(t, 1, state.setAlarmCalls)
	require.Equal(t, Overdue, state.alarmName)
	require.Equal(t, "PT10M", state.alarmProperties.Interval)
	require.WithinDuration(t, before.Add(time.Minute), state.alarmProperties.DueTime, time.Second)

	_, err = task.Invoke(ctx, Overdue, taskEnvelope{task: originalTask})
	require.NoError(t, err)
	require.Equal(t, 1, state.setAlarmCalls)
}

func TestOverdueAlarmSendsZap(t *testing.T) {
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
		client:           state,
		ticktick:         ticktick,
		pavlok:           pavlok,
		quiet:            config.QuietTimes{},
		maxNotifications: 5,
	}

	_, err := task.Invoke(ctx, Overdue, taskEnvelope{task: originalTask})
	require.NoError(t, err)
	require.NoError(t, task.Alarm(ctx, Overdue, nil))
	require.Equal(t, 1, ticktick.calls)
	require.Equal(t, originalTask.ProjectID, ticktick.projectID)
	require.Equal(t, originalTask.ID, ticktick.taskID)
	require.Equal(t, 1, pavlok.calls)
	require.Equal(t, "Overdue task: "+originalTask.Title, pavlok.task)
	require.Equal(t, client.Zap, pavlok.stimulus)
	require.Equal(t, &originalTask, state.state.Task)
	require.NotNil(t, state.state.LastNotifiedAt)
	require.Equal(t, 1, state.state.NotificationCounter)
}

func TestOverdueAlarmDoesNotZapDuringQuietTime(t *testing.T) {
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
		client:           state,
		ticktick:         ticktick,
		pavlok:           pavlok,
		quiet:            quietTimesAround(time.Now()),
		maxNotifications: 5,
	}

	_, err := task.Invoke(ctx, Overdue, taskEnvelope{task: originalTask})
	require.NoError(t, err)
	require.NoError(t, task.Alarm(ctx, Overdue, nil))
	require.Equal(t, 1, ticktick.calls)
	require.Zero(t, pavlok.calls)
	require.Nil(t, state.state.LastNotifiedAt)
	require.Zero(t, state.state.NotificationCounter)
}

func TestOverdueAlarmCancelsWhenTaskIsCompleted(t *testing.T) {
	ctx := t.Context()

	originalTask := client.Task{
		ID:        "task-complete",
		ProjectID: "project-1",
		Title:     "Pay bill",
		Status:    client.TaskStatusNormal,
	}
	completedTask := originalTask
	completedTask.Status = 1
	ticktick := &mockTaskGetter{task: completedTask}
	state := &mockActorClient{state: taskState{
		Task:      &originalTask,
		Onboarded: true,
	}}
	task := &Task{
		client:           state,
		ticktick:         ticktick,
		pavlok:           &mockStimulusSender{},
		maxNotifications: 5,
	}

	require.NoError(t, task.Alarm(ctx, Overdue, nil))
	require.True(t, state.state.Completed)
	require.Equal(t, Overdue, state.deletedAlarm)
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
