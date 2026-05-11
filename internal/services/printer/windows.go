//go:build windows

package printer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
	"unicode"

	"github.com/goopedir/go-impressao/internal/config"
	"github.com/goopedir/go-impressao/internal/models"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

type Printer interface {
	PrintText(ctx context.Context, printerName string, text string) error
}

// WindowsPrinter implementa impressão no Windows via PowerShell (Get-Printer / Out-Printer).
// Ele valida:
// - se a impressora existe no Windows
// - se está offline
type WindowsPrinter struct {
	logger *log.Logger
	cfg    *config.Manager
}

func NewWindowsPrinter(logger *log.Logger, cfg *config.Manager) *WindowsPrinter {
	return &WindowsPrinter{logger: logger, cfg: cfg}
}

func (p *WindowsPrinter) PrintText(ctx context.Context, printerName string, text string) error {
	printerName = strings.TrimSpace(printerName)

	if printerName == "" {
		return errors.New("nome da impressora não informado")
	}

	_, workOffline, err := p.getPrinterInfo(ctx, printerName)
	if err != nil {
		return err
	}
	if workOffline {
		return fmt.Errorf("a impressora %q está indisponível/offline", printerName)
	}

	var margins config.PrinterMargins
	hasMargins := false
	if p.cfg != nil {
		if pc, ok := p.cfg.GetPrinterConfig(printerName); ok {
			margins = pc.Margens
			hasMargins = true
		}
	}

	payload := buildEscPOS80mm(text, hasMargins, margins, config.LogoConfig{}, nil)

	if err := ctx.Err(); err != nil {
		return err
	}

	p.logger.Printf("impressão: enviando para impressora_windows=%q modo=raw_escpos bytes=%d", printerName, len(payload))
	if err := printRAW(printerName, payload); err == nil {
		return nil
	}

	tmp, err := os.CreateTemp("", "comanda-*.txt")
	if err != nil {
		return fmt.Errorf("erro ao criar arquivo temporário: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := os.WriteFile(tmpPath, []byte(text), 0o600); err != nil {
		return fmt.Errorf("erro ao gravar arquivo temporário: %w", err)
	}

	script := fmt.Sprintf(
		"Get-Content -LiteralPath '%s' -Raw -Encoding UTF8 | Out-Printer -Name '%s'",
		escapePSSingleQuoted(tmpPath),
		escapePSSingleQuoted(printerName),
	)

	p.logger.Printf("impressão: fallback Out-Printer impressora_windows=%q arquivo=%q", printerName, tmpPath)
	if _, err := runPowerShell(ctx, script); err != nil {
		return fmt.Errorf("falha ao enviar para impressão: %w", err)
	}
	return nil
}

func (p *WindowsPrinter) PrintTest(ctx context.Context, printerName string) error {
	printerName = strings.TrimSpace(printerName)
	if printerName == "" {
		return errors.New("nome da impressora não informado")
	}

	_, workOffline, err := p.getPrinterInfo(ctx, printerName)
	if err != nil {
		return err
	}
	if workOffline {
		return fmt.Errorf("a impressora %q está indisponível/offline", printerName)
	}

	var margins config.PrinterMargins
	hasMargins := false
	if p.cfg != nil {
		if pc, ok := p.cfg.GetPrinterConfig(printerName); ok {
			margins = pc.Margens
			hasMargins = true
		}
	}

	printedAt := time.Now()
	payload := buildEscPOSTest80mm(printerName, printedAt, hasMargins, margins)

	if err := ctx.Err(); err != nil {
		return err
	}

	p.logger.Printf("impressão: teste impressora_windows=%q modo=raw_escpos bytes=%d", printerName, len(payload))
	if err := printRAW(printerName, payload); err == nil {
		return nil
	}

	tmp, err := os.CreateTemp("", "teste-impressao-*.txt")
	if err != nil {
		return fmt.Errorf("erro ao criar arquivo temporário: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	fallbackText := buildFallbackTestText(printerName, printedAt)
	if err := os.WriteFile(tmpPath, []byte(fallbackText), 0o600); err != nil {
		return fmt.Errorf("erro ao gravar arquivo temporário: %w", err)
	}

	script := fmt.Sprintf(
		"Get-Content -LiteralPath '%s' -Raw -Encoding UTF8 | Out-Printer -Name '%s'",
		escapePSSingleQuoted(tmpPath),
		escapePSSingleQuoted(printerName),
	)

	p.logger.Printf("impressão: teste fallback Out-Printer impressora_windows=%q arquivo=%q", printerName, tmpPath)
	if _, err := runPowerShell(ctx, script); err != nil {
		return fmt.Errorf("falha ao enviar para impressão: %w", err)
	}
	return nil
}

func (p *WindowsPrinter) PrintConferencia(ctx context.Context, req models.ConferenciaRequest) error {
	printerName := strings.TrimSpace(req.Driver)
	if printerName == "" {
		return errors.New("nome da impressora não informado")
	}
	if p.cfg == nil {
		return errors.New("configuração não disponível para carregar parâmetros")
	}
	empresaCfg, ok := p.cfg.GetEmpresaParametros()
	if !ok {
		_, _ = p.cfg.RefreshEmpresaParametros(ctx)
		empresaCfg, ok = p.cfg.GetEmpresaParametros()
		if !ok {
			return errors.New("parâmetros da empresa não carregados (http://localhost:2121/v2/parametros)")
		}
	}

	_, workOffline, err := p.getPrinterInfo(ctx, printerName)
	if err != nil {
		return err
	}
	if workOffline {
		return fmt.Errorf("a impressora %q está indisponível/offline", printerName)
	}

	modelo := strings.ToLower(strings.TrimSpace(req.Modelo))
	maxWidthDots := 576
	maxCols := 48
	if modelo == "56mm" || modelo == "58mm" {
		maxWidthDots = 384
		maxCols = 32
	}

	var margins config.PrinterMargins
	hasMargins := false
	if p.cfg != nil {
		if pc, ok := p.cfg.GetPrinterConfig(printerName); ok {
			margins = pc.Margens
			hasMargins = true
		}
	}

	printedAt := time.Now()
	fallbackCols := maxCols - 6
	if fallbackCols < 20 {
		fallbackCols = 20
	}
	textFallback := buildFallbackConferenciaText(req, empresaCfg, printedAt, fallbackCols)
	payload := buildEscPOSConferencia(req, empresaCfg, printedAt, hasMargins, margins, maxWidthDots, maxCols)

	if err := ctx.Err(); err != nil {
		return err
	}

	p.logger.Printf("impressão: conferência impressora_windows=%q modelo=%q modo=raw_escpos bytes=%d", printerName, req.Modelo, len(payload))
	if err := printRAW(printerName, payload); err == nil {
		return nil
	}

	tmp, err := os.CreateTemp("", "conferencia-*.txt")
	if err != nil {
		return fmt.Errorf("erro ao criar arquivo temporário: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := os.WriteFile(tmpPath, []byte(textFallback), 0o600); err != nil {
		return fmt.Errorf("erro ao gravar arquivo temporário: %w", err)
	}

	script := fmt.Sprintf(
		"Get-Content -LiteralPath '%s' -Raw -Encoding UTF8 | Out-Printer -Name '%s'",
		escapePSSingleQuoted(tmpPath),
		escapePSSingleQuoted(printerName),
	)

	p.logger.Printf("impressão: conferência fallback Out-Printer impressora_windows=%q arquivo=%q", printerName, tmpPath)
	if _, err := runPowerShell(ctx, script); err != nil {
		return fmt.Errorf("falha ao enviar para impressão: %w", err)
	}
	return nil
}

func (p *WindowsPrinter) PrintSangria(ctx context.Context, req models.SangriaRequest) error {
	printerName := strings.TrimSpace(req.Driver)
	if printerName == "" {
		return errors.New("nome da impressora não informado")
	}
	if p.cfg == nil {
		return errors.New("configuração não disponível para carregar parâmetros")
	}
	empresaCfg, ok := p.cfg.GetEmpresaParametros()
	if !ok {
		_, _ = p.cfg.RefreshEmpresaParametros(ctx)
		empresaCfg, ok = p.cfg.GetEmpresaParametros()
		if !ok {
			return errors.New("parâmetros da empresa não carregados (http://localhost:2121/v2/parametros)")
		}
	}

	_, workOffline, err := p.getPrinterInfo(ctx, printerName)
	if err != nil {
		return err
	}
	if workOffline {
		return fmt.Errorf("a impressora %q está indisponível/offline", printerName)
	}

	modelo := strings.ToLower(strings.TrimSpace(req.Modelo))
	maxWidthDots := 576
	maxCols := 48
	if modelo == "56mm" || modelo == "58mm" {
		maxWidthDots = 384
		maxCols = 32
	}

	var margins config.PrinterMargins
	hasMargins := false
	if p.cfg != nil {
		if pc, ok := p.cfg.GetPrinterConfig(printerName); ok {
			margins = pc.Margens
			hasMargins = true
		}
	}

	printedAt := time.Now()
	fallbackCols := maxCols - 6
	if fallbackCols < 20 {
		fallbackCols = 20
	}
	textFallback := buildFallbackSangriaText(req, empresaCfg, printedAt, fallbackCols)
	payload := buildEscPOSSangria(req, empresaCfg, printedAt, hasMargins, margins, maxWidthDots, maxCols)

	if err := ctx.Err(); err != nil {
		return err
	}

	p.logger.Printf("impressão: sangria impressora_windows=%q modelo=%q modo=raw_escpos bytes=%d", printerName, req.Modelo, len(payload))
	if err := printRAW(printerName, payload); err == nil {
		return nil
	}

	tmp, err := os.CreateTemp("", "sangria-*.txt")
	if err != nil {
		return fmt.Errorf("erro ao criar arquivo temporário: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := os.WriteFile(tmpPath, []byte(textFallback), 0o600); err != nil {
		return fmt.Errorf("erro ao gravar arquivo temporário: %w", err)
	}

	script := fmt.Sprintf(
		"Get-Content -LiteralPath '%s' -Raw -Encoding UTF8 | Out-Printer -Name '%s'",
		escapePSSingleQuoted(tmpPath),
		escapePSSingleQuoted(printerName),
	)

	p.logger.Printf("impressão: sangria fallback Out-Printer impressora_windows=%q arquivo=%q", printerName, tmpPath)
	if _, err := runPowerShell(ctx, script); err != nil {
		return fmt.Errorf("falha ao enviar para impressão: %w", err)
	}
	return nil
}

func (p *WindowsPrinter) PrintCaixaFechamento(ctx context.Context, req models.CaixaFechamentoRequest) error {
	printerName := strings.TrimSpace(req.Driver)
	if printerName == "" {
		return errors.New("nome da impressora não informado")
	}
	if p.cfg == nil {
		return errors.New("configuração não disponível para carregar parâmetros")
	}
	empresaCfg, ok := p.cfg.GetEmpresaParametros()
	if !ok {
		_, _ = p.cfg.RefreshEmpresaParametros(ctx)
		empresaCfg, ok = p.cfg.GetEmpresaParametros()
		if !ok {
			return errors.New("parâmetros da empresa não carregados (http://localhost:2121/v2/parametros)")
		}
	}

	_, workOffline, err := p.getPrinterInfo(ctx, printerName)
	if err != nil {
		return err
	}
	if workOffline {
		return fmt.Errorf("a impressora %q está indisponível/offline", printerName)
	}

	modelo := strings.ToLower(strings.TrimSpace(req.Modelo))
	maxWidthDots := 576
	maxCols := 48
	if modelo == "56mm" || modelo == "58mm" {
		maxWidthDots = 384
		maxCols = 32
	}

	var margins config.PrinterMargins
	hasMargins := false
	if p.cfg != nil {
		if pc, ok := p.cfg.GetPrinterConfig(printerName); ok {
			margins = pc.Margens
			hasMargins = true
		}
	}

	printedAt := time.Now()
	fallbackCols := maxCols - 6
	if fallbackCols < 20 {
		fallbackCols = 20
	}
	textFallback := buildFallbackCaixaFechamentoText(req, empresaCfg, printedAt, fallbackCols)
	payload := buildEscPOSCaixaFechamento(req, empresaCfg, printedAt, hasMargins, margins, maxWidthDots, maxCols)

	if err := ctx.Err(); err != nil {
		return err
	}

	p.logger.Printf("impressão: caixa_fechamento impressora_windows=%q modelo=%q modo=raw_escpos bytes=%d", printerName, req.Modelo, len(payload))
	if err := printRAW(printerName, payload); err == nil {
		return nil
	}

	tmp, err := os.CreateTemp("", "caixa-*.txt")
	if err != nil {
		return fmt.Errorf("erro ao criar arquivo temporário: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := os.WriteFile(tmpPath, []byte(textFallback), 0o600); err != nil {
		return fmt.Errorf("erro ao gravar arquivo temporário: %w", err)
	}

	script := fmt.Sprintf(
		"Get-Content -LiteralPath '%s' -Raw -Encoding UTF8 | Out-Printer -Name '%s'",
		escapePSSingleQuoted(tmpPath),
		escapePSSingleQuoted(printerName),
	)

	p.logger.Printf("impressão: caixa_fechamento fallback Out-Printer impressora_windows=%q arquivo=%q", printerName, tmpPath)
	if _, err := runPowerShell(ctx, script); err != nil {
		return fmt.Errorf("falha ao enviar para impressão: %w", err)
	}
	return nil
}

func (p *WindowsPrinter) getPrinterInfo(ctx context.Context, printerName string) (driverName string, workOffline bool, err error) {
	script := fmt.Sprintf(
		"$p = Get-Printer -Name '%s' -ErrorAction SilentlyContinue; "+
			"if ($null -eq $p) { Write-Output '__NOT_FOUND__'; exit 0 }; "+
			"Write-Output ($p.DriverName); "+
			"Write-Output ($p.WorkOffline)",
		escapePSSingleQuoted(printerName),
	)

	out, err := runPowerShell(ctx, script)
	if err != nil {
		return "", false, fmt.Errorf("erro ao consultar impressora no Windows: %w", err)
	}

	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(out), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return "", false, errors.New("não foi possível consultar a impressora")
	}
	if strings.TrimSpace(lines[0]) == "__NOT_FOUND__" {
		return "", false, fmt.Errorf("impressora %q não encontrada no Windows", printerName)
	}
	driverName = strings.TrimSpace(lines[0])

	if len(lines) >= 2 {
		workOffline = strings.EqualFold(strings.TrimSpace(lines[1]), "True")
	}
	return driverName, workOffline, nil
}

func runPowerShell(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

func escapePSSingleQuoted(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// DefaultPrintContext define um timeout padrão para chamadas ao PowerShell/impressão.
func DefaultPrintContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func buildEscPOS80mm(text string, hasMargins bool, margins config.PrinterMargins, logo config.LogoConfig, logoBytes []byte) []byte {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	headerLine, rest, ok := strings.Cut(text, "\n")
	header := strings.TrimSpace(headerLine)
	if !ok {
		rest = ""
	}

	const dotsPerMM = 8
	const maxWidthDots = 576
	const dotsPerCol = 12

	buf := make([]byte, 0, len(text)+96)
	buf = append(buf, 0x1B, 0x40)
	buf = append(buf, 0x1D, 0x21, 0x01)
	buf = append(buf, 0x1B, 0x74, 0x02)
	buf = append(buf, 0x1B, 0x61, 0x00)

	leftDots := 0
	widthDots := maxWidthDots
	topLines := 0
	bottomLines := 0
	if hasMargins {
		leftDots = margins.EsquerdaMM * dotsPerMM
		rightDots := margins.DireitaMM * dotsPerMM
		widthDots = maxWidthDots - leftDots - rightDots
		if widthDots < 128 {
			widthDots = 128
		}
		topLines = mmToLines(margins.TopoMM)
		bottomLines = mmToLines(margins.BaseMM)
		buf = append(buf, 0x1D, 0x4C, byte(leftDots%256), byte(leftDots/256))
		buf = append(buf, 0x1D, 0x57, byte(widthDots%256), byte(widthDots/256))
		if topLines > 0 {
			buf = append(buf, 0x1B, 0x64, byte(minInt(topLines, 255)))
		}
	}

	if logo.Habilitado && len(logoBytes) > 0 {
		if rasterCmd, ok := buildLogoRaster(logoBytes, logo, widthDots); ok {
			buf = append(buf, rasterCmd...)
			buf = append(buf, '\n')
		}
		buf = append(buf, 0x1B, 0x61, 0x00)
	}

	cols := widthDots / dotsPerCol
	if cols < 20 {
		cols = 20
	}
	if cols > 48 {
		cols = 48
	}

	if header != "" {
		header = padCenter(header, cols)
		buf = append(buf, 0x1D, 0x42, 0x01)
		buf = append(buf, 0x1B, 0x45, 0x01)
		buf = append(buf, encodeCP850(header)...)
		buf = append(buf, 0x1B, 0x45, 0x00)
		buf = append(buf, 0x1D, 0x42, 0x00)
		buf = append(buf, '\n')
	}

	buf = append(buf, encodeCP850(rest)...)

	if bottomLines > 0 {
		buf = append(buf, 0x1B, 0x64, byte(minInt(bottomLines, 255)))
	}
	buf = append(buf, 0x1D, 0x56, 0x42, 0x00)
	return buf
}

func padCenter(s string, cols int) string {
	if cols <= 0 {
		return s
	}
	if len(s) > cols {
		return s[:cols]
	}
	left := (cols - len(s)) / 2
	right := cols - len(s) - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

func encodeCP850(s string) []byte {
	r := transform.NewReader(strings.NewReader(s), charmap.CodePage850.NewEncoder())
	b, err := io.ReadAll(r)
	if err != nil {
		return []byte(s)
	}
	return b
}

func mmToLines(mm int) int {
	if mm <= 0 {
		return 0
	}
	return int(math.Ceil(float64(mm) / 4.0))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sanitizeTextASCII(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r == '\n' {
			b.WriteByte('\n')
			continue
		}
		if r == '\t' {
			b.WriteByte(' ')
			continue
		}
		if r < 0x20 || r == 0x7F {
			continue
		}
		if r <= 0x7E {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func buildFallbackTestText(printerName string, printedAt time.Time) string {
	printerName = sanitizeTextASCII(printerName)
	title := sanitizeTextASCII("Teste de Impressao Goopedir")
	url := "www.goopedir.com.br"
	ts := printedAt.Format("02/01/2006 15:04:05")

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(printerName)
	b.WriteString("\n\n")
	b.WriteString(url)
	b.WriteString("\n\n")
	b.WriteString(ts)
	b.WriteString("\n")
	return b.String()
}

func buildEscPOSTest80mm(printerName string, printedAt time.Time, hasMargins bool, margins config.PrinterMargins) []byte {
	const dotsPerMM = 8
	const maxWidthDots = 576

	title := sanitizeTextASCII("Teste de Impressao Goopedir")
	printerName = sanitizeTextASCII(printerName)
	url := "www.goopedir.com.br"
	ts := printedAt.Format("02/01/2006 15:04:05")

	buf := make([]byte, 0, 256)
	buf = append(buf, 0x1B, 0x40)
	buf = append(buf, 0x1D, 0x21, 0x01)
	buf = append(buf, 0x1B, 0x74, 0x02)

	widthDots := maxWidthDots
	leftDots := 0
	topLines := 0
	bottomLines := 0
	if hasMargins {
		leftDots = margins.EsquerdaMM * dotsPerMM
		rightDots := margins.DireitaMM * dotsPerMM
		widthDots = maxWidthDots - leftDots - rightDots
		if widthDots < 128 {
			widthDots = 128
		}
		topLines = mmToLines(margins.TopoMM)
		bottomLines = mmToLines(margins.BaseMM)
		buf = append(buf, 0x1D, 0x4C, byte(leftDots%256), byte(leftDots/256))
		buf = append(buf, 0x1D, 0x57, byte(widthDots%256), byte(widthDots/256))
		if topLines > 0 {
			buf = append(buf, 0x1B, 0x64, byte(minInt(topLines, 255)))
		}
	}

	buf = append(buf, 0x1B, 0x61, 0x01)
	buf = append(buf, encodeCP850(title)...)
	buf = append(buf, '\n', '\n')

	buf = append(buf, 0x1B, 0x45, 0x01)
	buf = append(buf, encodeCP850(printerName)...)
	buf = append(buf, 0x1B, 0x45, 0x00)
	buf = append(buf, '\n', '\n')

	buf = append(buf, escposQRCode(url, 7)...)
	buf = append(buf, '\n')

	buf = append(buf, encodeCP850(url)...)
	buf = append(buf, '\n', '\n')

	buf = append(buf, encodeCP850(ts)...)
	buf = append(buf, '\n')

	buf = append(buf, 0x1B, 0x61, 0x00)
	if bottomLines > 0 {
		buf = append(buf, 0x1B, 0x64, byte(minInt(bottomLines, 255)))
	}
	buf = append(buf, 0x1D, 0x56, 0x42, 0x00)
	return buf
}

func escposQRCode(data string, size int) []byte {
	if size < 1 {
		size = 1
	}
	if size > 16 {
		size = 16
	}

	data = sanitizeTextASCII(data)
	d := []byte(data)

	var b []byte
	b = append(b, 0x1D, 0x28, 0x6B, 0x04, 0x00, 0x31, 0x41, 0x32, 0x00)
	b = append(b, 0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x43, byte(size))
	b = append(b, 0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x45, 0x31)

	storeLen := len(d) + 3
	pL := byte(storeLen % 256)
	pH := byte(storeLen / 256)
	b = append(b, 0x1D, 0x28, 0x6B, pL, pH, 0x31, 0x50, 0x30)
	b = append(b, d...)

	b = append(b, 0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x51, 0x30)
	return b
}

func buildFallbackConferenciaText(req models.ConferenciaRequest, empresaCfg config.EmpresaParametros, printedAt time.Time, cols int) string {
	if isComandaConferencia(req) {
		return buildFallbackConferenciaComanda(req, empresaCfg, printedAt, cols)
	}

	empresa := sanitizeOneLineASCII(empresaCfg.Nome)
	if empresa == "" {
		empresa = sanitizeOneLineASCII(empresaCfg.Razao)
	}
	cnpj := "CNPJ: " + sanitizeOneLineASCII(empresaCfg.CNPJ)
	fantasia := sanitizeOneLineASCII(empresaCfg.Razao)
	endereco := sanitizeOneLineASCII(fmt.Sprintf("%s, %s, %s/%s - CEP %s", empresaCfg.Rua, empresaCfg.Bairro, empresaCfg.Cidade, empresaCfg.Estado, empresaCfg.CEP))
	ie := "IE: " + sanitizeOneLineASCII(string(empresaCfg.IE))
	mesa := strings.ToUpper(sanitizeOneLineASCII(req.Mesa))

	var b strings.Builder
	for _, l := range wrapTextASCII(empresa, cols) {
		b.WriteString(l)
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(cnpj, cols) {
		b.WriteString(l)
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(fantasia, cols) {
		b.WriteString(l)
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(endereco, cols) {
		b.WriteString(l)
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(ie, cols) {
		b.WriteString(l)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(mesa)
	b.WriteString("\n\n")

	totalProdutos := calcTotalProdutos(req)
	taxaValor := calcTaxaServicoValor(req, totalProdutos)
	taxaEntrega := calcTaxaEntregaValor(req)
	valorDesconto := calcValorDesconto(req)
	totalGeral := calcTotalGeral(req, totalProdutos, taxaValor, taxaEntrega, valorDesconto)

	all := flattenConferenciaProdutos(req)
	sort.SliceStable(all, func(i, j int) bool {
		return sortKeyASCII(all[i].Categoria) < sortKeyASCII(all[j].Categoria)
	})
	all = groupConferenciaProdutos(all)

	lastProdCat := ""
	for _, p := range all {
		prodCat := strings.ToUpper(strings.TrimSpace(sanitizeTextASCII(p.Categoria)))
		if prodCat != "" && prodCat != lastProdCat {
			b.WriteString(dottedCategoryLine(prodCat, cols))
			b.WriteString("\n")
			lastProdCat = prodCat
		}
		writeFallbackProduto(&b, p, cols)
	}

	b.WriteString("\n")
	if strings.TrimSpace(req.Desconto) != "" {
		b.WriteString(centerASCII(strings.ToUpper(sanitizeOneLineASCII(req.Desconto)), cols))
		b.WriteString("\n")
	}
	b.WriteString(leftRightASCII("TOTAL PRODUTOS", fmtMoney(totalProdutos), cols))
	b.WriteString("\n")
	if valorDesconto > 0 {
		b.WriteString(leftRightASCII("DESCONTO", "(-) "+fmtMoney(valorDesconto), cols))
		b.WriteString("\n")
	}
	if taxaEntrega > 0 {
		b.WriteString(leftRightASCII("TAXA ENTREGA", "(+) "+fmtMoney(taxaEntrega), cols))
		b.WriteString("\n")
	}
	if req.TaxaServicoPercent > 0 || taxaValor > 0 {
		label := "TAXA SERVICO"
		if req.TaxaServicoPercent > 0 {
			label = fmt.Sprintf("TAXA SERVICO (%.2f%%)", req.TaxaServicoPercent)
		}
		b.WriteString(leftRightASCII(label, fmtMoney(taxaValor), cols))
		b.WriteString("\n")
		b.WriteString(leftRightASCII("TOTAL GERAL", fmtMoney(totalGeral), cols))
		b.WriteString("\n")
	} else {
		b.WriteString(leftRightASCII("TOTAL", fmtMoney(totalGeral), cols))
		b.WriteString("\n")
	}

	if strings.TrimSpace(req.NFCENumero) != "" || strings.TrimSpace(req.NFCEProtocolo) != "" || strings.TrimSpace(req.NFCEChave) != "" {
		b.WriteString("\n")
		if strings.TrimSpace(req.NFCENumero) != "" {
			b.WriteString("NFCe Numero: " + sanitizeOneLineASCII(req.NFCENumero))
			b.WriteString("\n")
		}
		if strings.TrimSpace(req.NFCEProtocolo) != "" {
			b.WriteString("NFCe Protocolo: " + sanitizeOneLineASCII(req.NFCEProtocolo))
			b.WriteString("\n")
		}
		if strings.TrimSpace(req.NFCEChave) != "" {
			for _, l := range wrapTextASCII("NFCe Chave: "+sanitizeTextASCII(req.NFCEChave), cols) {
				b.WriteString(l)
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\n\n")
	if strings.TrimSpace(req.Operador) != "" || strings.TrimSpace(req.CX) != "" {
		operador := sanitizeOneLineASCII(req.Operador)
		cx := sanitizeOneLineASCII(req.CX)
		if cx == "" {
			b.WriteString("Operador: " + operador)
			b.WriteString("\n")
		} else {
			b.WriteString(leftRightASCII("Operador: "+operador, "CX: "+cx, cols))
			b.WriteString("\n")
		}
	}
	b.WriteString(printedAt.Format("02/01/2006 15:04:05"))
	b.WriteString("\n")
	b.WriteString(centerASCII("www.goopedir.com.br", cols))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("-", cols))
	b.WriteString("\n")

	return b.String()
}

func buildFallbackSangriaText(req models.SangriaRequest, empresaCfg config.EmpresaParametros, printedAt time.Time, cols int) string {
	empresa := sanitizeOneLineASCII(empresaCfg.Nome)
	if empresa == "" {
		empresa = sanitizeOneLineASCII(empresaCfg.Razao)
	}
	cnpj := "CNPJ: " + sanitizeOneLineASCII(empresaCfg.CNPJ)
	fantasia := sanitizeOneLineASCII(empresaCfg.Razao)
	endereco := sanitizeOneLineASCII(fmt.Sprintf("%s, %s, %s/%s - CEP %s", empresaCfg.Rua, empresaCfg.Bairro, empresaCfg.Cidade, empresaCfg.Estado, empresaCfg.CEP))
	ie := "IE: " + sanitizeOneLineASCII(string(empresaCfg.IE))

	var b strings.Builder
	for _, l := range wrapTextASCII(empresa, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(cnpj, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(fantasia, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(endereco, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(ie, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(centerASCII("SANGRIA", cols))
	b.WriteString("\n\n")

	desc := sanitizeTextASCII(req.Descricao)
	if strings.TrimSpace(desc) != "" {
		for _, l := range wrapTextASCII("Descricao: "+desc, cols) {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
	b.WriteString(leftRightASCII("Valor", fmtMoney(req.Valor), cols))
	b.WriteString("\n")

	operador := sanitizeOneLineASCII(req.Operador)
	cx := sanitizeOneLineASCII(req.CX)
	if operador != "" || cx != "" {
		if cx == "" {
			b.WriteString("Operador: " + operador)
			b.WriteString("\n")
		} else {
			b.WriteString(leftRightASCII("Operador: "+operador, "CX: "+cx, cols))
			b.WriteString("\n")
		}
	}
	b.WriteString(printedAt.Format("02/01/2006 15:04:05"))
	b.WriteString("\n\n\n")

	b.WriteString(centerASCII(strings.Repeat("_", cols), cols))
	b.WriteString("\n")
	b.WriteString(centerASCII("Assinatura", cols))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("-", cols))
	b.WriteString("\n")
	return b.String()
}

func buildFallbackCaixaFechamentoText(req models.CaixaFechamentoRequest, empresaCfg config.EmpresaParametros, printedAt time.Time, cols int) string {
	empresa := sanitizeOneLineASCII(empresaCfg.Nome)
	if empresa == "" {
		empresa = sanitizeOneLineASCII(empresaCfg.Razao)
	}
	cnpj := "CNPJ: " + sanitizeOneLineASCII(empresaCfg.CNPJ)
	fantasia := sanitizeOneLineASCII(empresaCfg.Razao)
	endereco := sanitizeOneLineASCII(fmt.Sprintf("%s, %s, %s/%s - CEP %s", empresaCfg.Rua, empresaCfg.Bairro, empresaCfg.Cidade, empresaCfg.Estado, empresaCfg.CEP))
	ie := "IE: " + sanitizeOneLineASCII(string(empresaCfg.IE))

	var b strings.Builder
	for _, l := range wrapTextASCII(empresa, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(cnpj, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(fantasia, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(endereco, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(ie, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(centerASCII("FECHAMENTO DE CAIXA", cols))
	b.WriteString("\n\n")

	c0 := models.CaixaComputadoItem{}
	if len(req.Computado) > 0 {
		c0 = req.Computado[0]
	}
	b.WriteString(centerASCII(caixaIDMask(c0.ID), cols))
	b.WriteString("\n")

	ab := strings.TrimSpace(strings.TrimSpace(c0.DataAbertura) + " " + strings.TrimSpace(c0.HoraAbertura))
	fc := strings.TrimSpace(strings.TrimSpace(c0.DataFechamento) + " " + strings.TrimSpace(c0.HoraFechamento))
	if ab != "" {
		b.WriteString(leftRightASCII("Abertura", ab, cols))
		b.WriteString("\n")
	}
	if fc != "" {
		b.WriteString(leftRightASCII("Fechamento", fc, cols))
		b.WriteString("\n")
	}
	if float64(c0.ValorAbertura) != 0 {
		b.WriteString(leftRightASCII("Valor Abertura", fmtMoney(float64(c0.ValorAbertura)), cols))
		b.WriteString("\n")
	}
	if float64(c0.ValorFechamento) != 0 {
		b.WriteString(leftRightASCII("Valor Fechamento", fmtMoney(float64(c0.ValorFechamento)), cols))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	comp := append([]models.CaixaComputadoItem(nil), req.Computado...)
	sort.SliceStable(comp, func(i, j int) bool {
		return float64(comp[i].Valor) > float64(comp[j].Valor)
	})
	computadoOrder := make([]string, 0, len(comp))
	b.WriteString(centerASCII("COMPUTADO", cols))
	b.WriteString("\n")
	totalComp := 0.0
	for _, it := range comp {
		desc := strings.ToUpper(sanitizeOneLineASCII(it.Descricao))
		if strings.TrimSpace(desc) == "" {
			continue
		}
		computadoOrder = append(computadoOrder, desc)
		val := float64(it.Valor)
		totalComp += val
		b.WriteString(leftRightASCII(desc, fmtMoney(val), cols))
		b.WriteString("\n")
	}
	b.WriteString(leftRightASCII("Total", fmtMoney(totalComp), cols))
	b.WriteString("\n\n")

	inf := append([]models.CaixaLancadoItem(nil), req.Lancado...)
	orderIndex := make(map[string]int, len(computadoOrder))
	for i, name := range computadoOrder {
		if _, ok := orderIndex[name]; ok {
			continue
		}
		orderIndex[name] = i
	}
	sort.SliceStable(inf, func(i, j int) bool {
		di := strings.ToUpper(sanitizeOneLineASCII(inf[i].Descricao))
		dj := strings.ToUpper(sanitizeOneLineASCII(inf[j].Descricao))
		ii, okI := orderIndex[di]
		jj, okJ := orderIndex[dj]
		if okI && okJ {
			return ii < jj
		}
		if okI != okJ {
			return okI
		}
		return float64(inf[i].Valor) > float64(inf[j].Valor)
	})
	b.WriteString(centerASCII("INFORMADO", cols))
	b.WriteString("\n")
	totalInf := 0.0
	for _, it := range inf {
		desc := strings.ToUpper(sanitizeOneLineASCII(it.Descricao))
		if strings.TrimSpace(desc) == "" {
			continue
		}
		val := float64(it.Valor)
		totalInf += val
		b.WriteString(leftRightASCII(desc, fmtMoney(val), cols))
		b.WriteString("\n")
	}
	b.WriteString(leftRightASCII("Total", fmtMoney(totalInf), cols))
	b.WriteString("\n\n")

	if len(req.Categorias) > 0 {
		cats := append([]models.CaixaCategoriaItem(nil), req.Categorias...)
		sort.SliceStable(cats, func(i, j int) bool {
			return float64(cats[i].TotalGeral) > float64(cats[j].TotalGeral)
		})

		b.WriteString(centerASCII("CATEGORIAS", cols))
		b.WriteString("\n")
		b.WriteString(caixaNameQtyMoneyHeader("NOME", cols))
		b.WriteString("\n")
		totalCatQty := 0
		totalCatVal := 0.0
		for _, it := range cats {
			q := it.Quantidade
			v := float64(it.TotalGeral)
			totalCatQty += q
			totalCatVal += v
			b.WriteString(caixaNameQtyMoneyLine(strings.ToUpper(it.Produto), q, v, cols))
			b.WriteString("\n")
		}
		b.WriteString(caixaNameQtyMoneyLine("TOTAL", totalCatQty, totalCatVal, cols))
		b.WriteString("\n\n")
	}

	if len(req.Produtos) > 0 {
		prods := append([]models.CaixaProdutoItem(nil), req.Produtos...)
		sort.SliceStable(prods, func(i, j int) bool {
			return float64(prods[i].Total) > float64(prods[j].Total)
		})

		b.WriteString(centerASCII("PRODUTOS", cols))
		b.WriteString("\n")
		b.WriteString(caixaNameQtyMoneyHeader("NOME", cols))
		b.WriteString("\n")
		totalProdQty := 0
		totalProdVal := 0.0
		for _, it := range prods {
			q := it.Quantidade
			v := float64(it.Total)
			totalProdQty += q
			totalProdVal += v
			b.WriteString(caixaNameQtyMoneyLine(strings.ToUpper(it.Produto), q, v, cols))
			b.WriteString("\n")
		}
		b.WriteString(caixaNameQtyMoneyLine("TOTAL", totalProdQty, totalProdVal, cols))
		b.WriteString("\n\n")
	}

	if len(req.Motoboy) > 0 {
		b.WriteString(centerASCII("MOTOBOY", cols))
		b.WriteString("\n")
		type agg struct{ taxa float64 }
		m := make(map[string]agg, len(req.Motoboy))
		for _, it := range req.Motoboy {
			n := strings.ToUpper(sanitizeOneLineASCII(it.Motoboy))
			if strings.TrimSpace(n) == "" {
				n = "MOTOBOY"
			}
			a, ok := m[n]
			_ = ok
			a.taxa += float64(it.TaxaEntrega)
			m[n] = a
		}
		order := make([]string, 0, len(m))
		for name := range m {
			order = append(order, name)
		}
		sort.SliceStable(order, func(i, j int) bool {
			return m[order[i]].taxa > m[order[j]].taxa
		})
		totalTaxas := 0.0
		for _, name := range order {
			a := m[name]
			totalTaxas += a.taxa
			b.WriteString(leftRightASCII(name, fmtMoney(a.taxa), cols))
			b.WriteString("\n")
		}
		b.WriteString(leftRightASCII("TOTAL TAXAS", fmtMoney(totalTaxas), cols))
		b.WriteString("\n")
	}

	valMesa := float64(c0.ValorMesa)
	valServico := float64(c0.Servico)
	valRetirada := float64(c0.ValorVemBuscar)
	valEntrega := float64(c0.ValorDelivery)
	sum := valMesa + valServico + valRetirada + valEntrega

	b.WriteString("\n")
	b.WriteString(leftRightASCII("TOTAL MESA", fmtMoney(valMesa), cols))
	b.WriteString("\n")
	b.WriteString(leftRightASCII("TOTAL TAXA SERVICO", fmtMoney(valServico), cols))
	b.WriteString("\n")
	b.WriteString(leftRightASCII("TOTAL RETIRADA", fmtMoney(valRetirada), cols))
	b.WriteString("\n")
	b.WriteString(leftRightASCII("TOTAL ENTREGA", fmtMoney(valEntrega), cols))
	b.WriteString("\n")
	b.WriteString(leftRightASCII("TOTAL", fmtMoney(sum), cols))
	b.WriteString("\n")

	b.WriteString("\n")
	if strings.TrimSpace(c0.Usuario) != "" {
		b.WriteString(centerASCII(strings.ToUpper(sanitizeOneLineASCII(c0.Usuario)), cols))
		b.WriteString("\n")
	}
	b.WriteString(printedAt.Format("02/01/2006 15:04:05"))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("-", cols))
	b.WriteString("\n")
	return b.String()
}

func buildEscPOSConferencia(req models.ConferenciaRequest, empresaCfg config.EmpresaParametros, printedAt time.Time, hasMargins bool, margins config.PrinterMargins, maxWidthDots int, maxCols int) []byte {
	const dotsPerMM = 8
	const dotsPerCol = 12

	leftDots := 8
	widthDots := maxWidthDots - leftDots
	topLines := 0
	bottomLines := 0
	if hasMargins {
		topLines = mmToLines(margins.TopoMM)
		bottomLines = mmToLines(margins.BaseMM)
	}
	cols := widthDots / dotsPerCol
	if cols < 20 {
		cols = 20
	}
	if cols > maxCols {
		cols = maxCols
	}
	cols -= 6
	if cols < 20 {
		cols = 20
	}

	buf := make([]byte, 0, 1024)
	buf = append(buf, 0x1B, 0x40)
	buf = append(buf, 0x1B, 0x4D, 0x00)
	buf = append(buf, 0x1D, 0x21, 0x00)
	buf = append(buf, 0x1B, 0x32)
	buf = append(buf, 0x1B, 0x74, 0x02)

	buf = append(buf, 0x1D, 0x4C, byte(leftDots%256), byte(leftDots/256))
	buf = append(buf, 0x1D, 0x57, byte(widthDots%256), byte(widthDots/256))
	if topLines > 0 {
		buf = append(buf, 0x1B, 0x64, byte(minInt(topLines, 255)))
	}

	empresa := sanitizeOneLineASCII(empresaCfg.Nome)
	if empresa == "" {
		empresa = sanitizeOneLineASCII(empresaCfg.Razao)
	}
	cnpj := "CNPJ: " + sanitizeOneLineASCII(empresaCfg.CNPJ)
	fantasia := sanitizeOneLineASCII(empresaCfg.Razao)
	endereco := sanitizeOneLineASCII(fmt.Sprintf("%s, %s, %s/%s - CEP %s", empresaCfg.Rua, empresaCfg.Bairro, empresaCfg.Cidade, empresaCfg.Estado, empresaCfg.CEP))
	ie := "IE: " + sanitizeOneLineASCII(string(empresaCfg.IE))
	mesa := strings.ToUpper(sanitizeOneLineASCII(req.Mesa))

	buf = append(buf, 0x1B, 0x61, 0x01)
	buf = append(buf, 0x1B, 0x45, 0x01)
	for _, l := range wrapTextASCII(empresa, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}
	buf = append(buf, 0x1B, 0x45, 0x00)
	for _, l := range wrapTextASCII(cnpj, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}
	for _, l := range wrapTextASCII(fantasia, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}
	for _, l := range wrapTextASCII(endereco, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}
	for _, l := range wrapTextASCII(ie, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}

	buf = append(buf, '\n')
	title := mesa
	if isComandaConferencia(req) {
		title = buildTipoSequencial(req)
	}
	buf = append(buf, 0x1B, 0x61, 0x00)
	buf = append(buf, 0x1D, 0x42, 0x01)
	buf = append(buf, 0x1B, 0x45, 0x01)
	buf = append(buf, encodeCP850(padCenter(strings.ToUpper(title), cols))...)
	buf = append(buf, 0x1B, 0x45, 0x00)
	buf = append(buf, 0x1D, 0x42, 0x00)
	buf = append(buf, '\n', '\n')

	buf = append(buf, 0x1B, 0x61, 0x00)

	if isComandaConferencia(req) {
		appendEscPOSComandaInfo(&buf, req, printedAt, cols)
	}

	totalProdutos := calcTotalProdutos(req)
	taxaValor := calcTaxaServicoValor(req, totalProdutos)
	taxaEntrega := calcTaxaEntregaValor(req)
	valorDesconto := calcValorDesconto(req)
	totalGeral := calcTotalGeral(req, totalProdutos, taxaValor, taxaEntrega, valorDesconto)

	all := flattenConferenciaProdutos(req)
	sort.SliceStable(all, func(i, j int) bool {
		return sortKeyASCII(all[i].Categoria) < sortKeyASCII(all[j].Categoria)
	})
	all = groupConferenciaProdutos(all)

	lastProdCat := ""
	for _, p := range all {
		prodCat := strings.ToUpper(strings.TrimSpace(sanitizeTextASCII(p.Categoria)))
		if prodCat != "" && prodCat != lastProdCat {
			buf = append(buf, 0x1B, 0x61, 0x01)
			buf = append(buf, 0x1B, 0x45, 0x01)
			buf = append(buf, encodeCP850(dottedCategoryLine(prodCat, cols))...)
			buf = append(buf, 0x1B, 0x45, 0x00)
			buf = append(buf, '\n')
			buf = append(buf, 0x1B, 0x61, 0x00)
			lastProdCat = prodCat
		}
		buf = append(buf, encodeCP850(buildEscPOSProdutoLines(p, cols))...)
	}

	buf = append(buf, '\n')
	if strings.TrimSpace(req.Desconto) != "" {
		buf = append(buf, 0x1B, 0x61, 0x01)
		buf = append(buf, 0x1B, 0x45, 0x01)
		buf = append(buf, encodeCP850(centerASCII(strings.ToUpper(sanitizeOneLineASCII(req.Desconto)), cols))...)
		buf = append(buf, 0x1B, 0x45, 0x00)
		buf = append(buf, 0x1B, 0x61, 0x00)
		buf = append(buf, '\n')
	}
	buf = append(buf, encodeCP850(leftRightASCII("TOTAL PRODUTOS", fmtMoney(totalProdutos), cols))...)
	buf = append(buf, '\n')
	if valorDesconto > 0 {
		buf = append(buf, encodeCP850(leftRightASCII("DESCONTO", "(-) "+fmtMoney(valorDesconto), cols))...)
		buf = append(buf, '\n')
	}
	if taxaEntrega > 0 {
		buf = append(buf, encodeCP850(leftRightASCII("TAXA ENTREGA", "(+) "+fmtMoney(taxaEntrega), cols))...)
		buf = append(buf, '\n')
	}
	if req.TaxaServicoPercent > 0 || taxaValor > 0 {
		label := "TAXA SERVICO"
		if req.TaxaServicoPercent > 0 {
			label = fmt.Sprintf("TAXA SERVICO (%.2f%%)", req.TaxaServicoPercent)
		}
		buf = append(buf, encodeCP850(leftRightASCII(label, fmtMoney(taxaValor), cols))...)
		buf = append(buf, '\n')
		buf = append(buf, 0x1B, 0x45, 0x01)
		buf = append(buf, encodeCP850(leftRightASCII("TOTAL GERAL", fmtMoney(totalGeral), cols))...)
		buf = append(buf, 0x1B, 0x45, 0x00)
		buf = append(buf, '\n')
	} else {
		buf = append(buf, 0x1B, 0x45, 0x01)
		buf = append(buf, encodeCP850(leftRightASCII("TOTAL", fmtMoney(totalGeral), cols))...)
		buf = append(buf, 0x1B, 0x45, 0x00)
		buf = append(buf, '\n')
	}

	if strings.TrimSpace(req.NFCENumero) != "" || strings.TrimSpace(req.NFCEProtocolo) != "" || strings.TrimSpace(req.NFCEChave) != "" {
		buf = append(buf, '\n')
		if strings.TrimSpace(req.NFCENumero) != "" {
			buf = append(buf, encodeCP850("NFCe Numero: "+sanitizeOneLineASCII(req.NFCENumero))...)
			buf = append(buf, '\n')
		}
		if strings.TrimSpace(req.NFCEProtocolo) != "" {
			buf = append(buf, encodeCP850("NFCe Protocolo: "+sanitizeOneLineASCII(req.NFCEProtocolo))...)
			buf = append(buf, '\n')
		}
		if strings.TrimSpace(req.NFCEChave) != "" {
			for _, l := range wrapTextASCII("NFCe Chave: "+sanitizeTextASCII(req.NFCEChave), cols) {
				buf = append(buf, encodeCP850(l)...)
				buf = append(buf, '\n')
			}
		}
	}

	buf = append(buf, '\n', '\n')
	if strings.TrimSpace(req.Operador) != "" || strings.TrimSpace(req.CX) != "" {
		operador := sanitizeOneLineASCII(req.Operador)
		cx := sanitizeOneLineASCII(req.CX)
		if cx == "" {
			buf = append(buf, encodeCP850("Operador: "+operador)...)
			buf = append(buf, '\n')
		} else {
			buf = append(buf, encodeCP850(leftRightASCII("Operador: "+operador, "CX: "+cx, cols))...)
			buf = append(buf, '\n')
		}
	}

	if isComandaConferencia(req) && len(req.Pagamentos) > 0 {
		appendEscPOSPagamentos(&buf, req, cols)
	}

	buf = append(buf, encodeCP850(conferenceDatetime(req, printedAt).Format("02/01/2006 15:04:05"))...)
	buf = append(buf, '\n')
	buf = append(buf, 0x1B, 0x61, 0x01)
	buf = append(buf, encodeCP850(centerASCII("www.goopedir.com.br", cols))...)
	buf = append(buf, '\n')
	buf = append(buf, encodeCP850(strings.Repeat("-", cols))...)
	buf = append(buf, '\n')
	buf = append(buf, 0x1B, 0x61, 0x00)

	if bottomLines > 0 {
		buf = append(buf, 0x1B, 0x64, byte(minInt(bottomLines, 255)))
	}
	buf = append(buf, 0x1D, 0x56, 0x42, 0x00)
	return buf
}

func buildEscPOSSangria(req models.SangriaRequest, empresaCfg config.EmpresaParametros, printedAt time.Time, hasMargins bool, margins config.PrinterMargins, maxWidthDots int, maxCols int) []byte {
	const dotsPerMM = 8
	const dotsPerCol = 12

	leftDots := 8
	widthDots := maxWidthDots - leftDots
	topLines := 0
	bottomLines := 0
	if hasMargins {
		topLines = mmToLines(margins.TopoMM)
		bottomLines = mmToLines(margins.BaseMM)
	}
	cols := widthDots / dotsPerCol
	if cols < 20 {
		cols = 20
	}
	if cols > maxCols {
		cols = maxCols
	}
	cols -= 6
	if cols < 20 {
		cols = 20
	}

	buf := make([]byte, 0, 768)
	buf = append(buf, 0x1B, 0x40)
	buf = append(buf, 0x1B, 0x4D, 0x00)
	buf = append(buf, 0x1D, 0x21, 0x00)
	buf = append(buf, 0x1B, 0x32)
	buf = append(buf, 0x1B, 0x74, 0x02)

	buf = append(buf, 0x1D, 0x4C, byte(leftDots%256), byte(leftDots/256))
	buf = append(buf, 0x1D, 0x57, byte(widthDots%256), byte(widthDots/256))
	if topLines > 0 {
		buf = append(buf, 0x1B, 0x64, byte(minInt(topLines, 255)))
	}

	empresa := sanitizeOneLineASCII(empresaCfg.Nome)
	if empresa == "" {
		empresa = sanitizeOneLineASCII(empresaCfg.Razao)
	}
	cnpj := "CNPJ: " + sanitizeOneLineASCII(empresaCfg.CNPJ)
	fantasia := sanitizeOneLineASCII(empresaCfg.Razao)
	endereco := sanitizeOneLineASCII(fmt.Sprintf("%s, %s, %s/%s - CEP %s", empresaCfg.Rua, empresaCfg.Bairro, empresaCfg.Cidade, empresaCfg.Estado, empresaCfg.CEP))
	ie := "IE: " + sanitizeOneLineASCII(string(empresaCfg.IE))

	buf = append(buf, 0x1B, 0x61, 0x01)
	buf = append(buf, 0x1B, 0x45, 0x01)
	for _, l := range wrapTextASCII(empresa, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}
	buf = append(buf, 0x1B, 0x45, 0x00)
	for _, l := range wrapTextASCII(cnpj, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}
	for _, l := range wrapTextASCII(fantasia, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}
	for _, l := range wrapTextASCII(endereco, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}
	for _, l := range wrapTextASCII(ie, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}

	buf = append(buf, '\n')
	buf = append(buf, 0x1B, 0x61, 0x01)
	buf = append(buf, 0x1B, 0x45, 0x01)
	buf = append(buf, encodeCP850(centerASCII("SANGRIA", cols))...)
	buf = append(buf, 0x1B, 0x45, 0x00)
	buf = append(buf, '\n', '\n')
	buf = append(buf, 0x1B, 0x61, 0x00)

	desc := sanitizeTextASCII(req.Descricao)
	if strings.TrimSpace(desc) != "" {
		for _, l := range wrapTextASCII("Descricao: "+desc, cols) {
			buf = append(buf, encodeCP850(l)...)
			buf = append(buf, '\n')
		}
	}
	buf = append(buf, encodeCP850(leftRightASCII("Valor", fmtMoney(req.Valor), cols))...)
	buf = append(buf, '\n')

	operador := sanitizeOneLineASCII(req.Operador)
	cx := sanitizeOneLineASCII(req.CX)
	if operador != "" || cx != "" {
		if cx == "" {
			buf = append(buf, encodeCP850("Operador: "+operador)...)
			buf = append(buf, '\n')
		} else {
			buf = append(buf, encodeCP850(leftRightASCII("Operador: "+operador, "CX: "+cx, cols))...)
			buf = append(buf, '\n')
		}
	}

	buf = append(buf, encodeCP850(printedAt.Format("02/01/2006 15:04:05"))...)
	buf = append(buf, '\n', '\n', '\n')

	buf = append(buf, 0x1B, 0x61, 0x01)
	buf = append(buf, encodeCP850(centerASCII(strings.Repeat("_", cols), cols))...)
	buf = append(buf, '\n')
	buf = append(buf, encodeCP850(centerASCII("Assinatura", cols))...)
	buf = append(buf, '\n')
	buf = append(buf, 0x1B, 0x61, 0x00)

	buf = append(buf, encodeCP850(strings.Repeat("-", cols))...)
	buf = append(buf, '\n')

	if bottomLines > 0 {
		buf = append(buf, 0x1B, 0x64, byte(minInt(bottomLines, 255)))
	}
	buf = append(buf, 0x1D, 0x56, 0x42, 0x00)
	return buf
}

func buildEscPOSCaixaFechamento(req models.CaixaFechamentoRequest, empresaCfg config.EmpresaParametros, printedAt time.Time, hasMargins bool, margins config.PrinterMargins, maxWidthDots int, maxCols int) []byte {
	const dotsPerMM = 8
	const dotsPerCol = 12

	leftDots := 8
	widthDots := maxWidthDots - leftDots
	topLines := 0
	bottomLines := 0
	if hasMargins {
		topLines = mmToLines(margins.TopoMM)
		bottomLines = mmToLines(margins.BaseMM)
	}
	cols := widthDots / dotsPerCol
	if cols < 20 {
		cols = 20
	}
	if cols > maxCols {
		cols = maxCols
	}
	cols -= 6
	if cols < 20 {
		cols = 20
	}

	buf := make([]byte, 0, 1536)
	buf = append(buf, 0x1B, 0x40)
	buf = append(buf, 0x1B, 0x4D, 0x00)
	buf = append(buf, 0x1D, 0x21, 0x00)
	buf = append(buf, 0x1B, 0x32)
	buf = append(buf, 0x1B, 0x74, 0x02)

	buf = append(buf, 0x1D, 0x4C, byte(leftDots%256), byte(leftDots/256))
	buf = append(buf, 0x1D, 0x57, byte(widthDots%256), byte(widthDots/256))
	if topLines > 0 {
		buf = append(buf, 0x1B, 0x64, byte(minInt(topLines, 255)))
	}

	empresa := sanitizeOneLineASCII(empresaCfg.Nome)
	if empresa == "" {
		empresa = sanitizeOneLineASCII(empresaCfg.Razao)
	}
	cnpj := "CNPJ: " + sanitizeOneLineASCII(empresaCfg.CNPJ)
	fantasia := sanitizeOneLineASCII(empresaCfg.Razao)
	endereco := sanitizeOneLineASCII(fmt.Sprintf("%s, %s, %s/%s - CEP %s", empresaCfg.Rua, empresaCfg.Bairro, empresaCfg.Cidade, empresaCfg.Estado, empresaCfg.CEP))
	ie := "IE: " + sanitizeOneLineASCII(string(empresaCfg.IE))

	buf = append(buf, 0x1B, 0x61, 0x01)
	buf = append(buf, 0x1B, 0x45, 0x01)
	for _, l := range wrapTextASCII(empresa, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}
	buf = append(buf, 0x1B, 0x45, 0x00)
	for _, l := range wrapTextASCII(cnpj, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}
	for _, l := range wrapTextASCII(fantasia, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}
	for _, l := range wrapTextASCII(endereco, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}
	for _, l := range wrapTextASCII(ie, cols) {
		buf = append(buf, encodeCP850(centerASCII(l, cols))...)
		buf = append(buf, '\n')
	}

	buf = append(buf, '\n')
	buf = append(buf, 0x1B, 0x61, 0x01)
	buf = append(buf, 0x1B, 0x45, 0x01)
	buf = append(buf, encodeCP850(centerASCII("FECHAMENTO DE CAIXA", cols))...)
	buf = append(buf, 0x1B, 0x45, 0x00)
	buf = append(buf, '\n', '\n')

	c0 := models.CaixaComputadoItem{}
	if len(req.Computado) > 0 {
		c0 = req.Computado[0]
	}
	buf = append(buf, 0x1B, 0x45, 0x01)
	buf = append(buf, encodeCP850(centerASCII(caixaIDMask(c0.ID), cols))...)
	buf = append(buf, 0x1B, 0x45, 0x00)
	buf = append(buf, '\n')

	buf = append(buf, 0x1B, 0x61, 0x00)
	ab := strings.TrimSpace(strings.TrimSpace(c0.DataAbertura) + " " + strings.TrimSpace(c0.HoraAbertura))
	fc := strings.TrimSpace(strings.TrimSpace(c0.DataFechamento) + " " + strings.TrimSpace(c0.HoraFechamento))
	if ab != "" {
		buf = append(buf, encodeCP850(leftRightASCII("Abertura", sanitizeOneLineASCII(ab), cols))...)
		buf = append(buf, '\n')
	}
	if fc != "" {
		buf = append(buf, encodeCP850(leftRightASCII("Fechamento", sanitizeOneLineASCII(fc), cols))...)
		buf = append(buf, '\n')
	}
	if float64(c0.ValorAbertura) != 0 {
		buf = append(buf, encodeCP850(leftRightASCII("Valor Abertura", fmtMoney(float64(c0.ValorAbertura)), cols))...)
		buf = append(buf, '\n')
	}
	if float64(c0.ValorFechamento) != 0 {
		buf = append(buf, encodeCP850(leftRightASCII("Valor Fechamento", fmtMoney(float64(c0.ValorFechamento)), cols))...)
		buf = append(buf, '\n')
	}
	buf = append(buf, '\n')

	comp := append([]models.CaixaComputadoItem(nil), req.Computado...)
	sort.SliceStable(comp, func(i, j int) bool {
		return float64(comp[i].Valor) > float64(comp[j].Valor)
	})
	computadoOrder := make([]string, 0, len(comp))
	buf = append(buf, 0x1B, 0x61, 0x01)
	buf = append(buf, 0x1B, 0x45, 0x01)
	buf = append(buf, encodeCP850(centerASCII("COMPUTADO", cols))...)
	buf = append(buf, 0x1B, 0x45, 0x00)
	buf = append(buf, 0x1B, 0x61, 0x00)
	buf = append(buf, '\n')
	buf = append(buf, 0x1B, 0x61, 0x00)
	buf = append(buf, 0x1B, 0x45, 0x00)
	totalComp := 0.0
	for _, it := range comp {
		desc := strings.ToUpper(sanitizeOneLineASCII(it.Descricao))
		if strings.TrimSpace(desc) == "" {
			continue
		}
		computadoOrder = append(computadoOrder, desc)
		val := float64(it.Valor)
		totalComp += val
		buf = append(buf, encodeCP850(leftRightASCII(desc, fmtMoney(val), cols))...)
		buf = append(buf, '\n')
	}
	buf = append(buf, encodeCP850(leftRightASCII("TOTAL", fmtMoney(totalComp), cols))...)
	buf = append(buf, '\n', '\n')

	inf := append([]models.CaixaLancadoItem(nil), req.Lancado...)
	orderIndex := make(map[string]int, len(computadoOrder))
	for i, name := range computadoOrder {
		if _, ok := orderIndex[name]; ok {
			continue
		}
		orderIndex[name] = i
	}
	sort.SliceStable(inf, func(i, j int) bool {
		di := strings.ToUpper(sanitizeOneLineASCII(inf[i].Descricao))
		dj := strings.ToUpper(sanitizeOneLineASCII(inf[j].Descricao))
		ii, okI := orderIndex[di]
		jj, okJ := orderIndex[dj]
		if okI && okJ {
			return ii < jj
		}
		if okI != okJ {
			return okI
		}
		return float64(inf[i].Valor) > float64(inf[j].Valor)
	})
	buf = append(buf, 0x1B, 0x61, 0x01)
	buf = append(buf, 0x1B, 0x45, 0x01)
	buf = append(buf, encodeCP850(centerASCII("INFORMADO", cols))...)
	buf = append(buf, 0x1B, 0x45, 0x00)
	buf = append(buf, 0x1B, 0x61, 0x00)
	buf = append(buf, '\n')
	buf = append(buf, 0x1B, 0x61, 0x00)
	buf = append(buf, 0x1B, 0x45, 0x00)
	totalInf := 0.0
	for _, it := range inf {
		desc := strings.ToUpper(sanitizeOneLineASCII(it.Descricao))
		if strings.TrimSpace(desc) == "" {
			continue
		}
		val := float64(it.Valor)
		totalInf += val
		buf = append(buf, encodeCP850(leftRightASCII(desc, fmtMoney(val), cols))...)
		buf = append(buf, '\n')
	}
	buf = append(buf, encodeCP850(leftRightASCII("TOTAL", fmtMoney(totalInf), cols))...)
	buf = append(buf, '\n', '\n')

	if len(req.Categorias) > 0 {
		cats := append([]models.CaixaCategoriaItem(nil), req.Categorias...)
		sort.SliceStable(cats, func(i, j int) bool {
			return float64(cats[i].TotalGeral) > float64(cats[j].TotalGeral)
		})

		buf = append(buf, 0x1B, 0x61, 0x01)
		buf = append(buf, 0x1B, 0x45, 0x01)
		buf = append(buf, encodeCP850(centerASCII("CATEGORIAS", cols))...)
		buf = append(buf, 0x1B, 0x45, 0x00)
		buf = append(buf, '\n')
		buf = append(buf, 0x1B, 0x61, 0x00)
		buf = append(buf, encodeCP850(caixaNameQtyMoneyHeader("NOME", cols))...)
		buf = append(buf, '\n')
		totalCatQty := 0
		totalCatVal := 0.0
		for _, it := range cats {
			q := it.Quantidade
			v := float64(it.TotalGeral)
			totalCatQty += q
			totalCatVal += v
			buf = append(buf, encodeCP850(caixaNameQtyMoneyLine(strings.ToUpper(it.Produto), q, v, cols))...)
			buf = append(buf, '\n')
		}
		buf = append(buf, encodeCP850(caixaNameQtyMoneyLine("TOTAL", totalCatQty, totalCatVal, cols))...)
		buf = append(buf, '\n', '\n')
	}

	if len(req.Produtos) > 0 {
		prods := append([]models.CaixaProdutoItem(nil), req.Produtos...)
		sort.SliceStable(prods, func(i, j int) bool {
			return float64(prods[i].Total) > float64(prods[j].Total)
		})

		buf = append(buf, 0x1B, 0x61, 0x01)
		buf = append(buf, 0x1B, 0x45, 0x01)
		buf = append(buf, encodeCP850(centerASCII("PRODUTOS", cols))...)
		buf = append(buf, 0x1B, 0x45, 0x00)
		buf = append(buf, '\n')
		buf = append(buf, 0x1B, 0x61, 0x00)
		buf = append(buf, encodeCP850(caixaNameQtyMoneyHeader("NOME", cols))...)
		buf = append(buf, '\n')
		totalProdQty := 0
		totalProdVal := 0.0
		for _, it := range prods {
			q := it.Quantidade
			v := float64(it.Total)
			totalProdQty += q
			totalProdVal += v
			buf = append(buf, encodeCP850(caixaNameQtyMoneyLine(strings.ToUpper(it.Produto), q, v, cols))...)
			buf = append(buf, '\n')
		}
		buf = append(buf, encodeCP850(caixaNameQtyMoneyLine("TOTAL", totalProdQty, totalProdVal, cols))...)
		buf = append(buf, '\n', '\n')
	}

	if len(req.Motoboy) > 0 {
		buf = append(buf, 0x1B, 0x61, 0x01)
		buf = append(buf, 0x1B, 0x45, 0x01)
		buf = append(buf, encodeCP850(centerASCII("MOTOBOY", cols))...)
		buf = append(buf, 0x1B, 0x45, 0x00)
		buf = append(buf, '\n')
		buf = append(buf, 0x1B, 0x61, 0x00)
		type agg struct{ taxa float64 }
		m := make(map[string]agg, len(req.Motoboy))
		for _, it := range req.Motoboy {
			n := strings.ToUpper(sanitizeOneLineASCII(it.Motoboy))
			if strings.TrimSpace(n) == "" {
				n = "MOTOBOY"
			}
			a, ok := m[n]
			_ = ok
			a.taxa += float64(it.TaxaEntrega)
			m[n] = a
		}
		order := make([]string, 0, len(m))
		for name := range m {
			order = append(order, name)
		}
		sort.SliceStable(order, func(i, j int) bool {
			return m[order[i]].taxa > m[order[j]].taxa
		})

		totalTaxas := 0.0
		for _, name := range order {
			a := m[name]
			totalTaxas += a.taxa
			buf = append(buf, encodeCP850(leftRightASCII(name, fmtMoney(a.taxa), cols))...)
			buf = append(buf, '\n')
		}
		buf = append(buf, encodeCP850(leftRightASCII("TOTAL TAXAS", fmtMoney(totalTaxas), cols))...)
		buf = append(buf, '\n')
	}

	valMesa := float64(c0.ValorMesa)
	valServico := float64(c0.Servico)
	valRetirada := float64(c0.ValorVemBuscar)
	valEntrega := float64(c0.ValorDelivery)
	sum := valMesa + valServico + valRetirada + valEntrega

	buf = append(buf, '\n')
	buf = append(buf, encodeCP850(leftRightASCII("TOTAL MESA", fmtMoney(valMesa), cols))...)
	buf = append(buf, '\n')
	buf = append(buf, encodeCP850(leftRightASCII("TOTAL TAXA SERVICO", fmtMoney(valServico), cols))...)
	buf = append(buf, '\n')
	buf = append(buf, encodeCP850(leftRightASCII("TOTAL RETIRADA", fmtMoney(valRetirada), cols))...)
	buf = append(buf, '\n')
	buf = append(buf, encodeCP850(leftRightASCII("TOTAL ENTREGA", fmtMoney(valEntrega), cols))...)
	buf = append(buf, '\n')
	buf = append(buf, 0x1B, 0x45, 0x01)
	buf = append(buf, encodeCP850(leftRightASCII("TOTAL", fmtMoney(sum), cols))...)
	buf = append(buf, 0x1B, 0x45, 0x00)
	buf = append(buf, '\n')

	buf = append(buf, '\n')
	if strings.TrimSpace(c0.Usuario) != "" {
		buf = append(buf, 0x1B, 0x61, 0x01)
		buf = append(buf, encodeCP850(centerASCII(strings.ToUpper(sanitizeOneLineASCII(c0.Usuario)), cols))...)
		buf = append(buf, '\n')
		buf = append(buf, 0x1B, 0x61, 0x00)
	}
	buf = append(buf, encodeCP850(printedAt.Format("02/01/2006 15:04:05"))...)
	buf = append(buf, '\n')
	buf = append(buf, encodeCP850(strings.Repeat("-", cols))...)
	buf = append(buf, '\n')

	if bottomLines > 0 {
		buf = append(buf, 0x1B, 0x64, byte(minInt(bottomLines, 255)))
	}
	buf = append(buf, 0x1D, 0x56, 0x42, 0x00)
	return buf
}

func buildEscPOSProdutoLines(p models.Produto, cols int) string {
	nome := sanitizeTextASCII(p.Nome)
	var b strings.Builder
	total := p.ValorTotal
	if total <= 0 {
		total = (float64(p.Quantidade) * p.ValorUnitario) + p.ValorAdicional
	}
	titlePrefix := fmt.Sprintf("%dx - ", p.Quantidade)
	first, rest := wrapFirstLine(nameToWords(nome), cols-len(titlePrefix))
	b.WriteString(titlePrefix)
	b.WriteString(first)
	b.WriteString("\n")
	for _, l := range wrapTextASCII(rest, cols-len(titlePrefix)) {
		if strings.TrimSpace(l) == "" {
			continue
		}
		b.WriteString(strings.Repeat(" ", len(titlePrefix)))
		b.WriteString(l)
		b.WriteString("\n")
	}

	totalSabores := 0
	for _, e := range p.Extras {
		if strings.EqualFold(strings.TrimSpace(sanitizeTextASCII(e.Categoria)), "SABORES") {
			totalSabores += e.Quantidade
		}
	}

	extras := make([]models.Extra, len(p.Extras))
	copy(extras, p.Extras)
	sort.SliceStable(extras, func(i, j int) bool {
		return extraCategoryRankASCII(extras[i].Categoria) < extraCategoryRankASCII(extras[j].Categoria)
	})

	lastCat := ""
	for _, e := range extras {
		cat := sanitizeTextASCII(e.Categoria)
		catUpper := strings.ToUpper(strings.TrimSpace(cat))
		if catUpper != "" && catUpper != lastCat {
			b.WriteString(catUpper)
			b.WriteString("\n")
			lastCat = catUpper
		}

		name := sanitizeTextASCII(e.Nome)
		line := name
		if catUpper == "SABORES" && totalSabores > 0 {
			line = fmt.Sprintf("%d/%d %s", e.Quantidade, totalSabores, line)
		} else if e.Quantidade > 1 {
			line = fmt.Sprintf("%dUn %s", e.Quantidade, line)
		}
		for _, l := range wrapTextASCII("  - "+line, cols) {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}

	if strings.TrimSpace(p.Observacoes) != "" {
		obs := sanitizeTextASCII(p.Observacoes)
		for _, l := range wrapTextASCII("Obs: "+obs, cols) {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}

	b.WriteString(leftRightASCII("", fmtMoney(total), cols))
	b.WriteString("\n")
	return b.String()
}

func writeFallbackProduto(b *strings.Builder, p models.Produto, cols int) {
	b.WriteString(buildEscPOSProdutoLines(p, cols))
}

func flattenConferenciaProdutos(req models.ConferenciaRequest) []models.Produto {
	var out []models.Produto
	for _, it := range req.Itens {
		out = append(out, it.Produtos...)
	}
	return out
}

func groupConferenciaProdutos(all []models.Produto) []models.Produto {
	if len(all) == 0 {
		return all
	}

	out := make([]models.Produto, 0, len(all))
	indexByKey := make(map[string]int, len(all))

	for _, p := range all {
		if !isGroupableConferenciaProduto(p) {
			out = append(out, p)
			continue
		}

		cat := strings.ToUpper(strings.TrimSpace(sanitizeTextASCII(p.Categoria)))
		name := strings.ToUpper(strings.TrimSpace(sanitizeTextASCII(p.Nome)))
		if name == "" {
			out = append(out, p)
			continue
		}

		key := cat + "\n" + name
		if idx, ok := indexByKey[key]; ok {
			out[idx].Quantidade += p.Quantidade
			out[idx].ValorTotal += produtoTotalConferencia(p)
			continue
		}

		cp := p
		cp.ValorTotal = produtoTotalConferencia(p)
		cp.ValorUnitario = 0
		cp.ValorAdicional = 0
		out = append(out, cp)
		indexByKey[key] = len(out) - 1
	}

	return out
}

func isGroupableConferenciaProduto(p models.Produto) bool {
	if len(p.Extras) > 0 {
		return false
	}
	if strings.TrimSpace(p.Observacoes) != "" {
		return false
	}
	if p.ValorAdicional != 0 {
		return false
	}
	if p.Quantidade <= 0 {
		return false
	}
	return true
}

func produtoTotalConferencia(p models.Produto) float64 {
	total := p.ValorTotal
	if total <= 0 {
		total = (float64(p.Quantidade) * p.ValorUnitario) + p.ValorAdicional
	}
	return total
}

func caixaIDMask(id int) string {
	if id <= 0 {
		return "000"
	}
	if id < 1000 {
		return fmt.Sprintf("%03d", id)
	}
	return fmt.Sprintf("%d", id)
}

func caixaNameQtyMoneyHeader(nameLabel string, cols int) string {
	nameW, qtyW, moneyW := caixaTableWidths(cols)
	nameLabel = sanitizeOneLineASCII(nameLabel)
	if strings.TrimSpace(nameLabel) == "" {
		nameLabel = "Item"
	}
	return padRightASCII(strings.ToUpper(nameLabel), nameW) + " " + padLeftASCII("QTD", qtyW) + " " + padLeftASCII("TOTAL", moneyW)
}

func caixaNameQtyMoneyLine(name string, qty int, money float64, cols int) string {
	nameW, qtyW, moneyW := caixaTableWidths(cols)
	n := sanitizeOneLineASCII(name)
	if strings.TrimSpace(n) == "" {
		n = "-"
	}
	q := fmt.Sprintf("%d", qty)
	m := fmtAmountBR(money)
	return padRightASCII(truncASCII(n, nameW), nameW) + " " + padLeftASCII(q, qtyW) + " " + padLeftASCII(m, moneyW)
}

func caixaTableWidths(cols int) (nameW int, qtyW int, moneyW int) {
	if cols < 20 {
		cols = 20
	}
	qtyW = 4
	moneyW = 13
	minName := 6
	for cols-(qtyW+moneyW+2) < minName && moneyW > 9 {
		moneyW--
	}
	nameW = cols - (qtyW + moneyW + 2)
	if nameW < minName {
		nameW = minName
	}
	return nameW, qtyW, moneyW
}

func padLeftASCII(s string, width int) string {
	if width <= 0 {
		return s
	}
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

func padRightASCII(s string, width int) string {
	if width <= 0 {
		return s
	}
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func truncASCII(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func fmtAmountBR(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	return strings.ReplaceAll(s, ".", ",")
}

func dottedCategoryLine(cat string, cols int) string {
	cat = strings.TrimSpace(cat)
	if cat == "" || cols <= 0 {
		return cat
	}
	core := " " + cat + " "
	if len(core) >= cols {
		return core[:cols]
	}
	dots := cols - len(core)
	left := dots / 2
	right := dots - left
	return strings.Repeat(".", left) + core + strings.Repeat(".", right)
}

func sortKeyASCII(s string) string {
	return strings.ToUpper(strings.TrimSpace(sanitizeTextASCII(s)))
}

func nameToWords(s string) []string {
	return strings.Fields(strings.TrimSpace(s))
}

func wrapFirstLine(words []string, width int) (string, string) {
	if width < 1 {
		width = 1
	}
	if len(words) == 0 {
		return "", ""
	}
	var first []string
	cur := 0
	i := 0
	for ; i < len(words); i++ {
		w := words[i]
		wLen := len(w)
		if cur == 0 {
			if wLen > width {
				first = append(first, w[:width])
				words[i] = w[width:]
				i--
				cur = width
				break
			}
			first = append(first, w)
			cur = wLen
			continue
		}
		if cur+1+wLen > width {
			break
		}
		first = append(first, w)
		cur += 1 + wLen
	}
	firstLine := strings.Join(first, " ")
	restWords := words[i:]
	rest := strings.Join(restWords, " ")
	return firstLine, rest
}

func isComandaConferencia(req models.ConferenciaRequest) bool {
	return strings.TrimSpace(req.Cliente.Nome) != "" || len(req.Pagamentos) > 0 || strings.TrimSpace(req.Tipo) != "" || req.Sequencial > 0
}

func buildTipoSequencial(req models.ConferenciaRequest) string {
	t := strings.TrimSpace(req.Tipo)
	if t == "" {
		return strings.ToUpper(strings.TrimSpace(req.Mesa))
	}
	t = strings.ToUpper(t)
	if req.Sequencial <= 0 {
		return t
	}
	return fmt.Sprintf("%s %03d", t, req.Sequencial)
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func formatCelularBR(s string) string {
	d := onlyDigits(s)
	if len(d) == 10 {
		d = d[:2] + "9" + d[2:]
	}
	if len(d) >= 11 {
		ddd := d[:2]
		num := d[2:]
		if len(num) > 9 {
			num = num[:9]
		}
		return fmt.Sprintf("(%s) %s-%s", ddd, num[:5], num[5:])
	}
	if len(d) == 9 {
		return fmt.Sprintf("%s-%s", d[:5], d[5:])
	}
	return d
}

func pedidosTexto(n int) string {
	if n <= 0 {
		n = 1
	}
	if n == 1 {
		return "Primeiro Pedido"
	}
	return fmt.Sprintf("%d Pedidos No Restaurante", n)
}

func buildEnderecoConferencia(e models.ConferenciaEndereco) string {
	rua := sanitizeOneLineASCII(e.Rua)
	num := sanitizeOneLineASCII(e.Numero)
	bairro := sanitizeOneLineASCII(e.Bairro)
	cidade := sanitizeOneLineASCII(e.Cidade)
	comp := sanitizeOneLineASCII(e.Complemento)

	ruaNum := strings.TrimSpace(rua + " " + num)
	if strings.TrimSpace(ruaNum) == "" && strings.TrimSpace(bairro) == "" && strings.TrimSpace(cidade) == "" && strings.TrimSpace(comp) == "" {
		return ""
	}

	base := strings.TrimSpace(strings.Join([]string{
		ruaNum,
		bairro,
		cidade,
	}, ", "))
	if strings.Trim(base, " ,") == "" {
		return ""
	}
	if comp != "" {
		base = base + " [" + comp + "]"
	}
	return base
}

func appendEscPOSComandaInfo(buf *[]byte, req models.ConferenciaRequest, printedAt time.Time, cols int) {
	data := conferenceDatetime(req, printedAt).Format("02/01/2006 15:04")
	*buf = append(*buf, encodeCP850("Data: "+data)...)
	*buf = append(*buf, '\n')

	clienteNome := sanitizeOneLineASCII(req.Cliente.Nome)
	if clienteNome != "" {
		*buf = append(*buf, encodeCP850("Cliente: "+clienteNome)...)
		*buf = append(*buf, '\n')
	}

	cel := formatCelularBR(req.Cliente.Celular)
	if strings.TrimSpace(cel) != "" {
		*buf = append(*buf, encodeCP850("Celular: "+cel)...)
		*buf = append(*buf, '\n')
	}

	// Para DELIVERY/RETIRADA (quando "Tipo" vem preenchido), exibimos apenas o contador numérico,
	// sem os textos "Primeiro Pedido" / "X Pedidos ...".
	if strings.TrimSpace(req.Tipo) != "" && req.Cliente.Pedidos > 0 {
		*buf = append(*buf, encodeCP850(fmt.Sprintf("Pedidos: %d", int(req.Cliente.Pedidos)))...)
		*buf = append(*buf, '\n')
	}

	end := buildEnderecoConferencia(req.Endereco)
	if strings.TrimSpace(end) != "" {
		for _, l := range wrapTextASCII("Endereco: "+end, cols) {
			*buf = append(*buf, encodeCP850(l)...)
			*buf = append(*buf, '\n')
		}
	}

	*buf = append(*buf, '\n')
}

func appendEscPOSPagamentos(buf *[]byte, req models.ConferenciaRequest, cols int) {
	*buf = append(*buf, 0x1B, 0x61, 0x01)
	*buf = append(*buf, 0x1B, 0x45, 0x01)
	*buf = append(*buf, encodeCP850(centerASCII("PAGAMENTO", cols))...)
	*buf = append(*buf, 0x1B, 0x45, 0x00)
	*buf = append(*buf, '\n')
	*buf = append(*buf, 0x1B, 0x61, 0x00)

	clienteNome := sanitizeOneLineASCII(req.Cliente.Nome)

	for _, p := range req.Pagamentos {
		desc := sanitizeOneLineASCII(p.Descricao)
		if desc == "" {
			continue
		}

		right := ""
		if p.Troco > 0 {
			right = "Troco " + fmtMoney(p.Troco)
		} else if p.Valor > 0 {
			right = fmtMoney(p.Valor)
		}
		*buf = append(*buf, encodeCP850(leftRightASCII(desc, right, cols))...)
		*buf = append(*buf, '\n')

		if strings.TrimSpace(p.Nome) != "" {
			*buf = append(*buf, '\n', '\n')
			n := clienteNome
			if n == "" {
				n = sanitizeOneLineASCII(p.Nome)
			}
			if n == "" {
				n = "Cliente"
			}
			lineLen := cols
			*buf = append(*buf, 0x1B, 0x61, 0x01)
			*buf = append(*buf, encodeCP850(centerASCII(strings.Repeat("_", lineLen), cols))...)
			*buf = append(*buf, '\n')
			*buf = append(*buf, encodeCP850(centerASCII(n, cols))...)
			*buf = append(*buf, 0x1B, 0x61, 0x00)
			*buf = append(*buf, '\n')
		}
	}
	*buf = append(*buf, '\n')
}

func buildFallbackConferenciaComanda(req models.ConferenciaRequest, empresaCfg config.EmpresaParametros, printedAt time.Time, cols int) string {
	empresa := sanitizeOneLineASCII(empresaCfg.Nome)
	if empresa == "" {
		empresa = sanitizeOneLineASCII(empresaCfg.Razao)
	}
	cnpj := "CNPJ: " + sanitizeOneLineASCII(empresaCfg.CNPJ)
	fantasia := sanitizeOneLineASCII(empresaCfg.Razao)
	endereco := sanitizeOneLineASCII(fmt.Sprintf("%s, %s, %s/%s - CEP %s", empresaCfg.Rua, empresaCfg.Bairro, empresaCfg.Cidade, empresaCfg.Estado, empresaCfg.CEP))
	ie := "IE: " + sanitizeOneLineASCII(string(empresaCfg.IE))
	title := buildTipoSequencial(req)

	var b strings.Builder
	for _, l := range wrapTextASCII(empresa, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(cnpj, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(fantasia, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(endereco, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	for _, l := range wrapTextASCII(ie, cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(centerASCII(title, cols))
	b.WriteString("\n\n")

	data := conferenceDatetime(req, printedAt).Format("02/01/2006 15:04")
	b.WriteString("Data: " + data + "\n")
	clienteNome := sanitizeOneLineASCII(req.Cliente.Nome)
	if clienteNome != "" {
		b.WriteString("Cliente: " + clienteNome + "\n")
	}
	cel := formatCelularBR(req.Cliente.Celular)
	if strings.TrimSpace(cel) != "" {
		b.WriteString("Celular: " + cel + "\n")
	}
	// Para DELIVERY/RETIRADA (quando "Tipo" vem preenchido), exibimos apenas o contador numérico,
	// sem os textos "Primeiro Pedido" / "X Pedidos ...".
	if strings.TrimSpace(req.Tipo) != "" && req.Cliente.Pedidos > 0 {
		b.WriteString(fmt.Sprintf("Pedidos: %d\n", int(req.Cliente.Pedidos)))
	}
	end := buildEnderecoConferencia(req.Endereco)
	if strings.TrimSpace(end) != "" {
		for _, l := range wrapTextASCII("Endereco: "+end, cols) {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	all := flattenConferenciaProdutos(req)
	sort.SliceStable(all, func(i, j int) bool {
		return sortKeyASCII(all[i].Categoria) < sortKeyASCII(all[j].Categoria)
	})
	all = groupConferenciaProdutos(all)
	lastProdCat := ""
	for _, p := range all {
		prodCat := strings.ToUpper(strings.TrimSpace(sanitizeTextASCII(p.Categoria)))
		if prodCat != "" && prodCat != lastProdCat {
			b.WriteString(dottedCategoryLine(prodCat, cols))
			b.WriteString("\n")
			lastProdCat = prodCat
		}
		writeFallbackProduto(&b, p, cols)
	}

	b.WriteString("\n")
	totalProdutos := calcTotalProdutos(req)
	taxaValor := calcTaxaServicoValor(req, totalProdutos)
	taxaEntrega := calcTaxaEntregaValor(req)
	valorDesconto := calcValorDesconto(req)
	totalGeral := calcTotalGeral(req, totalProdutos, taxaValor, taxaEntrega, valorDesconto)
	if strings.TrimSpace(req.Desconto) != "" {
		b.WriteString(centerASCII(strings.ToUpper(sanitizeOneLineASCII(req.Desconto)), cols))
		b.WriteString("\n")
	}
	b.WriteString(leftRightASCII("TOTAL PRODUTOS", fmtMoney(totalProdutos), cols))
	b.WriteString("\n")
	if valorDesconto > 0 {
		b.WriteString(leftRightASCII("DESCONTO", "(-) "+fmtMoney(valorDesconto), cols))
		b.WriteString("\n")
	}
	if taxaEntrega > 0 {
		b.WriteString(leftRightASCII("TAXA ENTREGA", "(+) "+fmtMoney(taxaEntrega), cols))
		b.WriteString("\n")
	}
	if req.TaxaServicoPercent > 0 || taxaValor > 0 {
		label := "TAXA SERVICO"
		if req.TaxaServicoPercent > 0 {
			label = fmt.Sprintf("TAXA SERVICO (%.2f%%)", req.TaxaServicoPercent)
		}
		b.WriteString(leftRightASCII(label, fmtMoney(taxaValor), cols))
		b.WriteString("\n")
		b.WriteString(leftRightASCII("TOTAL GERAL", fmtMoney(totalGeral), cols))
		b.WriteString("\n")
	} else {
		b.WriteString(leftRightASCII("TOTAL", fmtMoney(totalGeral), cols))
		b.WriteString("\n")
	}

	if strings.TrimSpace(req.NFCENumero) != "" || strings.TrimSpace(req.NFCEProtocolo) != "" || strings.TrimSpace(req.NFCEChave) != "" {
		b.WriteString("\n")
		if strings.TrimSpace(req.NFCENumero) != "" {
			b.WriteString("NFCe Numero: " + sanitizeOneLineASCII(req.NFCENumero))
			b.WriteString("\n")
		}
		if strings.TrimSpace(req.NFCEProtocolo) != "" {
			b.WriteString("NFCe Protocolo: " + sanitizeOneLineASCII(req.NFCEProtocolo))
			b.WriteString("\n")
		}
		if strings.TrimSpace(req.NFCEChave) != "" {
			for _, l := range wrapTextASCII("NFCe Chave: "+sanitizeTextASCII(req.NFCEChave), cols) {
				b.WriteString(l)
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\n\n")
	operador := sanitizeOneLineASCII(req.Operador)
	cx := sanitizeOneLineASCII(req.CX)
	if operador != "" || cx != "" {
		if cx == "" {
			b.WriteString("Operador: " + operador)
			b.WriteString("\n")
		} else {
			b.WriteString(leftRightASCII("Operador: "+operador, "CX: "+cx, cols))
			b.WriteString("\n")
		}
	}

	if len(req.Pagamentos) > 0 {
		b.WriteString(centerASCII("PAGAMENTO", cols))
		b.WriteString("\n")
		for _, p := range req.Pagamentos {
			desc := sanitizeOneLineASCII(p.Descricao)
			if desc == "" {
				continue
			}
			right := ""
			if p.Troco > 0 {
				right = "Troco " + fmtMoney(p.Troco)
			} else if p.Valor > 0 {
				right = fmtMoney(p.Valor)
			}
			b.WriteString(leftRightASCII(desc, right, cols))
			b.WriteString("\n")
			if strings.TrimSpace(p.Nome) != "" {
				n := clienteNome
				if n == "" {
					n = sanitizeOneLineASCII(p.Nome)
				}
				if n == "" {
					n = "Cliente"
				}
				lineLen := len(n)
				if lineLen < 20 {
					lineLen = 20
				}
				if lineLen > cols {
					lineLen = cols
				}
				b.WriteString(strings.Repeat("_", lineLen))
				b.WriteString("\n")
				b.WriteString(n)
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	b.WriteString(conferenceDatetime(req, printedAt).Format("02/01/2006 15:04:05"))
	b.WriteString("\n")
	b.WriteString(centerASCII("www.goopedir.com.br", cols))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("-", cols))
	b.WriteString("\n")
	return b.String()
}

func conferenceDatetime(req models.ConferenciaRequest, fallback time.Time) time.Time {
	if req.Data <= 0 {
		return fallback
	}
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.Local)
	t := base.AddDate(0, 0, req.Data)
	if req.Hora > 0 {
		d := time.Duration(float64(24*time.Hour) * req.Hora)
		t = t.Add(d)
	}
	return t
}

func qtyPrefixASCII(qty int, indented bool) string {
	base := "- "
	if qty > 1 {
		base = fmt.Sprintf("%dUn - ", qty)
	}
	if indented {
		return "  " + base
	}
	return base
}

func saboresPrefixASCII(qty int, total int, indented bool) string {
	if total <= 0 {
		return qtyPrefixASCII(qty, indented)
	}
	base := fmt.Sprintf("%d/%d - ", qty, total)
	if indented {
		return "  " + base
	}
	return base
}

func extraCategoryRankASCII(cat string) int {
	cat = strings.TrimSpace(strings.ToUpper(sanitizeTextASCII(cat)))
	switch {
	case cat == "INGREDIENTES" || cat == "INGREDIENTE":
		return 0
	case strings.HasPrefix(cat, "ADICION"):
		return 1
	case cat == "SABORES" || cat == "SABOR":
		return 2
	case cat == "BORDA" || cat == "BORDAS":
		return 3
	default:
		return 4
	}
}

func wrapTextASCII(s string, width int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if width <= 1 {
		return []string{s}
	}

	words := strings.Fields(s)
	var lines []string
	var cur strings.Builder
	curLen := 0

	flush := func() {
		if curLen > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			curLen = 0
		}
	}

	for _, w := range words {
		if len(w) > width {
			flush()
			for len(w) > 0 {
				part := w
				if len(part) > width {
					part = part[:width]
				}
				lines = append(lines, part)
				w = w[len(part):]
			}
			continue
		}
		if curLen == 0 {
			cur.WriteString(w)
			curLen = len(w)
			continue
		}
		if curLen+1+len(w) <= width {
			cur.WriteByte(' ')
			cur.WriteString(w)
			curLen += 1 + len(w)
			continue
		}
		flush()
		cur.WriteString(w)
		curLen = len(w)
	}
	flush()
	return lines
}

func sanitizeOneLineASCII(s string) string {
	s = sanitizeTextASCII(s)
	parts := strings.Fields(strings.ReplaceAll(s, "\n", " "))
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func centerASCII(s string, cols int) string {
	s = strings.TrimSpace(s)
	if cols <= 0 || len(s) >= cols {
		return s
	}
	left := (cols - len(s)) / 2
	right := cols - len(s) - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

func leftRightASCII(left string, right string, cols int) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if cols <= 0 {
		return left + " " + right
	}
	if right == "" {
		if len(left) > cols {
			return left[:cols]
		}
		return left + strings.Repeat(" ", cols-len(left))
	}
	space := cols - len(left) - len(right)
	if space < 1 {
		maxLeft := cols - len(right) - 1
		if maxLeft < 0 {
			maxLeft = 0
		}
		if len(left) > maxLeft {
			left = left[:maxLeft]
		}
		space = cols - len(left) - len(right)
		if space < 1 {
			space = 1
		}
	}
	return left + strings.Repeat(" ", space) + right
}

func fmtMoney(v float64) string {
	if v < 0 {
		v = 0
	}
	s := fmt.Sprintf("%.2f", v)
	return "R$ " + strings.ReplaceAll(s, ".", ",")
}

func fmtQty(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%.0f", v)
	}
	s := fmt.Sprintf("%.2f", v)
	return strings.ReplaceAll(strings.TrimRight(strings.TrimRight(s, "0"), "."), ".", ",")
}

func calcTotalProdutos(req models.ConferenciaRequest) float64 {
	if req.TotalProdutos > 0 {
		return req.TotalProdutos
	}
	return 0
}

func calcTaxaServicoValor(req models.ConferenciaRequest, totalProdutos float64) float64 {
	if req.TaxaServicoValor > 0 {
		return req.TaxaServicoValor
	}
	if req.TaxaServicoPercent > 0 {
		return totalProdutos * (req.TaxaServicoPercent / 100.0)
	}
	return 0
}

func calcTaxaEntregaValor(req models.ConferenciaRequest) float64 {
	if req.TaxaEntregaValor > 0 {
		return req.TaxaEntregaValor
	}
	return 0
}

func calcValorDesconto(req models.ConferenciaRequest) float64 {
	if req.ValorDesconto > 0 {
		return req.ValorDesconto
	}
	return 0
}

func calcTotalGeral(req models.ConferenciaRequest, totalProdutos float64, taxaValor float64, taxaEntrega float64, valorDesconto float64) float64 {
	if req.TotalGeral > 0 {
		return req.TotalGeral
	}
	return totalProdutos + taxaValor + taxaEntrega - valorDesconto
}

func buildLogoRaster(logoBytes []byte, cfg config.LogoConfig, maxWidthDots int) ([]byte, bool) {
	img, _, err := image.Decode(bytes.NewReader(logoBytes))
	if err != nil {
		return nil, false
	}

	widthDots := maxWidthDots
	if cfg.LarguraMM > 0 {
		widthDots = minInt(maxWidthDots, cfg.LarguraMM*8)
	}
	if widthDots < 64 {
		widthDots = 64
	}
	if widthDots > maxWidthDots {
		widthDots = maxWidthDots
	}

	srcB := img.Bounds()
	srcW := srcB.Dx()
	srcH := srcB.Dy()
	if srcW <= 0 || srcH <= 0 {
		return nil, false
	}

	scale := float64(widthDots) / float64(srcW)
	heightDots := int(math.Round(float64(srcH) * scale))
	if heightDots < 1 {
		heightDots = 1
	}
	if heightDots > 800 {
		heightDots = 800
	}

	widthBytes := (widthDots + 7) / 8
	data := make([]byte, widthBytes*heightDots)

	opacity := cfg.Transparencia
	opF := float64(opacity) / 100.0

	for y := 0; y < heightDots; y++ {
		sy := srcB.Min.Y + int(float64(y)/scale)
		if sy >= srcB.Max.Y {
			sy = srcB.Max.Y - 1
		}
		row := y * widthBytes
		for x := 0; x < widthDots; x++ {
			sx := srcB.Min.X + int(float64(x)/scale)
			if sx >= srcB.Max.X {
				sx = srcB.Max.X - 1
			}
			r, g, b, a := img.At(sx, sy).RGBA()
			lum := (float64(r)*0.299 + float64(g)*0.587 + float64(b)*0.114) / 257.0
			alpha := (float64(a) / 65535.0) * opF
			effective := lum + (255.0 * (1.0 - alpha))
			black := effective < 180.0

			if black {
				i := row + (x / 8)
				data[i] |= 0x80 >> (x % 8)
			}
		}
	}

	var align byte = 0
	switch cfg.Alinhamento {
	case "right":
		align = 2
	case "center":
		align = 1
	default:
		align = 0
	}

	cmd := make([]byte, 0, 16+len(data))
	cmd = append(cmd, 0x1B, 0x61, align)
	cmd = append(cmd, 0x1D, 0x76, 0x30, 0x00)
	cmd = append(cmd, byte(widthBytes%256), byte(widthBytes/256))
	cmd = append(cmd, byte(heightDots%256), byte(heightDots/256))
	cmd = append(cmd, data...)
	cmd = append(cmd, 0x1B, 0x61, 0x00)
	return cmd, true
}

type docInfo1 struct {
	pDocName    *uint16
	pOutputFile *uint16
	pDatatype   *uint16
}

var (
	winspool            = syscall.NewLazyDLL("winspool.drv")
	procOpenPrinterW    = winspool.NewProc("OpenPrinterW")
	procClosePrinter    = winspool.NewProc("ClosePrinter")
	procStartDocPrinter = winspool.NewProc("StartDocPrinterW")
	procEndDocPrinter   = winspool.NewProc("EndDocPrinter")
	procStartPage       = winspool.NewProc("StartPagePrinter")
	procEndPage         = winspool.NewProc("EndPagePrinter")
	procWritePrinter    = winspool.NewProc("WritePrinter")
)

func printRAW(printerName string, data []byte) error {
	if len(data) == 0 {
		return errors.New("nada para imprimir")
	}

	pName, err := syscall.UTF16PtrFromString(printerName)
	if err != nil {
		return err
	}

	var h syscall.Handle
	r1, _, e1 := procOpenPrinterW.Call(uintptr(unsafe.Pointer(pName)), uintptr(unsafe.Pointer(&h)), 0)
	if r1 == 0 {
		return e1
	}
	defer procClosePrinter.Call(uintptr(h))

	docName, _ := syscall.UTF16PtrFromString("go-impressao")
	dataType, _ := syscall.UTF16PtrFromString("RAW")
	di := docInfo1{pDocName: docName, pDatatype: dataType}

	r2, _, e2 := procStartDocPrinter.Call(uintptr(h), 1, uintptr(unsafe.Pointer(&di)))
	if r2 == 0 {
		return e2
	}
	defer procEndDocPrinter.Call(uintptr(h))

	r3, _, e3 := procStartPage.Call(uintptr(h))
	if r3 == 0 {
		return e3
	}
	defer procEndPage.Call(uintptr(h))

	var written uint32
	r4, _, e4 := procWritePrinter.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(uint32(len(data))),
		uintptr(unsafe.Pointer(&written)),
	)
	if r4 == 0 {
		return e4
	}
	if int(written) != len(data) {
		return fmt.Errorf("impressão parcial: escrito=%d esperado=%d", written, len(data))
	}
	return nil
}
