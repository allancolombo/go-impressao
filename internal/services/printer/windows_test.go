//go:build windows

package printer

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/goopedir/go-impressao/internal/config"
	"github.com/goopedir/go-impressao/internal/models"
)

func TestConferenciaCols80mmUsesRightSpace(t *testing.T) {
	got := conferenciaCols(576, 48)
	if got != 47 {
		t.Fatalf("esperava 47 colunas para conferencia 80mm, got: %d", got)
	}
}

func TestConferenciaCols56mmUsesRightSpace(t *testing.T) {
	got := conferenciaCols(384, 32)
	if got != 31 {
		t.Fatalf("esperava 31 colunas para conferencia 56mm, got: %d", got)
	}
}

func TestReportFallbackCols80mmUsesFullWidth(t *testing.T) {
	got := reportFallbackCols("80mm", 48)
	if got != 48 {
		t.Fatalf("esperava 48 colunas no fallback 80mm, got: %d", got)
	}
}

func TestReportFallbackCols56mmKeepsReservedSpace(t *testing.T) {
	got := reportFallbackCols("56mm", 32)
	if got != 26 {
		t.Fatalf("esperava 26 colunas no fallback 56mm, got: %d", got)
	}
}

func TestEmpresaHeaderLines_HidesEmptyFieldsAndAvoidsDuplicateName(t *testing.T) {
	got := empresaHeaderLines(config.EmpresaParametros{
		Nome:   "GooPedir",
		Razao:  "GooPedir",
		Rua:    "Rua A",
		Bairro: "Centro",
		Cidade: "Blumenau",
		Estado: "SC",
	})

	if len(got) != 2 {
		t.Fatalf("quantidade de linhas inesperada: %#v", got)
	}
	if got[0] != "GooPedir" {
		t.Fatalf("primeira linha inesperada: %q", got[0])
	}
	if got[1] != "Rua A, Centro, Blumenau/SC" {
		t.Fatalf("endereco inesperado: %q", got[1])
	}
}

func TestEmpresaHeaderLines_ShowsOptionalFieldsOnlyWhenPresent(t *testing.T) {
	got := empresaHeaderLines(config.EmpresaParametros{
		Nome:   "Fantasia",
		Razao:  "Razao LTDA",
		CNPJ:   "123",
		Rua:    "Rua A",
		Cidade: "Blumenau",
		Estado: "SC",
		CEP:    "89000-000",
		IE:     "456",
	})

	want := []string{
		"Fantasia",
		"Razao LTDA",
		"CNPJ: 123",
		"Rua A, Blumenau/SC - CEP 89000-000",
		"IE: 456",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("linhas inesperadas: got=%#v want=%#v", got, want)
	}
}

func TestAppendEscPOSProdutoLines_ExtrasUseConfiguredFontWithoutBold(t *testing.T) {
	var buf []byte
	fontStyle := conferenciaFont(config.ConferenciaConfig{Fonte: "grande"})
	appendEscPOSProdutoLines(&buf, models.Produto{
		Nome:       "Pizza",
		Quantidade: 1,
		Extras: []models.Extra{
			{Categoria: "Adicionais", Nome: "Bacon", Quantidade: 2},
		},
	}, 47, fontStyle, config.ConferenciaConfig{Fonte: "grande", Delimitador: "."}, true)

	catSeq := append([]byte{0x1B, 0x4D, 0x00, 0x1D, 0x21, 0x01, 0x1B, 0x45, 0x01}, encodeCP850("ADICIONAIS")...)
	extraBoldSeq := append([]byte{0x1B, 0x4D, 0x01, 0x1B, 0x45, 0x01}, encodeCP850("- 2Un Bacon")...)
	extraConfiguredSeq := append([]byte{0x1B, 0x4D, 0x00, 0x1D, 0x21, 0x01}, encodeCP850("- 2Un Bacon")...)

	if !bytes.Contains(buf, catSeq) {
		t.Fatalf("esperava categoria com fonte configurada + negrito, bytes=%v", buf)
	}
	if bytes.Contains(buf, extraBoldSeq) {
		t.Fatalf("nao esperava adicional em negrito, bytes=%v", buf)
	}
	if !bytes.Contains(buf, extraConfiguredSeq) {
		t.Fatalf("esperava adicional com fonte configurada sem negrito, bytes=%v", buf)
	}
	if !bytes.Contains(buf, encodeCP850(conferenciaMoneyLine("", "R$ 0,00", 47, config.ConferenciaConfig{Delimitador: "."}))) {
		t.Fatalf("esperava valor preenchido com delimitador configurado, bytes=%v", buf)
	}
}

func TestAppendEscPOSPagamentos_CenterDescFor56mm(t *testing.T) {
	var buf []byte
	req := models.ConferenciaRequest{
		Pagamentos: []models.ConferenciaPagamento{
			{Descricao: "Cartao", Valor: 10},
		},
	}

	appendEscPOSPagamentos(&buf, req, 31, true)

	centeredDesc := encodeCP850(centerASCII("Cartao", 31))
	leftRightDesc := encodeCP850(leftRightASCII("Cartao", "R$ 10,00", 31))
	if !bytes.Contains(buf, centeredDesc) {
		t.Fatalf("esperava descricao centralizada em 56mm, bytes=%v", buf)
	}
	if bytes.Contains(buf, leftRightDesc) {
		t.Fatalf("nao esperava layout esquerda/direita em 56mm, bytes=%v", buf)
	}
}

func TestBuildFallbackConferenciaComanda_CenterDescFor56mm(t *testing.T) {
	req := models.ConferenciaRequest{
		Tipo:     "delivery",
		Modelo:   "56mm",
		Driver:   "COZINHA",
		Operador: "Maria",
		Pagamentos: []models.ConferenciaPagamento{
			{Descricao: "Cartao", Valor: 10},
		},
	}
	empresa := config.EmpresaParametros{Nome: "GooPedir", CNPJ: "123", Razao: "GooPedir"}

	got := buildFallbackConferenciaComanda(req, empresa, time.Date(2026, 6, 26, 17, 0, 0, 0, time.Local), 31, config.DefaultConferenciaConfig())

	if !strings.Contains(got, centerASCII("Cartao", 31)+"\n") {
		t.Fatalf("esperava descricao centralizada no fallback 56mm, got=%q", got)
	}
	if strings.Contains(got, leftRightASCII("Cartao", "R$ 10,00", 31)) {
		t.Fatalf("nao esperava layout esquerda/direita no fallback 56mm, got=%q", got)
	}
}

func TestAppendEscPOSComandaHeader_UsesSequencialAndTipoWithoutBlackBar(t *testing.T) {
	var buf []byte
	req := models.ConferenciaRequest{Tipo: "vem busca", Sequencial: 7}

	appendEscPOSComandaHeader(&buf, req, 31)

	if !bytes.Contains(buf, encodeCP850(centerASCII("#007", 31))) {
		t.Fatalf("esperava numero formatado no cabecalho, bytes=%v", buf)
	}
	if !bytes.Contains(buf, encodeCP850(centerASCII("Vem Buscar", 31))) {
		t.Fatalf("esperava tipo formatado no cabecalho, bytes=%v", buf)
	}
	if bytes.Contains(buf, []byte{0x1D, 0x42, 0x01}) {
		t.Fatalf("nao esperava tarja preta no novo cabecalho, bytes=%v", buf)
	}
}

func TestBuildFallbackConferenciaComanda_UsesSequencialAndTipoLines(t *testing.T) {
	req := models.ConferenciaRequest{
		Tipo:       "delivery",
		Sequencial: 12,
		Driver:     "COZINHA",
	}
	empresa := config.EmpresaParametros{Nome: "GooPedir", CNPJ: "123", Razao: "GooPedir"}

	got := buildFallbackConferenciaComanda(req, empresa, time.Date(2026, 6, 26, 17, 0, 0, 0, time.Local), 31, config.DefaultConferenciaConfig())

	if !strings.Contains(got, centerASCII("#012", 31)+"\n") {
		t.Fatalf("esperava sequencial em linha propria, got=%q", got)
	}
	if !strings.Contains(got, centerASCII("Delivery", 31)+"\n") {
		t.Fatalf("esperava tipo em linha propria, got=%q", got)
	}
	if strings.Contains(got, "DELIVERY 012") {
		t.Fatalf("nao esperava formato antigo tipo+numero na mesma linha, got=%q", got)
	}
}

func TestAppendEscPOSComandaInfo_PrintsFidelidadeWhenGreaterThanZero(t *testing.T) {
	var buf []byte
	req := models.ConferenciaRequest{
		Tipo: "delivery",
		Cliente: models.ConferenciaCliente{
			Pedidos:    3,
			Fidelidade: 25,
		},
	}

	appendEscPOSComandaInfo(&buf, req, time.Date(2026, 6, 26, 17, 0, 0, 0, time.Local), 31)

	boldSeq := append([]byte{0x1B, 0x45, 0x01}, encodeCP850("25 pontos de fidelidade")...)
	if !bytes.Contains(buf, boldSeq) {
		t.Fatalf("esperava fidelidade em negrito, bytes=%v", buf)
	}
}

func TestBuildFallbackConferenciaComanda_SkipsFidelidadeWhenZero(t *testing.T) {
	req := models.ConferenciaRequest{
		Tipo: "delivery",
		Cliente: models.ConferenciaCliente{
			Pedidos:    3,
			Fidelidade: 0,
		},
		Driver: "COZINHA",
	}
	empresa := config.EmpresaParametros{Nome: "GooPedir", CNPJ: "123", Razao: "GooPedir"}

	got := buildFallbackConferenciaComanda(req, empresa, time.Date(2026, 6, 26, 17, 0, 0, 0, time.Local), 31, config.DefaultConferenciaConfig())

	if strings.Contains(got, "pontos de fidelidade") {
		t.Fatalf("nao esperava fidelidade quando zerada, got=%q", got)
	}
}

func TestOperadorLinha_AlignsNameToRight(t *testing.T) {
	got := operadorLinha("Maria", "", 31)
	want := leftRightASCII("Operador:", "Maria", 31)
	if got != want {
		t.Fatalf("linha do operador inesperada: got=%q want=%q", got, want)
	}
}

func TestCaixaNameQtyMoneyLine_FormatsFractionalQuantity(t *testing.T) {
	got := caixaNameQtyMoneyLine("LANCHE", 1.5, 10, 31)
	if !strings.Contains(got, "1,5") {
		t.Fatalf("esperava quantidade fracionada formatada com vírgula, got=%q", got)
	}
}

func TestBuildFallbackConferenciaComanda_UsesBlankValueLineForSingleProdutoAndMessage(t *testing.T) {
	req := models.ConferenciaRequest{
		Tipo:   "delivery",
		Driver: "COZINHA",
		Itens: []models.ConferenciaItem{
			{Produtos: []models.Produto{{Categoria: "Lanches", Nome: "X Salada", Quantidade: 1}}},
		},
	}
	empresa := config.EmpresaParametros{Nome: "GooPedir"}
	cfg := config.ConferenciaConfig{Fonte: "normal", Delimitador: "=", MensagemFinal: "Obrigado pela preferencia!"}

	got := buildFallbackConferenciaComanda(req, empresa, time.Date(2026, 6, 26, 17, 0, 0, 0, time.Local), 31, cfg)

	if !strings.Contains(got, centerASCII("Obrigado pela preferencia!", 31)) {
		t.Fatalf("esperava mensagem final configurada, got=%q", got)
	}
	if !strings.Contains(got, conferenciaMoneyLineWithFill("", "R$ 0,00", 31, cfg, false)) {
		t.Fatalf("esperava linha de valor com espacos para produto unico, got=%q", got)
	}
	if !strings.Contains(got, conferenciaCategoryLine("LANCHES", 31, cfg)) {
		t.Fatalf("esperava delimitador configurado na categoria, got=%q", got)
	}
	if !strings.Contains(got, strings.Repeat("=", 31)) {
		t.Fatalf("esperava delimitador configurado no rodape, got=%q", got)
	}
}

func TestBuildFallbackConferenciaComanda_DoesNotAddExtraDelimiterBelowProduto(t *testing.T) {
	req := models.ConferenciaRequest{
		Tipo:   "delivery",
		Driver: "COZINHA",
		Itens: []models.ConferenciaItem{
			{Produtos: []models.Produto{{Categoria: "Lanches", Nome: "X Salada", Quantidade: 2}}},
		},
	}
	empresa := config.EmpresaParametros{Nome: "GooPedir"}
	cfg := config.ConferenciaConfig{Fonte: "normal", Delimitador: "-", MensagemFinal: ""}

	got := buildFallbackConferenciaComanda(req, empresa, time.Date(2026, 6, 26, 17, 0, 0, 0, time.Local), 31, cfg)

	if strings.Contains(got, "2x - X Salada\n"+conferenciaMoneyLine("", "R$ 0,00", 31, cfg)+"\n-------------------------------\n") {
		t.Fatalf("nao esperava linha extra abaixo do produto, got=%q", got)
	}
}

func TestBuildEscPOSConferencia_UsesConfiguredFontBlankValueLineAndFooter(t *testing.T) {
	req := models.ConferenciaRequest{
		Tipo:   "delivery",
		Driver: "COZINHA",
		Itens: []models.ConferenciaItem{
			{Produtos: []models.Produto{{Categoria: "Lanches", Nome: "X Salada", Quantidade: 1}}},
		},
	}
	empresa := config.EmpresaParametros{Nome: "GooPedir"}
	cfg := config.ConferenciaConfig{Fonte: "pequena", Delimitador: "*", MensagemFinal: "Valeu!"}

	got := buildEscPOSConferencia(req, empresa, time.Date(2026, 6, 26, 17, 0, 0, 0, time.Local), false, config.PrinterMargins{}, 576, 48, 47, cfg)

	if !bytes.Contains(got, []byte{0x1B, 0x4D, 0x01, 0x1D, 0x21, 0x00}) {
		t.Fatalf("esperava fonte pequena configurada no ESC/POS, bytes=%v", got)
	}
	if !bytes.Contains(got, encodeCP850(conferenciaMoneyLineWithFill("", "R$ 0,00", 47, cfg, false))) {
		t.Fatalf("esperava linha de valor com espacos para produto unico, bytes=%v", got)
	}
	if !bytes.Contains(got, encodeCP850(centerASCII("Valeu!", 47))) {
		t.Fatalf("esperava mensagem final configurada no ESC/POS, bytes=%v", got)
	}
	if !bytes.Contains(got, encodeCP850(conferenciaCategoryLine("LANCHES", 47, cfg))) {
		t.Fatalf("esperava delimitador configurado na categoria ESC/POS, bytes=%v", got)
	}
}

func TestBuildEscPOSConferencia_DoesNotAddExtraDelimiterBelowProduto(t *testing.T) {
	req := models.ConferenciaRequest{
		Tipo:   "delivery",
		Driver: "COZINHA",
		Itens: []models.ConferenciaItem{
			{Produtos: []models.Produto{{Categoria: "Lanches", Nome: "X Salada", Quantidade: 2}}},
		},
	}
	empresa := config.EmpresaParametros{Nome: "GooPedir"}
	cfg := config.ConferenciaConfig{Fonte: "normal", Delimitador: "-", MensagemFinal: ""}

	got := buildEscPOSConferencia(req, empresa, time.Date(2026, 6, 26, 17, 0, 0, 0, time.Local), false, config.PrinterMargins{}, 576, 48, 47, cfg)

	if bytes.Contains(got, encodeCP850("2x - X Salada\n"+conferenciaMoneyLine("", "R$ 0,00", 47, cfg)+"\n-----------------------------------------------\n")) {
		t.Fatalf("nao esperava linha extra abaixo do produto no ESC/POS, bytes=%v", got)
	}
}

func TestBuildFallbackConferenciaComanda_UsesDelimiterOnlyBeforeNextProdutoInCategory(t *testing.T) {
	req := models.ConferenciaRequest{
		Tipo:   "delivery",
		Driver: "COZINHA",
		Itens: []models.ConferenciaItem{
			{Produtos: []models.Produto{
				{Categoria: "Lanches", Nome: "X Salada", Quantidade: 1},
				{Categoria: "Lanches", Nome: "X Bacon", Quantidade: 1},
			}},
		},
	}
	empresa := config.EmpresaParametros{Nome: "GooPedir"}
	cfg := config.ConferenciaConfig{Fonte: "normal", Delimitador: "-", MensagemFinal: ""}

	got := buildFallbackConferenciaComanda(req, empresa, time.Date(2026, 6, 26, 17, 0, 0, 0, time.Local), 31, cfg)

	if !strings.Contains(got, "1x - X Salada\n"+conferenciaMoneyLineWithFill("", "R$ 0,00", 31, cfg, true)+"\n1x - X Bacon\n"+conferenciaMoneyLineWithFill("", "R$ 0,00", 31, cfg, false)) {
		t.Fatalf("esperava delimitador so antes do proximo produto da mesma categoria, got=%q", got)
	}
}

func TestBuildEscPOSConferencia_UsesDelimiterOnlyBeforeNextProdutoInCategory(t *testing.T) {
	req := models.ConferenciaRequest{
		Tipo:   "delivery",
		Driver: "COZINHA",
		Itens: []models.ConferenciaItem{
			{Produtos: []models.Produto{
				{Categoria: "Lanches", Nome: "X Salada", Quantidade: 1},
				{Categoria: "Lanches", Nome: "X Bacon", Quantidade: 1},
			}},
		},
	}
	empresa := config.EmpresaParametros{Nome: "GooPedir"}
	cfg := config.ConferenciaConfig{Fonte: "normal", Delimitador: "-", MensagemFinal: ""}

	got := buildEscPOSConferencia(req, empresa, time.Date(2026, 6, 26, 17, 0, 0, 0, time.Local), false, config.PrinterMargins{}, 576, 48, 47, cfg)

	if !bytes.Contains(got, encodeCP850("1x - X Salada\n"+conferenciaMoneyLineWithFill("", "R$ 0,00", 47, cfg, true)+"\n")) {
		t.Fatalf("esperava delimitador no valor do primeiro produto da categoria no ESC/POS, bytes=%v", got)
	}
	if !bytes.Contains(got, encodeCP850("1x - X Bacon\n")) {
		t.Fatalf("esperava segundo produto na mesma categoria no ESC/POS, bytes=%v", got)
	}
	if !bytes.Contains(got, encodeCP850(conferenciaMoneyLineWithFill("", "R$ 0,00", 47, cfg, false))) {
		t.Fatalf("esperava delimitador so antes do proximo produto da mesma categoria no ESC/POS, bytes=%v", got)
	}
}

func TestDuplicateConferenciaPayload_RepeatsByCopies(t *testing.T) {
	got := duplicateConferenciaPayload([]byte{0x01, 0x02}, 3)
	want := []byte{0x01, 0x02, 0x01, 0x02, 0x01, 0x02}
	if !bytes.Equal(got, want) {
		t.Fatalf("payload duplicado inesperado: got=%v want=%v", got, want)
	}
}

func TestDuplicateConferenciaText_RepeatsByCopies(t *testing.T) {
	got := duplicateConferenciaText("ABC\n", 2)
	if got != "ABC\nABC\n" {
		t.Fatalf("texto duplicado inesperado: got=%q", got)
	}
}
