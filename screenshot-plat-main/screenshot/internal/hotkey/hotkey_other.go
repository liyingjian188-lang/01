//go:build !windows

package hotkey

import "fmt"

func ListenF8() (<-chan struct{}, error) {
	return nil, fmt.Errorf("全局 F8 热键当前仅支持 Windows")
}
