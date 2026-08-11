package main

import (
	"embed"

	backend "online.nonoka.subtitle/desktop-wails/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	backend.Run(assets)
}
