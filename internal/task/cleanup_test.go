package task

import (
	"context"
	"errors"
	"testing"

	"github.com/italypaleale/francis/actor"
	"github.com/stretchr/testify/require"
)

type mockTaskStateStore struct {
	states      actor.StateList
	listErr     error
	listType    string
	listOpts    *actor.ListStatesOpts
	invokeErrs  map[string]error
	invocations []taskInvocation
}

type taskInvocation struct {
	actorType string
	actorID   string
	method    string
}

func (m *mockTaskStateStore) ListStates(
	_ context.Context,
	actorType string,
	opts *actor.ListStatesOpts,
) (actor.StateList, error) {
	m.listType = actorType
	m.listOpts = opts
	return m.states, m.listErr
}

func (m *mockTaskStateStore) DeleteState(_ context.Context, _ string, actorID string) error {
	return nil
}

func (m *mockTaskStateStore) Invoke(
	_ context.Context,
	actorType string,
	actorID string,
	method string,
	_ any,
	_ ...actor.InvokeOption,
) (actor.Envelope, error) {
	m.invocations = append(m.invocations, taskInvocation{
		actorType: actorType,
		actorID:   actorID,
		method:    method,
	})
	return nil, m.invokeErrs[actorID]
}

type taskStateEnvelope struct {
	state taskState
	err   error
}

func (e taskStateEnvelope) Decode(into any) error {
	if e.err != nil {
		return e.err
	}
	*into.(*taskState) = e.state
	return nil
}

func TestCleanupTasksDeletesCompletedTaskStates(t *testing.T) {
	store := &mockTaskStateStore{states: actor.StateList{States: []actor.StateInfo{
		{ActorID: "completed", Data: taskStateEnvelope{state: taskState{Completed: true}}},
		{ActorID: "active", Data: taskStateEnvelope{state: taskState{Completed: false}}},
	}}}

	require.NoError(t, cleanupTasks(store)(t.Context()))
	require.Equal(t, []taskInvocation{{actorType: Type, actorID: "completed", method: Stop}}, store.invocations)
	require.Equal(t, Type, store.listType)
	require.NotNil(t, store.listOpts)
	require.True(t, store.listOpts.IncludeData)
	require.Equal(t, 25, store.listOpts.Limit)
}

func TestCleanupTasksContinuesWhenAStateCannotBeStopped(t *testing.T) {
	store := &mockTaskStateStore{
		states: actor.StateList{States: []actor.StateInfo{
			{ActorID: "first", Data: taskStateEnvelope{state: taskState{Completed: true}}},
			{ActorID: "second", Data: taskStateEnvelope{state: taskState{Completed: true}}},
		}},
		invokeErrs: map[string]error{"first": errors.New("host unavailable")},
	}

	require.NoError(t, cleanupTasks(store)(t.Context()))
	require.Equal(t, []taskInvocation{
		{actorType: Type, actorID: "first", method: Stop},
		{actorType: Type, actorID: "second", method: Stop},
	}, store.invocations)
}

func TestCleanupTasksContinuesWhenOneStateCannotBeDecoded(t *testing.T) {
	store := &mockTaskStateStore{states: actor.StateList{States: []actor.StateInfo{
		{ActorID: "invalid", Data: taskStateEnvelope{err: errors.New("invalid state")}},
		{ActorID: "completed", Data: taskStateEnvelope{state: taskState{Completed: true}}},
	}}}

	require.NoError(t, cleanupTasks(store)(t.Context()))
	require.Equal(t, []taskInvocation{{actorType: Type, actorID: "completed", method: Stop}}, store.invocations)
}

func TestCleanupTasksReturnsListStatesError(t *testing.T) {
	store := &mockTaskStateStore{listErr: errors.New("storage unavailable")}

	err := cleanupTasks(store)(t.Context())
	require.ErrorContains(t, err, "list task actors")
	require.Empty(t, store.invocations)
}

var _ taskStateStore = (*mockTaskStateStore)(nil)
