package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

// pontiMock simula el backend de Ponti para validar:
//   - El adapter llama el path correcto.
//   - El header X-Tenant-Id se propaga.
//   - El bearer token se envía.
//   - La respuesta cruda llega al ResultJSON sin alteración.
//   - Si la URL de un tenant intentara consultar datos de otro, el mock
//     devuelve 403 — espejando lo que haría el Ponti real.
type pontiMock struct {
	server      *httptest.Server
	calls       []recordedCall
	tenantData  map[string]map[string]any // org_id -> insight_id -> insight
	expectToken string
}

type recordedCall struct {
	Method   string
	Path     string
	OrgID    string
	AuthHdr  string
	RawQuery string
}

func newPontiMock(t *testing.T) *pontiMock {
	t.Helper()
	m := &pontiMock{
		expectToken: "Bearer ponti-test-key",
		tenantData: map[string]map[string]any{
			"tenant-A": {
				"insight-A1": map[string]any{
					"id":         "insight-A1",
					"title":      "Stock negativo en producto X (tenant A)",
					"event_type": "ponti.stock.negative",
				},
			},
			"tenant-B": {
				"insight-B1": map[string]any{
					"id":         "insight-B1",
					"title":      "Confidential B-only insight",
					"event_type": "ponti.stock.negative",
				},
			},
		},
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID := r.Header.Get("X-Tenant-Id")
		auth := r.Header.Get("Authorization")
		m.calls = append(m.calls, recordedCall{
			Method:   r.Method,
			Path:     r.URL.Path,
			OrgID:    orgID,
			AuthHdr:  auth,
			RawQuery: r.URL.RawQuery,
		})
		if auth != m.expectToken {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if orgID == "" {
			http.Error(w, `{"error":"missing tenant"}`, http.StatusBadRequest)
			return
		}
		tenantInsights, ok := m.tenantData[orgID]
		if !ok {
			http.Error(w, `{"error":"tenant not found"}`, http.StatusForbidden)
			return
		}

		switch {
		case r.URL.Path == "/api/v1/insights":
			items := make([]any, 0, len(tenantInsights))
			for _, v := range tenantInsights {
				items = append(items, v)
			}
			writeJSON(w, map[string]any{"items": items})
		case r.URL.Path == "/api/v1/insights/summary":
			writeJSON(w, map[string]any{
				"summary": map[string]any{
					"total":       len(tenantInsights),
					"by_status":   map[string]int{"notified": len(tenantInsights)},
					"by_severity": map[string]int{"high": len(tenantInsights)},
					"by_kind":     map[string]int{"stock_negative": len(tenantInsights)},
				},
				"evidence": map[string]any{
					"source_ref":   "ponti.businessinsights.summary",
					"captured_at":  "2026-05-06T12:00:00Z",
					"tenant_scope": orgID,
				},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/insights/") && strings.HasSuffix(r.URL.Path, "/explain"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/insights/"), "/explain")
			insight, ok := tenantInsights[id]
			if !ok {
				// Defense-in-depth: si el caller pidió un ID que no le
				// pertenece (cross-tenant probe), devolvemos 404 sin filtrar
				// info del otro tenant.
				http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
				return
			}
			writeJSON(w, map[string]any{
				"insight": insight,
				"evidence": map[string]any{
					"source_ref":   "ponti.businessinsights.candidate:" + id,
					"tenant_scope": orgID,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(m.server.Close)
	return m
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func newPontiConnector(t *testing.T, m *pontiMock) (*PontiConnector, uuid.UUID) {
	t.Helper()
	client := NewPontiClient(m.server.URL, "ponti-test-key")
	conn := NewPontiConnector(client)
	return conn, uuid.New()
}

func TestPontiConnector_ListInsights_PropagatesTenant(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	conn, connID := newPontiConnector(t, mock)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connID,
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "ponti.insights.list",
		Payload:     json.RawMessage(`{"limit":10}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Status != domain.ExecSuccess {
		t.Fatalf("expected success, got %s err=%s", res.Status, res.ErrorMessage)
	}

	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 ponti call, got %d", len(mock.calls))
	}
	got := mock.calls[0]
	if got.OrgID != "tenant-A" {
		t.Errorf("expected X-Tenant-Id=tenant-A, got %q", got.OrgID)
	}
	if got.AuthHdr != "Bearer ponti-test-key" {
		t.Errorf("expected bearer token, got %q", got.AuthHdr)
	}
	if !strings.Contains(got.RawQuery, "limit=10") {
		t.Errorf("expected limit=10 in query, got %q", got.RawQuery)
	}
	// La respuesta del mock debe llegar verbatim.
	var body map[string]any
	if err := json.Unmarshal(res.ResultJSON, &body); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if _, ok := body["items"]; !ok {
		t.Fatalf("expected items key, got %s", string(res.ResultJSON))
	}
	// Evidence pack obligatorio.
	if len(res.EvidenceJSON) == 0 {
		t.Fatal("expected evidence_json populated")
	}
}

func TestPontiConnector_Summary_RespondsWithCounts(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	conn, connID := newPontiConnector(t, mock)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connID,
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "ponti.insights.summary",
		Payload:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecSuccess {
		t.Fatalf("expected success, got %s err=%s", res.Status, res.ErrorMessage)
	}
	var body map[string]any
	json.Unmarshal(res.ResultJSON, &body)
	if _, ok := body["summary"]; !ok {
		t.Fatalf("expected summary key, got %s", string(res.ResultJSON))
	}
}

func TestPontiConnector_Explain_RejectsCrossTenantProbe(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	conn, connID := newPontiConnector(t, mock)

	// Tenant A intenta pedir explain de un insight que pertenece al tenant B.
	// El mock (espejando Ponti real) responde 404 en lugar de filtrar info.
	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connID,
		OrgID:       "tenant-A",
		ActorID:     "attacker",
		Operation:   "ponti.insights.explain",
		Payload:     json.RawMessage(`{"insight_id":"insight-B1"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecFailure {
		t.Fatalf("expected failure (cross-tenant probe), got %s", res.Status)
	}
	if !strings.Contains(strings.ToLower(res.ErrorMessage), "not found") &&
		!strings.Contains(res.ErrorMessage, "404") {
		t.Errorf("expected 404 not_found error, got %q", res.ErrorMessage)
	}
	// Crítico: el ResultJSON del fallo no debe contener el title del tenant B.
	if strings.Contains(string(res.ResultJSON), "Confidential B-only insight") {
		t.Fatal("cross-tenant leakage: tenant A response contains tenant B data")
	}
	if strings.Contains(res.ErrorMessage, "Confidential B-only") {
		t.Fatal("cross-tenant leakage: error message contains tenant B data")
	}
}

func TestPontiConnector_Explain_HappyPath(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	conn, connID := newPontiConnector(t, mock)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connID,
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "ponti.insights.explain",
		Payload:     json.RawMessage(`{"insight_id":"insight-A1"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecSuccess {
		t.Fatalf("expected success, got %s err=%s", res.Status, res.ErrorMessage)
	}
	var body map[string]any
	json.Unmarshal(res.ResultJSON, &body)
	insight, ok := body["insight"].(map[string]any)
	if !ok {
		t.Fatalf("expected insight key, got %s", string(res.ResultJSON))
	}
	if insight["id"] != "insight-A1" {
		t.Fatalf("expected insight-A1, got %v", insight["id"])
	}
}

func TestPontiConnector_Explain_RequiresInsightID(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	conn, connID := newPontiConnector(t, mock)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connID,
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "ponti.insights.explain",
		Payload:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecFailure {
		t.Fatalf("expected failure when insight_id missing, got %s", res.Status)
	}
	if !strings.Contains(res.ErrorMessage, "insight_id is required") {
		t.Errorf("expected validation error, got %q", res.ErrorMessage)
	}
}

func TestPontiConnector_RejectsInvalidToken(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	// Forzamos un token mal configurado: el mock devuelve 401.
	client := NewPontiClient(mock.server.URL, "wrong-key")
	conn := NewPontiConnector(client)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: uuid.New(),
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "ponti.insights.list",
		Payload:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecFailure {
		t.Fatalf("expected failure on bad token, got %s", res.Status)
	}
	if !strings.Contains(res.ErrorMessage, "401") {
		t.Errorf("expected 401 in error, got %q", res.ErrorMessage)
	}
}

func TestPontiConnector_Capabilities_AreReadOnly(t *testing.T) {
	t.Parallel()
	conn := NewPontiConnector(nil)
	caps := conn.Capabilities()
	if len(caps) != 3 {
		t.Fatalf("expected 3 capabilities (list, summary, explain), got %d", len(caps))
	}
	for _, c := range caps {
		if !c.ReadOnly {
			t.Errorf("capability %q must be read_only=true in fase 1", c.ID)
		}
		if c.RequiresReview {
			t.Errorf("capability %q must NOT require review in fase 1 (read-only)", c.ID)
		}
		if c.RiskClass != domain.RiskClassLow {
			t.Errorf("capability %q must risk_class=low, got %s", c.ID, c.RiskClass)
		}
		if c.AuthMode.Type != "delegated_user" {
			t.Errorf("capability %q must auth_mode=delegated_user, got %s", c.ID, c.AuthMode.Type)
		}
	}
}
