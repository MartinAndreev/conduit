package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/MartinAndreev/conduit/internal/app/startup"
	"time"
)

const cleanupWait = 250 * time.Millisecond

func (o *operation) awaitExit() tea.Cmd {
	return func() tea.Msg {
		timer := time.NewTimer(cleanupWait)
		defer timer.Stop()
		select {
		case <-o.done:
			return exitDeadlineMsg{o.generation, false}
		case <-timer.C:
			return exitDeadlineMsg{o.generation, true}
		}
	}
}

func (o *operation) execute(service Initializer) tea.Cmd {
	return func() tea.Msg {
		o.outcome = service.Initialize(o.ctx, o.progress)
		close(o.done)
		return outcomeMsg{o.generation, o.outcome}
	}
}

func (o *operation) awaitProgress() tea.Cmd {
	return func() tea.Msg {
		select {
		case <-o.ctx.Done():
			return nil
		case <-o.done:
			return nil
		case _, open := <-o.progress.Changed():
			if !open {
				return nil
			}
			snapshot, available := o.progress.Latest()
			if !available {
				return nil
			}
			return progressMsg{o.generation, snapshot}
		}
	}
}

func awaitCleanup(generation uint64, observer startup.CleanupObserver, wait time.Duration) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		defer cancel()
		observation, err := observer.Await(ctx)
		return cleanupMsg{generation, observation, err != nil && !observation.Complete}
	}
}
