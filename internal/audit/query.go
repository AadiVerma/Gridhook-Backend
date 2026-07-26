package audit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gridhook.dev/connector-backend/internal/models"
)

// ListFilter covers everything /audit-logs accepts: status/connector/server/
// tool + a date range, plus pagination.
type ListFilter struct {
	Status      string
	ConnectorID int64
	ServerID    int64
	ToolID      int64
	From        *time.Time
	To          *time.Time
	Page        int
	PageSize    int
}

func listInvocations(ctx context.Context, pool *pgxpool.Pool, orgID int64, f ListFilter) ([]*models.ToolInvocation, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 200 {
		f.PageSize = 50
	}

	where := []string{"organization_id = $1"}
	args := []any{orgID}
	add := func(cond string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.ConnectorID != 0 {
		add("connector_id = $%d", f.ConnectorID)
	}
	if f.ServerID != 0 {
		add("mcp_server_id = $%d", f.ServerID)
	}
	if f.ToolID != 0 {
		add("tool_id = $%d", f.ToolID)
	}
	if f.From != nil {
		add("created_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("created_at <= $%d", *f.To)
	}
	whereClause := strings.Join(where, " AND ")

	var total int
	countSQL := "SELECT count(*) FROM tool_invocations WHERE " + whereClause
	if err := pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("audit: count: %w", err)
	}

	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	listSQL := fmt.Sprintf(`
		SELECT id, tool_id, connector_id, connector_api_id, coalesce(mcp_server_id,0), organization_id,
		       coalesce(user_id,0), coalesce(user_email,''), status, coalesce(http_code,0), duration_ms,
		       input, output, coalesce(error,''), created_at
		FROM tool_invocations
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, len(args)-1, len(args))

	rows, err := pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("audit: list: %w", err)
	}
	defer rows.Close()

	var out []*models.ToolInvocation
	for rows.Next() {
		inv := &models.ToolInvocation{}
		if err := rows.Scan(&inv.ID, &inv.ToolID, &inv.ConnectorID, &inv.ConnectorAPIID, &inv.MCPServerID,
			&inv.OrganizationID, &inv.UserID, &inv.UserEmail, &inv.Status, &inv.HTTPCode, &inv.DurationMs,
			&inv.Input, &inv.Output, &inv.Error, &inv.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("audit: scan: %w", err)
		}
		out = append(out, inv)
	}
	return out, total, rows.Err()
}

func getInvocation(ctx context.Context, pool *pgxpool.Pool, orgID, id int64) (*models.ToolInvocation, error) {
	inv := &models.ToolInvocation{}
	err := pool.QueryRow(ctx, `
		SELECT id, tool_id, connector_id, connector_api_id, coalesce(mcp_server_id,0), organization_id,
		       coalesce(user_id,0), coalesce(user_email,''), status, coalesce(http_code,0), duration_ms,
		       input, output, coalesce(error,''), created_at
		FROM tool_invocations
		WHERE organization_id = $1 AND id = $2
	`, orgID, id).Scan(&inv.ID, &inv.ToolID, &inv.ConnectorID, &inv.ConnectorAPIID, &inv.MCPServerID,
		&inv.OrganizationID, &inv.UserID, &inv.UserEmail, &inv.Status, &inv.HTTPCode, &inv.DurationMs,
		&inv.Input, &inv.Output, &inv.Error, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("audit: invocation %d not found", id)
	}
	return inv, err
}
