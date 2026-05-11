package web

import (
	"html"
	"strings"
)

type PreviewPageData struct {
	JobID        string
	ComandaTexto string
	Status       string
	Erro         string
}

func RenderPreviewHTML(d PreviewPageData) string {
	comandaEsc := html.EscapeString(d.ComandaTexto)
	comandaEsc = strings.ReplaceAll(comandaEsc, "\n", "&#10;")

	return `<!doctype html>
<html lang="pt-br">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Pré-visualização da comanda</title>
  <style>
    body { font-family: system-ui, -apple-system, Segoe UI, Roboto, Arial, sans-serif; margin: 24px; background: #f6f6f6; }
    .wrap { max-width: 960px; }
    .ticket { width: 80mm; background: #fff; padding: 8mm 6mm; box-shadow: 0 1px 8px rgba(0,0,0,.08); }
    textarea { width: 100%; min-height: 420px; border: 0; outline: 0; resize: none; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 16px; line-height: 1.25; padding: 0; }
    button { padding: 10px 14px; font-size: 14px; cursor: pointer; }
    .row { display: flex; gap: 12px; align-items: center; margin: 12px 0; flex-wrap: wrap; }
    .status { padding: 6px 10px; border-radius: 6px; background: #f2f2f2; }
    .err { color: #b00020; }
    @media print {
      body { background: #fff; margin: 0; }
      .wrap { max-width: none; }
      .row, h1, p { display: none; }
      .ticket { width: 80mm; padding: 0; box-shadow: none; }
      textarea { min-height: auto; }
    }
  </style>
</head>
<body>
  <div class="wrap">
    <h1>Pré-visualização da comanda</h1>
    <div class="row">
      <div class="status" id="status">Status: ` + html.EscapeString(d.Status) + `</div>
      <div class="err" id="erro">` + html.EscapeString(d.Erro) + `</div>
    </div>
    <div class="ticket">
      <textarea readonly aria-label="Comanda">` + comandaEsc + `</textarea>
    </div>
    <div class="row">
      <button id="btnImprimir" type="button">Imprimir</button>
      <button id="btnAtualizar" type="button">Atualizar status</button>
    </div>
    <p>Job: <code>` + html.EscapeString(d.JobID) + `</code></p>
  </div>

<script>
const jobId = ` + jsString(d.JobID) + `;

async function status() {
  const res = await fetch("/impressao/cozinha/status/" + encodeURIComponent(jobId));
  const data = await res.json();
  document.getElementById("status").textContent = "Status: " + (data.status || "desconhecido");
  document.getElementById("erro").textContent = data.erro || "";
  return data;
}

async function imprimir() {
  const res = await fetch("/impressao/cozinha/imprimir/" + encodeURIComponent(jobId), { method: "POST" });
  const data = await res.json();
  document.getElementById("status").textContent = "Status: " + (data.status || "desconhecido");
  document.getElementById("erro").textContent = data.erro || "";
  for (let i = 0; i < 20; i++) {
    const s = await status();
    if (s.status === "impresso" || s.status === "erro") return;
    await new Promise(r => setTimeout(r, 700));
  }
}

document.getElementById("btnImprimir").addEventListener("click", imprimir);
document.getElementById("btnAtualizar").addEventListener("click", status);
status();
</script>
</body>
</html>`
}

func jsString(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}
