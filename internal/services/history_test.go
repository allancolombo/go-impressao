package services

import (
	"testing"

	"github.com/goopedir/go-impressao/internal/models"
)

func TestBuildResumo(t *testing.T) {
	req := models.ImpressaoCozinhaRequest{
		Produtos: []models.Produto{
			{Nome: "X Salada", Quantidade: 2},
			{Nome: "Coca", Quantidade: 1},
			{Nome: "Batata", Quantidade: 1},
			{Nome: "Doce", Quantidade: 1},
		},
	}

	got := BuildResumo(req)
	if got != "2Un X Salada, Coca, Batata, …" {
		t.Fatalf("resumo inesperado: %q", got)
	}
}

func TestParseCursor(t *testing.T) {
	if n, err := ParseCursor(""); err != nil || n != 0 {
		t.Fatalf("esperava 0,nil: %d,%v", n, err)
	}
	if _, err := ParseCursor("-1"); err == nil {
		t.Fatalf("esperava erro")
	}
	if _, err := ParseCursor("x"); err == nil {
		t.Fatalf("esperava erro")
	}
}
