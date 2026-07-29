package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/engines"
	"gridhook.dev/connector-backend/internal/httpx"
	"gridhook.dev/connector-backend/internal/models"
)

type ToolStore interface {
	ResolveForServer(ctx context.Context, orgID, mcpServerID int64, toolName string) (*models.ResolvedTool, error)
}

type CredentialResolver interface {
	Resolve(ctx context.Context, api *models.ConnectorAPI) (schemes.Credentials, error)
}

type AuditWriter interface {
	Write(ctx context.Context, inv *models.ToolInvocation)
}

type Identity struct {
	OrganizationID int64
	UserID         int64
	UserEmail      string
}

type Outcome struct {
	Status     models.InvocationStatus `json:"status"`
	HTTPCode   int                     `json:"httpCode,omitempty"`
	Body       any                     `json:"body,omitempty"`
	Error      string                  `json:"error,omitempty"`
	DurationMs int                     `json:"durationMs"`
}

type Dispatcher struct {
	tools    ToolStore
	broker   CredentialResolver
	registry *engines.Registry
	audit    AuditWriter
}

func New(tools ToolStore, broker CredentialResolver, registry *engines.Registry, audit AuditWriter) *Dispatcher {
	return &Dispatcher{tools: tools, broker: broker, registry: registry, audit: audit}
}

func (d *Dispatcher) Invoke(ctx context.Context, mcpServerID int64, toolName string, input map[string]any, ident Identity) (*Outcome, error) {
	lookup, err := d.tools.ResolveForServer(ctx, ident.OrganizationID, mcpServerID, toolName)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: resolve tool %q: %w", toolName, err)
	}
	return d.dispatch(ctx, lookup, mcpServerID, input, ident)
}

func (d *Dispatcher) InvokeDirect(ctx context.Context, tool *models.MCPTool, api *models.ConnectorAPI, input map[string]any, ident Identity) (*Outcome, error) {
	lookup := &models.ResolvedTool{Tool: tool, API: api}
	return d.dispatch(ctx, lookup, 0, input, ident)
}

func (d *Dispatcher) dispatch(ctx context.Context, lookup *models.ResolvedTool, mcpServerID int64, input map[string]any, ident Identity) (*Outcome, error) {
	start := time.Now()

	creds, err := d.broker.Resolve(ctx, lookup.API)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: resolve credentials: %w", err)
	}

	engine, err := d.registry.For(lookup.Tool.EngineType)
	if err != nil {
		return nil, err
	}

	result, execErr := engine.Execute(ctx, lookup.API, lookup.Tool, creds, input)
	outcome := normalize(result, execErr, time.Since(start))

	if outcome.Status == models.InvocationSuccess {
		outcome.Body = applyResponseMapping(lookup.Tool.ResponseMapping, outcome.Body)
	}

	d.audit.Write(ctx, &models.ToolInvocation{
		ToolID:         lookup.Tool.ID,
		ConnectorID:    lookup.ConnectorID(),
		ConnectorAPIID: lookup.API.ID,
		MCPServerID:    mcpServerID,
		OrganizationID: ident.OrganizationID,
		UserID:         ident.UserID,
		UserEmail:      ident.UserEmail,
		Status:         outcome.Status,
		HTTPCode:       outcome.HTTPCode,
		DurationMs:     outcome.DurationMs,
		Input:          input,
		Output:         asMap(outcome.Body),
		Error:          outcome.Error,
		CreatedAt:      start,
	})

	return outcome, nil
}

func normalize(result *engines.Result, err error, duration time.Duration) *Outcome {
	ms := int(duration.Milliseconds())

	if err != nil {
		return &Outcome{
			Status:     classifyError(err),
			Error:      httpx.SanitizeError(err).Error(),
			DurationMs: ms,
		}
	}
	if result == nil {
		return &Outcome{
			Status:     models.InvocationError,
			Error:      "engine returned no result",
			DurationMs: ms,
		}
	}

	outcome := &Outcome{
		Status:     models.InvocationSuccess,
		HTTPCode:   result.StatusCode,
		Body:       result.Body,
		DurationMs: ms,
	}
	if result.StatusCode >= 400 {
		outcome.Status = models.InvocationError

		outcome.Error = fmt.Sprintf("upstream returned HTTP %d", result.StatusCode)
	}
	return outcome
}

func classifyError(err error) models.InvocationStatus {
	if errors.Is(err, context.DeadlineExceeded) {
		return models.InvocationTimeout
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return models.InvocationTimeout
	}
	return models.InvocationError
}

func asMap(v any) map[string]any {
	switch typed := v.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return typed
	default:
		return map[string]any{"value": typed}
	}
}
