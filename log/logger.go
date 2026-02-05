package log

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

func NewLogger(logFile string) (*log.Logger, io.Closer, error) {
	if logFile == "" {
		return log.New(os.Stdout, "", log.LstdFlags), io.NopCloser(nil), nil
	}

	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return nil, nil, err
	}

	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}

	writer := io.MultiWriter(os.Stdout, file)
	return log.New(writer, "", log.LstdFlags), file, nil
}
