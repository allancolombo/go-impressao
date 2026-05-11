package models

import "testing"

func TestImpressaoCozinhaRequestValidate_OK(t *testing.T) {
	req := ImpressaoCozinhaRequest{
		Tipo:       "mesa/retirar",
		Numero:     10,
		Usuario:    "Maria",
		Driver:     "EPSON TM-T20",
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

func TestImpressaoCozinhaRequestValidate_Erros(t *testing.T) {
	req := ImpressaoCozinhaRequest{}
	req.Normalize()
	if err := req.Validate(); err == nil {
		t.Fatalf("esperava erro, mas veio nil")
	}
}
