package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"screenshot/internal/capture"
	"screenshot/internal/hotkey"
	"screenshot/internal/protocol"
	"sync"
)

// Run connects to the server and captures the screen when F8 is pressed.
func Run(address string) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		fmt.Println("连接服务器失败:", err)
		return
	}
	defer conn.Close()

	events, err := hotkey.ListenF8()
	if err != nil {
		fmt.Println("注册全局 F8 热键失败:", err)
		return
	}

	var writeMu sync.Mutex
	send := func(resp protocol.Response) error {
		b, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("编码响应: %w", err)
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		return protocol.SendWithLengthPrefix(conn, b)
	}

	connectionErrors := make(chan error, 1)
	go listenServerCommands(conn, send, connectionErrors)

	fmt.Println("已连接到服务器，按 F8 或 Ctrl+Alt+S 截图并识别")
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
			if err := send(captureResponse("hotkey")); err != nil {
				fmt.Println("发送截图失败:", err)
				return
			}
			fmt.Println("已提交截图，识别结果将发送到手机")
		case err := <-connectionErrors:
			fmt.Println("服务器连接已断开:", err)
			return
		}
	}
}

func listenServerCommands(conn net.Conn, send func(protocol.Response) error, errors chan<- error) {
	for {
		commandBytes, err := protocol.ReadWithLengthPrefix(conn)
		if err != nil {
			if err == io.EOF {
				errors <- fmt.Errorf("服务器已关闭连接")
			} else {
				errors <- err
			}
			return
		}

		resp := protocol.Response{Code: 400, Error: "Unknown command"}
		switch string(commandBytes) {
		case "1":
			resp = captureResponse("remote")
		case "2":
			resp = captureResponse("hotkey")
		}
		if err := send(resp); err != nil {
			errors <- err
			return
		}
	}
}

func captureResponse(trigger string) protocol.Response {
	resp := protocol.Response{Code: 200, Trigger: trigger}
	pngBytes, err := capture.PrimaryPNG()
	if err != nil {
		resp.Code = 500
		resp.Error = err.Error()
		return resp
	}
	resp.Data = pngBytes
	return resp
}
