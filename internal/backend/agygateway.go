package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AgyGateway connects Aegys Bot to AetherCore-Works/agy-mobile over HTTP.
type AgyGateway struct {
	BaseURL string
	Secret  string
	Client  *http.Client
}

func NewAgyGateway(baseURL, secret string) *AgyGateway {
	return &AgyGateway{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Secret:  secret,
		Client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (g *AgyGateway) Name() string { return "agy-gateway" }

func (g *AgyGateway) doJSON(ctx context.Context, method, path string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.BaseURL+path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+g.Secret)
	}

	resp, err := g.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("agy-gateway %s %s failed (%d): %s", method, path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (g *AgyGateway) Ask(ctx context.Context, req AskRequest) (AskResponse, error) {
	var res struct {
		Reply  string `json:"reply"`
		ConvID string `json:"conv_id"`
		Model  string `json:"model"`
	}
	payload := map[string]any{
		"prompt":  req.Prompt,
		"conv_id": req.ConversationID,
		"wait":    true,
	}
	if err := g.doJSON(ctx, http.MethodPost, "/api/webhook/prompt", payload, &res); err != nil {
		return AskResponse{}, err
	}
	return AskResponse{
		Reply:          res.Reply,
		ConversationID: res.ConvID,
		Model:          res.Model,
	}, nil
}

func (g *AgyGateway) Health(ctx context.Context) (string, error) {
	var res map[string]any
	if err := g.doJSON(ctx, http.MethodGet, "/api/health", nil, &res); err != nil {
		return "", err
	}
	return fmt.Sprintf("agy-gateway reachable (%s)", g.BaseURL), nil
}

func (g *AgyGateway) ListTasks(ctx context.Context) ([]TaskInfo, error) {
	var res struct {
		Automations []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			CronExpr string `json:"cron_expr"`
			Enabled  bool   `json:"enabled"`
		} `json:"automations"`
	}
	if err := g.doJSON(ctx, http.MethodGet, "/api/automations", nil, &res); err != nil {
		return nil, err
	}
	out := make([]TaskInfo, 0, len(res.Automations))
	for _, a := range res.Automations {
		out = append(out, TaskInfo{
			ID:       a.ID,
			Name:     a.Name,
			Schedule: a.CronExpr,
			Enabled:  a.Enabled,
		})
	}
	return out, nil
}

func (g *AgyGateway) RunTask(ctx context.Context, taskID string) (string, error) {
	var res map[string]any
	if err := g.doJSON(ctx, http.MethodPost, "/api/automations/"+taskID+"/run", nil, &res); err != nil {
		return "", err
	}
	return fmt.Sprintf("Task `%s` triggered on agy-gateway.", taskID), nil
}

func (g *AgyGateway) WakeHost(ctx context.Context) (string, error) {
	var res map[string]any
	if err := g.doJSON(ctx, http.MethodPost, "/api/wol", nil, &res); err != nil {
		return "", err
	}
	return "Wake-on-LAN magic packet dispatched via agy-gateway.", nil
}
