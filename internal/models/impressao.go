package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// TipoImpressao representa o tipo de comanda enviada no payload.
// Não há validação de valores: o serviço apenas imprime exatamente o texto informado.
type TipoImpressao string

const (
	TipoMesa    TipoImpressao = "mesa"
	TipoRetirar TipoImpressao = "retirar"
)

// ImpressaoCozinhaRequest é o payload do endpoint POST /impressao/cozinha.
type ImpressaoCozinhaRequest struct {
	Tipo          TipoImpressao `json:"tipo"`
	Numero        int           `json:"numero"`
	UUID          string        `json:"uuid,omitempty"`
	Cliente       string        `json:"cliente,omitempty"`
	Usuario       string        `json:"usuario"`
	Driver        string        `json:"driver"`
	Impressora    string        `json:"impressora"`
	ImprimirAgora bool          `json:"imprimir_agora"`
	Produtos      []Produto     `json:"produtos"`
}

// Produto representa um item da comanda.
type Produto struct {
	UUID           string  `json:"uuid,omitempty"`
	Categoria      string  `json:"categoria"`
	Nome           string  `json:"nome"`
	Quantidade     int     `json:"quantidade"`
	ValorUnitario  float64 `json:"valor_unitario,omitempty"`
	ValorTotal     float64 `json:"valor_total,omitempty"`
	ValorAdicional float64 `json:"valor_adicional,omitempty"`
	Observacoes    string  `json:"observacoes"`
	Extras         []Extra `json:"extras"`
}

// Extra representa um adicional dentro de um produto.
type Extra struct {
	Categoria  string `json:"categoria"`
	Nome       string `json:"nome"`
	Quantidade int    `json:"quantidade"`
}

type ConferenciaRequest struct {
	Tipo               string                 `json:"tipo,omitempty"`
	Sequencial         int                    `json:"sequencial,omitempty"`
	Mesa               string                 `json:"mesa"`
	TaxaServicoPercent float64                `json:"taxa_servico_percent"`
	TaxaServicoValor   float64                `json:"taxa_servico_valor"`
	TaxaEntregaValor   float64                `json:"taxa_entrega_valor,omitempty"`
	Desconto           string                 `json:"desconto,omitempty"`
	ValorDesconto      float64                `json:"valor_desconto,omitempty"`
	TotalProdutos      float64                `json:"total_produtos"`
	TotalGeral         float64                `json:"total_geral"`
	Operador           string                 `json:"operador"`
	CX                 string                 `json:"cx"`
	ImprimirAgora      bool                   `json:"imprimir_agora"`
	Driver             string                 `json:"driver"`
	Modelo             string                 `json:"modelo"` // "56mm" | "58mm" | "80mm"
	Itens              []ConferenciaItem `json:"itens"`
	Cliente            ConferenciaCliente     `json:"cliente,omitempty"`
	Pagamentos         []ConferenciaPagamento `json:"-"`
	Codigo             int                    `json:"codigo,omitempty"`
	Data               int                    `json:"data,omitempty"`
	Hora               float64                `json:"hora,omitempty"`
	Endereco           ConferenciaEndereco    `json:"endereco,omitempty"`
	NFCEChave          string                 `json:"nfceChave,omitempty"`
	NFCEProtocolo      string                 `json:"nfceProtocolo,omitempty"`
	NFCENumero         string                 `json:"nfceNumero,omitempty"`
}

type SangriaRequest struct {
	Descricao     string  `json:"descricao"`
	Valor         float64 `json:"valor"`
	Operador      string  `json:"operador"`
	CX            string  `json:"cx"`
	ImprimirAgora bool    `json:"imprimir_agora"`
	Driver        string  `json:"driver"`
	Modelo        string  `json:"modelo"` // "56mm" | "58mm" | "80mm"
}

type CaixaFechamentoRequest struct {
	ImprimirAgora bool   `json:"imprimir_agora"`
	Driver        string `json:"driver"`
	Modelo        string `json:"modelo"` // "56mm" | "58mm" | "80mm"

	Computado  []CaixaComputadoItem `json:"-"`
	Lancado    []CaixaLancadoItem   `json:"lancado"`
	Sangria    []CaixaSangriaItem   `json:"sangria"`
	Produtos   []CaixaProdutoItem   `json:"produtos"`
	Categorias []CaixaCategoriaItem `json:"categorias"`
	Motoboy    []CaixaMotoboyItem   `json:"motoboy"`
}

type CaixaComputadoItem struct {
	ID             int       `json:"id"`
	DataAbertura   string    `json:"dataAbertura"`
	HoraAbertura   string    `json:"horaAbertura"`
	DataFechamento string    `json:"dataFechamento"`
	HoraFechamento string    `json:"horaFechamento"`
	ValorAbertura  FlexFloat `json:"valorAbertura"`
	ValorFechamento FlexFloat `json:"valorFechamento"`

	Descricao string    `json:"descricao"`
	Valor     FlexFloat `json:"valor"`
	Usuario   string    `json:"usuario"`

	ValorMesa      FlexFloat `json:"valorMesa"`
	ValorVemBuscar FlexFloat `json:"valorVemBuscar"`
	ValorDelivery  FlexFloat `json:"valorDelivery"`
	Servico        FlexFloat `json:"servico"`
}

type CaixaLancadoItem struct {
	ID        int       `json:"id,omitempty"`
	Descricao string    `json:"descricao"`
	Valor     FlexFloat `json:"valor"`
}

type CaixaSangriaItem struct {
	Descricao string    `json:"descricao"`
	Valor     FlexFloat `json:"valor"`
}

type CaixaProdutoItem struct {
	ID          int       `json:"id,omitempty"`
	Produto     string    `json:"produto"`
	Quantidade  int       `json:"quantidade"`
	Total       FlexFloat `json:"total"`
	TotalGeral  FlexFloat `json:"totalGeral,omitempty"`
	TotalAdicional FlexFloat `json:"totalAdicional,omitempty"`
}

type CaixaCategoriaItem struct {
	ID            int       `json:"id,omitempty"`
	Produto       string    `json:"produto"`
	Quantidade    int       `json:"quantidade"`
	TotalGeral    FlexFloat `json:"totalGeral"`
	Total         FlexFloat `json:"total,omitempty"`
	TotalAdicional FlexFloat `json:"totalAdicional,omitempty"`
}

type CaixaMotoboyItem struct {
	ID         int       `json:"id,omitempty"`
	Motoboy    string    `json:"motoboy"`
	Codigo     string    `json:"codigo,omitempty"`
	TaxaEntrega FlexFloat `json:"taxaEntrega"`
	Total      FlexFloat `json:"total"`
	Bairro     string    `json:"bairro,omitempty"`
}

type ConferenciaItem struct {
	Produtos []Produto `json:"produtos"`
}

type ConferenciaCliente struct {
	Nome    string                 `json:"nome"`
	CPF     string                 `json:"cpf,omitempty"`
	Celular string                 `json:"celular"`
	Pedidos FlexInt                `json:"pedidos"`
}

type ConferenciaEndereco struct {
	Rua         string `json:"rua"`
	Numero      string `json:"numero"`
	Bairro      string `json:"bairro"`
	Cidade      string `json:"cidade"`
	Complemento string `json:"complemento"`
}

type ConferenciaPagamento struct {
	Descricao string  `json:"descricao"`
	Nome      string  `json:"nome"`
	Valor     float64 `json:"valor"`
	Troco     float64 `json:"troco"`
	Faturado  bool    `json:"faturado,omitempty"`
	Transacao string  `json:"transacao,omitempty"`
}

type FlexInt int

func (n *FlexInt) UnmarshalJSON(b []byte) error {
	raw := strings.TrimSpace(string(b))
	if raw == "null" || raw == "" {
		*n = 0
		return nil
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*n = 0
			return nil
		}
		i, err := strconv.Atoi(s)
		if err != nil {
			*n = 0
			return nil
		}
		*n = FlexInt(i)
		return nil
	}
	var i int
	if err := json.Unmarshal(b, &i); err == nil {
		*n = FlexInt(i)
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*n = FlexInt(int(f))
		return nil
	}
	*n = 0
	return nil
}

func (r *ConferenciaRequest) UnmarshalJSON(b []byte) error {
	type Alias ConferenciaRequest
	var a struct {
		Alias
		Pagamento  []ConferenciaPagamento `json:"pagamento"`
		Pagamentos []ConferenciaPagamento `json:"pagamentos"`
		TaxaEntrega      float64          `json:"taxa_entrega"`
		TaxaEntregaValor float64          `json:"taxa_entrega_valor"`
		ValorDesconto    float64          `json:"valor_desconto"`
		NFCEChave        string           `json:"nfceChave"`
		NFCEProtocolo    string           `json:"nfceProtocolo"`
		NFCENumero       string           `json:"nfceNumero"`
		NFCEChaveSnake     string         `json:"nfce_chave"`
		NFCEProtocoloSnake string         `json:"nfce_protocolo"`
		NFCENumeroSnake    string         `json:"nfce_numero"`
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*r = ConferenciaRequest(a.Alias)
	if len(a.Pagamentos) > 0 {
		r.Pagamentos = a.Pagamentos
	} else {
		r.Pagamentos = a.Pagamento
	}
	if a.TaxaEntregaValor > 0 {
		r.TaxaEntregaValor = a.TaxaEntregaValor
	} else if a.TaxaEntrega > 0 {
		r.TaxaEntregaValor = a.TaxaEntrega
	}
	if a.ValorDesconto > 0 {
		r.ValorDesconto = a.ValorDesconto
	}

	if strings.TrimSpace(a.NFCEChave) != "" {
		r.NFCEChave = a.NFCEChave
	} else if strings.TrimSpace(a.NFCEChaveSnake) != "" {
		r.NFCEChave = a.NFCEChaveSnake
	}
	if strings.TrimSpace(a.NFCEProtocolo) != "" {
		r.NFCEProtocolo = a.NFCEProtocolo
	} else if strings.TrimSpace(a.NFCEProtocoloSnake) != "" {
		r.NFCEProtocolo = a.NFCEProtocoloSnake
	}
	if strings.TrimSpace(a.NFCENumero) != "" {
		r.NFCENumero = a.NFCENumero
	} else if strings.TrimSpace(a.NFCENumeroSnake) != "" {
		r.NFCENumero = a.NFCENumeroSnake
	}
	return nil
}

func (r *CaixaFechamentoRequest) UnmarshalJSON(b []byte) error {
	type Alias CaixaFechamentoRequest
	var a struct {
		Alias
		Computado  []CaixaComputadoItem `json:"computado"`
		Compuatado []CaixaComputadoItem `json:"compuatado"`
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*r = CaixaFechamentoRequest(a.Alias)
	if len(a.Computado) > 0 {
		r.Computado = a.Computado
	} else {
		r.Computado = a.Compuatado
	}
	return nil
}

// Normalize aplica normalizações básicas (trim) antes da validação e do processamento.
func (r *ImpressaoCozinhaRequest) Normalize() {
	r.Usuario = strings.TrimSpace(r.Usuario)
	r.Driver = strings.TrimSpace(r.Driver)
	r.Impressora = strings.TrimSpace(r.Impressora)
	r.Tipo = TipoImpressao(strings.TrimSpace(string(r.Tipo)))
	r.UUID = strings.ToLower(strings.TrimSpace(r.UUID))
	r.Cliente = strings.TrimSpace(r.Cliente)

	for i := range r.Produtos {
		r.Produtos[i].UUID = strings.ToLower(strings.TrimSpace(r.Produtos[i].UUID))
		r.Produtos[i].Categoria = strings.TrimSpace(r.Produtos[i].Categoria)
		r.Produtos[i].Nome = strings.TrimSpace(r.Produtos[i].Nome)
		r.Produtos[i].Observacoes = strings.TrimSpace(r.Produtos[i].Observacoes)
		for j := range r.Produtos[i].Extras {
			r.Produtos[i].Extras[j].Categoria = strings.TrimSpace(r.Produtos[i].Extras[j].Categoria)
			r.Produtos[i].Extras[j].Nome = strings.TrimSpace(r.Produtos[i].Extras[j].Nome)
		}
	}
}

// Validate valida as regras mínimas do payload, retornando mensagens claras em português.
func (r ImpressaoCozinhaRequest) Validate() error {
	var errs []string

	if strings.TrimSpace(string(r.Tipo)) == "" {
		errs = append(errs, `campo "tipo" é obrigatório`)
	}
	if r.Numero < 0 {
		errs = append(errs, `campo "numero" deve ser maior ou igual a zero`)
	}
	if strings.TrimSpace(r.UUID) != "" && len(r.UUID) < 6 {
		errs = append(errs, `campo "uuid" inválido`)
	}
	if r.Usuario == "" {
		errs = append(errs, `campo "usuario" é obrigatório`)
	}
	if r.Driver == "" {
		errs = append(errs, `campo "driver" é obrigatório`)
	}
	if r.Impressora == "" {
		errs = append(errs, `campo "impressora" é obrigatório`)
	}
	if len(r.Produtos) == 0 {
		errs = append(errs, `campo "produtos" deve conter ao menos 1 item`)
	}

	for i, p := range r.Produtos {
		if strings.TrimSpace(p.UUID) != "" && len(p.UUID) < 6 {
			errs = append(errs, fmt.Sprintf(`produto[%d].uuid inválido`, i))
		}
		if p.Nome == "" {
			errs = append(errs, fmt.Sprintf(`produto[%d].nome é obrigatório`, i))
		}
		if p.Quantidade <= 0 {
			errs = append(errs, fmt.Sprintf(`produto[%d].quantidade deve ser maior que zero`, i))
		}
		for j, e := range p.Extras {
			if e.Nome == "" {
				errs = append(errs, fmt.Sprintf(`produto[%d].extras[%d].nome é obrigatório`, i, j))
			}
			if e.Quantidade <= 0 {
				errs = append(errs, fmt.Sprintf(`produto[%d].extras[%d].quantidade deve ser maior que zero`, i, j))
			}
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (r *ConferenciaRequest) Normalize() {
	r.Tipo = strings.TrimSpace(r.Tipo)
	r.Mesa = strings.TrimSpace(r.Mesa)
	r.Driver = strings.TrimSpace(r.Driver)
	r.Modelo = strings.TrimSpace(strings.ToLower(r.Modelo))
	r.Operador = strings.TrimSpace(r.Operador)
	r.CX = strings.TrimSpace(r.CX)
	r.Desconto = strings.TrimSpace(r.Desconto)
	r.NFCEChave = strings.TrimSpace(r.NFCEChave)
	r.NFCEProtocolo = strings.TrimSpace(r.NFCEProtocolo)
	r.NFCENumero = strings.TrimSpace(r.NFCENumero)

	r.Cliente.Nome = strings.TrimSpace(r.Cliente.Nome)
	r.Cliente.CPF = strings.TrimSpace(r.Cliente.CPF)
	r.Cliente.Celular = strings.TrimSpace(r.Cliente.Celular)
	r.Endereco.Rua = strings.TrimSpace(r.Endereco.Rua)
	r.Endereco.Numero = strings.TrimSpace(r.Endereco.Numero)
	r.Endereco.Bairro = strings.TrimSpace(r.Endereco.Bairro)
	r.Endereco.Cidade = strings.TrimSpace(r.Endereco.Cidade)
	r.Endereco.Complemento = strings.TrimSpace(r.Endereco.Complemento)
	for i := range r.Pagamentos {
		r.Pagamentos[i].Descricao = strings.TrimSpace(r.Pagamentos[i].Descricao)
		r.Pagamentos[i].Nome = strings.TrimSpace(r.Pagamentos[i].Nome)
	}

	for i := range r.Itens {
		for j := range r.Itens[i].Produtos {
			r.Itens[i].Produtos[j].Categoria = strings.TrimSpace(r.Itens[i].Produtos[j].Categoria)
			r.Itens[i].Produtos[j].Nome = strings.TrimSpace(r.Itens[i].Produtos[j].Nome)
			r.Itens[i].Produtos[j].Observacoes = strings.TrimSpace(r.Itens[i].Produtos[j].Observacoes)
			for k := range r.Itens[i].Produtos[j].Extras {
				r.Itens[i].Produtos[j].Extras[k].Categoria = strings.TrimSpace(r.Itens[i].Produtos[j].Extras[k].Categoria)
				r.Itens[i].Produtos[j].Extras[k].Nome = strings.TrimSpace(r.Itens[i].Produtos[j].Extras[k].Nome)
			}
		}
	}
}

func (r *SangriaRequest) Normalize() {
	r.Descricao = strings.TrimSpace(r.Descricao)
	r.Operador = strings.TrimSpace(r.Operador)
	r.CX = strings.TrimSpace(r.CX)
	r.Driver = strings.TrimSpace(r.Driver)
	r.Modelo = strings.TrimSpace(strings.ToLower(r.Modelo))
}

func (r *CaixaFechamentoRequest) Normalize() {
	r.Driver = strings.TrimSpace(r.Driver)
	r.Modelo = strings.TrimSpace(strings.ToLower(r.Modelo))
	for i := range r.Computado {
		r.Computado[i].DataAbertura = strings.TrimSpace(r.Computado[i].DataAbertura)
		r.Computado[i].HoraAbertura = strings.TrimSpace(r.Computado[i].HoraAbertura)
		r.Computado[i].DataFechamento = strings.TrimSpace(r.Computado[i].DataFechamento)
		r.Computado[i].HoraFechamento = strings.TrimSpace(r.Computado[i].HoraFechamento)
		r.Computado[i].Descricao = strings.TrimSpace(r.Computado[i].Descricao)
		r.Computado[i].Usuario = strings.TrimSpace(r.Computado[i].Usuario)
	}
	for i := range r.Lancado {
		r.Lancado[i].Descricao = strings.TrimSpace(r.Lancado[i].Descricao)
	}
	for i := range r.Produtos {
		r.Produtos[i].Produto = strings.TrimSpace(r.Produtos[i].Produto)
	}
	for i := range r.Categorias {
		r.Categorias[i].Produto = strings.TrimSpace(r.Categorias[i].Produto)
	}
	for i := range r.Motoboy {
		r.Motoboy[i].Motoboy = strings.TrimSpace(r.Motoboy[i].Motoboy)
		r.Motoboy[i].Bairro = strings.TrimSpace(r.Motoboy[i].Bairro)
	}
}

func (r ConferenciaRequest) Validate() error {
	var errs []string

	if strings.TrimSpace(r.Tipo) == "" && strings.TrimSpace(r.Mesa) == "" {
		errs = append(errs, `campo "mesa" é obrigatório (ou informe "tipo")`)
	}
	if r.Sequencial < 0 {
		errs = append(errs, `campo "sequencial" deve ser maior ou igual a zero`)
	}
	if r.Driver == "" {
		errs = append(errs, `campo "driver" é obrigatório`)
	}
	if r.Modelo == "" {
		errs = append(errs, `campo "modelo" é obrigatório`)
	} else if r.Modelo != "56mm" && r.Modelo != "58mm" && r.Modelo != "80mm" {
		errs = append(errs, `campo "modelo" deve ser "56mm", "58mm" ou "80mm"`)
	}
	if len(r.Itens) == 0 {
		errs = append(errs, `campo "itens" deve conter ao menos 1 item`)
	}
	for i, it := range r.Itens {
		if len(it.Produtos) == 0 {
			errs = append(errs, fmt.Sprintf(`itens[%d].produtos deve conter ao menos 1 item`, i))
			continue
		}
		for j, p := range it.Produtos {
			if strings.TrimSpace(p.Nome) == "" {
				errs = append(errs, fmt.Sprintf(`itens[%d].produtos[%d].nome é obrigatório`, i, j))
			}
			if p.Quantidade <= 0 {
				errs = append(errs, fmt.Sprintf(`itens[%d].produtos[%d].quantidade deve ser maior que zero`, i, j))
			}
			for k, e := range p.Extras {
				if strings.TrimSpace(e.Nome) == "" {
					errs = append(errs, fmt.Sprintf(`itens[%d].produtos[%d].extras[%d].nome é obrigatório`, i, j, k))
				}
				if e.Quantidade <= 0 {
					errs = append(errs, fmt.Sprintf(`itens[%d].produtos[%d].extras[%d].quantidade deve ser maior que zero`, i, j, k))
				}
			}
		}
	}
	if r.TaxaServicoPercent < 0 || r.TaxaServicoValor < 0 || r.TaxaEntregaValor < 0 || r.ValorDesconto < 0 {
		errs = append(errs, `taxas devem ser maiores ou iguais a zero`)
	}
	if r.TotalProdutos < 0 || r.TotalGeral < 0 {
		errs = append(errs, `totais devem ser maiores ou iguais a zero`)
	}
	for i, p := range r.Pagamentos {
		if strings.TrimSpace(p.Descricao) == "" {
			errs = append(errs, fmt.Sprintf(`pagamentos[%d].descricao é obrigatório`, i))
		}
		if p.Valor < 0 || p.Troco < 0 {
			errs = append(errs, fmt.Sprintf(`pagamentos[%d] valores devem ser maiores ou iguais a zero`, i))
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (r SangriaRequest) Validate() error {
	var errs []string
	if strings.TrimSpace(r.Descricao) == "" {
		errs = append(errs, `campo "descricao" é obrigatório`)
	}
	if r.Valor <= 0 {
		errs = append(errs, `campo "valor" deve ser maior que zero`)
	}
	if strings.TrimSpace(r.Driver) == "" {
		errs = append(errs, `campo "driver" é obrigatório`)
	}
	if strings.TrimSpace(r.Modelo) == "" {
		errs = append(errs, `campo "modelo" é obrigatório`)
	} else if r.Modelo != "56mm" && r.Modelo != "58mm" && r.Modelo != "80mm" {
		errs = append(errs, `campo "modelo" deve ser "56mm", "58mm" ou "80mm"`)
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (r CaixaFechamentoRequest) Validate() error {
	var errs []string
	if strings.TrimSpace(r.Driver) == "" {
		errs = append(errs, `campo "driver" é obrigatório`)
	}
	if strings.TrimSpace(r.Modelo) == "" {
		errs = append(errs, `campo "modelo" é obrigatório`)
	} else if r.Modelo != "56mm" && r.Modelo != "58mm" && r.Modelo != "80mm" {
		errs = append(errs, `campo "modelo" deve ser "56mm", "58mm" ou "80mm"`)
	}
	if len(r.Computado) == 0 {
		errs = append(errs, `campo "compuatado" (ou "computado") deve conter ao menos 1 item`)
	} else if r.Computado[0].ID <= 0 {
		errs = append(errs, `compuatado[0].id inválido`)
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

type FlexFloat float64

func (n *FlexFloat) UnmarshalJSON(b []byte) error {
	raw := strings.TrimSpace(string(b))
	if raw == "null" || raw == "" {
		*n = 0
		return nil
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*n = 0
			return nil
		}
		f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil {
			*n = 0
			return nil
		}
		*n = FlexFloat(f)
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*n = FlexFloat(f)
		return nil
	}
	var i int
	if err := json.Unmarshal(b, &i); err == nil {
		*n = FlexFloat(float64(i))
		return nil
	}
	*n = 0
	return nil
}
