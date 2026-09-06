package config

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_OldConfigOnlyBaseURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"base_url":"http://localhost:2121"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	m := NewManager(log.New(io.Discard, "", 0))
	m.path = path

	if err := m.load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	if got, ok := m.GetBaseURL(); !ok || got != "http://localhost:2121" {
		t.Fatalf("base_url inesperada: %q ok=%v", got, ok)
	}

	if len(m.GetAllPrinters()) != 0 {
		t.Fatalf("não esperava printers no config antigo")
	}

	l := m.GetLogo()
	if l.Alinhamento == "" {
		t.Fatalf("alinhamento deveria ter default")
	}
}

func TestLoad_NewConfigWithPrintersAndLogo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := `{
  "base_url": "http://localhost:2121",
  "printers": {
    "COZINHA (Windows)": {
      "margens": { "topo_mm": 4, "base_mm": 8, "esquerda_mm": 2, "direita_mm": 2 },
      "colunas": { "80mm": 42, "56mm": 29 }
    }
  },
  "logo": { "habilitado": true, "arquivo": "logo.png", "largura_mm": 60, "alinhamento": "center", "transparencia": 80 },
  "conferencia": { "fonte": "pequena", "delimitador": "*", "mensagem_final": "Obrigado!", "vias": 3 }
}`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	m := NewManager(log.New(io.Discard, "", 0))
	m.path = path

	if err := m.load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	pc, ok := m.GetPrinterConfig("COZINHA (Windows)")
	if !ok {
		t.Fatalf("esperava config da impressora")
	}
	if pc.Margens.TopoMM != 4 || pc.Margens.BaseMM != 8 {
		t.Fatalf("margens inesperadas: %+v", pc.Margens)
	}
	if got := pc.Colunas["80mm"]; got != 42 {
		t.Fatalf("colunas 80mm inesperadas: %d", got)
	}
	if got := pc.Colunas["56mm"]; got != 29 {
		t.Fatalf("colunas 56mm inesperadas: %d", got)
	}

	l := m.GetLogo()
	if !l.Habilitado || l.Arquivo != "logo.png" || l.LarguraMM != 60 || l.Alinhamento != "center" || l.Transparencia != 80 {
		t.Fatalf("logo inesperado: %+v", l)
	}

	c := m.GetConferenciaConfig()
	if c.Fonte != "pequena" || c.Delimitador != "*" || c.MensagemFinal != "Obrigado!" || c.Vias != 3 {
		t.Fatalf("conferÃªncia inesperada: %+v", c)
	}
}

func TestEffectiveColsByModelo(t *testing.T) {
	m := NewManager(log.New(io.Discard, "", 0))
	m.printers = map[string]PrinterConfig{
		"COZINHA (Windows)": {
			Margens: PrinterMargins{EsquerdaMM: 2, DireitaMM: 2},
		},
	}

	if got := m.EffectiveColsByModelo("COZINHA (Windows)", "80mm"); got != 45 {
		t.Fatalf("esperava 45 colunas em 80mm com margens, got: %d", got)
	}
	if got := m.EffectiveColsByModelo("COZINHA (Windows)", "56mm"); got != 29 {
		t.Fatalf("esperava 29 colunas em 56mm com margens, got: %d", got)
	}
	if got := m.EffectiveColsByModelo("", "56mm"); got != 32 {
		t.Fatalf("esperava fallback 32 colunas em 56mm sem impressora, got: %d", got)
	}
}

func TestConfiguredColsByModeloAndEnsurePrinterCols(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	m := NewManager(log.New(io.Discard, "", 0))
	m.path = path

	if err := m.EnsurePrinterCols("EPSON 01", "80mm", 47); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if got, ok := m.ConfiguredColsByModelo("EPSON 01", "80mm"); !ok || got != 47 {
		t.Fatalf("colunas configuradas inesperadas: got=%d ok=%v", got, ok)
	}

	if err := m.SetPrinterCols("EPSON 01", "80mm", 43); err != nil {
		t.Fatalf("set cols: %v", err)
	}
	if got := m.EffectiveColsByModelo("EPSON 01", "80mm"); got != 43 {
		t.Fatalf("esperava override de 43 colunas, got: %d", got)
	}
}

func TestSetConferenciaConfig_NormalizesAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	m := NewManager(log.New(io.Discard, "", 0))
	m.path = path

	if err := m.SetConferenciaConfig(ConferenciaConfig{
		Fonte:         "  GRANDE  ",
		Delimitador:   " = ",
		MensagemFinal: " Obrigado pela preferencia! ",
		Vias:          2,
	}); err != nil {
		t.Fatalf("set conferencia: %v", err)
	}

	got := m.GetConferenciaConfig()
	if got.Fonte != "grande" || got.Delimitador != "=" || got.MensagemFinal != "Obrigado pela preferencia!" || got.Vias != 2 {
		t.Fatalf("conferÃªncia normalizada inesperada: %+v", got)
	}
}

func TestValidateConferenciaConfig_RejectsMultiCharDelimiter(t *testing.T) {
	err := ValidateConferenciaConfig(ConferenciaConfig{Fonte: "normal", Delimitador: "**"})
	if err == nil {
		t.Fatal("esperava erro para delimitador com mais de 1 caractere")
	}
}

func TestDefaultConferenciaConfig_UsesOneCopy(t *testing.T) {
	got := normalizeConferenciaConfig(ConferenciaConfig{Fonte: "normal", Delimitador: "-", Vias: 0})
	if got.Vias != 1 {
		t.Fatalf("esperava 1 via por padrÃ£o, got=%d", got.Vias)
	}
}
