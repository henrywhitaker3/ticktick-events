# TickTick Events

TickTick Events watches for overdue TickTick tasks and sends a Pavlok zap when
an overdue task remains incomplete. Redis prevents the same task from producing
duplicate notifications for 10 minutes.

## How it works

1. The service polls TickTick for overdue, unfinished tasks at the configured
   interval.
2. It queues each task once while the service is running and checks Redis for
   an existing notification marker.
3. For a task without a marker, it waits for the configured interaction window,
   then fetches the task again from TickTick.
4. If the task is no longer open, it does nothing. Otherwise, it sends a Pavlok
   `zap` with a three-second request timeout.
5. After a successful zap, it records a Redis marker for 10 minutes. A repeat
   event during that window is ignored.

## Requirements

- Go 1.27 or later
- A running Redis instance
- A TickTick OAuth token with task read access
- A Pavlok API token
- Docker, when running the integration test suite

## Run locally

Set the required tokens and start the service:

```sh
export TICKTICK_TOKEN="..."
export PAVLOK_TOKEN="..."

go run . \
  --redis-url 127.0.0.1:6379 \
  --check-interval 1m \
  --interaction-wait 1m \
  --log-level info
```

Available flags:

| Flag | Default | Description |
| --- | --- | --- |
| `--redis-url` | `127.0.0.1:6379` | Redis server address. |
| `--check-interval` | `1m` | How often to look for overdue tasks. |
| `--interaction-wait` | `1m` | Time to wait before checking whether a task was completed. |
| `--log-level` | `info` | Log level: `debug`, `info`, `warn`, or `error`. |

If you use [mise](https://mise.jdx.dev/), `mise run` retrieves the tokens from
the configured 1Password items and starts the service.

## Development

Run the test suite:

```sh
go test ./...
```

The event-handler integration test starts a `redis:7-alpine` container through
Testcontainers and uses the production Rueidis client. Docker must be available
to run that test; it is skipped when Docker cannot be reached. With mise, run:

```sh
mise test
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
