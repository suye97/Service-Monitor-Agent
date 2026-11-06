package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app, err := NewApp("")
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	err = wails.Run(&options.App{
		Title: "Service Monitor Agent", Width: 1440, Height: 900, MinWidth: 1080, MinHeight: 680,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 12, G: 18, B: 31, A: 1},
		OnStartup:        app.startup, OnShutdown: app.shutdown,
		Bind: []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
