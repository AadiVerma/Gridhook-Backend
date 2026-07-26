package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/engines"
	"gridhook.dev/connector-backend/internal/models"
)

type ToolLookup struct {
	Tool        *models.MCPTool
	API         *models.ConnectorAPI
	ConnectorID int64
}

type ToolStore interface {
	ResolveForServer(ctx context.Context, mcpServerID int64, toolName string) (*ToolLookup, error)
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
	Status     models.InvocationStatus
	HTTPCode   int
	Body       any
	Error      string
	DurationMs int
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
	lookup, err := d.tools.ResolveForServer(ctx, mcpServerID, toolName)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: resolve tool %q: %w", toolName, err)
	}
	return d.dispatch(ctx, lookup, mcpServerID, input, ident)
}

func (d *Dispatcher) InvokeDirect(ctx context.Context, tool *models.MCPTool, api *models.ConnectorAPI, input map[string]any, ident Identity) (*Outcome, error) {
	lookup := &ToolLookup{Tool: tool, API: api, ConnectorID: api.ConnectorID}
	return d.dispatch(ctx, lookup, 0, input, ident)
}

func (d *Dispatcher) dispatch(ctx context.Context, lookup *ToolLookup, mcpServerID int64, input map[string]any, ident Identity) (*Outcome, error) {
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
	duration := time.Since(start)

	outcome := normalize(result, execErr, duration)
	if outcome.Status == models.InvocationSuccess {
		outcome.Body = applyResponseMapping(lookup.Tool.ResponseMapping, outcome.Body)
	}

	d.audit.Write(ctx, &models.ToolInvocation{
		ToolID:         lookup.Tool.ID,
		ConnectorID:    lookup.ConnectorID,
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
		status := models.InvocationError
		var netErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
			status = models.InvocationTimeout
		}
		return &Outcome{Status: status, Error: err.Error(), DurationMs: ms}
	}
	status := models.InvocationSuccess
	if result.StatusCode >= 400 {
		status = models.InvocationError
	}
	return &Outcome{Status: status, HTTPCode: result.StatusCode, Body: result.Body, DurationMs: ms}
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	if v == nil {
		return map[string]any{}
	}
	return map[string]any{"value": v}
}
