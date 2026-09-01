# TickTick Events

TickTick Events watches for overdue TickTick tasks and sends a Pavlok zap when
an overdue task remains incomplete. Redis prevents the same task from producing
duplicate notifications for 30 days.

## How it works

1. The service polls TickTick for undone tasks that are due.
2. Each overdue task is dispatched once per polling cycle.
3. Before acting, it checks Redis for a notification marker for that task.
4. It waits one minute, refreshes the task from TickTick, and stops if the task
   is complete.
5. Otherwise, it sends a Pavlok `zap` and records the notification marker in
   Redis.

## Requirements

- Go 1.27 or later
- A running Redis instance
- A TickTick OAuth token with task read access
- A Pavlok API token

## Run locally

Set the required tokens and start the service:

```sh
export TICKTICK_TOKEN="..."
export PAVLOK_TOKEN="..."

go run . --redis-url 127.0.0.1:6379 --log-level info
```

Available flags:

| Flag | Default | Description |
| --- | --- | --- |
| `--redis-url` | `127.0.0.1:6379` | Redis server address. |
| `--log-level` | `info` | Log level: `debug`, `info`, `warn`, or `error`. |

If you use [mise](https://mise.jdx.dev/), `mise run` retrieves the tokens from
the configured 1Password items and starts the service.

## Development

Run the test suite:

```sh
go test ./...
```

The TickTick client is generated from the focused OpenAPI definition. Regenerate
it after changing `internal/ticktickapi/openapi.yaml`:

```sh
go generate ./internal/ticktickapi
```

## Configuration and security

Tokens are read only from `TICKTICK_TOKEN` and `PAVLOK_TOKEN` environment
variables. Keep them out of source control and shell history. The service logs
JSON to standard output.
