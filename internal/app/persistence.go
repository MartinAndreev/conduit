package app

import (
	"context"

	"github.com/MartinAndreev/conduit/internal/events"
	"github.com/MartinAndreev/conduit/internal/sessions"
)

// Persistence groups state changes that must commit atomically. The callback
// receives domain ports only; database connections and transactions stay inside
// the storage implementation. The callback ports are valid only until the
// callback returns.
type Persistence interface {
	WithinUnitOfWork(context.Context, func(sessions.Writer, events.Appender) error) error
}
