package connectors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/devpablocristo/core/http/go/httpjson"
	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

const (
	scopeCompanionConnectorsExecute = "companion:connectors:execute"
	scopeCompanionConnectorsAdmin   = "companion:connectors:admin"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func principalOrgID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Org-ID"))
}

func principalActorID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-User-ID"))
}

func canAccessConnectorOrg(r *http.Request, connector domain.Connector) bool {
	orgID := principalOrgID(r)
	if requestHasNoAuthContext(r) || orgID == "" || strings.TrimSpace(connector.OrgID) == "" {
		return true
	}
	return strings.TrimSpace(connector.OrgID) == orgID
}

func canAccessExecutionOrg(r *http.Request, execution domain.ExecutionResult) bool {
	orgID := principalOrgID(r)
	if requestHasNoAuthContext(r) || orgID == "" || strings.TrimSpace(execution.OrgID) == "" {
		return true
	}
	return strings.TrimSpace(execution.OrgID) == orgID
}

func bindPayloadToPrincipalOrg(r *http.Request, raw json.RawMessage) (json.RawMessage, bool) {
	orgID := principalOrgID(r)
	if orgID == "" {
		if len(raw) == 0 {
			return json.RawMessage(`{}`), true
		}
		return raw, true
	}
	var payload map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return raw, true
		}
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	if requested, ok := payload["org_id"]; ok {
		if requestedOrg := strings.TrimSpace(rawToString(requested)); requestedOrg != "" && requestedOrg != orgID {
			return nil, false
		}
	}
	payload["org_id"] = orgID
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return json.RawMessage(out), true
}

func requestHasNoAuthContext(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("X-Auth-Method")) == "" &&
		strings.TrimSpace(r.Header.Get("X-Auth-Scopes")) == ""
}

func requestHasScope(r *http.Request, scopes ...string) bool {
	have := parseHeaderScopes(r.Header.Get("X-Auth-Scopes"))
	for _, scope := range scopes {
		if _, ok := have[scope]; ok {
			return true
		}
	}
	return false
}

func parseHeaderScopes(raw string) map[string]struct{} {
	raw = strings.NewReplacer(",", " ", ";", " ", "+", " ").Replace(raw)
	fields := strings.Fields(raw)
	out := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if scope := strings.TrimSpace(field); scope != "" {
			out[scope] = struct{}{}
		}
	}
	return out
}

func rawToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}
