package orchestrator

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/henrywhitaker3/ticktick-events/internal/client"
	"github.com/redis/rueidis"
	"github.com/stretchr/testify/require"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
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

func TestHandleOverdueTaskMarksTaskAsProcessed(t *testing.T) {
	ctx := t.Context()
	redisClient := newTestRedisClient(t)

	task := client.Task{
		ID:        "task-1",
		ProjectID: "project-1",
		Title:     "Pay bill",
		Status:    client.TaskStatusNormal,
	}
	ticktick := &mockTaskGetter{task: task}
	pavlok := &mockStimulusSender{}
	handler := HandleOverdueTask(ticktick, pavlok, redisClient, 0)

	require.NoError(t, handler(ctx, OverdueTask{Task: task}))
	require.Equal(t, 1, ticktick.calls)
	require.Equal(t, task.ProjectID, ticktick.projectID)
	require.Equal(t, task.ID, ticktick.taskID)
	require.Equal(t, 1, pavlok.calls)
	require.Equal(t, task.ID, pavlok.task.ID)
	require.Equal(t, client.Zap, pavlok.stimulus)

	value, err := redisClient.Do(ctx, redisClient.B().Get().Key("ticktick:"+task.ID).Build()).
		ToString()
	require.NoError(t, err)
	require.Equal(t, "notified", value)

	ttl, err := redisClient.Do(ctx, redisClient.B().Ttl().Key("ticktick:"+task.ID).Build()).
		AsInt64()
	require.NoError(t, err)
	require.Greater(t, ttl, int64((9 * time.Minute).Seconds()))
	require.LessOrEqual(t, ttl, int64((10 * time.Minute).Seconds()))

	require.NoError(t, handler(ctx, OverdueTask{Task: task}))
	require.Equal(t, 1, ticktick.calls)
	require.Equal(t, 1, pavlok.calls)
}

func newTestRedisClient(t *testing.T) rueidis.Client {
	t.Helper()

	ctx := context.Background()
	redisContainer, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("Docker is required for this integration test: %v", err)
	}
	t.Cleanup(func() {
		require.NoError(t, redisContainer.Terminate(ctx))
	})

	connectionString, err := redisContainer.ConnectionString(ctx)
	require.NoError(t, err)
	endpoint, err := url.Parse(connectionString)
	require.NoError(t, err)

	redisClient, err := rueidis.NewClient(
		rueidis.ClientOption{InitAddress: []string{endpoint.Host}},
	)
	require.NoError(t, err)
	t.Cleanup(redisClient.Close)

	return redisClient
}

var _ TaskGetter = (*mockTaskGetter)(nil)
var _ StimulusSender = (*mockStimulusSender)(nil)
