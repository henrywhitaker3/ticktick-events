package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type PavlokClient struct {
	token string
}

func NewPavlokClient(token string) *PavlokClient {
	return &PavlokClient{
		token: token,
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

func (p *PavlokClient) Send(ctx context.Context, task Task, s Stimulus) error {
	body, err := json.Marshal(SendStimulusRequest{
		Stimulus: struct {
			Type   Stimulus "json:\"stimulusType\""
			Value  int      "json:\"stimulusValue\""
			Reason string   "json:\"reason,omitempty\""
		}{
			Type:   s,
			Value:  100,
			Reason: fmt.Sprintf("Overdue task: %s", task.Title),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		"https://api.pavlok.com/api/v5/stimulus/send",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req = req.WithContext(ctx)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", p.token))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("read response body: %w", err)
		}
		defer res.Body.Close()
		return fmt.Errorf("send stimulus: %s", string(body))
	}

	return nil
}
