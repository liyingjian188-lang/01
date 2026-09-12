package main

import (
	"screensot-server/internal/app"
	"screensot-server/internal/runlog"
)

func main() {
	logFile, err := runlog.Start("server.log")
	if err == nil {
		defer logFile.Close()
	}
	app.New().Run()
}
