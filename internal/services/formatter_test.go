package services

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/goopedir/go-impressao/internal/models"
)

func TestFormatComandaCozinha(t *testing.T) {
	f := NewFormatter()
	when := time.Date(2026, 5, 4, 10, 11, 12, 0, time.Local)

	req := models.ImpressaoCozinhaRequest{
		Tipo:       models.TipoMesa,
		Numero:     1,
		Usuario:    "João",
		Driver:     "DriverQualquer",
		Impressora: "Cozinha",
		Produtos: []models.Produto{
			{
				Nome:       "X-Burger",
				Quantidade: 2,
				Extras: []models.Extra{
					{Nome: "Bacon", Quantidade: 1},
				},
				Observacoes: "sem cebola",
			},
		},
	}

	got := f.FormatComandaCozinha(req, when)
	want := centerLine("mesa 1", 48) + "\n" +
		"\n" +
		"2Un - X-Burger\n" +
		"  - Bacon\n" +
		"sem cebola\n" +
		strings.Repeat("-", 48) + "\n" +
		"Joao (Cozinha)\n" +
		"04/05/2026 10:11:12\n" +
		centerLine("By GooPedir", 48) + "\n"

	if got != want {
		t.Fatalf("formatação inesperada.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestFormatComandaCozinha_NumeroZero_NaoImprimeZero(t *testing.T) {
	f := NewFormatter()
	when := time.Date(2026, 5, 4, 10, 11, 12, 0, time.Local)

	req := models.ImpressaoCozinhaRequest{
		Tipo:       "mesa",
		Numero:     0,
		Usuario:    "João",
		Driver:     "DriverQualquer",
		Impressora: "Cozinha",
		Produtos: []models.Produto{
			{Nome: "X-Burger", Quantidade: 1},
		},
	}

	got := f.FormatComandaCozinha(req, when)
	if strings.Contains(got, "mesa 0") {
		t.Fatalf("não deveria imprimir numero=0 no cabeçalho: %q", got)
	}
	if !strings.HasPrefix(got, centerLine("mesa", 48)+"\n") {
		t.Fatalf("cabeçalho inesperado: %q", got)
	}
}

func TestFormatComandaCozinha_NumeroZero_ComCliente(t *testing.T) {
	f := NewFormatter()
	when := time.Date(2026, 5, 4, 10, 11, 12, 0, time.Local)

	req := models.ImpressaoCozinhaRequest{
		Tipo:       "retirar",
		Numero:     0,
		Cliente:    "Allan Colombo",
		Usuario:    "João",
		Driver:     "DriverQualquer",
		Impressora: "Cozinha",
		Produtos: []models.Produto{
			{Nome: "X-Burger", Quantidade: 1},
		},
	}

	got := f.FormatComandaCozinha(req, when)
	if !strings.Contains(got, centerLine("Allan Colombo", 48)+"\n") {
		t.Fatalf("esperava cliente centralizado abaixo do cabeçalho, got: %q", got)
	}
}

func TestFormatComandaCozinha_Wrap80mm(t *testing.T) {
	f := NewFormatter()
	when := time.Date(2026, 5, 4, 10, 11, 12, 0, time.Local)

	req := models.ImpressaoCozinhaRequest{
		Tipo:       "mesa",
		Numero:     99,
		Usuario:    "Allan",
		Driver:     "COZINHA (Windows)",
		Impressora: "Bar (ERP)",
		Produtos: []models.Produto{
			{
				Nome:       "Hambúrguer artesanal com queijo, bacon e cebola caramelizada",
				Quantidade: 1,
				Extras: []models.Extra{
					{Nome: "Molho especial da casa com pimenta e alho", Quantidade: 1},
				},
				Observacoes: "Entregar no balcão do fundo, por favor",
			},
		},
	}

	got := f.FormatComandaCozinha(req, when)
	for _, line := range strings.Split(strings.ReplaceAll(got, "\r\n", "\n"), "\n") {
		if len(line) == 0 {
			continue
		}
		if utf8.RuneCountInString(line) > 48 {
			t.Fatalf("linha excedeu 48 colunas: %q (%d)", line, utf8.RuneCountInString(line))
		}
	}
}

func TestFormatComandaCozinha_Wrap56mm(t *testing.T) {
	f := NewFormatter()
	when := time.Date(2026, 5, 4, 10, 11, 12, 0, time.Local)

	req := models.ImpressaoCozinhaRequest{
		Tipo:       "mesa",
		Numero:     7,
		Usuario:    "Allan",
		Driver:     "COZINHA (Windows)",
		Modelo:     "56mm",
		Impressora: "Bar (ERP)",
		Produtos: []models.Produto{
			{
				Nome:       "Hambúrguer artesanal com queijo, bacon e cebola caramelizada",
				Quantidade: 1,
				Extras: []models.Extra{
					{Nome: "Molho especial da casa com pimenta e alho", Quantidade: 1},
				},
				Observacoes: "Entregar no balcão do fundo, por favor",
			},
		},
	}

	got := f.FormatComandaCozinhaWithCols(req, when, 32)
	for _, line := range strings.Split(strings.ReplaceAll(got, "\r\n", "\n"), "\n") {
		if len(line) == 0 {
			continue
		}
		if utf8.RuneCountInString(line) > 32 {
			t.Fatalf("linha excedeu 32 colunas: %q (%d)", line, utf8.RuneCountInString(line))
		}
	}
}

func TestFormatComandaCozinha_Categorias(t *testing.T) {
	f := NewFormatter()
	when := time.Date(2026, 5, 4, 10, 11, 12, 0, time.Local)

	req := models.ImpressaoCozinhaRequest{
		Tipo:       "mesa",
		Numero:     1,
		Usuario:    "Allan",
		Driver:     "COZINHA (Windows)",
		Impressora: "Bar (ERP)",
		Produtos: []models.Produto{
			{
				Categoria:  "Lanches",
				Nome:       "X Salada",
				Quantidade: 3,
				Extras: []models.Extra{
					{Categoria: "Adicionais", Nome: "Bacon", Quantidade: 2},
				},
				Observacoes: "Para as 22h",
			},
			{
				Categoria:  "Bebidas",
				Nome:       "Coca",
				Quantidade: 1,
			},
		},
	}

	got := f.FormatComandaCozinha(req, when)
	if !strings.Contains(got, "LANCHES\n") {
		t.Fatalf("esperava categoria de produto: %q", got)
	}
	if !strings.Contains(got, "ADICIONAIS\n") {
		t.Fatalf("esperava categoria de extra: %q", got)
	}
	if !strings.Contains(got, "3Un - X Salada\n") {
		t.Fatalf("esperava quantidade com Un: %q", got)
	}
	if !strings.Contains(got, "  - 2Un Bacon\n") {
		t.Fatalf("esperava extra com Un: %q", got)
	}
	if !strings.Contains(got, "\nBEBIDAS\n1Un - Coca\n") {
		t.Fatalf("esperava mudança de categoria no segundo produto: %q", got)
	}
}

func TestFormatComandaCozinha_Sabores_ImprimeFracao(t *testing.T) {
	f := NewFormatter()
	when := time.Date(2026, 5, 4, 10, 11, 12, 0, time.Local)

	req := models.ImpressaoCozinhaRequest{
		Tipo:       "mesa",
		Numero:     10,
		Usuario:    "Allan",
		Driver:     "COZINHA (Windows)",
		Impressora: "Cozinha",
		Produtos: []models.Produto{
			{
				Nome:       "Pizza",
				Quantidade: 1,
				Extras: []models.Extra{
					{Categoria: "SABORES", Nome: "Calabresa", Quantidade: 2},
					{Categoria: "SABORES", Nome: "Frango", Quantidade: 2},
				},
			},
		},
	}

	got := f.FormatComandaCozinha(req, when)
	if !strings.Contains(got, "  - 2/4 Calabresa\n") {
		t.Fatalf("esperava fração no sabor Calabresa, got: %q", got)
	}
	if !strings.Contains(got, "  - 2/4 Frango\n") {
		t.Fatalf("esperava fração no sabor Frango, got: %q", got)
	}
}

func TestFormatComandaCozinha_OrdenaExtrasPorCategoria(t *testing.T) {
	f := NewFormatter()
	when := time.Date(2026, 5, 4, 10, 11, 12, 0, time.Local)

	req := models.ImpressaoCozinhaRequest{
		Tipo:       "mesa",
		Numero:     10,
		Usuario:    "Allan",
		Driver:     "COZINHA (Windows)",
		Impressora: "Cozinha",
		Produtos: []models.Produto{
			{
				Nome:       "Pizza",
				Quantidade: 1,
				Extras: []models.Extra{
					{Categoria: "SABORES", Nome: "Calabresa", Quantidade: 2},
					{Categoria: "Adicionais", Nome: "Bacon", Quantidade: 1},
					{Categoria: "Ingredientes", Nome: "Massa fina", Quantidade: 1},
					{Categoria: "Bordas", Nome: "Catupiry", Quantidade: 1},
					{Categoria: "Outros", Nome: "Observacao", Quantidade: 1},
					{Categoria: "SABORES", Nome: "Frango", Quantidade: 2},
				},
			},
		},
	}

	got := f.FormatComandaCozinha(req, when)

	iIng := strings.Index(got, "INGREDIENTES\n")
	iAdd := strings.Index(got, "ADICIONAIS\n")
	iSab := strings.Index(got, "SABORES\n")
	iBor := strings.Index(got, "BORDAS\n")
	iOut := strings.Index(got, "OUTROS\n")

	if iIng == -1 || iAdd == -1 || iSab == -1 || iBor == -1 || iOut == -1 {
		t.Fatalf("faltou alguma categoria esperada, got: %q", got)
	}
	if !(iIng < iAdd && iAdd < iSab && iSab < iBor && iBor < iOut) {
		t.Fatalf("ordem incorreta de categorias: ing=%d add=%d sab=%d bor=%d out=%d, got: %q", iIng, iAdd, iSab, iBor, iOut, got)
	}
}
