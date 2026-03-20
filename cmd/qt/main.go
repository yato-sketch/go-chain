package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/version"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	if err := wails.Run(&options.App{
		Title:     coinparams.Name + " Wallet",
		Width:     1200,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "%s Wallet v%s: %v\n", coinparams.Name, version.String(), err)
		os.Exit(1)
	}
}
