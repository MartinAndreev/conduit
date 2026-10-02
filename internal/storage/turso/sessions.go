package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/table"
	jet "github.com/go-jet/jet/v2/sqlite"
)

type sessionRepository struct{ db database }

func (r sessionRepository) Get(ctx context.Context, id sessions.ID) (sessions.Record, error) {
	query, args := jet.SELECT(table.Sessions.AllColumns).FROM(table.Sessions).WHERE(table.Sessions.ID.EQ(jet.String(string(id)))).Sql()
	row := r.db.QueryRowContext(ctx, query, args...)
	var record sessions.Record
	var created, updated string
	err := row.Scan(&record.ID, &record.ProjectID, &record.RunID, &record.AttemptID, &record.AssignmentID, &record.Revision, &record.AdapterSessionID, &record.NativeBinding, &record.State, &record.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return sessions.Record{}, sessions.ErrNotFound
	}
	if err != nil {
		return sessions.Record{}, fmt.Errorf("read session: %w", err)
	}
	record.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return sessions.Record{}, fmt.Errorf("parse session creation time: %w", err)
	}
	record.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return sessions.Record{}, fmt.Errorf("parse session update time: %w", err)
	}
	return record, nil
}

func (r sessionRepository) Save(ctx context.Context, record sessions.Record, expectedVersion uint64) (sessions.Record, error) {
	if expectedVersion == 0 {
		record.Version = 1
		statement := table.Sessions.INSERT(table.Sessions.AllColumns).VALUES(record.ID, record.ProjectID, record.RunID, record.AttemptID, record.AssignmentID, record.Revision, record.AdapterSessionID, record.NativeBinding, record.State, record.Version, record.CreatedAt.UTC().Format(time.RFC3339Nano), record.UpdatedAt.UTC().Format(time.RFC3339Nano))
		query, args := statement.Sql()
		_, err := r.db.ExecContext(ctx, query, args...)
		if err != nil {
			if _, getErr := r.Get(ctx, record.ID); getErr == nil {
				return sessions.Record{}, sessions.ErrVersionConflict
			}
			return sessions.Record{}, fmt.Errorf("create session: %w", err)
		}
		return record, nil
	}
	existing, err := r.Get(ctx, record.ID)
	if errors.Is(err, sessions.ErrNotFound) {
		return sessions.Record{}, sessions.ErrVersionConflict
	}
	if err != nil {
		return sessions.Record{}, err
	}
	record.CreatedAt = existing.CreatedAt
	record.Version = expectedVersion + 1
	statement := table.Sessions.UPDATE(table.Sessions.ProjectID, table.Sessions.RunID, table.Sessions.AttemptID, table.Sessions.AssignmentID, table.Sessions.Revision, table.Sessions.AdapterSessionID, table.Sessions.NativeBinding, table.Sessions.State, table.Sessions.Version, table.Sessions.UpdatedAt).SET(record.ProjectID, record.RunID, record.AttemptID, record.AssignmentID, record.Revision, record.AdapterSessionID, record.NativeBinding, record.State, record.Version, record.UpdatedAt.UTC().Format(time.RFC3339Nano)).WHERE(table.Sessions.ID.EQ(jet.String(string(record.ID))).AND(table.Sessions.Version.EQ(jet.Int64(int64(expectedVersion)))))
	query, args := statement.Sql()
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return sessions.Record{}, fmt.Errorf("update session: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return sessions.Record{}, fmt.Errorf("inspect session update: %w", err)
	}
	if changed != 1 {
		return sessions.Record{}, sessions.ErrVersionConflict
	}
	return record, nil
}
