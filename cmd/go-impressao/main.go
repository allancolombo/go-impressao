package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/getlantern/systray"
	"github.com/goopedir/go-impressao/internal/config"
	"github.com/goopedir/go-impressao/internal/handlers"
	"github.com/goopedir/go-impressao/internal/middleware"
	"github.com/goopedir/go-impressao/internal/services"
	"github.com/goopedir/go-impressao/internal/services/printer"
	"golang.org/x/sys/windows"
)

//go:embed app.ico
var trayIconICO []byte

func main() {
	logger := log.New(makeLogWriter(), "", log.LstdFlags|log.Lmicroseconds)

	releaseLock, ok := acquireSingleInstanceLock(logger)
	if !ok {
		return
	}
	defer releaseLock()

	cfgManager := config.NewManager(logger)
	cfgManager.Init(context.Background())
	baseURL, hasBaseURL := cfgManager.GetBaseURL()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if _, ok := cfgManager.GetEmpresaParametros(); ok {
				return
			}
			_, _ = cfgManager.RefreshEmpresaParametros(context.Background())
		}
	}()

	// Estruturas principais (armazenamento em memória + serviços).
	jobStore := services.NewJobStore()
	formatter := services.NewFormatter()
	historyStore := services.NewHistoryStore(logger)
	historyStore.Init()
	printerSvc := printer.NewWindowsPrinter(logger, cfgManager)
	printService := services.NewPrintService(logger, jobStore, formatter, printerSvc, historyStore, cfgManager)

	impressaoHandler := handlers.NewImpressaoHandler(logger, jobStore, formatter, printService, historyStore, cfgManager)
	configHandler := handlers.NewConfigHandler(logger, cfgManager)
	historicoHandler := handlers.NewHistoricoHandler(logger, historyStore, jobStore, formatter, printService)

	// Rotas HTTP + middlewares.
	mux := http.NewServeMux()
	impressaoHandler.Register(mux)
	configHandler.Register(mux)
	historicoHandler.Register(mux)

	handler := middleware.WithCORS(middleware.WithRequestLog(logger, mux))

	addrToListen := ":0"
	if isGoRun() {
		addrToListen = ":21210"
	}
	listener, err := net.Listen("tcp", addrToListen)
	if err != nil {
		logger.Fatalf("erro ao iniciar listener TCP: %v", err)
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	addr := listener.Addr().String()
	appURL := ""
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
		logger.Printf("servidor iniciado em http://localhost:%d (addr=%s)", tcpAddr.Port, addr)
		appURL = fmt.Sprintf("http://localhost:%d", tcpAddr.Port)
		if !hasBaseURL {
			go openBrowser(logger, "http://localhost:"+itoa(tcpAddr.Port)+"/config")
		}
	} else {
		logger.Printf("servidor iniciado em http://%s", addr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		cancel()
	}()

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("erro no servidor HTTP: %v", err)
		}
	}()

	if hasBaseURL && appURL != "" {
		go func() {
			notifyConfigGO(logger, baseURL, appURL)
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					notifyConfigGO(logger, baseURL, appURL)
				}
			}
		}()
	}

	if appURL != "" {
		go func() {
			<-ctx.Done()
			systray.Quit()
		}()
		runTray(logger, appURL, cancel)
	} else {
		<-ctx.Done()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Printf("encerrando servidor...")
	_ = server.Shutdown(shutdownCtx)
}

func isGoRun() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	exe = strings.ToLower(exe)
	return strings.Contains(exe, "\\go-build") || strings.Contains(exe, "/go-build")
}

func acquireSingleInstanceLock(logger *log.Logger) (func(), bool) {
	handle, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr("Global\\GooImpressao"))
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		logger.Printf("mutex: erro ao criar mutex: %v", err)
		return func() {}, true
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(handle)
		return func() {}, false
	}

	return func() {
		_ = windows.ReleaseMutex(handle)
		_ = windows.CloseHandle(handle)
	}, true
}

func makeLogWriter() io.Writer {
	dir, err := os.UserConfigDir()
	if err != nil {
		return os.Stdout
	}
	dir = filepath.Join(dir, "go-impressao")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "go-impressao.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return os.Stdout
	}
	return io.MultiWriter(os.Stdout, f)
}

func runTray(logger *log.Logger, appURL string, cancel context.CancelFunc) {
	systray.Run(func() {
		systray.SetTitle("GooImpressão")
		systray.SetTooltip("Serviço de impressão do GooPedir")
		if len(trayIconICO) > 0 {
			systray.SetIcon(trayIconICO)
		} else if icon, ok := buildTrayIconICO(); ok {
			systray.SetIcon(icon)
		}

		mConfig := systray.AddMenuItem("Configuração", "Abrir configurações")
		mHistorico := systray.AddMenuItem("Histórico", "Abrir histórico de impressões")
		systray.AddSeparator()
		mSair := systray.AddMenuItem("Sair", "Encerrar o serviço")

		go func() {
			for {
				select {
				case <-mConfig.ClickedCh:
					openBrowser(logger, strings.TrimRight(appURL, "/")+"/config")
				case <-mHistorico.ClickedCh:
					openBrowser(logger, strings.TrimRight(appURL, "/")+"/historico")
				case <-mSair.ClickedCh:
					cancel()
					systray.Quit()
					return
				}
			}
		}()
	}, func() {})
}

func buildTrayIconICO() ([]byte, bool) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	bg := color.RGBA{R: 24, G: 28, B: 45, A: 255}
	fg := color.RGBA{R: 240, G: 245, B: 255, A: 255}
	ac := color.RGBA{R: 77, G: 139, B: 255, A: 255}

	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetRGBA(x, y, bg)
		}
	}

	for x := 1; x < 15; x++ {
		img.SetRGBA(x, 1, ac)
		img.SetRGBA(x, 14, ac)
	}
	for y := 1; y < 15; y++ {
		img.SetRGBA(1, y, ac)
		img.SetRGBA(14, y, ac)
	}

	for y := 4; y <= 11; y++ {
		img.SetRGBA(4, y, fg)
	}
	for x := 4; x <= 11; x++ {
		img.SetRGBA(x, 4, fg)
		img.SetRGBA(x, 11, fg)
	}
	for y := 8; y <= 11; y++ {
		img.SetRGBA(11, y, fg)
	}
	for x := 8; x <= 11; x++ {
		img.SetRGBA(x, 8, fg)
	}

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return nil, false
	}
	pngBytes := pngBuf.Bytes()

	iconDir := make([]byte, 6+16)
	binary.LittleEndian.PutUint16(iconDir[0:2], 0)
	binary.LittleEndian.PutUint16(iconDir[2:4], 1)
	binary.LittleEndian.PutUint16(iconDir[4:6], 1)

	iconDir[6] = 16
	iconDir[7] = 16
	iconDir[8] = 0
	iconDir[9] = 0
	binary.LittleEndian.PutUint16(iconDir[10:12], 1)
	binary.LittleEndian.PutUint16(iconDir[12:14], 32)
	binary.LittleEndian.PutUint32(iconDir[14:18], uint32(len(pngBytes)))
	binary.LittleEndian.PutUint32(iconDir[18:22], uint32(len(iconDir)))

	out := make([]byte, 0, len(iconDir)+len(pngBytes))
	out = append(out, iconDir...)
	out = append(out, pngBytes...)
	return out, true
}

func openBrowser(logger *log.Logger, url string) {
	logger.Printf("abrindo navegador em: %s", url)
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err != nil {
		logger.Printf("não foi possível abrir o navegador automaticamente: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [32]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(buf[i:])
}

func notifyConfigGO(logger *log.Logger, baseURL string, appURL string) {
	target := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/impressao/config/go"
	appURL = strings.TrimSpace(appURL)
	if target == "/impressao/config/go" || appURL == "" {
		return
	}

	bodyBytes, _ := json.Marshal(map[string]string{"url": appURL})
	backoff := []time.Duration{0, 1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}

	for attempt := 1; attempt <= len(backoff); attempt++ {
		if backoff[attempt-1] > 0 {
			time.Sleep(backoff[attempt-1])
		}

		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(bodyBytes))
		if err != nil {
			cancel()
			logger.Printf("config-go: tentativa=%d erro ao criar requisição: %v", attempt, err)
			continue
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")

		logger.Printf("config-go: tentativa=%d POST %s body.url=%s", attempt, target, appURL)
		resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
		cancel()
		if err != nil {
			logger.Printf("config-go: tentativa=%d falha de rede: %v", attempt, err)
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			logger.Printf("config-go: sucesso status=%d", resp.StatusCode)
			return
		}

		logger.Printf("config-go: tentativa=%d falhou status=%d", attempt, resp.StatusCode)
	}

	logger.Printf("config-go: desistindo após %d tentativas", len(backoff))
}
