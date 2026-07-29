package audit

import (
	"context"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
)

type Reader struct {
	db *gorm.DB
}

func NewReader(gdb *gorm.DB) *Reader {
	return &Reader{db: gdb}
}

func (r *Reader) List(ctx context.Context, orgID int64, filter ListFilter) ([]*models.ToolInvocation, int, error) {
	return listInvocations(ctx, r.db, orgID, filter)
}

func (r *Reader) Get(ctx context.Context, orgID, id int64) (*models.ToolInvocation, error) {
	return getInvocation(ctx, r.db, orgID, id)
}
