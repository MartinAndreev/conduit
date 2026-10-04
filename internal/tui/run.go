package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"github.com/MartinAndreev/conduit/internal/presentation/i18n"
)

// Run owns terminal teardown even when input, rendering, or the program fails.
// An explicit Exit now relinquishes observation only when the app actually exits.
func Run(service Initializer, options ...tea.ProgramOption) error {
	return RunLocale(service, i18n.English, options...)
}

func RunLocale(service Initializer, locale i18n.Locale, options ...tea.ProgramOption) error {
	model := New(service, locale)
	_, err := tea.NewProgram(model, options...).Run()
	if model.active == nil {
		return err
	}
	model.active.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), cleanupWait)
	defer cancel()
	select {
	case <-model.active.done:
		model.active.progress.Close()
		if observer := model.active.outcome.PendingCleanup; observer != nil {
			observation, cleanupErr := observer.Await(ctx)
			if !observation.Complete {
				err = errors.Join(err, cleanupErr)
			}
		}
	case <-ctx.Done():
		err = errors.Join(err, ctx.Err())
	}
	return err
}
