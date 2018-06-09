package application

import (
	"context"

	"cloud.google.com/go/bigtable"
	"github.com/takashabe/btcli/api/domain"
	"github.com/takashabe/btcli/api/domain/repository"
)

// RowsInteractor provide rows data
type RowsInteractor struct {
	repository repository.Bigtable
}

// NewRowsInteractor returns initialized RowsInteractor
func NewRowsInteractor(r repository.Bigtable) *RowsInteractor {
	return &RowsInteractor{
		repository: r,
	}
}

// GetRow returns a single row
func (t *RowsInteractor) GetRow(ctx context.Context, table, key string) (*domain.Row, error) {
	tbl, err := t.repository.Get(ctx, table, key)
	if err != nil {
		return nil, err
	}
	return tbl.Rows[0], nil
}

// GetRows returns rows
func (t *RowsInteractor) GetRows(ctx context.Context, table string, rr bigtable.RowRange, opts ...bigtable.ReadOption) ([]*domain.Row, error) {
	tbl, err := t.repository.GetRows(ctx, table, rr, opts...)
	if err != nil {
		return nil, err
	}
	return tbl.Rows, nil
}
