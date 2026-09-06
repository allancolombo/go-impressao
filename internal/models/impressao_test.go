package models

import (
	"encoding/json"
	"testing"
)

func TestImpressaoCozinhaRequestValidate_OK(t *testing.T) {
	req := ImpressaoCozinhaRequest{
		Tipo:       "mesa/retirar",
		Numero:     10,
		Usuario:    "Maria",
		Driver:     "EPSON 01",
		Impressora: "Cozinha",
		Produtos: []Produto{
			{
				Nome:       "Pizza",
				Quantidade: 1,
				Observacoes: "",
				Extras: []Extra{
					{Nome: "Borda recheada", Quantidade: 1},
				},
			},
		},
	}

	req.Normalize()
	if err := req.Validate(); err != nil {
		t.Fatalf("esperava válido, mas deu erro: %v", err)
	}
}

func TestImpressaoCozinhaRequestValidate_NumeroZero_OK(t *testing.T) {
	req := ImpressaoCozinhaRequest{
		Tipo:       "mesa",
		Numero:     0,
		Usuario:    "Maria",
		Driver:     "COZINHA (Windows)",
		Impressora: "Bar (ERP)",
		Produtos: []Produto{
			{Nome: "Café", Quantidade: 1},
		},
	}

	req.Normalize()
	if err := req.Validate(); err != nil {
		t.Fatalf("esperava válido com numero=0, mas deu erro: %v", err)
	}
}

func TestImpressaoCozinhaRequestValidate_ModeloOpcional_OK(t *testing.T) {
	req := ImpressaoCozinhaRequest{
		Tipo:       "mesa",
		Numero:     1,
		Usuario:    "Maria",
		Driver:     "COZINHA (Windows)",
		Modelo:     "56MM",
		Impressora: "Bar (ERP)",
		Produtos: []Produto{
			{Nome: "Café", Quantidade: 1},
		},
	}

	req.Normalize()
	if req.Modelo != "56mm" {
		t.Fatalf("esperava modelo normalizado, got: %q", req.Modelo)
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("esperava válido com modelo=56mm, mas deu erro: %v", err)
	}
}

func TestImpressaoCozinhaRequestValidate_ModeloInvalido(t *testing.T) {
	req := ImpressaoCozinhaRequest{
		Tipo:       "mesa",
		Numero:     1,
		Usuario:    "Maria",
		Driver:     "COZINHA (Windows)",
		Modelo:     "42mm",
		Impressora: "Bar (ERP)",
		Produtos: []Produto{
			{Nome: "Café", Quantidade: 1},
		},
	}

	req.Normalize()
	if err := req.Validate(); err == nil {
		t.Fatalf("esperava erro para modelo inválido, mas veio nil")
	}
}

func TestImpressaoCozinhaRequestValidate_Erros(t *testing.T) {
	req := ImpressaoCozinhaRequest{}
	req.Normalize()
	if err := req.Validate(); err == nil {
		t.Fatalf("esperava erro, mas veio nil")
	}
}

func TestCaixaFechamentoRequestUnmarshal_QuantidadeFracionada(t *testing.T) {
	body := []byte(`{
		"imprimir_agora": true,
		"driver": "EPSON 01",
		"modelo": "80mm",
		"compuatado": [{"id": 955}],
		"categorias": [{"produto": "LANCHE", "quantidade": 89.5, "totalGeral": 2456.98}],
		"produtos": [{"produto": "X SALADA", "quantidade": 47.5, "total": 1188.00}]
	}`)

	var req CaixaFechamentoRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("não deveria falhar no unmarshal com quantidade fracionada: %v", err)
	}
	if got := float64(req.Categorias[0].Quantidade); got != 89.5 {
		t.Fatalf("quantidade fracionada da categoria inesperada: %v", got)
	}
	if got := float64(req.Produtos[0].Quantidade); got != 47.5 {
		t.Fatalf("quantidade fracionada do produto inesperada: %v", got)
	}
}
