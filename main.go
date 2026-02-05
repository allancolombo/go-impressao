package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	logpkg "go-impressao/log"
	"go-impressao/printer"
	"go-impressao/ws"
)

type Config struct {
	WSURL            string            `json:"wsUrl"`
	ReconnectSeconds int               `json:"reconnectSeconds"`
	PrinterMappings  map[string]string `json:"printerMappings"`
	LogFile          string            `json:"logFile"`
	TmpDir           string            `json:"tmpDir"`
}

type Handler struct {
	Logger   *log.Logger
	Manager  *printer.Manager
	Mappings map[string]string
}

func (h *Handler) HandlePrint(ctx context.Context, job ws.PrintJob) ws.PrintResult {
	printerName := job.PrinterName
	if mapped, ok := h.Mappings[job.PrinterName]; ok {
		printerName = mapped
	}
	if printerName == "" {
		return ws.PrintResult{
			JobID:   job.JobID,
			Status:  "error",
			Message: "impressora nao encontrada",
		}
	}

	request := printer.PrintRequest{
		JobID:       job.JobID,
		PrinterName: printerName,
		Paper:       job.Paper,
		Copies:      job.Copies,
		HTML:        job.HTML,
	}

	err := h.Manager.Enqueue(ctx, printerName, request)
	if err != nil {
		h.Logger.Printf("print error job %s: %v", job.JobID, err)
		return ws.PrintResult{
			JobID:   job.JobID,
			Status:  "error",
			Message: err.Error(),
		}
	}

	return ws.PrintResult{
		JobID:     job.JobID,
		Status:    "printed",
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

func main() {
	ctx := context.Background()
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = filepath.Join("config", "config.json")
	}

	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	logger, closer, err := logpkg.NewLogger(config.LogFile)
	if err != nil {
		log.Fatalf("failed to init logger: %v", err)
	}
	if closer != nil {
		defer closer.Close()
	}

	if config.TmpDir == "" {
		config.TmpDir = "tmp"
	}

	manager := printer.NewManager(config.TmpDir, 100)
	client := ws.Client{
		URL:              config.WSURL,
		ReconnectSeconds: config.ReconnectSeconds,
		Logger:           logger,
		Handler: &Handler{
			Logger:   logger,
			Manager:  manager,
			Mappings: config.PrinterMappings,
		},
	}

	logger.Printf("print agent started (ws: %s)", config.WSURL)
	client.Run(ctx)
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, err
	}

	if config.WSURL == "" {
		return Config{}, errors.New("wsUrl obrigatorio")
	}

	if config.PrinterMappings == nil {
		config.PrinterMappings = make(map[string]string)
	}

	if config.LogFile != "" {
		config.LogFile = filepath.Clean(config.LogFile)
	}

	if config.TmpDir != "" {
		config.TmpDir = filepath.Clean(config.TmpDir)
	}

	return config, nil
}
