package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

type PrinterHandler interface {
	HandlePrint(ctx context.Context, job PrintJob) PrintResult
}

type Client struct {
	URL              string
	ReconnectSeconds int
	Logger           *log.Logger
	Handler          PrinterHandler
}

type PrintJob struct {
	JobID       string          `json:"jobId"`
	PrinterName string          `json:"printerName"`
	Paper       string          `json:"paper"`
	Copies      int             `json:"copies"`
	HTML        string          `json:"html"`
	Meta        json.RawMessage `json:"meta"`
}

type PrintResult struct {
	JobID     string `json:"jobId"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (c *Client) Run(ctx context.Context) {
	if c.ReconnectSeconds <= 0 {
		c.ReconnectSeconds = 5
	}

	for {
		if err := c.connectAndListen(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			c.Logger.Printf("websocket error: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(c.ReconnectSeconds) * time.Second):
		}
	}
}

func (c *Client) connectAndListen(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.Dial(c.URL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	c.Logger.Printf("connected to %s", c.URL)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		var job PrintJob
		if err := json.Unmarshal(message, &job); err != nil {
			c.Logger.Printf("invalid json: %v", err)
			writeResult(conn, PrintResult{Status: "error", Message: "json invalido"})
			continue
		}

		if job.JobID == "" {
			writeResult(conn, PrintResult{Status: "error", Message: "jobId obrigatorio"})
			continue
		}

		result := c.Handler.HandlePrint(ctx, job)
		if err := writeResult(conn, result); err != nil {
			c.Logger.Printf("failed to write result: %v", err)
		}
	}
}

func writeResult(conn *websocket.Conn, result PrintResult) error {
	if result.Status == "" {
		return errors.New("missing status")
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, payload)
}
