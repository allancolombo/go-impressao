package printer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

var ErrUnsupported = errors.New("printing only supported on windows")

type PrintRequest struct {
	JobID       string
	PrinterName string
	Paper       string
	Copies      int
	HTML        string
}

func PrintHTML(ctx context.Context, tmpDir string, request PrintRequest) error {
	if runtime.GOOS != "windows" {
		return ErrUnsupported
	}
	if request.Copies < 1 {
		request.Copies = 1
	}

	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}

	adjustedHTML := WrapHTMLWithPaper(request.HTML, request.Paper)
	filename := fmt.Sprintf("print-%s-%d.html", request.JobID, time.Now().UnixNano())
	path := filepath.Join(tmpDir, filename)
	if err := os.WriteFile(path, []byte(adjustedHTML), 0o644); err != nil {
		return err
	}

	for i := 0; i < request.Copies; i++ {
		if err := runPrintCommand(ctx, request.PrinterName, path); err != nil {
			return err
		}
	}

	return nil
}

func runPrintCommand(ctx context.Context, printerName string, filePath string) error {
	args := []string{"printui.dll,PrintUIEntry", "/y", "/n", printerName}
	if err := exec.CommandContext(ctx, "rundll32.exe", args...).Run(); err != nil {
		return fmt.Errorf("set default printer: %w", err)
	}

	printArgs := []string{"mshtml,PrintHTML", filePath}
	if err := exec.CommandContext(ctx, "rundll32.exe", printArgs...).Run(); err != nil {
		return fmt.Errorf("print command: %w", err)
	}
	return nil
}
