package main

import (
	"screenshot/internal/app"
	"screenshot/internal/runlog"
)

// 修改远程部署时的服务端地址即可
var address = "127.0.0.1:12345"

func main() {
	logFile, err := runlog.Start("client.log")
	if err == nil {
		defer logFile.Close()
	}
	app.Run(address)
}
