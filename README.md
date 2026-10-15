# TickTick Events

TickTick Events sends Pavlok zaps for overdue TickTick tasks and missed habit
reminders. It uses a PostgreSQL-backed actor runtime to retain notification
state and scheduled alarms across service restarts.

## How it works

1. An orchestrator runs immediately at startup and then every
   `CHECK_INTERVAL` (one minute by default). It asks TickTick for unfinished
   tasks due within the past 14 days and for habits with configured reminders.
   It keeps only tasks whose due date has passed.
2. Each overdue task is delivered to a durable actor keyed by the TickTick task
   ID. The actor schedules its first check after `INTERACTION_WAIT` (one minute
   by default), then repeats the check every 10 minutes.
3. At each check, the actor fetches the task again from TickTick. It stops its
   alarm when the task has been completed, its due date has changed, or it has
   reached `MAX_NOTIFICATIONS`.
4. Otherwise, unless the current time is within a configured quiet period, it
   sends a Pavlok `zap` (strength 100) and records the notification. The
   default maximum is five zaps per due date.
5. Each reminded habit is given a one-shot alarm at its next TickTick reminder.
   When that alarm fires, the service refreshes the habit and its recent
   check-ins. It requests the prior 14 calendar days through tomorrow, so a
   check-in made today is included whether TickTick interprets the end date as
   inclusive or exclusive. A habit checked in on the reminder's calendar day
   is not zapped; otherwise it receives one zap for that reminder occurrence.

Changing a task's due date starts a new notification cycle. The actor state and
alarms live in PostgreSQL, so they are not lost when the process restarts.

## Requirements

- Go 1.27.2 or later
- PostgreSQL
- A TickTick OAuth token with task read access
- A Pavlok API token
- Docker, if you use the supplied local PostgreSQL and Jaeger services

## Run locally

Start PostgreSQL (and optionally Jaeger for traces):

```sh
docker compose up -d
```

Set the required configuration and run the service:

```sh
export TICKTICK_TOKEN="..."
export PAVLOK_TOKEN="..."
export RUNTIME_DB_URL="postgres://tick:tick@localhost:5432/tick?sslmode=disable"
export RUNTIME_PSK="$(mise run psk)"
export OTEL_EXPORTER_OTLP_ENDPOINT="http://localhost:4318" # optional

go run .
```

`RUNTIME_HOST` is optional for a single local instance; set it to the hostname
or address advertised by the actor runtime when needed. The runtime listens on
`RUNTIME_PORT`, which defaults to `7571`.

If you use [mise](https://mise.jdx.dev/), `mise run serve` retrieves the tokens
from the configured 1Password items, generates a runtime PSK, and starts the
service with the local PostgreSQL and Jaeger defaults.

## Configuration

All application settings are environment variables.

| Variable | Default | Description |
| --- | --- | --- |
| `TICKTICK_TOKEN` | required | OAuth bearer token used to read TickTick tasks. |
| `TICKTICK_TIME_ZONE` | required | IANA timezone configured for the TickTick account (for example, `Europe/London`). Habit reminder clock times are resolved in this timezone and then scheduled in UTC. |
| `PAVLOK_TOKEN` | required | Bearer token used to send Pavlok stimuli. |
| `RUNTIME_DB_URL` | required | PostgreSQL connection string for durable actor state and alarms. |
| `RUNTIME_PSK` | required | Pre-shared key for the actor runtime. Generate one with `openssl rand -hex 32` or `mise run psk`. |
| `RUNTIME_HOST` | empty | Address advertised by the actor runtime. |
| `RUNTIME_PORT` | `7571` | Actor runtime port. |
| `CHECK_INTERVAL` | `1m` | Frequency for discovering overdue TickTick tasks. |
| `INTERACTION_WAIT` | `1m` | Delay before the first check and potential zap. |
| `MAX_NOTIFICATIONS` | `5` | Maximum zaps sent for a task's current due date. |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, or `error`. |

### Quiet times

Set `QUIET_TIMES_<DAY>` to suppress overdue-task zaps during that day's
inclusive time range. `<DAY>` is one of `MONDAY` through `SUNDAY`; values use
`HH:mm:ss-HH:mm:ss` in the service host's local time. Ranges may cross
midnight.

```sh
export QUIET_TIMES_MONDAY="22:00:00-06:00:00"
export QUIET_TIMES_TUESDAY="09:00:00-17:00:00"
```

An overdue-task alarm still runs during quiet time; that occurrence simply
sends no zap and does not count towards `MAX_NOTIFICATIONS`. Habit reminder
alerts are not currently subject to quiet times.

## Health, metrics, and tracing

The service exposes an HTTP server on port `8080`:

| Endpoint | Purpose |
| --- | --- |
| `/healthz` | Returns `OK` when the metrics server is running. |
| `/metrics` | Prometheus metrics, including `zaps_sent_total`. |

TickTick and Pavlok HTTP calls are instrumented with OpenTelemetry. Set the
standard OpenTelemetry exporter variables (for example,
`OTEL_EXPORTER_OTLP_ENDPOINT`) to export traces. The included Jaeger service is
available at [http://localhost:16686](http://localhost:16686).

## Development

Run the tests:

```sh
go test ./...
```

Or, with mise:

```sh
mise test
```

The TickTick client is generated from the focused OpenAPI definition.
Regenerate it after changing `internal/ticktickapi/openapi.yaml`:

```sh
go generate ./internal/ticktickapi
```

Keep `TICKTICK_TOKEN`, `PAVLOK_TOKEN`, `RUNTIME_DB_URL`, and `RUNTIME_PSK` out
of source control and shell history. Logs are emitted as structured JSON to
standard output.
