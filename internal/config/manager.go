package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	logger *log.Logger
	path   string

	mu       sync.RWMutex
	baseURL  string
	printers map[string]PrinterConfig
	logo     LogoConfig
	empresa  EmpresaParametros
	hasEmpresa bool
}

type fileConfig struct {
	BaseURL  string                  `json:"base_url"`
	Printers map[string]PrinterConfig `json:"printers,omitempty"`
	Logo     LogoConfig              `json:"logo,omitempty"`
}

func NewManager(logger *log.Logger) *Manager {
	return &Manager{
		logger: logger,
		path:   defaultConfigPath(),
	}
}

type PrinterMargins struct {
	TopoMM    int `json:"topo_mm"`
	BaseMM    int `json:"base_mm"`
	EsquerdaMM int `json:"esquerda_mm"`
	DireitaMM  int `json:"direita_mm"`
}

type PrinterConfig struct {
	Margens PrinterMargins `json:"margens"`
}

type LogoConfig struct {
	Habilitado   bool   `json:"habilitado"`
	Arquivo      string `json:"arquivo,omitempty"`
	LarguraMM    int    `json:"largura_mm"`
	Alinhamento  string `json:"alinhamento,omitempty"` // left|center|right
	Transparencia int   `json:"transparencia"`         // 0-100 (100 = sem transparência)
}

type FlexString string

func (s *FlexString) UnmarshalJSON(b []byte) error {
	raw := strings.TrimSpace(string(b))
	if raw == "null" || raw == "" {
		*s = ""
		return nil
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = FlexString(strings.TrimSpace(v))
		return nil
	}
	*s = FlexString(strings.TrimSpace(raw))
	return nil
}

type EmpresaParametros struct {
	Bairro string `json:"bairro"`
	Cidade string `json:"cidade"`
	CEP    FlexString `json:"cep"`
	Estado string `json:"estado"`
	Rua    string `json:"rua"`

	CNPJ  string `json:"cnpj"`
	IE    FlexString `json:"ie"`
	Nome  string `json:"nome"`
	Razao string `json:"razao"`
}

func (m *Manager) Init(ctx context.Context) {
	m.logger.Printf("config: iniciando verificação de URL principal")

	if err := m.load(); err != nil {
		m.logger.Printf("config: erro ao ler configuração local: %v", err)
	}

	m.tryAdoptLocalLogo()
	_, _ = m.RefreshEmpresaParametros(ctx)

	if u, ok := m.GetBaseURL(); ok {
		m.logger.Printf("config: URL principal carregada do disco: %s", u)
		return
	}

	auto := "http://localhost:2121"
	testCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	m.logger.Printf("config: nenhuma URL configurada. tentando automaticamente: %s", auto)
	if err := TestBaseURL(testCtx, auto); err != nil {
		m.logger.Printf("config: tentativa automática falhou: %v", err)
		return
	}

	if err := m.setAndSave(auto); err != nil {
		m.logger.Printf("config: falha ao persistir URL automática: %v", err)
		return
	}
	m.logger.Printf("config: URL principal configurada automaticamente: %s", auto)
}

func (m *Manager) GetEmpresaParametros() (EmpresaParametros, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.hasEmpresa {
		return EmpresaParametros{}, false
	}
	return m.empresa, true
}

func (m *Manager) RefreshEmpresaParametros(ctx context.Context) (EmpresaParametros, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:2121/v2/parametros", nil)
	if err != nil {
		return EmpresaParametros{}, false
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return EmpresaParametros{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return EmpresaParametros{}, false
	}

	var raw any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return EmpresaParametros{}, false
	}

	var p EmpresaParametros
	switch v := raw.(type) {
	case []any:
		if len(v) == 0 {
			return EmpresaParametros{}, false
		}
		b, _ := json.Marshal(v[0])
		_ = json.Unmarshal(b, &p)
	case map[string]any:
		b, _ := json.Marshal(v)
		_ = json.Unmarshal(b, &p)
	default:
		return EmpresaParametros{}, false
	}

	p.Bairro = strings.TrimSpace(p.Bairro)
	p.Cidade = strings.TrimSpace(p.Cidade)
	p.CEP = FlexString(strings.TrimSpace(string(p.CEP)))
	p.Estado = strings.TrimSpace(p.Estado)
	p.Rua = strings.TrimSpace(p.Rua)
	p.CNPJ = strings.TrimSpace(p.CNPJ)
	p.IE = FlexString(strings.TrimSpace(string(p.IE)))
	p.Nome = strings.TrimSpace(p.Nome)
	p.Razao = strings.TrimSpace(p.Razao)
	if p.Nome == "" && p.Razao == "" && p.CNPJ == "" {
		return EmpresaParametros{}, false
	}

	m.mu.Lock()
	m.empresa = p
	m.hasEmpresa = true
	m.mu.Unlock()
	m.logger.Printf("parametros: carregados de /v2/parametros")
	return p, true
}

func (m *Manager) tryAdoptLocalLogo() {
	current := m.GetLogo()
	if current.Habilitado && strings.TrimSpace(current.Arquivo) != "" {
		path := filepath.Join(m.ConfigDir(), current.Arquivo)
		if _, err := os.Stat(path); err == nil {
			return
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return
	}
	exeDir := filepath.Dir(exe)
	candidate := filepath.Join(exeDir, "logo.png")
	if _, err := os.Stat(candidate); err != nil {
		return
	}

	b, err := os.ReadFile(candidate)
	if err != nil {
		return
	}

	fileName, err := m.SaveLogoFile("logo.png", b)
	if err != nil {
		m.logger.Printf("logo: não foi possível adotar logo.png: %v", err)
		return
	}

	_ = m.SetLogoConfig(LogoConfig{
		Habilitado:   true,
		Arquivo:      fileName,
		LarguraMM:    60,
		Alinhamento:  "center",
		Transparencia: 100,
	})
	m.logger.Printf("logo: logo.png adotado automaticamente (%s)", candidate)
}

func (m *Manager) GetBaseURL() (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if strings.TrimSpace(m.baseURL) == "" {
		return "", false
	}
	return m.baseURL, true
}

func (m *Manager) GetPrinterConfig(printerName string) (PrinterConfig, bool) {
	printerName = strings.TrimSpace(printerName)
	if printerName == "" {
		return PrinterConfig{}, false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.printers == nil {
		return PrinterConfig{}, false
	}
	cfg, ok := m.printers[printerName]
	return cfg, ok
}

func (m *Manager) GetAllPrinters() map[string]PrinterConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]PrinterConfig, len(m.printers))
	for k, v := range m.printers {
		out[k] = v
	}
	return out
}

func (m *Manager) SetPrinterMargins(printerName string, margins PrinterMargins) error {
	printerName = strings.TrimSpace(printerName)
	if printerName == "" {
		return errors.New("nome da impressora é obrigatório")
	}
	if err := ValidateMargins(margins); err != nil {
		return err
	}

	m.mu.Lock()
	if m.printers == nil {
		m.printers = make(map[string]PrinterConfig)
	}
	cfg := m.printers[printerName]
	cfg.Margens = margins
	m.printers[printerName] = cfg
	m.mu.Unlock()

	return m.save()
}

func (m *Manager) GetLogo() LogoConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.logo
}

func (m *Manager) SetLogoConfig(cfg LogoConfig) error {
	if cfg.Alinhamento == "" {
		cfg.Alinhamento = "center"
	}
	if err := ValidateLogo(cfg); err != nil {
		return err
	}
	m.mu.Lock()
	m.logo.Habilitado = cfg.Habilitado
	m.logo.LarguraMM = cfg.LarguraMM
	m.logo.Alinhamento = cfg.Alinhamento
	m.logo.Transparencia = cfg.Transparencia
	if cfg.Arquivo != "" {
		m.logo.Arquivo = cfg.Arquivo
	}
	m.mu.Unlock()
	return m.save()
}

func (m *Manager) SaveLogoFile(originalName string, data []byte) (string, error) {
	originalName = strings.TrimSpace(originalName)
	if originalName == "" {
		originalName = "logo.png"
	}
	ext := strings.ToLower(filepath.Ext(originalName))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
		return "", errors.New("formato de logo não suportado (use PNG/JPG/GIF)")
	}

	dir := m.ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("não foi possível criar diretório de configuração: %w", err)
	}

	fileName := "logo" + ext
	dst := filepath.Join(dir, fileName)
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return "", fmt.Errorf("não foi possível gravar o logo: %w", err)
	}

	m.mu.Lock()
	m.logo.Arquivo = fileName
	m.mu.Unlock()

	if err := m.save(); err != nil {
		return "", err
	}
	return fileName, nil
}

func (m *Manager) ConfigDir() string {
	return filepath.Dir(m.path)
}

func (m *Manager) EffectiveCols(printerName string) int {
	const maxWidthDots = 576
	const dotsPerMM = 8
	const dotsPerCol = 12

	printerName = strings.TrimSpace(printerName)
	if printerName == "" {
		return 48
	}

	margins := PrinterMargins{}
	ok := false
	if pc, has := m.GetPrinterConfig(printerName); has {
		margins = pc.Margens
		ok = true
	}
	if !ok {
		return 48
	}

	widthDots := maxWidthDots - (margins.EsquerdaMM+margins.DireitaMM)*dotsPerMM
	if widthDots < 128 {
		widthDots = 128
	}

	cols := widthDots / dotsPerCol
	if cols < 20 {
		cols = 20
	}
	if cols > 48 {
		cols = 48
	}
	return cols
}

func ValidateMargins(m PrinterMargins) error {
	if m.TopoMM < 0 || m.BaseMM < 0 || m.EsquerdaMM < 0 || m.DireitaMM < 0 {
		return errors.New("margens devem ser maiores ou iguais a zero")
	}
	if m.TopoMM > 100 || m.BaseMM > 100 || m.EsquerdaMM > 50 || m.DireitaMM > 50 {
		return errors.New("margens muito grandes para 80mm")
	}
	return nil
}

func ValidateLogo(cfg LogoConfig) error {
	if cfg.Alinhamento != "left" && cfg.Alinhamento != "center" && cfg.Alinhamento != "right" {
		return errors.New("alinhamento inválido (use left, center ou right)")
	}
	if cfg.LarguraMM < 0 || cfg.LarguraMM > 80 {
		return errors.New("largura do logo inválida (0 a 80mm)")
	}
	if cfg.Transparencia < 0 || cfg.Transparencia > 100 {
		return errors.New("transparência inválida (0 a 100)")
	}
	return nil
}

func (m *Manager) ValidateAndSave(ctx context.Context, baseURL string) error {
	baseURL = strings.TrimSpace(baseURL)
	if err := ValidateBaseURLFormat(baseURL); err != nil {
		return err
	}
	if err := TestBaseURL(ctx, baseURL); err != nil {
		return err
	}
	return m.setAndSave(baseURL)
}

func (m *Manager) setAndSave(baseURL string) error {
	m.mu.Lock()
	m.baseURL = baseURL
	m.mu.Unlock()

	if err := m.save(); err != nil {
		return err
	}
	return nil
}

func ValidateBaseURLFormat(baseURL string) error {
	if strings.TrimSpace(baseURL) == "" {
		return errors.New("URL não informada")
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return errors.New("URL inválida: verifique o formato")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("URL inválida: protocolo deve ser http ou https")
	}
	if u.Host == "" {
		return errors.New("URL inválida: host é obrigatório")
	}
	if u.Path != "" && u.Path != "/" {
		return errors.New("URL inválida: informe apenas a base (ex.: http://localhost:2121)")
	}
	return nil
}

func TestBaseURL(ctx context.Context, baseURL string) error {
	if err := ValidateBaseURLFormat(baseURL); err != nil {
		return err
	}

	target := strings.TrimRight(baseURL, "/") + "/impressao/padrao"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("erro ao criar requisição de teste: %w", err)
	}

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("falha de rede ao testar %s: %v", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("endpoint /impressao/padrao retornou status %d", resp.StatusCode)
	}
	return nil
}

func (m *Manager) load() error {
	b, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var cfg fileConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return errors.New("arquivo de configuração corrompido")
	}

	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	m.mu.Lock()
	if cfg.BaseURL != "" {
		if err := ValidateBaseURLFormat(cfg.BaseURL); err != nil {
			m.logger.Printf("config: URL salva no disco é inválida e será ignorada: %v", err)
		} else {
			m.baseURL = cfg.BaseURL
		}
	}
	m.printers = cfg.Printers
	m.logo = cfg.Logo
	if m.logo.Alinhamento == "" {
		m.logo.Alinhamento = "center"
	}
	if m.logo.Transparencia == 0 {
		m.logo.Transparencia = 100
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) save() error {
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("não foi possível criar diretório de configuração: %w", err)
	}

	m.mu.RLock()
	cfg := fileConfig{BaseURL: m.baseURL, Printers: m.printers, Logo: m.logo}
	m.mu.RUnlock()

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("não foi possível serializar configuração: %w", err)
	}

	if err := os.WriteFile(m.path, b, 0o600); err != nil {
		return fmt.Errorf("não foi possível gravar configuração: %w", err)
	}
	return nil
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(dir, "go-impressao", "config.json")
}
