package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	zapsSent = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "zaps_sent_total",
	})
)

func init() {
	_ = prometheus.DefaultRegisterer.Register(zapsSent)
}

const pavlokStimulusURL = "https://api.pavlok.com/api/v5/stimulus/send"

type PavlokClient struct {
	token      string
	httpClient *http.Client
}

func NewPavlokClient(token string) *PavlokClient {
	return newPavlokClient(token, http.DefaultClient)
}

func newPavlokClient(token string, httpClient *http.Client) *PavlokClient {
	return &PavlokClient{
		token:      token,
		httpClient: newInstrumentedHTTPClient(httpClient, "pavlok HTTP request"),
	}
}

type Stimulus string

const (
	Zap  Stimulus = "zap"
	Beep Stimulus = "beep"
	Vibe Stimulus = "vibe"
)

type SendStimulusRequest struct {
	Stimulus struct {
		Type   Stimulus `json:"stimulusType"`
		Value  int      `json:"stimulusValue"`
		Reason string   `json:"reason,omitempty"`
	} `json:"stimulus"`
}

func (p *PavlokClient) Send(ctx context.Context, task string, s Stimulus) error {
	body, err := json.Marshal(SendStimulusRequest{
		Stimulus: struct {
			Type   Stimulus "json:\"stimulusType\""
			Value  int      "json:\"stimulusValue\""
			Reason string   "json:\"reason,omitempty\""
		}{
			Type:   s,
			Value:  100,
			Reason: task,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		pavlokStimulusURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", p.token))

	res, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("read response body: %w", err)
		}
		return fmt.Errorf("send stimulus: %s", string(body))
	}

	zapsSent.Inc()

	return nil
}
