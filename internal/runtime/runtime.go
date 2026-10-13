// Package runtime
package runtime

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/henrywhitaker3/ticktick-events/internal/config"
	"github.com/italypaleale/francis/components/postgres"
	"github.com/italypaleale/francis/host/local"
)

func NewHost(conf config.Runtime) (*local.Host, error) {
	host, err := local.NewHost(
		local.WithAddress(net.JoinHostPort(conf.Host, fmt.Sprint(conf.Port))),
		local.WithRuntimePSKs([]byte(conf.PSK)),
		local.WithLogger(slog.With("component", "runtime")),
		local.WithPostgresProvider(postgres.PostgresProviderOptions{
			ConnectionString: string(conf.PostgresURL),
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("init host: %w", err)
	}
	return host, nil
}
