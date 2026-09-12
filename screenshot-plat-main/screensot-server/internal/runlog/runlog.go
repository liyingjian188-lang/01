package runlog

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Start redirects process output to a log file beside the executable.
func Start(name string) (*os.File, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	logDir := filepath.Join(filepath.Dir(executable), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(logDir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	os.Stdout = file
	os.Stderr = file
	log.SetOutput(file)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	fmt.Fprintf(file, "\n[%s] process started\n", time.Now().Format(time.RFC3339))
	return file, nil
}
