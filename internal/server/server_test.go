package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AetherCore-Works/aegys-bot/internal/backend"
	"github.com/AetherCore-Works/aegys-bot/internal/channels"
	"github.com/AetherCore-Works/aegys-bot/internal/channels/mock"
	"github.com/AetherCore-Works/aegys-bot/internal/config"
	"github.com/AetherCore-Works/aegys-bot/internal/rbac"
	"github.com/AetherCore-Works/aegys-bot/internal/router"
)

func TestNotifyAndDevSimulateEndpoints(t *testing.T) {
	cfg := config.Settings{
		BotName:    "Aegys",
		APISecret:  "super-secret",
		AIBackend:  "mock",
		EnableMock: true,
	}
	guard := rbac.New([]string{"mock:admin"}, []string{"mock:user"}, true)
	rt := router.New(cfg.BotName, guard, backend.NewMock(), nil)
	reg := channels.NewRegistry()
	mockCh := mock.NewAdapter()
	reg.Register(mockCh)

	srv := &Server{Cfg: cfg, Router: rt, Registry: reg}
	mux := srv.Routes()

	// 1. /healthz
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /healthz, got %d", rec.Code)
	}

	// 2. /api/v1/notify rejects missing token
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/notify", strings.NewReader(`{"platform":"mock","recipient":"user-1","text":"hi"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without bearer token, got %d", rec.Code)
	}

	// 3. /api/v1/notify succeeds with valid token
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify", strings.NewReader(`{"platform":"mock","recipient":"user-1","text":"Deploy complete"}`))
	req.Header.Set("Authorization", "Bearer super-secret")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(mockCh.History) != 1 || mockCh.History[0].Text != "Deploy complete" {
		t.Fatalf("unexpected notify result: code=%d history=%+v", rec.Code, mockCh.History)
	}

	// 4. /api/v1/dev/simulate runs hybrid router locally
	simReq := httptest.NewRequest(http.MethodPost, "/api/v1/dev/simulate", strings.NewReader(`{"platform":"mock","user_id":"admin","text":"/ping"}`))
	simRec := httptest.NewRecorder()
	mux.ServeHTTP(simRec, simReq)
	var simBody map[string]any
	_ = json.NewDecoder(simRec.Body).Decode(&simBody)
	if simRec.Code != http.StatusOK || simBody["path"] != "fast" || simBody["allowed"] != true {
		t.Fatalf("unexpected simulate response: code=%d body=%+v", simRec.Code, simBody)
	}
}
