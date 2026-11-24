package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AgyGateway connects Aegys Bot to AetherCore-Works/agy-mobile over HTTP.
type AgyGateway struct {
	BaseURL          string
	Secret           string
	Client           *http.Client
	StreamClient     *http.Client
	PromptTimeoutSec int
}

func NewAgyGateway(baseURL, secret string) *AgyGateway {
	return &AgyGateway{
		BaseURL:          strings.TrimRight(baseURL, "/"),
		Secret:           secret,
		Client:           &http.Client{Timeout: 75 * time.Second},
		StreamClient:     &http.Client{Timeout: 10 * time.Minute},
		PromptTimeoutSec: 55,
	}
}

func (g *AgyGateway) Name() string { return "agy-gateway" }

func (g *AgyGateway) doJSON(ctx context.Context, method, path string, payload any, out any) (int, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.BaseURL+path, body)
	if err != nil {
		return 0, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+g.Secret)
	}

	resp, err := g.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return resp.StatusCode, fmt.Errorf("agy-gateway %s %s failed (%d): %s", method, path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func (g *AgyGateway) Ask(ctx context.Context, req AskRequest) (AskResponse, error) {
	var res struct {
		OK             *bool    `json:"ok"`
		JobID          string   `json:"job_id"`
		ConversationID string   `json:"conversation_id"`
		ConvID         string   `json:"conv_id"`
		Status         string   `json:"status"`
		Pending        bool     `json:"pending"`
		Reply          string   `json:"reply"`
		Model          string   `json:"model"`
		OutboxFiles    []string `json:"outbox_files"`
		Error          string   `json:"error"`
	}
	timeoutSec := g.PromptTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 55
	}
	payload := map[string]any{
		"prompt":      req.Prompt,
		"wait":        true,
		"timeout_sec": timeoutSec,
	}
	if cid := strings.TrimSpace(req.ConversationID); cid != "" {
		payload["conversation_id"] = cid
	}
	statusCode, err := g.doJSON(ctx, http.MethodPost, "/api/webhook/prompt", payload, &res)
	if err != nil {
		return AskResponse{}, err
	}

	if statusCode == http.StatusAccepted || (res.Pending && res.JobID != "") {
		if strings.TrimSpace(res.JobID) == "" {
			return AskResponse{}, fmt.Errorf("agy-gateway returned 202 pending without job_id")
		}
		return g.followJobEvents(ctx, res.JobID, req.ConversationID)
	}

	if res.Error != "" {
		return AskResponse{}, fmt.Errorf("agy-gateway job error: %s", res.Error)
	}
	if res.Status == "error" || res.Status == "cancelled" {
		return AskResponse{}, fmt.Errorf("agy-gateway job ended with status %q", res.Status)
	}

	convID := res.ConversationID
	if convID == "" {
		convID = res.ConvID
	}
	return AskResponse{
		Reply:          res.Reply,
		ConversationID: convID,
		Model:          res.Model,
		OutboxFiles:    res.OutboxFiles,
	}, nil
}

func (g *AgyGateway) followJobEvents(ctx context.Context, jobID, fallbackConvID string) (AskResponse, error) {
	endpoint := g.BaseURL + "/api/jobs/" + url.PathEscape(jobID) + "/events?after=0"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return AskResponse{}, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if g.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+g.Secret)
	}

	client := g.StreamClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return AskResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return AskResponse{}, fmt.Errorf("agy-gateway stream %s failed (%d): %s", jobID, resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var deltaBuf strings.Builder
	out := AskResponse{ConversationID: fallbackConvID}
	var jobErr string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" {
			continue
		}
		if raw == "[DONE]" {
			break
		}

		var ev map[string]any
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			continue
		}
		evType, _ := ev["type"].(string)

		if t, ok := ev["text"].(string); ok && t != "" && (evType == "" || evType == "delta") {
			deltaBuf.WriteString(t)
		}
		if r, ok := ev["response"].(string); ok && strings.TrimSpace(r) != "" {
			out.Reply = r
		}
		if cid, ok := ev["conversation_id"].(string); ok && cid != "" {
			out.ConversationID = cid
		}
		if m, ok := ev["model"].(string); ok && m != "" {
			out.Model = m
		}
		if files, ok := ev["outbox_files"].([]any); ok {
			out.OutboxFiles = out.OutboxFiles[:0]
			for _, f := range files {
				if s, ok := f.(string); ok && s != "" {
					out.OutboxFiles = append(out.OutboxFiles, s)
				}
			}
		}
		if evType == "error" {
			if msg, ok := ev["message"].(string); ok && msg != "" {
				jobErr = msg
			} else if msg, ok := ev["error"].(string); ok && msg != "" {
				jobErr = msg
			} else {
				jobErr = "remote job failed"
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return AskResponse{}, fmt.Errorf("agy-gateway event stream error: %w", err)
	}
	if strings.TrimSpace(out.Reply) == "" {
		out.Reply = deltaBuf.String()
	}
	if jobErr != "" && strings.TrimSpace(out.Reply) == "" {
		return AskResponse{}, fmt.Errorf("agy-gateway job error: %s", jobErr)
	}
	return out, nil
}

func (g *AgyGateway) Health(ctx context.Context) (string, error) {
	var res map[string]any
	if _, err := g.doJSON(ctx, http.MethodGet, "/api/health", nil, &res); err != nil {
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
	if _, err := g.doJSON(ctx, http.MethodGet, "/api/automations", nil, &res); err != nil {
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
	if _, err := g.doJSON(ctx, http.MethodPost, "/api/automations/"+url.PathEscape(taskID)+"/run", nil, &res); err != nil {
		return "", err
	}
	return fmt.Sprintf("Task `%s` triggered on agy-gateway.", taskID), nil
}

func (g *AgyGateway) WakeHost(ctx context.Context) (string, error) {
	var res map[string]any
	if _, err := g.doJSON(ctx, http.MethodPost, "/api/wol", nil, &res); err != nil {
		return "", err
	}
	return "Wake-on-LAN magic packet dispatched via agy-gateway.", nil
}

// SubmitFeedback records user thumbs-up/down or correction notes in agy-gateway's evolution telemetry.
func (g *AgyGateway) SubmitFeedback(ctx context.Context, convID, rating, correction string) (string, error) {
	payload := map[string]any{
		"conversation_id": strings.TrimSpace(convID),
		"rating":          strings.ToLower(strings.TrimSpace(rating)),
		"correction":      strings.TrimSpace(correction),
	}
	var res struct {
		OK   bool   `json:"ok"`
		Kind string `json:"kind"`
	}
	if _, err := g.doJSON(ctx, http.MethodPost, "/api/feedback", payload, &res); err != nil {
		return "", err
	}
	if res.Kind == "" {
		return "feedback", nil
	}
	return res.Kind, nil
}

// EvolutionReport fetches the evolution loop status and proposals from agy-gateway.
func (g *AgyGateway) EvolutionReport(ctx context.Context) (EvolutionReportSummary, error) {
	var res struct {
		Enabled   bool                       `json:"enabled"`
		Proposals []EvolutionProposalSummary `json:"proposals"`
	}
	if _, err := g.doJSON(ctx, http.MethodGet, "/api/evolution/report", nil, &res); err != nil {
		return EvolutionReportSummary{}, err
	}
	pending := 0
	for _, p := range res.Proposals {
		if strings.EqualFold(strings.TrimSpace(p.Status), "pending") {
			pending++
		}
	}
	return EvolutionReportSummary{
		Enabled:      res.Enabled,
		PendingCount: pending,
		Proposals:    res.Proposals,
	}, nil
}

// DecideProposal approves or rejects an evolution proposal on agy-gateway.
func (g *AgyGateway) DecideProposal(ctx context.Context, proposalID string, approve bool) (string, error) {
	action := "reject"
	if approve {
		action = "approve"
	}
	var res map[string]any
	path := "/api/evolution/proposals/" + url.PathEscape(strings.TrimSpace(proposalID)) + "/" + action
	if _, err := g.doJSON(ctx, http.MethodPost, path, nil, &res); err != nil {
		return "", err
	}
	if approve {
		return fmt.Sprintf("Proposal `%s` approved on agy-gateway.", proposalID), nil
	}
	return fmt.Sprintf("Proposal `%s` rejected on agy-gateway.", proposalID), nil
}
