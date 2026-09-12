//go:build windows

package hotkey

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	hotkeyIDF8       = 1
	hotkeyIDFallback = 2
	wmHotkey         = 0x0312
	modAlt           = 0x0001
	modControl       = 0x0002
	vkF8             = 0x77
	vkS              = 0x53
)

var (
	user32             = syscall.NewLazyDLL("user32.dll")
	procRegisterHotKey = user32.NewProc("RegisterHotKey")
	procGetMessageW    = user32.NewProc("GetMessageW")
)

type point struct {
	x int32
	y int32
}

type message struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	point   point
	private uint32
}

// ListenF8 registers F8 as a system-wide hotkey.
func ListenF8() (<-chan struct{}, error) {
	events := make(chan struct{}, 1)
	ready := make(chan error, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		f8Registered, _, f8Err := procRegisterHotKey.Call(0, hotkeyIDF8, 0, vkF8)
		fallbackRegistered, _, fallbackErr := procRegisterHotKey.Call(0, hotkeyIDFallback, modControl|modAlt, vkS)
		if f8Registered == 0 && fallbackRegistered == 0 {
			ready <- fmt.Errorf("RegisterHotKey: F8=%v, Ctrl+Alt+S=%v", f8Err, fallbackErr)
			close(events)
			return
		}
		ready <- nil

		for {
			var msg message
			result, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(result) <= 0 {
				close(events)
				return
			}
			if msg.message == wmHotkey && (msg.wParam == hotkeyIDF8 || msg.wParam == hotkeyIDFallback) {
				select {
				case events <- struct{}{}:
				default:
				}
			}
		}
	}()

	if err := <-ready; err != nil {
		return nil, err
	}
	return events, nil
}
