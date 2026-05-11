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
    "COZINHA (Windows)": { "margens": { "topo_mm": 4, "base_mm": 8, "esquerda_mm": 2, "direita_mm": 2 } }
  },
  "logo": { "habilitado": true, "arquivo": "logo.png", "largura_mm": 60, "alinhamento": "center", "transparencia": 80 }
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

	l := m.GetLogo()
	if !l.Habilitado || l.Arquivo != "logo.png" || l.LarguraMM != 60 || l.Alinhamento != "center" || l.Transparencia != 80 {
		t.Fatalf("logo inesperado: %+v", l)
	}
}

