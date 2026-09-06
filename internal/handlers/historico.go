package handlers

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/goopedir/go-impressao/internal/services"
)

type HistoricoHandler struct {
	logger    *log.Logger
	history   *services.HistoryStore
	jobStore  *services.JobStore
	formatter *services.Formatter
	printSvc  *services.PrintService
}

func NewHistoricoHandler(logger *log.Logger, history *services.HistoryStore, jobStore *services.JobStore, formatter *services.Formatter, printSvc *services.PrintService) *HistoricoHandler {
	return &HistoricoHandler{
		logger:    logger,
		history:   history,
		jobStore:  jobStore,
		formatter: formatter,
		printSvc:  printSvc,
	}
}

func (h *HistoricoHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/historico", h.handleGlobalPage)
	mux.HandleFunc("/historico/api", h.handleGlobalList)
	mux.HandleFunc("/historico/api/", h.handleGlobalItem)

	mux.HandleFunc("/impressao/cozinha/historico", h.handlePage)
	mux.HandleFunc("/impressao/cozinha/historico/api", h.handleList)
	mux.HandleFunc("/impressao/cozinha/historico/api/", h.handleItem)
}

func (h *HistoricoHandler) handleGlobalPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, renderHistoricoGlobalHTML())
}

func (h *HistoricoHandler) handleGlobalList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	filter, cursor, limit, err := parseHistoryQuery(r.URL)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	items, next := h.history.List(filter, cursor, limit)
	resp := map[string]any{
		"items": items,
	}
	if next != nil {
		resp["next_cursor"] = *next
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *HistoricoHandler) handleGlobalItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/historico/api/")
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		writeJSONError(w, http.StatusBadRequest, "id não informado")
		return
	}

	parts := strings.Split(path, "/")
	id := strings.TrimSpace(parts[0])
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "id não informado")
		return
	}

	if len(parts) == 1 && r.Method == http.MethodGet {
		rec, ok := h.history.Get(id)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "registro não encontrado")
			return
		}
		if strings.TrimSpace(rec.Canal) == "" {
			rec.Canal = "cozinha"
		}
		writeJSON(w, http.StatusOK, rec)
		return
	}

	writeJSONError(w, http.StatusNotFound, "rota não encontrada")
}

func (h *HistoricoHandler) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, renderHistoricoHTML())
}

func (h *HistoricoHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	filter, cursor, limit, err := parseHistoryQuery(r.URL)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	t := "cozinha"
	filter.Canal = &t

	items, next := h.history.List(filter, cursor, limit)
	resp := map[string]any{
		"items": items,
	}
	if next != nil {
		resp["next_cursor"] = *next
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *HistoricoHandler) handleItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/impressao/cozinha/historico/api/")
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		writeJSONError(w, http.StatusBadRequest, "id não informado")
		return
	}

	parts := strings.Split(path, "/")
	id := strings.TrimSpace(parts[0])
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "id não informado")
		return
	}

	if len(parts) == 1 && r.Method == http.MethodGet {
		rec, ok := h.history.Get(id)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "registro não encontrado")
			return
		}
		if strings.TrimSpace(rec.Canal) == "" {
			rec.Canal = "cozinha"
		}
		writeJSON(w, http.StatusOK, rec)
		return
	}

	if len(parts) == 2 && parts[1] == "reimprimir" {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
			return
		}

		rec, ok := h.history.Get(id)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "registro não encontrado")
			return
		}
		if strings.TrimSpace(rec.Canal) != "" && strings.TrimSpace(strings.ToLower(rec.Canal)) != "cozinha" {
			writeJSONError(w, http.StatusConflict, "reimpressão disponível apenas para cozinha")
			return
		}

		preview := h.formatter.FormatComandaCozinha(rec.Payload, time.Now())
		job := h.jobStore.Create(rec.Payload, preview)

		h.logger.Printf("reimpressao: historico_id=%s novo_job=%s numero=%d impressora_windows=%q", rec.ID, job.ID, rec.Numero, rec.Payload.Driver)

		if err := h.printSvc.StartPrint(job.ID); err != nil {
			_ = h.jobStore.Update(job.ID, func(j *services.Job) {
				j.Status = services.StatusErro
				j.ErrorPublic = err.Error()
			})
			writeJSON(w, http.StatusConflict, map[string]any{
				"ok":     false,
				"erro":   err.Error(),
				"job_id": job.ID,
			})
			return
		}

		writeJSON(w, http.StatusAccepted, map[string]any{
			"ok":          true,
			"job_id":      job.ID,
			"status":      job.Status,
			"preview_url": fmt.Sprintf("/impressao/cozinha/preview/%s", job.ID),
		})
		return
	}

	writeJSONError(w, http.StatusNotFound, "rota não encontrada")
}

func parseHistoryQuery(u *url.URL) (services.HistoryFilter, int, int, error) {
	q := u.Query()

	var f services.HistoryFilter

	if s := strings.TrimSpace(q.Get("from")); s != "" {
		t, err := parseDateTime(s)
		if err != nil {
			return f, 0, 0, errorString(`parâmetro "from" inválido (use YYYY-MM-DDTHH:MM)`)
		}
		f.From = &t
	}
	if s := strings.TrimSpace(q.Get("to")); s != "" {
		t, err := parseDateTime(s)
		if err != nil {
			return f, 0, 0, errorString(`parâmetro "to" inválido (use YYYY-MM-DDTHH:MM)`)
		}
		f.To = &t
	}
	if s := strings.TrimSpace(q.Get("numero")); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return f, 0, 0, errorString(`parâmetro "numero" inválido`)
		}
		f.Numero = &n
	}
	if s := strings.TrimSpace(q.Get("tipo")); s != "" {
		t := strings.TrimSpace(strings.ToLower(s))
		if t != "" {
			f.Canal = &t
		}
	}
	if s := strings.TrimSpace(q.Get("status")); s != "" {
		st := services.PrintStatus(s)
		if st != services.StatusPendente && st != services.StatusImprimindo && st != services.StatusImpresso && st != services.StatusErro {
			return f, 0, 0, errorString(`parâmetro "status" inválido`)
		}
		f.Status = &st
	}

	cursor, err := services.ParseCursor(q.Get("cursor"))
	if err != nil {
		return f, 0, 0, errorString(err.Error())
	}

	limit := 50
	if s := strings.TrimSpace(q.Get("limit")); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return f, 0, 0, errorString(`parâmetro "limit" inválido`)
		}
		limit = n
	}

	return f, cursor, limit, nil
}

func parseDateTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("vazio")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02T15:04", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("inválido")
}

func renderHistoricoHTML() string {
	return `<!doctype html>
<html lang="pt-br">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Histórico de Impressões - Cozinha</title>
  <style>
    :root { --line:#e8e8e8; --muted:#6b7280; --ok:#0a7a0a; --err:#b00020; }
    body { margin:0; font-family: system-ui, -apple-system, Segoe UI, Roboto, Arial, sans-serif; background:#fff; color:#111; }
    header { padding: 14px 18px; border-bottom: 1px solid var(--line); position: sticky; top: 0; background: rgba(255,255,255,.96); backdrop-filter: blur(6px); z-index: 5; }
    h1 { margin: 0; font-size: 18px; }
    .filters { display:flex; gap:10px; flex-wrap: wrap; margin-top: 10px; align-items: end; }
    label { display:flex; flex-direction: column; gap: 6px; font-size: 12px; color: var(--muted); }
    input, select { padding: 10px 10px; border-radius: 10px; border: 1px solid var(--line); background: #fff; color: #111; min-width: 160px; font-size: 14px; }
    button { padding: 10px 12px; border-radius: 10px; border: 1px solid var(--line); background: #fff; color: #111; font-weight: 600; cursor: pointer; }
    button.primary { background: #111; color: #fff; border-color: #111; }
    main { padding: 12px 18px; }
    .card { border: 1px solid var(--line); border-radius: 12px; overflow: hidden; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    thead th { font-weight: 700; text-align: left; padding: 12px 10px; background: #fafafa; border-bottom: 1px solid var(--line); }
    tbody td { padding: 12px 10px; border-bottom: 1px solid #f1f1f1; vertical-align: middle; }
    tbody tr { cursor: pointer; }
    tbody tr:hover { background: #fbfbfb; }
    .prod { font-weight: 700; }
    .sub { color: var(--muted); margin-top: 3px; font-size: 12px; }
    .status { display:inline-flex; padding: 6px 10px; border-radius: 999px; border: 1px solid var(--line); font-size: 12px; }
    .s-ok { color: var(--ok); border-color: rgba(10,122,10,.25); }
    .s-err { color: var(--err); border-color: rgba(176,0,32,.25); }
    .pager { display:flex; gap: 10px; align-items: center; justify-content: flex-end; padding: 10px 12px; border-top: 1px solid var(--line); background:#fff; flex-wrap: wrap; }
    .pager .muted { color: var(--muted); font-size: 12px; }
    dialog { border: 1px solid var(--line); border-radius: 16px; padding: 0; background: #fff; color: #111; width: min(820px, 96vw); }
    .dlg-head { padding: 12px 14px; border-bottom: 1px solid var(--line); display:flex; gap: 10px; align-items: center; justify-content: space-between; }
    .dlg-body { padding: 12px 14px; display:grid; grid-template-columns: 1fr; gap: 10px; }
    pre { margin:0; padding: 12px; border-radius: 12px; border: 1px solid var(--line); background: #fafafa; overflow: auto; font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 14px; line-height: 1.25; }
    .dlg-actions { padding: 12px 14px; border-top: 1px solid var(--line); display:flex; gap: 10px; justify-content: flex-end; flex-wrap: wrap; }
    .toast { position: fixed; bottom: 14px; left: 50%; transform: translateX(-50%); background: rgba(17,17,17,.96); border: 1px solid rgba(255,255,255,.08); padding: 10px 12px; border-radius: 12px; color: #fff; max-width: 92vw; display:none; }
    @media (max-width: 820px) {
      thead th:nth-child(5), tbody td:nth-child(5) { display:none; }
    }
  </style>
</head>
<body>
  <header>
    <h1>Histórico de Impressões (Cozinha)</h1>
    <div class="filters">
      <label>De
        <input id="fFrom" type="datetime-local" />
      </label>
      <label>Até
        <input id="fTo" type="datetime-local" />
      </label>
      <label>Número
        <input id="fNumero" type="number" min="0" placeholder="Ex.: 123" />
      </label>
      <label>Status
        <select id="fStatus">
          <option value="">Todos</option>
          <option value="impresso">Impresso</option>
          <option value="erro">Erro</option>
          <option value="imprimindo">Imprimindo</option>
          <option value="pendente">Pendente</option>
        </select>
      </label>
      <button id="btnAplicar" class="primary" type="button">Aplicar</button>
      <button id="btnLimpar" type="button">Limpar</button>
    </div>
  </header>

  <main>
    <div class="card">
      <table>
        <thead>
          <tr>
            <th>Pedido</th>
            <th style="width:140px">Status</th>
            <th style="width:180px">Data/Hora</th>
            <th style="width:220px">Resumo</th>
            <th style="width:140px">Usuário</th>
          </tr>
        </thead>
        <tbody id="rows"></tbody>
      </table>
      <div class="pager">
        <span class="muted">Linhas por página:</span>
        <select id="rowsPerPage" style="min-width:80px">
          <option value="10">10</option>
          <option value="25">25</option>
          <option value="50" selected>50</option>
          <option value="100">100</option>
        </select>
        <span id="range" class="muted"></span>
        <button id="btnPrev" type="button">‹</button>
        <button id="btnNext" type="button">›</button>
      </div>
    </div>
  </main>

  <dialog id="dlg">
    <div class="dlg-head">
      <div id="dlgTitle" style="font-weight:800"></div>
      <button id="dlgClose" type="button">Fechar</button>
    </div>
    <div class="dlg-body">
      <div id="dlgMeta" style="color:var(--muted); font-size:13px"></div>
      <pre id="dlgText"></pre>
      <div id="dlgErro" style="color:var(--err); font-size:13px"></div>
    </div>
    <div class="dlg-actions">
      <button id="btnReimprimir" class="primary" type="button">Reimprimir</button>
    </div>
  </dialog>

  <div id="toast" class="toast"></div>

<script>
const rowsEl = document.getElementById("rows");
const dlg = document.getElementById("dlg");
const dlgTitle = document.getElementById("dlgTitle");
const dlgMeta = document.getElementById("dlgMeta");
const dlgText = document.getElementById("dlgText");
const dlgErro = document.getElementById("dlgErro");
const btnReimprimir = document.getElementById("btnReimprimir");
const toastEl = document.getElementById("toast");
const rangeEl = document.getElementById("range");

let cursor = 0;
let nextCursor = null;
let loading = false;
let selectedId = null;
let lastTopId = null;

function toast(msg) {
  toastEl.textContent = msg;
  toastEl.style.display = "block";
  setTimeout(() => toastEl.style.display = "none", 2400);
}

function q() {
  const p = new URLSearchParams();
  const from = document.getElementById("fFrom").value;
  const to = document.getElementById("fTo").value;
  const numero = document.getElementById("fNumero").value;
  const status = document.getElementById("fStatus").value;
  const limit = document.getElementById("rowsPerPage").value;
  if (from) p.set("from", from);
  if (to) p.set("to", to);
  if (numero) p.set("numero", numero);
  if (status) p.set("status", status);
  p.set("limit", limit || "50");
  p.set("cursor", String(cursor));
  return p;
}

function fmtDT(iso) {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const mo = String(d.getMonth()+1).padStart(2, "0");
  const yy = d.getFullYear();
  return dd + "/" + mo + "/" + yy + " " + hh + ":" + mm + ":" + ss;
}

function statusClass(s) {
  if (s === "impresso") return "status s-ok";
  if (s === "erro") return "status s-err";
  return "status";
}

function renderRow(it) {
  const tr = document.createElement("tr");
  tr.dataset.id = it.id;
  const pedido = ((it.tipo || "") + " " + (it.numero ?? "")).trim();
  tr.innerHTML =
    '<td><div class="prod">' + pedido + '</div></td>' +
    '<td><span class="' + statusClass(it.status) + '">' + (it.status || "") + '</span></td>' +
    '<td>' + fmtDT(it.occurred_at) + '</td>' +
    '<td>' + (it.resumo || "") + '</td>' +
    '<td>' + (it.usuario || "") + '</td>';
  tr.addEventListener("click", () => openDetail(it.id));
  return tr;
}

async function fetchPage() {
  if (loading) return;
  loading = true;
  rowsEl.innerHTML = '<tr><td colspan="5" style="padding:14px;color:var(--muted)">Carregando…</td></tr>';

  try {
    const p = q();
    const res = await fetch("/impressao/cozinha/historico/api?" + p.toString());
    const data = await res.json();
    if (!res.ok) throw new Error(data.erro || "Falha ao carregar histórico");

    const items = data.items || [];
    rowsEl.innerHTML = "";
    if (items.length === 0) {
      rowsEl.innerHTML = '<tr><td colspan="5" style="padding:14px;color:var(--muted)">Sem registros</td></tr>';
    } else {
      lastTopId = items[0].id;
      items.forEach(it => rowsEl.appendChild(renderRow(it)));
    }

    nextCursor = (data.next_cursor !== undefined) ? data.next_cursor : null;
    const limit = Number(document.getElementById("rowsPerPage").value || "50");
    const start = cursor + 1;
    const end = cursor + items.length;
    rangeEl.textContent = (items.length === 0) ? "" : (start + "–" + end);

    document.getElementById("btnPrev").disabled = cursor <= 0;
    document.getElementById("btnNext").disabled = nextCursor == null;
  } catch (e) {
    rowsEl.innerHTML = '<tr><td colspan="5" style="padding:14px;color:var(--muted)">Erro: ' + e.message + '</td></tr>';
    nextCursor = null;
  } finally {
    loading = false;
  }
}

async function openDetail(id) {
  selectedId = id;
  dlgErro.textContent = "";
  dlgMeta.textContent = "";
  dlgText.textContent = "";
  dlgTitle.textContent = "Carregando…";

  const res = await fetch("/impressao/cozinha/historico/api/" + encodeURIComponent(id));
  const data = await res.json();
  if (!res.ok) {
    toast(data.erro || "Registro não encontrado");
    return;
  }

  dlgTitle.textContent = ((data.tipo || "") + " " + (data.numero || "")).trim() + " • " + (data.status || "");
  dlgMeta.textContent = fmtDT(data.occurred_at) + " • " + (data.usuario || "") + " • ERP: " + (data.impressora_erp || "") + " • Windows: " + (data.impressora_windows || "");
  dlgText.textContent = data.texto_impressao || "";
  dlgErro.textContent = data.erro ? ("Erro: " + data.erro) : "";
  dlg.showModal();
}

document.getElementById("dlgClose").addEventListener("click", () => dlg.close());

btnReimprimir.addEventListener("click", async () => {
  if (!selectedId) return;
  if (!confirm("Deseja reimprimir este pedido?")) return;

  try {
    toast("Reimpressão solicitada…");
    const res = await fetch("/impressao/cozinha/historico/api/" + encodeURIComponent(selectedId) + "/reimprimir", { method: "POST" });
    const data = await res.json();
    if (!res.ok) throw new Error(data.erro || "Falha ao reimprimir");

    const jobId = data.job_id;
    for (let i = 0; i < 25; i++) {
      const sRes = await fetch("/impressao/cozinha/status/" + encodeURIComponent(jobId));
      const sData = await sRes.json();
      if (sData.status === "impresso") { toast("Reimpressão concluída com sucesso."); fetchPage(); return; }
      if (sData.status === "erro") { toast("Erro na reimpressão: " + (sData.erro || "falha")); fetchPage(); return; }
      await new Promise(r => setTimeout(r, 700));
    }
    toast("Reimpressão em andamento. Verifique o status.");
  } catch (e) {
    toast("Erro: " + e.message);
  }
});

document.getElementById("btnAplicar").addEventListener("click", () => { cursor = 0; fetchPage(); });
document.getElementById("btnLimpar").addEventListener("click", () => {
  document.getElementById("fFrom").value = "";
  document.getElementById("fTo").value = "";
  document.getElementById("fNumero").value = "";
  document.getElementById("fStatus").value = "";
  cursor = 0;
  fetchPage();
});

document.getElementById("rowsPerPage").addEventListener("change", () => { cursor = 0; fetchPage(); });
document.getElementById("btnPrev").addEventListener("click", () => {
  const limit = Number(document.getElementById("rowsPerPage").value || "50");
  cursor = Math.max(0, cursor - limit);
  fetchPage();
});
document.getElementById("btnNext").addEventListener("click", () => {
  if (nextCursor == null) return;
  cursor = nextCursor;
  fetchPage();
});

setInterval(async () => {
  const p = q();
  p.set("cursor", "0");
  p.set("limit", "1");
  const res = await fetch("/impressao/cozinha/historico/api?" + p.toString());
  const data = await res.json().catch(() => ({}));
  if (!res.ok) return;
  const items = data.items || [];
  if (items.length === 0) return;
  if (lastTopId && items[0].id === lastTopId) return;
  cursor = 0;
  fetchPage();
}, 2500);

fetchPage();
</script>
</body>
</html>`
}

func renderHistoricoGlobalHTML() string {
	return `<!doctype html>
<html lang="pt-br">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Histórico de Impressões</title>
  <style>
    :root { --line:#e8e8e8; --muted:#6b7280; --ok:#0a7a0a; --err:#b00020; }
    body { margin:0; font-family: system-ui, -apple-system, Segoe UI, Roboto, Arial, sans-serif; background:#fff; color:#111; }
    header { padding: 14px 18px; border-bottom: 1px solid var(--line); position: sticky; top: 0; background: rgba(255,255,255,.96); backdrop-filter: blur(6px); z-index: 5; }
    h1 { margin: 0; font-size: 18px; }
    .wrap { max-width: 1120px; margin: 0 auto; }
    .filters { display:flex; gap:10px; flex-wrap: wrap; margin-top: 10px; align-items: end; }
    label { display:flex; flex-direction: column; gap: 6px; font-size: 12px; color: var(--muted); }
    input, select { padding: 10px 10px; border-radius: 10px; border: 1px solid var(--line); background: #fff; color: #111; min-width: 160px; font-size: 14px; }
    button { padding: 10px 12px; border-radius: 10px; border: 1px solid var(--line); background: #fff; color: #111; font-weight: 600; cursor: pointer; }
    button.primary { background: #111; color: #fff; border-color: #111; }
    main { padding: 12px 18px; }
    .card { border: 1px solid var(--line); border-radius: 12px; overflow: hidden; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    thead th { font-weight: 700; text-align: left; padding: 12px 10px; background: #fafafa; border-bottom: 1px solid var(--line); }
    tbody td { padding: 12px 10px; border-bottom: 1px solid #f1f1f1; vertical-align: middle; }
    tbody tr { cursor: pointer; }
    tbody tr:hover { background: #fbfbfb; }
    .prod { font-weight: 700; }
    .sub { color: var(--muted); margin-top: 3px; font-size: 12px; }
    .status { display:inline-flex; padding: 6px 10px; border-radius: 999px; border: 1px solid var(--line); font-size: 12px; }
    .s-ok { color: var(--ok); border-color: rgba(10,122,10,.25); }
    .s-err { color: var(--err); border-color: rgba(176,0,32,.25); }
    .pager { display:flex; gap: 10px; align-items: center; justify-content: flex-end; padding: 10px 12px; border-top: 1px solid var(--line); background:#fff; flex-wrap: wrap; }
    .pager .muted { color: var(--muted); font-size: 12px; }
    dialog { border: 1px solid var(--line); border-radius: 16px; padding: 0; background: #fff; color: #111; width: min(920px, 96vw); }
    .dlg-head { padding: 12px 14px; border-bottom: 1px solid var(--line); display:flex; gap: 10px; align-items: center; justify-content: space-between; }
    .dlg-body { padding: 12px 14px; display:grid; grid-template-columns: 1fr; gap: 10px; }
    pre { margin:0; padding: 12px; border-radius: 12px; border: 1px solid var(--line); background: #fafafa; overflow: auto; font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 14px; line-height: 1.25; }
    .dlg-actions { padding: 12px 14px; border-top: 1px solid var(--line); display:flex; gap: 10px; justify-content: flex-end; flex-wrap: wrap; }
    .toast { position: fixed; bottom: 14px; left: 50%; transform: translateX(-50%); background: rgba(17,17,17,.96); border: 1px solid rgba(255,255,255,.08); padding: 10px 12px; border-radius: 12px; color: #fff; max-width: 92vw; display:none; }
    @media (max-width: 900px) {
      thead th:nth-child(6), tbody td:nth-child(6) { display:none; }
    }
  </style>
</head>
<body>
  <header>
    <div class="wrap">
      <h1>Histórico de Impressões</h1>
      <div class="filters">
        <label>Tipo
          <select id="fTipo">
            <option value="">Todos</option>
            <option value="cozinha">Cozinha</option>
            <option value="conferencia">Conferência</option>
            <option value="comanda">Comanda</option>
            <option value="sangria">Sangria</option>
            <option value="caixa">Caixa</option>
          </select>
        </label>
        <label>De
          <input id="fFrom" type="datetime-local" />
        </label>
        <label>Até
          <input id="fTo" type="datetime-local" />
        </label>
        <label>Número
          <input id="fNumero" type="number" min="0" placeholder="Ex.: 123" />
        </label>
        <label>Status
          <select id="fStatus">
            <option value="">Todos</option>
            <option value="impresso">Impresso</option>
            <option value="erro">Erro</option>
            <option value="imprimindo">Imprimindo</option>
            <option value="pendente">Pendente</option>
          </select>
        </label>
        <button id="btnAplicar" class="primary" type="button">Aplicar</button>
        <button id="btnLimpar" type="button">Limpar</button>
      </div>
    </div>
  </header>

  <main>
    <div class="wrap">
      <div class="card">
        <table>
          <thead>
            <tr>
              <th style="width:130px">Tipo</th>
              <th>Pedido</th>
              <th style="width:140px">Status</th>
              <th style="width:180px">Data/Hora</th>
              <th style="width:260px">Resumo</th>
              <th style="width:140px">Usuário</th>
            </tr>
          </thead>
          <tbody id="rows"></tbody>
        </table>
        <div class="pager">
          <span class="muted">Linhas por página:</span>
          <select id="rowsPerPage" style="min-width:80px">
            <option value="10">10</option>
            <option value="25">25</option>
            <option value="50" selected>50</option>
            <option value="100">100</option>
          </select>
          <span id="range" class="muted"></span>
          <button id="btnPrev" type="button">‹</button>
          <button id="btnNext" type="button">›</button>
        </div>
      </div>
    </div>
  </main>

  <dialog id="dlg">
    <div class="dlg-head">
      <div id="dlgTitle" style="font-weight:800"></div>
      <button id="dlgClose" type="button">Fechar</button>
    </div>
    <div class="dlg-body">
      <div id="dlgMeta" style="color:var(--muted); font-size:13px"></div>
      <pre id="dlgText"></pre>
      <div id="dlgErro" style="color:var(--err); font-size:13px"></div>
    </div>
    <div class="dlg-actions">
      <button id="btnReimprimir" class="primary" type="button">Reimprimir (Cozinha)</button>
    </div>
  </dialog>

  <div id="toast" class="toast"></div>

<script>
const rowsEl = document.getElementById("rows");
const dlg = document.getElementById("dlg");
const dlgTitle = document.getElementById("dlgTitle");
const dlgMeta = document.getElementById("dlgMeta");
const dlgText = document.getElementById("dlgText");
const dlgErro = document.getElementById("dlgErro");
const btnReimprimir = document.getElementById("btnReimprimir");
const toastEl = document.getElementById("toast");
const rangeEl = document.getElementById("range");

let cursor = 0;
let nextCursor = null;
let loading = false;
let selectedId = null;
let selectedCanal = null;
let lastTopId = null;

function toast(msg) {
  toastEl.textContent = msg;
  toastEl.style.display = "block";
  setTimeout(() => toastEl.style.display = "none", 2400);
}

function q() {
  const p = new URLSearchParams();
  const tipo = document.getElementById("fTipo").value;
  const from = document.getElementById("fFrom").value;
  const to = document.getElementById("fTo").value;
  const numero = document.getElementById("fNumero").value;
  const status = document.getElementById("fStatus").value;
  const limit = document.getElementById("rowsPerPage").value;
  if (tipo) p.set("tipo", tipo);
  if (from) p.set("from", from);
  if (to) p.set("to", to);
  if (numero) p.set("numero", numero);
  if (status) p.set("status", status);
  p.set("limit", limit || "50");
  p.set("cursor", String(cursor));
  return p;
}

function fmtDT(iso) {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const mo = String(d.getMonth()+1).padStart(2, "0");
  const yy = d.getFullYear();
  return dd + "/" + mo + "/" + yy + " " + hh + ":" + mm + ":" + ss;
}

function statusClass(s) {
  if (s === "impresso") return "status s-ok";
  if (s === "erro") return "status s-err";
  return "status";
}

function canalLabel(c) {
  c = (c || "").toLowerCase();
  if (c === "cozinha") return "Cozinha";
  if (c === "conferencia") return "Conferência";
  if (c === "comanda") return "Comanda";
  if (c === "sangria") return "Sangria";
  if (c === "caixa") return "Caixa";
  return c || "-";
}

function renderRow(it) {
  const tr = document.createElement("tr");
  tr.dataset.id = it.id;
  const pedido = ((it.tipo || "") + " " + (it.numero ?? "")).trim();
  tr.innerHTML =
    '<td><div class="prod">' + canalLabel(it.canal) + '</div></td>' +
    '<td><div class="prod">' + pedido + '</div></td>' +
    '<td><span class="' + statusClass(it.status) + '">' + (it.status || "") + '</span></td>' +
    '<td>' + fmtDT(it.occurred_at) + '</td>' +
    '<td>' + (it.resumo || "") + '</td>' +
    '<td>' + (it.usuario || "") + '</td>';
  tr.addEventListener("click", () => openDetail(it.id));
  return tr;
}

async function fetchPage() {
  if (loading) return;
  loading = true;
  rowsEl.innerHTML = '<tr><td colspan="6" style="padding:14px;color:var(--muted)">Carregando…</td></tr>';

  try {
    const p = q();
    const res = await fetch("/historico/api?" + p.toString());
    const data = await res.json();
    if (!res.ok) throw new Error(data.erro || "Falha ao carregar histórico");

    const items = data.items || [];
    rowsEl.innerHTML = "";
    if (items.length === 0) {
      rowsEl.innerHTML = '<tr><td colspan="6" style="padding:14px;color:var(--muted)">Sem registros</td></tr>';
    } else {
      lastTopId = items[0].id;
      items.forEach(it => rowsEl.appendChild(renderRow(it)));
    }

    nextCursor = (data.next_cursor !== undefined) ? data.next_cursor : null;
    const limit = Number(document.getElementById("rowsPerPage").value || "50");
    const start = cursor + 1;
    const end = cursor + items.length;
    rangeEl.textContent = (items.length === 0) ? "" : (start + "–" + end);

    document.getElementById("btnPrev").disabled = cursor <= 0;
    document.getElementById("btnNext").disabled = nextCursor == null;
  } catch (e) {
    rowsEl.innerHTML = '<tr><td colspan="6" style="padding:14px;color:var(--muted)">Erro: ' + e.message + '</td></tr>';
    nextCursor = null;
  } finally {
    loading = false;
  }
}

async function openDetail(id) {
  selectedId = id;
  selectedCanal = null;
  dlgErro.textContent = "";
  dlgMeta.textContent = "";
  dlgText.textContent = "";
  dlgTitle.textContent = "Carregando…";
  btnReimprimir.style.display = "none";

  const res = await fetch("/historico/api/" + encodeURIComponent(id));
  const data = await res.json();
  if (!res.ok) {
    toast(data.erro || "Registro não encontrado");
    return;
  }

  selectedCanal = (data.canal || "cozinha").toLowerCase();
  const pedido = ((data.tipo || "") + " " + (data.numero || "")).trim();
  dlgTitle.textContent = canalLabel(selectedCanal) + " • " + pedido + " • " + (data.status || "");
  dlgMeta.textContent = fmtDT(data.occurred_at) + " • " + (data.usuario || "") + " • Windows: " + (data.impressora_windows || "");
  dlgText.textContent = data.texto_impressao || "";
  dlgErro.textContent = data.erro ? ("Erro: " + data.erro) : "";

  if (selectedCanal === "cozinha") {
    btnReimprimir.style.display = "inline-flex";
  }
  dlg.showModal();
}

document.getElementById("dlgClose").addEventListener("click", () => dlg.close());

btnReimprimir.addEventListener("click", async () => {
  if (!selectedId) return;
  if (!confirm("Deseja reimprimir este pedido da cozinha?")) return;

  try {
    toast("Reimpressão solicitada…");
    const res = await fetch("/impressao/cozinha/historico/api/" + encodeURIComponent(selectedId) + "/reimprimir", { method: "POST" });
    const data = await res.json();
    if (!res.ok) throw new Error(data.erro || "Falha ao reimprimir");

    const jobId = data.job_id;
    for (let i = 0; i < 25; i++) {
      const sRes = await fetch("/impressao/cozinha/status/" + encodeURIComponent(jobId));
      const sData = await sRes.json();
      if (sData.status === "impresso") { toast("Reimpressão concluída com sucesso."); fetchPage(); return; }
      if (sData.status === "erro") { toast("Erro na reimpressão: " + (sData.erro || "falha")); fetchPage(); return; }
      await new Promise(r => setTimeout(r, 700));
    }
    toast("Reimpressão em andamento. Verifique o status.");
  } catch (e) {
    toast("Erro: " + e.message);
  }
});

document.getElementById("btnAplicar").addEventListener("click", () => { cursor = 0; fetchPage(); });
document.getElementById("btnLimpar").addEventListener("click", () => {
  document.getElementById("fTipo").value = "";
  document.getElementById("fFrom").value = "";
  document.getElementById("fTo").value = "";
  document.getElementById("fNumero").value = "";
  document.getElementById("fStatus").value = "";
  cursor = 0;
  fetchPage();
});

document.getElementById("rowsPerPage").addEventListener("change", () => { cursor = 0; fetchPage(); });
document.getElementById("btnPrev").addEventListener("click", () => {
  const limit = Number(document.getElementById("rowsPerPage").value || "50");
  cursor = Math.max(0, cursor - limit);
  fetchPage();
});
document.getElementById("btnNext").addEventListener("click", () => {
  if (nextCursor == null) return;
  cursor = nextCursor;
  fetchPage();
});

setInterval(async () => {
  const p = q();
  p.set("cursor", "0");
  p.set("limit", "1");
  const res = await fetch("/historico/api?" + p.toString());
  const data = await res.json().catch(() => ({}));
  if (!res.ok) return;
  const items = data.items || [];
  if (items.length === 0) return;
  if (lastTopId && items[0].id === lastTopId) return;
  cursor = 0;
  fetchPage();
}, 2500);

fetchPage();
</script>
</body>
</html>`
}
