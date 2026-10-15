package habit

import (
	"context"
	"testing"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/italypaleale/francis/actor"
	"github.com/stretchr/testify/require"
)

type mockHabitGetter struct {
	habit client.Habit
	calls int
}

func (m *mockHabitGetter) GetHabit(context.Context, string) (client.Habit, error) {
	m.calls++
	return m.habit, nil
}

type mockStimulusSender struct {
	calls int
	task  string
}

func (m *mockStimulusSender) Send(_ context.Context, task string, _ client.Stimulus) error {
	m.calls++
	m.task = task
	return nil
}

type mockActorClient struct {
	actor.Client[habitState]
	state    habitState
	setCalls int
}

func (m *mockActorClient) GetState(context.Context) (habitState, error) {
	return m.state, nil
}

func (m *mockActorClient) SetState(_ context.Context, state habitState, _ *actor.SetStateOpts) error {
	m.setCalls++
	m.state = state
	return nil
}

func TestProcessAlertDoesNotZapCheckedInHabit(t *testing.T) {
	alertTime := time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC)
	checkinTime := alertTime.Add(-time.Hour)
	state := &mockActorClient{state: habitState{
		Habit:     &client.Habit{ID: "habit-1", Name: "Read"},
		NextAlarm: alertTime,
	}}
	getter := &mockHabitGetter{habit: client.Habit{
		ID:          "habit-1",
		Name:        "Read",
		LastCheckin: &checkinTime,
	}}
	pavlok := &mockStimulusSender{}
	habit := &Habit{client: state, ticktick: getter, pavlok: pavlok}

	require.NoError(t, habit.processAlert(t.Context()))
	require.Equal(t, 1, getter.calls)
	require.Zero(t, pavlok.calls)
	require.NotNil(t, state.state.LastAlertFor)
	require.True(t, state.state.LastAlertFor.Equal(alertTime))
}

func TestProcessAlertZapsHabitWithoutCheckinForAlertDay(t *testing.T) {
	alertTime := time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC)
	previousCheckin := alertTime.AddDate(0, 0, -1)
	state := &mockActorClient{state: habitState{
		Habit:     &client.Habit{ID: "habit-1", Name: "Read"},
		NextAlarm: alertTime,
	}}
	getter := &mockHabitGetter{habit: client.Habit{
		ID:          "habit-1",
		Name:        "Read",
		LastCheckin: &previousCheckin,
	}}
	pavlok := &mockStimulusSender{}
	habit := &Habit{client: state, ticktick: getter, pavlok: pavlok}

	require.NoError(t, habit.processAlert(t.Context()))
	require.Equal(t, 1, getter.calls)
	require.Equal(t, 1, pavlok.calls)
	require.Equal(t, "Overdue habit: Read", pavlok.task)
}

func TestProcessAlertDoesNotZapCheckinOnSameLocalDay(t *testing.T) {
	location, err := time.LoadLocation("Europe/London")
	require.NoError(t, err)
	alertTime := time.Date(2026, time.October, 14, 10, 40, 0, 0, time.UTC)
	checkinTime := time.Date(2026, time.October, 13, 23, 0, 0, 0, time.UTC)
	state := &mockActorClient{state: habitState{
		Habit:     &client.Habit{ID: "habit-1", Name: "Read"},
		NextAlarm: alertTime,
	}}
	getter := &mockHabitGetter{habit: client.Habit{
		ID:          "habit-1",
		Name:        "Read",
		TimeZone:    location.String(),
		LastCheckin: &checkinTime,
	}}
	pavlok := &mockStimulusSender{}
	habit := &Habit{client: state, ticktick: getter, pavlok: pavlok}

	require.NoError(t, habit.processAlert(t.Context()))
	require.Zero(t, pavlok.calls)
}

var _ HabitGetter = (*mockHabitGetter)(nil)
var _ StimulusSender = (*mockStimulusSender)(nil)
