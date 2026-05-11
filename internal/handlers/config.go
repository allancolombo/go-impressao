package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/url"
	"net/http"
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
	mux.HandleFunc("/config/printer/margins", h.handleSavePrinterMargins)
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
		"base_url": baseURL,
		"printers": h.cfg.GetAllPrinters(),
		"logo":     h.cfg.GetLogo(),
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
		Printer     string `json:"printer"`
		TopoMM      int    `json:"topo_mm"`
		BaseMM      int    `json:"base_mm"`
		EsquerdaMM  int    `json:"esquerda_mm"`
		DireitaMM   int    `json:"direita_mm"`
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
    input { width: 100%; padding: 10px 12px; font-size: 14px; }
    button { padding: 10px 14px; font-size: 14px; cursor: pointer; }
    .row { display: flex; gap: 12px; align-items: center; margin: 12px 0; flex-wrap: wrap; }
    .ok { color: #0a7a0a; }
    .err { color: #b00020; }
    code { font-family: ui-monospace, Menlo, Consolas, monospace; }
    .card { border: 1px solid #e8e8e8; border-radius: 12px; padding: 14px; margin: 14px 0; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    th, td { padding: 10px 8px; border-bottom: 1px solid #eee; text-align: left; }
    th { font-weight: 700; background: #fafafa; }
    .muted { color: #6b7280; font-size: 13px; }
    .grid4 { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
    .grid2 { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
    select { width: 100%; padding: 10px 12px; font-size: 14px; }
    input[type="file"]{ padding: 6px 0; }
    img { max-width: 320px; max-height: 120px; display: block; border: 1px solid #eee; border-radius: 10px; padding: 8px; background: #fff; }
    @media (max-width: 820px) { .grid4 { grid-template-columns: repeat(2, minmax(0, 1fr)); } .grid2 { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <h1>Configuração</h1>
  <div class="card">
    <h2 style="margin:0 0 8px 0; font-size:16px">URL principal</h2>
    <p class="muted">Precisa responder <code>GET /impressao/padrao</code> com status 200.</p>
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
    <h2 style="margin:0 0 8px 0; font-size:16px">Margens por impressora</h2>
    <p class="muted">Valores em milímetros (mm). Aplicados somente na impressão RAW/ESC-POS.</p>
    <div class="row">
      <label style="flex:1; min-width: 260px">
        Impressora (Windows):
        <input id="mPrinter" placeholder="Ex.: COZINHA (Windows)" />
      </label>
    </div>
    <div class="grid4">
      <label>Topo (mm)<input id="mTopo" type="number" min="0" max="100" value="0" /></label>
      <label>Base (mm)<input id="mBase" type="number" min="0" max="100" value="0" /></label>
      <label>Esquerda (mm)<input id="mEsq" type="number" min="0" max="50" value="0" /></label>
      <label>Direita (mm)<input id="mDir" type="number" min="0" max="50" value="0" /></label>
    </div>
    <div class="row">
      <button id="btnSalvarMargens" type="button">Salvar margens</button>
      <div id="msgMargens" class=""></div>
    </div>
    <div class="row" style="width:100%">
      <table>
        <thead>
          <tr><th>Impressora</th><th>Topo</th><th>Base</th><th>Esq.</th><th>Dir.</th></tr>
        </thead>
        <tbody id="tblPrinters"></tbody>
      </table>
    </div>
  </div>

  <div class="card">
    <h2 style="margin:0 0 8px 0; font-size:16px">Logo da impressão</h2>
    <p class="muted">Upload do logo (PNG/JPG/GIF) e ajustes. Transparência 100 = opaco.</p>
    <div class="grid2">
      <div>
        <img id="logoPreview" alt="Prévia do logo" style="display:none" />
        <div class="muted" id="logoHint">Nenhum logo configurado.</div>
      </div>
      <div>
        <div class="row">
          <label style="width:100%">Arquivo<input id="logoFile" type="file" accept=".png,.jpg,.jpeg,.gif" /></label>
        </div>
        <div class="grid2">
          <label>Habilitado
            <select id="logoEnabled">
              <option value="true">Sim</option>
              <option value="false">Não</option>
            </select>
          </label>
          <label>Largura (mm)
            <input id="logoWidth" type="number" min="0" max="80" value="0" />
          </label>
          <label>Alinhamento
            <select id="logoAlign">
              <option value="left">Esquerda</option>
              <option value="center">Centro</option>
              <option value="right">Direita</option>
            </select>
          </label>
          <label>Transparência (0-100)
            <input id="logoTransp" type="number" min="0" max="100" value="100" />
          </label>
        </div>
        <div class="row">
          <button id="btnSalvarLogo" type="button">Salvar logo</button>
          <div id="msgLogo" class=""></div>
        </div>
      </div>
    </div>
  </div>

<script>
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

async function loadSettings() {
  const res = await fetch("/config/settings");
  const data = await res.json().catch(() => ({}));
  if (!res.ok) return;

  const tbl = document.getElementById("tblPrinters");
  tbl.innerHTML = "";
  const printers = data.printers || {};
  Object.keys(printers).sort().forEach(name => {
    const m = (printers[name] && printers[name].margens) || {};
    const tr = document.createElement("tr");
    tr.innerHTML = "<td>"+name+"</td><td>"+(m.topo_mm ?? 0)+"</td><td>"+(m.base_mm ?? 0)+"</td><td>"+(m.esquerda_mm ?? 0)+"</td><td>"+(m.direita_mm ?? 0)+"</td>";
    tbl.appendChild(tr);
  });

  const logo = data.logo || {};
  document.getElementById("logoEnabled").value = String(logo.habilitado ?? true);
  document.getElementById("logoWidth").value = String(logo.largura_mm ?? 0);
  document.getElementById("logoAlign").value = String(logo.alinhamento || "center");
  document.getElementById("logoTransp").value = String(logo.transparencia ?? 100);

  const img = document.getElementById("logoPreview");
  const hint = document.getElementById("logoHint");
  if (logo.arquivo) {
    img.src = "/config/logo?cache=" + Date.now();
    img.style.display = "block";
    hint.style.display = "none";
  } else {
    img.style.display = "none";
    hint.style.display = "block";
  }
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

document.getElementById("btnSalvarMargens").addEventListener("click", async () => {
  setMsgEl("msgMargens", "Salvando...", "");
  const printer = document.getElementById("mPrinter").value.trim();
  const topo = Number(document.getElementById("mTopo").value || "0");
  const base = Number(document.getElementById("mBase").value || "0");
  const esq = Number(document.getElementById("mEsq").value || "0");
  const dir = Number(document.getElementById("mDir").value || "0");

  if (!printer) { setMsgEl("msgMargens", "Informe o nome da impressora.", "err"); return; }

  try {
    await postJson("/config/printer/margins", { printer: printer, topo_mm: topo, base_mm: base, esquerda_mm: esq, direita_mm: dir });
    setMsgEl("msgMargens", "Margens salvas com sucesso.", "ok");
    loadSettings();
  } catch (e) {
    setMsgEl("msgMargens", "Erro: " + e.message, "err");
  }
});

document.getElementById("btnSalvarLogo").addEventListener("click", async () => {
  setMsgEl("msgLogo", "Salvando...", "");
  const file = document.getElementById("logoFile").files[0];
  const enabled = document.getElementById("logoEnabled").value;
  const width = document.getElementById("logoWidth").value;
  const align = document.getElementById("logoAlign").value;
  const transp = document.getElementById("logoTransp").value;

  const fd = new FormData();
  fd.append("habilitado", enabled);
  fd.append("largura_mm", width);
  fd.append("alinhamento", align);
  fd.append("transparencia", transp);
  if (file) fd.append("file", file, file.name);

  try {
    const res = await fetch("/config/logo", { method: "POST", body: fd });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.erro || "Falha ao salvar logo");
    setMsgEl("msgLogo", "Logo salvo com sucesso.", "ok");
    document.getElementById("logoFile").value = "";
    loadSettings();
  } catch (e) {
    setMsgEl("msgLogo", "Erro: " + e.message, "err");
  }
});

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
