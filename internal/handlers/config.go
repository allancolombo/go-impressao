package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/goopedir/go-impressao/internal/config"
)

type ConfigHandler struct {
	logger *log.Logger
	cfg    *config.Manager
}

func NewConfigHandler(logger *log.Logger, cfg *config.Manager) *ConfigHandler {
	return &ConfigHandler{logger: logger, cfg: cfg}
}

func (h *ConfigHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/config", h.handleConfigPage)
	mux.HandleFunc("/config/status", h.handleStatus)
	mux.HandleFunc("/config/settings", h.handleSettings)
	mux.HandleFunc("/config/test", h.handleTest)
	mux.HandleFunc("/config/save", h.handleSave)
	mux.HandleFunc("/config/conferencia", h.handleSaveConferencia)
	mux.HandleFunc("/config/printer/drivers", h.handlePrinterDrivers)
	mux.HandleFunc("/config/printer/config", h.handleSavePrinterConfig)
	mux.HandleFunc("/config/printer/margins", h.handleSavePrinterMargins)
	mux.HandleFunc("/config/printer/cols", h.handleSavePrinterCols)
	mux.HandleFunc("/config/logo", h.handleLogo)
}

func (h *ConfigHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	baseURL, ok := h.cfg.GetBaseURL()
	resp := map[string]any{
		"configurado": ok,
	}
	if ok {
		resp["base_url"] = baseURL
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *ConfigHandler) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	baseURL, _ := h.cfg.GetBaseURL()
	resp := map[string]any{
		"base_url":    baseURL,
		"printers":    h.cfg.GetAllPrinters(),
		"logo":        h.cfg.GetLogo(),
		"conferencia": h.cfg.GetConferenciaConfig(),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *ConfigHandler) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	req, err := readConfigRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.logger.Printf("config: testando base_url=%q", req.BaseURL)
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	if err := config.TestBaseURL(ctx, req.BaseURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":   false,
			"erro": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

func (h *ConfigHandler) handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	req, err := readConfigRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.logger.Printf("config: salvando base_url=%q", req.BaseURL)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := h.cfg.ValidateAndSave(ctx, req.BaseURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":   false,
			"erro": err.Error(),
		})
		return
	}

	baseURL, _ := h.cfg.GetBaseURL()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"base_url": baseURL,
	})
}

func (h *ConfigHandler) handleSaveConferencia(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
		return
	}
	defer r.Body.Close()

	var req struct {
		Fonte         string `json:"fonte"`
		Delimitador   string `json:"delimitador"`
		MensagemFinal string `json:"mensagem_final"`
		Vias          int    `json:"vias"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido: verifique a sintaxe")
		return
	}

	cfg := config.ConferenciaConfig{
		Fonte:         strings.TrimSpace(req.Fonte),
		Delimitador:   strings.TrimSpace(req.Delimitador),
		MensagemFinal: strings.TrimSpace(req.MensagemFinal),
		Vias:          req.Vias,
	}
	if err := h.cfg.SetConferenciaConfig(cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "erro": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"conferencia": h.cfg.GetConferenciaConfig(),
	})
}

func (h *ConfigHandler) handleSavePrinterMargins(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
		return
	}
	defer r.Body.Close()

	var req struct {
		Printer    string `json:"printer"`
		TopoMM     int    `json:"topo_mm"`
		BaseMM     int    `json:"base_mm"`
		EsquerdaMM int    `json:"esquerda_mm"`
		DireitaMM  int    `json:"direita_mm"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido: verifique a sintaxe")
		return
	}

	req.Printer = strings.TrimSpace(req.Printer)
	if err := h.cfg.SetPrinterMargins(req.Printer, config.PrinterMargins{
		TopoMM:     req.TopoMM,
		BaseMM:     req.BaseMM,
		EsquerdaMM: req.EsquerdaMM,
		DireitaMM:  req.DireitaMM,
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "erro": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *ConfigHandler) handlePrinterDrivers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	drivers, err := h.fetchPrinterDrivers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "erro": err.Error(), "items": []any{}})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": drivers})
}

func (h *ConfigHandler) handleSavePrinterConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
		return
	}
	defer r.Body.Close()

	var req struct {
		Printer    string `json:"printer"`
		Modelo     string `json:"modelo"`
		Cols       int    `json:"cols"`
		TopoMM     int    `json:"topo_mm"`
		BaseMM     int    `json:"base_mm"`
		EsquerdaMM int    `json:"esquerda_mm"`
		DireitaMM  int    `json:"direita_mm"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido: verifique a sintaxe")
		return
	}

	req.Printer = strings.TrimSpace(req.Printer)
	req.Modelo = strings.TrimSpace(req.Modelo)

	if err := h.cfg.SetPrinterMargins(req.Printer, config.PrinterMargins{
		TopoMM:     req.TopoMM,
		BaseMM:     req.BaseMM,
		EsquerdaMM: req.EsquerdaMM,
		DireitaMM:  req.DireitaMM,
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "erro": err.Error()})
		return
	}
	if err := h.cfg.SetPrinterCols(req.Printer, req.Modelo, req.Cols); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "erro": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *ConfigHandler) handleSavePrinterCols(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
		return
	}
	defer r.Body.Close()

	var req struct {
		Printer string `json:"printer"`
		Modelo  string `json:"modelo"`
		Cols    int    `json:"cols"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido: verifique a sintaxe")
		return
	}

	req.Printer = strings.TrimSpace(req.Printer)
	req.Modelo = strings.TrimSpace(req.Modelo)
	if err := h.cfg.SetPrinterCols(req.Printer, req.Modelo, req.Cols); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "erro": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *ConfigHandler) handleLogo(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		cfg := h.cfg.GetLogo()
		if strings.TrimSpace(cfg.Arquivo) == "" {
			http.NotFound(w, r)
			return
		}

		path := filepath.Join(h.cfg.ConfigDir(), cfg.Arquivo)
		b, err := os.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ext := strings.ToLower(filepath.Ext(cfg.Arquivo))
		switch ext {
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".jpg", ".jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".gif":
			w.Header().Set("Content-Type", "image/gif")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	ct := r.Header.Get("Content-Type")
	if !strings.Contains(ct, "multipart/form-data") {
		writeJSONError(w, http.StatusBadRequest, "content-type deve ser multipart/form-data")
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeJSONError(w, http.StatusBadRequest, "falha ao processar upload")
		return
	}

	habilitado := parseBoolForm(r.FormValue("habilitado"))
	larguraMM := parseIntForm(r.FormValue("largura_mm"))
	alinhamento := strings.TrimSpace(r.FormValue("alinhamento"))
	transp := parseIntForm(r.FormValue("transparencia"))

	var fileName string
	f, hdr, err := r.FormFile("file")
	if err == nil {
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, 10<<20))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "não foi possível ler o arquivo")
			return
		}
		name := ""
		if hdr != nil {
			name = hdr.Filename
		}
		fn, err := h.cfg.SaveLogoFile(name, data)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "erro": err.Error()})
			return
		}
		fileName = fn
	}

	cfg := h.cfg.GetLogo()
	cfg.Habilitado = habilitado
	if larguraMM >= 0 {
		cfg.LarguraMM = larguraMM
	}
	if alinhamento != "" {
		cfg.Alinhamento = alinhamento
	}
	if transp >= 0 {
		cfg.Transparencia = transp
	}
	if fileName != "" {
		cfg.Arquivo = fileName
	}

	if err := h.cfg.SetLogoConfig(cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "erro": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *ConfigHandler) handleConfigPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	baseURL, _ := h.cfg.GetBaseURL()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, renderConfigHTML(baseURL))
}

type printerDriverItem struct {
	ID     int    `json:"id"`
	Driver string `json:"driver"`
}

func (h *ConfigHandler) fetchPrinterDrivers(ctx context.Context) ([]printerDriverItem, error) {
	baseURL, ok := h.cfg.GetBaseURL()
	if !ok || strings.TrimSpace(baseURL) == "" {
		return nil, errorString("configure a URL principal antes de carregar os drivers")
	}

	target := strings.TrimRight(baseURL, "/") + "/v1/impressora/servidor/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errorString("não foi possível carregar os drivers da impressora no servidor")
	}

	var items []printerDriverItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, errorString("resposta inválida ao carregar drivers da impressora")
	}

	out := make([]printerDriverItem, 0, len(items))
	for _, item := range items {
		item.Driver = strings.TrimSpace(item.Driver)
		if item.Driver == "" {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

type configRequest struct {
	BaseURL string `json:"base_url"`
}

func readConfigRequest(r *http.Request) (configRequest, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return configRequest{}, err
	}
	defer r.Body.Close()

	var req configRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return configRequest{}, errorString("JSON inválido: verifique a sintaxe")
	}
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	if req.BaseURL == "" {
		return configRequest{}, errorString(`campo "base_url" é obrigatório`)
	}
	return req, nil
}

type errorString string

func (e errorString) Error() string { return string(e) }

func renderConfigHTML(baseURL string) string {
	esc := htmlEscape(baseURL)
	return `<!doctype html>
<html lang="pt-br">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Configuração</title>
  <style>
    body { font-family: system-ui, -apple-system, Segoe UI, Roboto, Arial, sans-serif; margin: 24px; max-width: 980px; }
    input, textarea { width: 100%; padding: 10px 12px; font-size: 14px; box-sizing: border-box; }
    button { padding: 10px 14px; font-size: 14px; cursor: pointer; }
    .row { display: flex; gap: 12px; align-items: center; margin: 12px 0; flex-wrap: wrap; }
    .ok { color: #0a7a0a; }
    .err { color: #b00020; }
    code { font-family: ui-monospace, Menlo, Consolas, monospace; }
    .card { border: 1px solid #e8e8e8; border-radius: 12px; padding: 14px; margin: 14px 0; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    th, td { padding: 10px 8px; border-bottom: 1px solid #eee; text-align: left; }
    th { font-weight: 700; background: #fafafa; }
    tbody tr.row-clickable { cursor: pointer; }
    tbody tr.row-clickable:hover { background: #fafafa; }
    .muted { color: #6b7280; font-size: 13px; }
    .grid4 { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
    .grid2 { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
    select { width: 100%; padding: 10px 12px; font-size: 14px; }
    textarea { min-height: 90px; resize: vertical; }
    input[type="file"]{ padding: 6px 0; }
    img { max-width: 320px; max-height: 120px; display: block; border: 1px solid #eee; border-radius: 10px; padding: 8px; background: #fff; }
    @media (max-width: 820px) { .grid4 { grid-template-columns: repeat(2, minmax(0, 1fr)); } .grid2 { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <h1>Configuração</h1>
  <div class="card">
    <h2 style="margin:0 0 8px 0; font-size:16px">URL principal</h2>
    <div class="row">
      <label style="width:100%">
        URL base:
        <input id="baseUrl" placeholder="http://localhost:2121" value="` + esc + `" />
      </label>
    </div>
    <div class="row">
      <button id="btnTestar" type="button">Testar</button>
      <button id="btnSalvar" type="button">Salvar</button>
      <div id="msg" class=""></div>
    </div>
  </div>

  <div class="card">
    <h2 style="margin:0 0 8px 0; font-size:16px">Configuração por impressora</h2>
    <div class="grid4">
      <label style="grid-column: span 2">Impressora (Windows)
        <select id="pPrinter">
          <option value="">Selecione...</option>
        </select>
      </label>
      <label>Modelo
        <select id="pModelo">
          <option value="80mm">80mm</option>
          <option value="58mm">58mm</option>
          <option value="56mm">56mm</option>
        </select>
      </label>
      <label>Colunas<input id="pCols" type="number" min="20" max="48" value="47" /></label>
    </div>
    <div class="grid4">
      <label>Topo (mm)<input id="pTopo" type="number" min="0" max="100" value="0" /></label>
      <label>Base (mm)<input id="pBase" type="number" min="0" max="100" value="0" /></label>
      <label>Esquerda (mm)<input id="pEsq" type="number" min="0" max="50" value="0" /></label>
      <label>Direita (mm)<input id="pDir" type="number" min="0" max="50" value="0" /></label>
    </div>
    <div class="row">
      <button id="btnSalvarPrinterConfig" type="button">Salvar configuração</button>
      <div id="msgPrinterConfig" class=""></div>
    </div>
    <div class="row" style="width:100%">
      <table>
        <thead>
          <tr><th>Impressora</th><th>Modelo</th><th>Colunas</th><th>Topo</th><th>Base</th><th>Esq.</th><th>Dir.</th></tr>
        </thead>
        <tbody id="tblPrinterConfig"></tbody>
      </table>
    </div>
  </div>

  <div class="card">
    <h2 style="margin:0 0 8px 0; font-size:16px">ConferÃªncia</h2>
    <div class="grid2">
      <label>Tamanho da fonte
        <select id="cFonte">
          <option value="normal">Normal</option>
          <option value="pequena">Pequena</option>
          <option value="grande">Grande</option>
        </select>
      </label>
      <label>Delimitador
        <input id="cDelimitador" maxlength="1" placeholder="-" value="-" />
      </label>
    </div>
    <div class="row">
      <label style="width:180px">Vias
        <input id="cVias" type="number" min="1" step="1" value="1" />
      </label>
    </div>
    <div class="row">
      <label style="width:100%">Mensagem final
        <textarea id="cMensagem" placeholder="Ex.: Obrigado pela preferÃªncia!"></textarea>
      </label>
    </div>
    <div class="row">
      <button id="btnSalvarConferencia" type="button">Salvar conferÃªncia</button>
      <div id="msgConferencia" class=""></div>
    </div>
  </div>


<script>
let currentPrinters = {};
let currentDrivers = [];

function normalizeStaticTexts() {
  document.title = "Configuração";
  const h1 = document.querySelector("h1");
  if (h1) h1.textContent = "Configuração";
  const cards = document.querySelectorAll(".card h2");
  if (cards[1]) cards[1].textContent = "Configuração por impressora";
  if (cards[2]) cards[2].textContent = "Conferência";
  const cDelimitador = document.getElementById("cDelimitador");
  if (cDelimitador) cDelimitador.maxLength = 1;
  const cMensagem = document.getElementById("cMensagem");
  if (cMensagem) cMensagem.placeholder = "Ex.: Obrigado pela preferência!";
  const btnSalvarConferencia = document.getElementById("btnSalvarConferencia");
  if (btnSalvarConferencia) btnSalvarConferencia.textContent = "Salvar conferência";
  const btnSalvarPrinterConfig = document.getElementById("btnSalvarPrinterConfig");
  if (btnSalvarPrinterConfig) btnSalvarPrinterConfig.textContent = "Salvar configuração";
}

function setMsg(text, kind) {
  const el = document.getElementById("msg");
  el.className = kind || "";
  el.textContent = text || "";
}

async function postJson(path, payload) {
  const res = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.erro || "Falha na requisição");
  }
  return data;
}

function setMsgEl(id, text, kind) {
  const el = document.getElementById(id);
  el.className = kind || "";
  el.textContent = text || "";
}

function uniqSorted(values) {
  return Array.from(new Set(values.filter(Boolean))).sort((a, b) => a.localeCompare(b, "pt-BR"));
}

function renderPrinterOptions() {
  const select = document.getElementById("pPrinter");
  const saved = Object.keys(currentPrinters || {});
  const server = (currentDrivers || []).map(it => (it && it.driver) || "").filter(Boolean);
  const names = uniqSorted(saved.concat(server));
  const current = select.value;

  select.innerHTML = '<option value="">Selecione...</option>';
  names.forEach(name => {
    const opt = document.createElement("option");
    opt.value = name;
    opt.textContent = name;
    select.appendChild(opt);
  });

  if (current && names.includes(current)) {
    select.value = current;
  } else if (!select.value && names.length > 0) {
    select.value = names[0];
  }
}

function syncPrinterForm() {
  const printer = document.getElementById("pPrinter").value;
  const modelo = document.getElementById("pModelo").value;
  const cfg = (currentPrinters && currentPrinters[printer]) || {};
  const m = cfg.margens || {};
  const colunas = cfg.colunas || {};

  document.getElementById("pTopo").value = String(m.topo_mm ?? 0);
  document.getElementById("pBase").value = String(m.base_mm ?? 0);
  document.getElementById("pEsq").value = String(m.esquerda_mm ?? 0);
  document.getElementById("pDir").value = String(m.direita_mm ?? 0);

  let fallbackCols = 47;
  if (modelo === "56mm" || modelo === "58mm") fallbackCols = 31;
  document.getElementById("pCols").value = String(colunas[modelo] ?? fallbackCols);
}

function selectPrinterConfig(printer, modelo) {
  const printerEl = document.getElementById("pPrinter");
  const modeloEl = document.getElementById("pModelo");
  if (printer) {
    printerEl.value = printer;
  }
  if (modelo) {
    modeloEl.value = modelo;
  }
  syncPrinterForm();
  printerEl.scrollIntoView({ behavior: "smooth", block: "center" });
  printerEl.focus();
}

async function loadDrivers() {
  const res = await fetch("/config/printer/drivers");
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    currentDrivers = [];
    setMsgEl("msgPrinterConfig", data.erro || "Não foi possível carregar os drivers.", "err");
    renderPrinterOptions();
    syncPrinterForm();
    return;
  }
  currentDrivers = data.items || [];
  renderPrinterOptions();
  syncPrinterForm();
}

async function loadSettings() {
  const res = await fetch("/config/settings");
  const data = await res.json().catch(() => ({}));
  if (!res.ok) return;

  const tbl = document.getElementById("tblPrinterConfig");
  tbl.innerHTML = "";
  currentPrinters = data.printers || {};

  Object.keys(currentPrinters).sort().forEach(name => {
    const cfg = currentPrinters[name] || {};
    const m = cfg.margens || {};
    const colunas = cfg.colunas || {};
    const modelos = Object.keys(colunas).sort();
    if (modelos.length === 0) {
      const tr = document.createElement("tr");
      tr.className = "row-clickable";
      tr.innerHTML = "<td>"+name+"</td><td>-</td><td>-</td><td>"+(m.topo_mm ?? 0)+"</td><td>"+(m.base_mm ?? 0)+"</td><td>"+(m.esquerda_mm ?? 0)+"</td><td>"+(m.direita_mm ?? 0)+"</td>";
      tr.addEventListener("click", () => selectPrinterConfig(name, document.getElementById("pModelo").value));
      tbl.appendChild(tr);
      return;
    }
    modelos.forEach(modelo => {
      const tr = document.createElement("tr");
      tr.className = "row-clickable";
      tr.innerHTML = "<td>"+name+"</td><td>"+modelo+"</td><td>"+(colunas[modelo] ?? "-")+"</td><td>"+(m.topo_mm ?? 0)+"</td><td>"+(m.base_mm ?? 0)+"</td><td>"+(m.esquerda_mm ?? 0)+"</td><td>"+(m.direita_mm ?? 0)+"</td>";
      tr.addEventListener("click", () => selectPrinterConfig(name, modelo));
      tbl.appendChild(tr);
    });
  });

  renderPrinterOptions();
  syncPrinterForm();

  const conferencia = data.conferencia || {};
  document.getElementById("cFonte").value = conferencia.fonte || "normal";
  document.getElementById("cDelimitador").value = (conferencia.delimitador ?? "-").slice(0, 1);
  document.getElementById("cVias").value = String(conferencia.vias ?? 1);
  document.getElementById("cMensagem").value = conferencia.mensagem_final || "";

  await loadDrivers();
}

document.getElementById("btnTestar").addEventListener("click", async () => {
  setMsg("Testando...", "");
  const baseUrl = document.getElementById("baseUrl").value.trim();
  try {
    await postJson("/config/test", { base_url: baseUrl });
    setMsg("OK: conexão e endpoint /impressao/padrao validado.", "ok");
  } catch (e) {
    setMsg("Erro: " + e.message, "err");
  }
});

document.getElementById("btnSalvar").addEventListener("click", async () => {
  setMsg("Validando e salvando...", "");
  const baseUrl = document.getElementById("baseUrl").value.trim();
  try {
    const data = await postJson("/config/save", { base_url: baseUrl });
    setMsg("Salvo com sucesso: " + (data.base_url || baseUrl), "ok");
  } catch (e) {
    setMsg("Erro: " + e.message, "err");
  }
});

document.getElementById("pPrinter").addEventListener("change", syncPrinterForm);
document.getElementById("pModelo").addEventListener("change", syncPrinterForm);

document.getElementById("btnSalvarPrinterConfig").addEventListener("click", async () => {
  setMsgEl("msgPrinterConfig", "Salvando...", "");
  const printer = document.getElementById("pPrinter").value.trim();
  const modelo = document.getElementById("pModelo").value.trim();
  const cols = Number(document.getElementById("pCols").value || "0");
  const topo = Number(document.getElementById("pTopo").value || "0");
  const base = Number(document.getElementById("pBase").value || "0");
  const esq = Number(document.getElementById("pEsq").value || "0");
  const dir = Number(document.getElementById("pDir").value || "0");

  if (!printer) { setMsgEl("msgPrinterConfig", "Selecione a impressora.", "err"); return; }

  try {
    await postJson("/config/printer/config", { printer: printer, modelo: modelo, cols: cols, topo_mm: topo, base_mm: base, esquerda_mm: esq, direita_mm: dir });
    setMsgEl("msgPrinterConfig", "Configuração salva com sucesso.", "ok");
    await loadSettings();
  } catch (e) {
    setMsgEl("msgPrinterConfig", "Erro: " + e.message, "err");
  }
});

document.getElementById("btnSalvarConferencia").addEventListener("click", async () => {
  setMsgEl("msgConferencia", "Salvando...", "");
  const fonte = document.getElementById("cFonte").value.trim();
  const delimitador = document.getElementById("cDelimitador").value.trim().slice(0, 1);
  const vias = Number(document.getElementById("cVias").value || "1");
  const mensagemFinal = document.getElementById("cMensagem").value.trim();

  try {
    const data = await postJson("/config/conferencia", { fonte: fonte, delimitador: delimitador, vias: vias, mensagem_final: mensagemFinal });
    const conferencia = data.conferencia || {};
    document.getElementById("cFonte").value = conferencia.fonte || "normal";
    document.getElementById("cDelimitador").value = (conferencia.delimitador ?? "-").slice(0, 1);
    document.getElementById("cVias").value = String(conferencia.vias ?? 1);
    document.getElementById("cMensagem").value = conferencia.mensagem_final || "";
    setMsgEl("msgConferencia", "ConfiguraÃ§Ã£o da conferÃªncia salva com sucesso.", "ok");
    setMsgEl("msgConferencia", "Configuração da conferência salva com sucesso.", "ok");
  } catch (e) {
    setMsgEl("msgConferencia", "Erro: " + e.message, "err");
  }
});

normalizeStaticTexts();
loadSettings();
</script>
</body>
</html>`
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

func parseIntForm(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return -1
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}

func parseBoolForm(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "1" || s == "true" || s == "sim" || s == "yes" || s == "on"
}

func normalizeURL(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return strings.TrimSpace(s)
	}
	u.Fragment = ""
	u.RawQuery = ""
	return u.String()
}
