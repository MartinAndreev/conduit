//go:build desktop

// conduit-gui is the native desktop composition root. CLI/TUI never imports Wails.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/presentation/gui/bridge"
	"github.com/MartinAndreev/conduit/internal/presentation/gui/views"
	"github.com/MartinAndreev/conduit/internal/presentation/i18n"
	"github.com/MartinAndreev/conduit/internal/startupinfra"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"os"
	"sync/atomic"
)

// App exposes only transport-safe presentation operations to JavaScript.
type App struct{ bridge *bridge.Bridge }

func (a *App) Start() bridge.State    { return a.bridge.Start() }
func (a *App) Snapshot() bridge.State { return a.bridge.Snapshot() }
func (a *App) Retry() error           { return a.bridge.Retry() }
func (a *App) Cancel()                { a.bridge.Cancel() }
func (a *App) Wait() bridge.State     { return a.bridge.Wait() }
func (a *App) Exit()                  { a.bridge.Exit() }
func (a *App) ExitNow()               { a.bridge.ExitNow() }
func main() {
	fixture := flag.String("fixture", "", "synthetic startup for visual verification (ready, warnings, blocked, slow)")
	language := flag.String("lang", "en", "presentation language: en or bg")
	flag.Parse()
	var initializer bridge.Initializer = startup.Service{Repository: startupinfra.Files{}, Detector: startupinfra.Detector{}}
	if *fixture != "" {
		var err error
		initializer, err = bridge.Fixture(*fixture)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	var appContext context.Context
	var forceClose atomic.Bool
	b := bridge.New(initializer, func(state bridge.State) {
		if appContext != nil {
			runtime.EventsEmit(appContext, "startup:state", state)
		}
	}, func() { forceClose.Store(true); runtime.Quit(appContext) })
	app := &App{bridge: b}
	err := wails.Run(&options.App{
		Title: "Conduit", Width: 1200, Height: 760, MinWidth: 640, MinHeight: 480,
		BackgroundColour: options.NewRGB(20, 24, 31),
		AssetServer:      &assetserver.Options{Handler: views.Handler(i18n.Normalize(*language))},
		Bind:             []interface{}{app},
		OnStartup:        func(ctx context.Context) { appContext = ctx; b.SetContext(ctx) },
		OnBeforeClose: func(context.Context) bool {
			if forceClose.Load() {
				return false
			}
			return b.BeforeClose()
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
