package dispatcher

import (
	"context"
	"errors"
	"testing"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/engines"
	"gridhook.dev/connector-backend/internal/httpx"
	"gridhook.dev/connector-backend/internal/models"
)

type fakeToolStore struct {
	gotOrgID   int64
	gotServer  int64
	gotName    string
	resolved   *models.ResolvedTool
	resolveErr error
}

func (f *fakeToolStore) ResolveForServer(_ context.Context, orgID, mcpServerID int64, toolName string) (*models.ResolvedTool, error) {
	f.gotOrgID, f.gotServer, f.gotName = orgID, mcpServerID, toolName
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return f.resolved, nil
}

type fakeResolver struct {
	creds schemes.Credentials
	err   error
}

func (f fakeResolver) Resolve(context.Context, *models.ConnectorAPI) (schemes.Credentials, error) {
	return f.creds, f.err
}

type fakeAudit struct {
	written []*models.ToolInvocation
}

func (f *fakeAudit) Write(_ context.Context, inv *models.ToolInvocation) {
	f.written = append(f.written, inv)
}

func testRegistry(t *testing.T) *engines.Registry {
	t.Helper()
	cfg := httpx.DefaultConfig()
	cfg.AllowPrivateNetworks = true
	client, err := httpx.New(cfg)
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	return engines.NewRegistry(client)
}

func resolvedTool() *models.ResolvedTool {
	return &models.ResolvedTool{
		Tool: &models.MCPTool{ID: 5, Name: "get_order", EngineType: models.EngineREST, Method: models.MethodGET},
		API:  &models.ConnectorAPI{ID: 9, ConnectorID: 3, BaseURL: "https://api.invalid"},
	}
}

func TestDispatcher_Invoke_PassesOrganizationToLookup(t *testing.T) {
	store := &fakeToolStore{resolveErr: errors.New("not found")}
	d := New(store, fakeResolver{}, testRegistry(t), &fakeAudit{})

	_, _ = d.Invoke(t.Context(), 77, "get_order", nil, Identity{OrganizationID: 1234})

	if store.gotOrgID != 1234 {
		t.Errorf("ResolveForServer received orgID %d, want 1234", store.gotOrgID)
	}
	if store.gotServer != 77 {
		t.Errorf("ResolveForServer received serverID %d, want 77", store.gotServer)
	}
	if store.gotName != "get_order" {
		t.Errorf("ResolveForServer received tool %q, want get_order", store.gotName)
	}
}

func TestDispatcher_Invoke_PropagatesLookupError(t *testing.T) {
	sentinel := errors.New("tool not exposed by this server")
	d := New(&fakeToolStore{resolveErr: sentinel}, fakeResolver{}, testRegistry(t), &fakeAudit{})

	if _, err := d.Invoke(t.Context(), 1, "nope", nil, Identity{OrganizationID: 1}); !errors.Is(err, sentinel) {
		t.Errorf("Invoke = %v, want the lookup error", err)
	}
}

func TestDispatcher_Invoke_CredentialFailureAborts(t *testing.T) {
	auditLog := &fakeAudit{}
	sentinel := errors.New("credentials incomplete")
	d := New(&fakeToolStore{resolved: resolvedTool()}, fakeResolver{err: sentinel}, testRegistry(t), auditLog)

	if _, err := d.Invoke(t.Context(), 1, "get_order", nil, Identity{OrganizationID: 1}); !errors.Is(err, sentinel) {
		t.Errorf("Invoke = %v, want the credential error", err)
	}
	if len(auditLog.written) != 0 {
		t.Errorf("wrote %d audit records, want 0 — nothing was dispatched", len(auditLog.written))
	}
}

func TestDispatcher_Invoke_UnknownEngine(t *testing.T) {
	lookup := resolvedTool()
	lookup.Tool.EngineType = models.EngineType("GRPC")

	d := New(&fakeToolStore{resolved: lookup}, fakeResolver{}, testRegistry(t), &fakeAudit{})
	if _, err := d.Invoke(t.Context(), 1, "get_order", nil, Identity{OrganizationID: 1}); err == nil {
		t.Error("Invoke accepted a tool with an unregistered engine type")
	}
}

func TestDispatcher_Invoke_AuditsFailedCalls(t *testing.T) {
	auditLog := &fakeAudit{}
	d := New(&fakeToolStore{resolved: resolvedTool()}, fakeResolver{}, testRegistry(t), auditLog)

	ident := Identity{OrganizationID: 42, UserID: 7, UserEmail: "ada@example.com"}
	outcome, err := d.Invoke(t.Context(), 88, "get_order", map[string]any{"id": "1"}, ident)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Status == models.InvocationSuccess {
		t.Fatal("expected the call to api.invalid to fail")
	}

	if len(auditLog.written) != 1 {
		t.Fatalf("wrote %d audit records, want 1", len(auditLog.written))
	}
	rec := auditLog.written[0]

	if rec.OrganizationID != 42 || rec.UserID != 7 || rec.UserEmail != "ada@example.com" {
		t.Errorf("audit identity = org %d user %d %q, want 42/7/ada@example.com",
			rec.OrganizationID, rec.UserID, rec.UserEmail)
	}
	if rec.ToolID != 5 || rec.ConnectorAPIID != 9 || rec.ConnectorID != 3 || rec.MCPServerID != 88 {
		t.Errorf("audit ids = tool %d api %d connector %d server %d",
			rec.ToolID, rec.ConnectorAPIID, rec.ConnectorID, rec.MCPServerID)
	}
	if rec.Error == "" {
		t.Error("audit record has no error text for a failed call")
	}
	if rec.CreatedAt.IsZero() {
		t.Error("audit record has a zero timestamp")
	}
}

func TestDispatcher_InvokeDirect_RecordsNoServer(t *testing.T) {
	auditLog := &fakeAudit{}
	d := New(&fakeToolStore{}, fakeResolver{}, testRegistry(t), auditLog)
	lookup := resolvedTool()

	if _, err := d.InvokeDirect(t.Context(), lookup.Tool, lookup.API, nil, Identity{OrganizationID: 1}); err != nil {
		t.Fatalf("InvokeDirect: %v", err)
	}
	if len(auditLog.written) != 1 {
		t.Fatalf("wrote %d audit records, want 1", len(auditLog.written))
	}
	if got := auditLog.written[0].MCPServerID; got != 0 {
		t.Errorf("MCPServerID = %d, want 0 for a direct invocation", got)
	}
}
