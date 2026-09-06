package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/goopedir/go-impressao/internal/config"
	"github.com/goopedir/go-impressao/internal/models"
	"github.com/goopedir/go-impressao/internal/services"
	printerpkg "github.com/goopedir/go-impressao/internal/services/printer"
	"github.com/goopedir/go-impressao/internal/web"
)

type ImpressaoHandler struct {
	logger    *log.Logger
	jobStore  *services.JobStore
	formatter *services.Formatter
	printer   *services.PrintService
	history   *services.HistoryStore
	cfg       *config.Manager

	conferenciaCooldownMu sync.Mutex
	conferenciaCooldown   map[string]time.Time
}

func kitchenCols(cfg *config.Manager, printerName string, modelo string) int {
	if cfg != nil {
		return cfg.EffectiveColsByModelo(printerName, modelo)
	}

	modelo = strings.ToLower(strings.TrimSpace(modelo))
	if modelo == "56mm" || modelo == "58mm" {
		return 32
	}
	return 48
}

// NewImpressaoHandler cria o handler HTTP do fluxo de comanda de cozinha.
func NewImpressaoHandler(logger *log.Logger, jobStore *services.JobStore, formatter *services.Formatter, printer *services.PrintService, history *services.HistoryStore, cfg *config.Manager) *ImpressaoHandler {
	return &ImpressaoHandler{
		logger:    logger,
		jobStore:  jobStore,
		formatter: formatter,
		printer:   printer,
		history:   history,
		cfg:       cfg,
		conferenciaCooldown: make(map[string]time.Time),
	}
}

func (h *ImpressaoHandler) reservaConferencia(key string, now time.Time, cooldown time.Duration) (ok bool, retryAfter time.Duration, prev time.Time, hadPrev bool) {
	h.conferenciaCooldownMu.Lock()
	defer h.conferenciaCooldownMu.Unlock()

	if key == "" {
		return true, 0, time.Time{}, false
	}

	if last, exists := h.conferenciaCooldown[key]; exists {
		if d := now.Sub(last); d >= 0 && d < cooldown {
			return false, cooldown - d, last, true
		}
		prev = last
		hadPrev = true
	}

	h.conferenciaCooldown[key] = now
	return true, 0, prev, hadPrev
}

func (h *ImpressaoHandler) rollbackReservaConferencia(key string, prev time.Time, hadPrev bool) {
	if key == "" {
		return
	}
	h.conferenciaCooldownMu.Lock()
	defer h.conferenciaCooldownMu.Unlock()
	if hadPrev {
		h.conferenciaCooldown[key] = prev
		return
	}
	delete(h.conferenciaCooldown, key)
}

func conferenciaCooldownKey(req models.ConferenciaRequest) string {
	driver := strings.ToLower(strings.TrimSpace(req.Driver))
	modelo := strings.ToLower(strings.TrimSpace(req.Modelo))
	tipo := strings.ToLower(strings.TrimSpace(req.Tipo))
	mesa := strings.ToLower(strings.TrimSpace(req.Mesa))

	var parts []string
	parts = append(parts, "driver="+driver)
	parts = append(parts, "modelo="+modelo)
	parts = append(parts, "tipo="+tipo)
	parts = append(parts, "sequencial="+fmt.Sprintf("%d", req.Sequencial))
	parts = append(parts, "mesa="+mesa)
	parts = append(parts, "codigo="+fmt.Sprintf("%d", req.Codigo))
	parts = append(parts, "total_produtos="+fmt.Sprintf("%.2f", req.TotalProdutos))
	parts = append(parts, "taxa_percent="+fmt.Sprintf("%.2f", req.TaxaServicoPercent))
	parts = append(parts, "taxa_valor="+fmt.Sprintf("%.2f", req.TaxaServicoValor))
	parts = append(parts, "total_geral="+fmt.Sprintf("%.2f", req.TotalGeral))

	var prodLines []string
	for _, it := range req.Itens {
		for _, p := range it.Produtos {
			name := strings.ToUpper(strings.TrimSpace(p.Nome))
			cat := strings.ToUpper(strings.TrimSpace(p.Categoria))
			obs := strings.TrimSpace(p.Observacoes)
			var extras []string
			for _, e := range p.Extras {
				extras = append(extras, fmt.Sprintf("%s|%s|%d",
					strings.ToUpper(strings.TrimSpace(e.Categoria)),
					strings.ToUpper(strings.TrimSpace(e.Nome)),
					e.Quantidade,
				))
			}
			sort.Strings(extras)
			prodLines = append(prodLines, fmt.Sprintf("%s|%s|%d|%s|%s|%.2f|%.2f|%.2f",
				cat,
				name,
				p.Quantidade,
				obs,
				strings.Join(extras, ";"),
				p.ValorUnitario,
				p.ValorTotal,
				p.ValorAdicional,
			))
		}
	}
	sort.Strings(prodLines)
	parts = append(parts, "produtos="+strings.Join(prodLines, "||"))

	var payLines []string
	for _, p := range req.Pagamentos {
		payLines = append(payLines, fmt.Sprintf("%s|%s|%.2f|%.2f|%t|%s",
			strings.ToUpper(strings.TrimSpace(p.Descricao)),
			strings.ToUpper(strings.TrimSpace(p.Nome)),
			p.Valor,
			p.Troco,
			p.Faturado,
			strings.ToUpper(strings.TrimSpace(p.Transacao)),
		))
	}
	sort.Strings(payLines)
	parts = append(parts, "pagamentos="+strings.Join(payLines, "||"))

	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return driver + ":" + hex.EncodeToString(sum[:])
}

func conferenciaCanal(req models.ConferenciaRequest) string {
	if strings.TrimSpace(req.Cliente.Nome) != "" || len(req.Pagamentos) > 0 || strings.TrimSpace(req.Tipo) != "" || req.Sequencial > 0 {
		return "comanda"
	}
	return "conferencia"
}

func conferenciaNumero(req models.ConferenciaRequest) int {
	if req.Sequencial > 0 {
		return req.Sequencial
	}
	if req.Codigo > 0 {
		return req.Codigo
	}
	return 0
}

func resumoConferencia(req models.ConferenciaRequest) string {
	title := strings.ToUpper(strings.TrimSpace(req.Mesa))
	if strings.TrimSpace(req.Tipo) != "" {
		t := strings.ToUpper(strings.TrimSpace(req.Tipo))
		if req.Sequencial > 0 {
			title = fmt.Sprintf("%s %03d", t, req.Sequencial)
		} else {
			title = t
		}
	}
	if title == "" {
		title = strings.ToUpper(conferenciaCanal(req))
	}

	var names []string
	for _, it := range req.Itens {
		for _, p := range it.Produtos {
			n := strings.TrimSpace(p.Nome)
			if n == "" {
				continue
			}
			if len(names) >= 3 {
				names = append(names, "…")
				goto done
			}
			if p.Quantidade > 1 {
				names = append(names, fmt.Sprintf("%dUn %s", p.Quantidade, n))
			} else {
				names = append(names, n)
			}
		}
	}
done:
	if len(names) == 0 {
		return title
	}
	return title + " • " + strings.Join(names, ", ")
}

func payloadFromConferencia(req models.ConferenciaRequest) models.ImpressaoCozinhaRequest {
	var prods []models.Produto
	for _, it := range req.Itens {
		for _, p := range it.Produtos {
			prods = append(prods, p)
		}
	}
	return models.ImpressaoCozinhaRequest{
		Tipo:       models.TipoImpressao(conferenciaCanal(req)),
		Numero:     conferenciaNumero(req),
		Usuario:    strings.TrimSpace(req.Operador),
		Driver:     strings.TrimSpace(req.Driver),
		Impressora: conferenciaCanal(req),
		Produtos:   prods,
	}
}

// Register registra as rotas:
// - POST /impressao/cozinha
// - GET  /impressao/cozinha/preview/{job_id}
// - POST /impressao/cozinha/imprimir/{job_id}
// - GET  /impressao/cozinha/status/{job_id}
func (h *ImpressaoHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/impressao/cozinha", h.handleCriar)
	mux.HandleFunc("/impressao/cozinha/preview/", h.handlePreview)
	mux.HandleFunc("/impressao/cozinha/imprimir/", h.handleImprimir)
	mux.HandleFunc("/impressao/cozinha/status/", h.handleStatus)
	mux.HandleFunc("/impressao/teste", h.handleTeste)
	mux.HandleFunc("/impressao/conferencia", h.handleConferencia)
	mux.HandleFunc("/impressao/sangria", h.handleSangria)
	mux.HandleFunc("/impressao/caixa/fechamento", h.handleCaixaFechamento)
}

func (h *ImpressaoHandler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, `<!doctype html>
<html lang="pt-br">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>go-impressao</title>
<style>body{font-family:system-ui,-apple-system,Segoe UI,Roboto,Arial,sans-serif;margin:24px;max-width:900px}code,pre{font-family:ui-monospace,Menlo,Consolas,monospace}</style>
</head>
<body>
<h1>go-impressao</h1>
<p>Configuração da URL principal: <a href="/config">/config</a></p>
<p>Histórico de impressões: <a href="/impressao/cozinha/historico">/impressao/cozinha/historico</a></p>
<p>Endpoint principal: <code>POST /impressao/cozinha</code></p>
<pre>curl -X POST http://127.0.0.1:PORTA/impressao/cozinha ^
  -H "Content-Type: application/json" ^
  -d "{...}"</pre>
<p>Ao criar, você receberá <code>preview_url</code> para visualizar e imprimir.</p>
</body></html>`)
}

func (h *ImpressaoHandler) handleCriar(w http.ResponseWriter, r *http.Request) {
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

	trim := strings.TrimSpace(string(body))
	if trim == "" {
		writeJSONError(w, http.StatusBadRequest, "corpo da requisição vazio")
		return
	}

	if strings.HasPrefix(trim, "[") {
		var reqs []models.ImpressaoCozinhaRequest
		if err := json.Unmarshal(body, &reqs); err != nil {
			writeJSONError(w, http.StatusBadRequest, "JSON inválido: verifique a sintaxe")
			return
		}
		if len(reqs) == 0 {
			writeJSONError(w, http.StatusUnprocessableEntity, `o array deve conter ao menos 1 item`)
			return
		}

		var errs []map[string]any
		for i := range reqs {
			reqs[i].Normalize()
			if err := reqs[i].Validate(); err != nil {
				errs = append(errs, map[string]any{
					"index": i,
					"erro":  err.Error(),
				})
			}
		}
		if len(errs) > 0 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"erros": errs,
			})
			return
		}

		type pos struct {
			reqIndex  int
			prodIndex int
		}
		firstPos := make(map[string]pos)
		for i := range reqs {
			for j := range reqs[i].Produtos {
				u := strings.TrimSpace(reqs[i].Produtos[j].UUID)
				if u == "" {
					continue
				}
				if first, ok := firstPos[u]; ok {
					writeJSON(w, http.StatusConflict, map[string]any{
						"erro":              "uuid de produto duplicado no mesmo payload",
						"uuid":              u,
						"first_req_index":   first.reqIndex,
						"first_prod_index":  first.prodIndex,
						"req_index":         i,
						"prod_index":        j,
					})
					return
				}
				firstPos[u] = pos{reqIndex: i, prodIndex: j}
			}
		}

		var items []map[string]any
		for reqIndex, req := range reqs {
			var ignored []string
			var kept []models.Produto
			for _, p := range req.Produtos {
				u := strings.TrimSpace(p.UUID)
				if u == "" {
					kept = append(kept, p)
					continue
				}
				if h.history != nil && h.history.HasPrintedProdutoUUID(u) {
					ignored = append(ignored, u)
					continue
				}
				if already := h.jobStore.MarkUUID(u); already {
					ignored = append(ignored, u)
					continue
				}
				kept = append(kept, p)
			}
			req.Produtos = kept
			if len(req.Produtos) == 0 {
				items = append(items, map[string]any{
					"status":        "duplicado",
					"req_index":     reqIndex,
					"ignored_uuids": ignored,
					"erro":          "todos os produtos já foram recebidos/impresso(s); para repetir use re-imprimir",
				})
				continue
			}

			cols := kitchenCols(h.cfg, req.Driver, req.Modelo)
			preview := h.formatter.FormatComandaCozinhaWithCols(req, time.Now(), cols)
			job := h.jobStore.Create(req, preview)

			h.logger.Printf("job=%s recebido(tipo=array) tipo=%q numero=%d usuario=%q impressora_erp=%q impressora_windows=%q produtos=%d",
				job.ID, req.Tipo, req.Numero, req.Usuario, req.Impressora, req.Driver, len(req.Produtos),
			)

			if req.ImprimirAgora {
				if err := h.printer.StartPrint(job.ID); err != nil {
					_ = h.jobStore.Update(job.ID, func(j *services.Job) {
						j.Status = services.StatusErro
						j.ErrorPublic = err.Error()
					})
				}
			}

			job, _ = h.jobStore.Get(job.ID)
			item := map[string]any{
				"job_id":         job.ID,
				"status":         job.Status,
				"imprimir_agora": req.ImprimirAgora,
				"preview_url":    fmt.Sprintf("/impressao/cozinha/preview/%s", job.ID),
				"comanda":        job.PreviewText,
			}
			if len(ignored) > 0 {
				item["ignored_uuids"] = ignored
			}
			if job.ErrorPublic != "" {
				item["erro"] = job.ErrorPublic
			}
			items = append(items, item)
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"items": items,
		})
		return
	}

	var req models.ImpressaoCozinhaRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido: verifique a sintaxe")
		return
	}

	req.Normalize()
	if err := req.Validate(); err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	uuidFirstIndex := make(map[string]int)
	for i := range req.Produtos {
		u := strings.TrimSpace(req.Produtos[i].UUID)
		if u == "" {
			continue
		}
		if first, ok := uuidFirstIndex[u]; ok {
			writeJSON(w, http.StatusConflict, map[string]any{
				"erro":        "uuid de produto duplicado no mesmo payload",
				"uuid":        u,
				"first_index": first,
				"index":       i,
			})
			return
		}
		uuidFirstIndex[u] = i
	}

	var ignored []string
	var kept []models.Produto
	for _, p := range req.Produtos {
		u := strings.TrimSpace(p.UUID)
		if u == "" {
			kept = append(kept, p)
			continue
		}
		if h.history != nil && h.history.HasPrintedProdutoUUID(u) {
			ignored = append(ignored, u)
			continue
		}
		if already := h.jobStore.MarkUUID(u); already {
			ignored = append(ignored, u)
			continue
		}
		kept = append(kept, p)
	}
	req.Produtos = kept
	if len(req.Produtos) == 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"erro":          "todos os produtos já foram recebidos/impresso(s); para repetir use re-imprimir",
			"status":        "duplicado",
			"ignored_uuids": ignored,
		})
		return
	}

	cols := kitchenCols(h.cfg, req.Driver, req.Modelo)
	preview := h.formatter.FormatComandaCozinhaWithCols(req, time.Now(), cols)
	job := h.jobStore.Create(req, preview)

	h.logger.Printf("job=%s recebido tipo=%q numero=%d usuario=%q impressora_erp=%q impressora_windows=%q produtos=%d",
		job.ID, req.Tipo, req.Numero, req.Usuario, req.Impressora, req.Driver, len(req.Produtos),
	)

	if req.ImprimirAgora {
		if err := h.printer.StartPrint(job.ID); err != nil {
			_ = h.jobStore.Update(job.ID, func(j *services.Job) {
				j.Status = services.StatusErro
				j.ErrorPublic = err.Error()
			})
		}
	}

	job, _ = h.jobStore.Get(job.ID)
	resp := map[string]any{
		"job_id":         job.ID,
		"status":         job.Status,
		"imprimir_agora": req.ImprimirAgora,
		"preview_url":    fmt.Sprintf("/impressao/cozinha/preview/%s", job.ID),
		"comanda":        job.PreviewText,
	}
	if len(ignored) > 0 {
		resp["ignored_uuids"] = ignored
	}
	if job.ErrorPublic != "" {
		resp["erro"] = job.ErrorPublic
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *ImpressaoHandler) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/impressao/cozinha/preview/")
	id = strings.TrimSpace(id)
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "id do job não informado")
		return
	}

	job, ok := h.jobStore.Get(id)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "comanda não encontrada")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, web.RenderPreviewHTML(web.PreviewPageData{
		JobID:        job.ID,
		ComandaTexto: job.PreviewText,
		Status:       string(job.Status),
		Erro:         job.ErrorPublic,
	}))
}

func (h *ImpressaoHandler) handleImprimir(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/impressao/cozinha/imprimir/")
	id = strings.TrimSpace(id)
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "id do job não informado")
		return
	}

	err := h.printer.StartPrint(id)
	if err != nil {
		job, ok := h.jobStore.Get(id)
		if ok {
			writeJSON(w, http.StatusConflict, map[string]any{
				"status": job.Status,
				"erro":   err.Error(),
			})
			return
		}
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	job, _ := h.jobStore.Get(id)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": job.Status,
	})
}

func (h *ImpressaoHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/impressao/cozinha/status/")
	id = strings.TrimSpace(id)
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "id do job não informado")
		return
	}

	job, ok := h.jobStore.Get(id)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "comanda não encontrada")
		return
	}

	resp := map[string]any{
		"job_id": job.ID,
		"status": job.Status,
	}
	if job.ErrorPublic != "" {
		resp["erro"] = job.ErrorPublic
	}
	if job.PrintedAt != nil {
		resp["impresso_em"] = job.PrintedAt.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *ImpressaoHandler) handleTeste(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Driver string `json:"driver"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido: verifique a sintaxe")
		return
	}

	ctx, cancel := printerpkg.DefaultPrintContext()
	defer cancel()
	if err := h.printer.PrintTest(ctx, req.Driver); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"impresso_em": time.Now().Format(time.RFC3339),
	})
}

func (h *ImpressaoHandler) handleConferencia(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
		return
	}
	if len(body) == 0 {
		writeJSONError(w, http.StatusBadRequest, "corpo da requisição vazio")
		return
	}

	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		var reqs []models.ConferenciaRequest
		if err := json.Unmarshal(body, &reqs); err != nil {
			writeJSONError(w, http.StatusBadRequest, "JSON inválido: verifique a sintaxe")
			return
		}
		items := make([]map[string]any, 0, len(reqs))
		for i := range reqs {
			reqs[i].Normalize()
			if err := reqs[i].Validate(); err != nil {
				items = append(items, map[string]any{
					"index": i,
					"status": "erro",
					"erro": err.Error(),
				})
				continue
			}
			if !reqs[i].ImprimirAgora {
				items = append(items, map[string]any{
					"index": i,
					"status": "pendente",
				})
				continue
			}

			now := time.Now()
			key := conferenciaCooldownKey(reqs[i])
			const cooldown = 60 * time.Second
			ok, retryAfter, prev, hadPrev := h.reservaConferencia(key, now, cooldown)
			if !ok {
				items = append(items, map[string]any{
					"index":        i,
					"status":       "cooldown",
					"retry_after":  int(retryAfter.Seconds()),
					"erro":         "cooldown ativo: impressão repetida bloqueada",
				})
				continue
			}

			ctx, cancel := printerpkg.DefaultPrintContext()
			err := h.printer.PrintConferencia(ctx, reqs[i])
			cancel()
			if err != nil {
				h.rollbackReservaConferencia(key, prev, hadPrev)
				if h.history != nil {
					j, _ := json.MarshalIndent(reqs[i], "", "  ")
					_ = h.history.Add(services.HistoryRecord{
						Status:            services.StatusErro,
						Erro:              err.Error(),
						Canal:             conferenciaCanal(reqs[i]),
						Tipo:              conferenciaCanal(reqs[i]),
						Numero:            conferenciaNumero(reqs[i]),
						Usuario:           strings.TrimSpace(reqs[i].Operador),
						ImpressoraERP:     "",
						ImpressoraWindows: strings.TrimSpace(reqs[i].Driver),
						Resumo:            resumoConferencia(reqs[i]),
						TextoImpressao:    string(j),
						Payload:           payloadFromConferencia(reqs[i]),
					})
				}
				items = append(items, map[string]any{
					"index": i,
					"status": "erro",
					"erro": err.Error(),
				})
				continue
			}
			if h.history != nil {
				j, _ := json.MarshalIndent(reqs[i], "", "  ")
				_ = h.history.Add(services.HistoryRecord{
					Status:            services.StatusImpresso,
					Canal:             conferenciaCanal(reqs[i]),
					Tipo:              conferenciaCanal(reqs[i]),
					Numero:            conferenciaNumero(reqs[i]),
					Usuario:           strings.TrimSpace(reqs[i].Operador),
					ImpressoraERP:     "",
					ImpressoraWindows: strings.TrimSpace(reqs[i].Driver),
					Resumo:            resumoConferencia(reqs[i]),
					TextoImpressao:    string(j),
					Payload:           payloadFromConferencia(reqs[i]),
				})
			}
			items = append(items, map[string]any{
				"index": i,
				"status": "ok",
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": items,
		})
		return
	}

	var req models.ConferenciaRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido: verifique a sintaxe")
		return
	}
	req.Normalize()
	if err := req.Validate(); err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if !req.ImprimirAgora {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "pendente",
		})
		return
	}

	now := time.Now()
	key := conferenciaCooldownKey(req)
	const cooldown = 60 * time.Second
	ok, retryAfter, prev, hadPrev := h.reservaConferencia(key, now, cooldown)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{
			"status":      "cooldown",
			"retry_after": int(retryAfter.Seconds()),
			"erro":        "cooldown ativo: impressão repetida bloqueada",
		})
		return
	}

	ctx, cancel := printerpkg.DefaultPrintContext()
	defer cancel()
	if err := h.printer.PrintConferencia(ctx, req); err != nil {
		h.rollbackReservaConferencia(key, prev, hadPrev)
		if h.history != nil {
			j, _ := json.MarshalIndent(req, "", "  ")
			_ = h.history.Add(services.HistoryRecord{
				Status:            services.StatusErro,
				Erro:              err.Error(),
				Canal:             conferenciaCanal(req),
				Tipo:              conferenciaCanal(req),
				Numero:            conferenciaNumero(req),
				Usuario:           strings.TrimSpace(req.Operador),
				ImpressoraERP:     "",
				ImpressoraWindows: strings.TrimSpace(req.Driver),
				Resumo:            resumoConferencia(req),
				TextoImpressao:    string(j),
				Payload:           payloadFromConferencia(req),
			})
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.history != nil {
		j, _ := json.MarshalIndent(req, "", "  ")
		_ = h.history.Add(services.HistoryRecord{
			Status:            services.StatusImpresso,
			Canal:             conferenciaCanal(req),
			Tipo:              conferenciaCanal(req),
			Numero:            conferenciaNumero(req),
			Usuario:           strings.TrimSpace(req.Operador),
			ImpressoraERP:     "",
			ImpressoraWindows: strings.TrimSpace(req.Driver),
			Resumo:            resumoConferencia(req),
			TextoImpressao:    string(j),
			Payload:           payloadFromConferencia(req),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"impresso_em": time.Now().Format(time.RFC3339),
	})
}

func (h *ImpressaoHandler) handleSangria(w http.ResponseWriter, r *http.Request) {
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
	if len(body) == 0 {
		writeJSONError(w, http.StatusBadRequest, "corpo da requisição vazio")
		return
	}

	var req models.SangriaRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido: verifique a sintaxe")
		return
	}
	req.Normalize()
	if err := req.Validate(); err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if !req.ImprimirAgora {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "pendente",
		})
		return
	}

	ctx, cancel := printerpkg.DefaultPrintContext()
	defer cancel()
	if err := h.printer.PrintSangria(ctx, req); err != nil {
		if h.history != nil {
			j, _ := json.MarshalIndent(req, "", "  ")
			_ = h.history.Add(services.HistoryRecord{
				Status:            services.StatusErro,
				Erro:              err.Error(),
				Canal:             "sangria",
				Tipo:              "sangria",
				Numero:            0,
				Usuario:           strings.TrimSpace(req.Operador),
				ImpressoraERP:     "",
				ImpressoraWindows: strings.TrimSpace(req.Driver),
				Resumo:            fmt.Sprintf("SANGRIA • %s • %s", fmtMoneyBR(req.Valor), strings.TrimSpace(req.Descricao)),
				TextoImpressao:    string(j),
				Payload: models.ImpressaoCozinhaRequest{
					Tipo:       "sangria",
					Numero:     0,
					Usuario:    strings.TrimSpace(req.Operador),
					Driver:     strings.TrimSpace(req.Driver),
					Impressora: "sangria",
					Produtos:   nil,
				},
			})
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if h.history != nil {
		j, _ := json.MarshalIndent(req, "", "  ")
		_ = h.history.Add(services.HistoryRecord{
			Status:            services.StatusImpresso,
			Canal:             "sangria",
			Tipo:              "sangria",
			Numero:            0,
			Usuario:           strings.TrimSpace(req.Operador),
			ImpressoraERP:     "",
			ImpressoraWindows: strings.TrimSpace(req.Driver),
			Resumo:            fmt.Sprintf("SANGRIA • %s • %s", fmtMoneyBR(req.Valor), strings.TrimSpace(req.Descricao)),
			TextoImpressao:    string(j),
			Payload: models.ImpressaoCozinhaRequest{
				Tipo:       "sangria",
				Numero:     0,
				Usuario:    strings.TrimSpace(req.Operador),
				Driver:     strings.TrimSpace(req.Driver),
				Impressora: "sangria",
				Produtos:   nil,
			},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"impresso_em": time.Now().Format(time.RFC3339),
	})
}

func (h *ImpressaoHandler) handleCaixaFechamento(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
		return
	}
	if len(body) == 0 {
		writeJSONError(w, http.StatusBadRequest, "corpo da requisição vazio")
		return
	}

	var req models.CaixaFechamentoRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido: verifique a sintaxe")
		return
	}
	req.Normalize()
	if err := req.Validate(); err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if !req.ImprimirAgora {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "pendente",
		})
		return
	}

	ctx, cancel := printerpkg.DefaultPrintContext()
	defer cancel()
	if err := h.printer.PrintCaixaFechamento(ctx, req); err != nil {
		if h.history != nil {
			j, _ := json.MarshalIndent(req, "", "  ")
			caixaID := 0
			if len(req.Computado) > 0 {
				caixaID = req.Computado[0].ID
			}
			_ = h.history.Add(services.HistoryRecord{
				Status:            services.StatusErro,
				Erro:              err.Error(),
				Canal:             "caixa",
				Tipo:              "caixa",
				Numero:            caixaID,
				Usuario:           "",
				ImpressoraERP:     "",
				ImpressoraWindows: strings.TrimSpace(req.Driver),
				Resumo:            fmt.Sprintf("CAIXA %03d • Fechamento", caixaID),
				TextoImpressao:    string(j),
				Payload: models.ImpressaoCozinhaRequest{
					Tipo:       "caixa",
					Numero:     caixaID,
					Driver:     strings.TrimSpace(req.Driver),
					Impressora: "caixa",
				},
			})
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if h.history != nil {
		j, _ := json.MarshalIndent(req, "", "  ")
		caixaID := 0
		if len(req.Computado) > 0 {
			caixaID = req.Computado[0].ID
		}
		_ = h.history.Add(services.HistoryRecord{
			Status:            services.StatusImpresso,
			Canal:             "caixa",
			Tipo:              "caixa",
			Numero:            caixaID,
			Usuario:           "",
			ImpressoraERP:     "",
			ImpressoraWindows: strings.TrimSpace(req.Driver),
			Resumo:            fmt.Sprintf("CAIXA %03d • Fechamento", caixaID),
			TextoImpressao:    string(j),
			Payload: models.ImpressaoCozinhaRequest{
				Tipo:       "caixa",
				Numero:     caixaID,
				Driver:     strings.TrimSpace(req.Driver),
				Impressora: "caixa",
			},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"impresso_em": time.Now().Format(time.RFC3339),
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"erro": msg,
	})
}

func fmtMoneyBR(v float64) string {
	s := fmt.Sprintf("R$ %.2f", v)
	return strings.ReplaceAll(s, ".", ",")
}
