package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"screensot-server/internal/protocol"
)

func (a *App) startTCPServer() {
	listener, err := net.Listen("tcp", ":12345")
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		return
	}
	defer listener.Close()
	fmt.Println("TCP Server listening on :12345")

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting:", err.Error())
			continue
		}
		fmt.Println("TCP client connected:", conn.RemoteAddr().String())

		a.clientsMutex.Lock()
		a.clients[conn] = true
		a.clientsMutex.Unlock()

		go a.handleTCPClient(conn)
	}
}

func (a *App) handleTCPClient(conn net.Conn) {
	defer func() {
		conn.Close()
		a.clientsMutex.Lock()
		delete(a.clients, conn)
		a.clientsMutex.Unlock()
	}()

	for {
		dataBytes, err := protocol.ReadWithLengthPrefix(conn)
		if err != nil {
			if err == io.EOF {
				fmt.Println("TCP client disconnected:", conn.RemoteAddr().String())
			} else {
				fmt.Println("Error reading data from client:", err)
			}
			return
		}

		var responseObj protocol.Response
		if err := json.Unmarshal(dataBytes, &responseObj); err != nil {
			fmt.Println("Error unmarshalling JSON from client:", err)
			continue
		}
		if responseObj.Trigger == "hotkey" && responseObj.Code != 200 {
			a.broadcastMobile(MobileEvent{Status: "error", Error: responseObj.Error})
			continue
		}
		if len(responseObj.Data) == 0 {
			if responseObj.Trigger == "hotkey" {
				a.broadcastMobile(MobileEvent{Status: "error", Error: "客户端未返回截图数据"})
			}
			continue
		}

		// 统一在 TCP 层转成 base64，HTTP 层只负责聚合
		base64Str := base64.StdEncoding.EncodeToString(responseObj.Data)
		fmt.Printf("Received image from %s, Base64 size: %d\n", conn.RemoteAddr().String(), len(base64Str))
		if responseObj.Trigger == "hotkey" {
			a.enqueueAnalysis(base64Str)
			continue
		}
		a.responseCollector <- base64Str
	}
}

func (a *App) sendCaptureCommandToClients() {
	a.sendCommandToClients("1")
}

func (a *App) sendAnalyzeCommandToClients() int {
	return a.sendCommandToClients("2")
}

func (a *App) sendCommandToClients(command string) int {
	a.clientsMutex.Lock()
	connections := make([]net.Conn, 0, len(a.clients))
	for c := range a.clients {
		connections = append(connections, c)
	}
	a.clientsMutex.Unlock()

	sent := 0
	for _, conn := range connections {
		a.clientWriteMutex.Lock()
		err := protocol.SendWithLengthPrefix(conn, []byte(command))
		a.clientWriteMutex.Unlock()
		if err != nil {
			fmt.Printf("Failed to send command to client %s: %v\n", conn.RemoteAddr().String(), err)
			continue
		}
		fmt.Printf("Sent command %s to client %s\n", command, conn.RemoteAddr().String())
		sent++
	}
	return sent
}
