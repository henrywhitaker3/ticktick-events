// Package metrics
package metrics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

type Server struct {
	server *http.Server
	slog   *slog.Logger
}

func New(port int) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})
	mux.Handle("GET /metrics", promhttp.Handler())

	exporter, _ := otelprom.New(
		otelprom.WithRegisterer(prometheus.DefaultRegisterer),
		otelprom.WithoutTargetInfo(),
	)
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
	)
	otel.SetMeterProvider(provider)

	return &Server{
		server: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
		slog: slog.With("component", "metrics"),
	}
}

func (s *Server) Run(ctx context.Context) error {
	ch := make(chan error)

	go func() {
		s.slog.Info("starting metrics server", "addr", s.server.Addr)
		if err := s.server.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				ch <- err
			}
		}
	}()

	select {
	case <-ctx.Done():
		cctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		return s.server.Shutdown(cctx)
	case err := <-ch:
		return err
	}
}
