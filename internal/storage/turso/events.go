package turso

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/MartinAndreev/conduit/internal/events"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/generated/table"
	jet "github.com/go-jet/jet/v2/sqlite"
)

type beginTransaction func(context.Context, *sql.TxOptions) (*sql.Tx, error)

type eventRepository struct {
	db    database
	begin beginTransaction
}

func (r eventRepository) Append(ctx context.Context, event events.Event) (events.Event, error) {
	if r.begin != nil {
		tx, err := r.begin(ctx, nil)
		if err != nil {
			return events.Event{}, fmt.Errorf("begin event append: %w", err)
		}
		defer tx.Rollback()
		stored, err := (eventRepository{db: tx}).Append(ctx, event)
		if err != nil {
			return events.Event{}, err
		}
		if err := tx.Commit(); err != nil {
			return events.Event{}, fmt.Errorf("commit event append: %w", err)
		}
		return stored, nil
	}
	var existing events.Event
	var observed string
	query, args := jet.SELECT(table.EventJournal.AllColumns).FROM(table.EventJournal).WHERE(table.EventJournal.EventID.EQ(jet.String(string(event.ID)))).Sql()
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&existing.ID, &existing.RunID, &existing.Sequence, &existing.Kind, &existing.PayloadVersion, &existing.Payload, &existing.Provenance, &existing.CausationID, &observed)
	if err == nil {
		existing.ObservedAt, err = time.Parse(time.RFC3339Nano, observed)
		if err != nil {
			return events.Event{}, fmt.Errorf("parse event time: %w", err)
		}
		if sameEvent(existing, event) {
			return existing, nil
		}
		return events.Event{}, events.ErrEventConflict
	}
	if err != sql.ErrNoRows {
		return events.Event{}, fmt.Errorf("check event identity: %w", err)
	}
	maxQuery, maxArgs := jet.SELECT(jet.MAX(table.EventJournal.Sequence)).FROM(table.EventJournal).WHERE(table.EventJournal.RunID.EQ(jet.String(string(event.RunID)))).Sql()
	var maximum sql.NullInt64
	if err := r.db.QueryRowContext(ctx, maxQuery, maxArgs...).Scan(&maximum); err != nil {
		return events.Event{}, fmt.Errorf("allocate event sequence: %w", err)
	}
	event.Sequence = uint64(maximum.Int64 + 1)
	insert := table.EventJournal.INSERT(table.EventJournal.AllColumns).VALUES(event.ID, event.RunID, event.Sequence, event.Kind, event.PayloadVersion, []byte(event.Payload), event.Provenance, event.CausationID, event.ObservedAt.UTC().Format(time.RFC3339Nano))
	query, args = insert.Sql()
	_, err = r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return events.Event{}, fmt.Errorf("append event: %w", err)
	}
	return event, nil
}

func sameEvent(stored, candidate events.Event) bool {
	return stored.ID == candidate.ID && stored.RunID == candidate.RunID && stored.Kind == candidate.Kind && stored.PayloadVersion == candidate.PayloadVersion && bytes.Equal(stored.Payload, candidate.Payload) && stored.Provenance == candidate.Provenance && stored.CausationID == candidate.CausationID && stored.ObservedAt.Equal(candidate.ObservedAt)
}

func (r eventRepository) Replay(ctx context.Context, runID events.RunID, after uint64, limit int) ([]events.Event, error) {
	if limit <= 0 {
		return []events.Event{}, nil
	}
	statement := jet.SELECT(table.EventJournal.AllColumns).FROM(table.EventJournal).WHERE(table.EventJournal.RunID.EQ(jet.String(string(runID))).AND(table.EventJournal.Sequence.GT(jet.Int64(int64(after))))).ORDER_BY(table.EventJournal.Sequence.ASC()).LIMIT(int64(limit))
	query, args := statement.Sql()
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("replay events: %w", err)
	}
	defer rows.Close()
	result := make([]events.Event, 0)
	for rows.Next() {
		var event events.Event
		var observed string
		if err := rows.Scan(&event.ID, &event.RunID, &event.Sequence, &event.Kind, &event.PayloadVersion, &event.Payload, &event.Provenance, &event.CausationID, &observed); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		event.ObservedAt, err = time.Parse(time.RFC3339Nano, observed)
		if err != nil {
			return nil, fmt.Errorf("parse event time: %w", err)
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return result, nil
}
