//go:build windows

package printer

import (
	"bytes"
	"strings"
	"testing"

	"github.com/goopedir/go-impressao/internal/models"
)

func TestWriteFallbackCupom_ImprimeCamposDoCupom(t *testing.T) {
	c := &models.ConferenciaCupom{
		Titulo:    "10% off na entrega",
		Descricao: "no seu próximo pedido",
		Codigo:    "uucg",
		Site:      "hamburgueria.goopedir.com",
		QRCode:    "https://hamburgueria.goopedir.com/cupom/UUCG",
		Validade:  "2026-11-30",
		Mensagem:  "Volte sempre, Carlos!",
	}

	var b strings.Builder
	writeFallbackCupom(&b, c, 47)
	got := b.String()

	for _, want := range []string{
		"10% OFF NA ENTREGA",
		"no seu proximo pedido",
		"Use o codigo no site hamburgueria.goopedir.com:",
		"UUCG",
		"https://hamburgueria.goopedir.com/cupom/UUCG",
		"Oferta valida no seu proximo pedido",
		"ate 30/11/2026",
		"Confira as regras no site.",
		"Volte sempre, Carlos!",
		"- - - -",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava %q no cupom, got:\n%s", want, got)
		}
	}
}

func TestWriteFallbackCupom_SemCodigoNaoImprime(t *testing.T) {
	var b strings.Builder
	writeFallbackCupom(&b, &models.ConferenciaCupom{Titulo: "10% OFF"}, 47)
	writeFallbackCupom(&b, nil, 47)
	if b.Len() != 0 {
		t.Fatalf("cupom sem codigo nao deveria imprimir, got:\n%s", b.String())
	}
}

func TestAppendEscPOSCupom_ImprimeQRCode(t *testing.T) {
	c := &models.ConferenciaCupom{Codigo: "UUCG", QRCode: "https://x.com/c/UUCG", Validade: "30/11/2026"}
	var buf []byte
	appendEscPOSCupom(&buf, c, 47, conferenciaFontStyle{})
	if !bytes.Contains(buf, []byte("https://x.com/c/UUCG")) {
		t.Fatalf("esperava o link do QR Code no payload")
	}
	if !bytes.Contains(buf, []byte{0x1D, 0x28, 0x6B}) {
		t.Fatalf("esperava comando ESC/POS de QR Code no payload")
	}
	if !bytes.Contains(buf, []byte("ate 30/11/2026")) {
		t.Fatalf("esperava a validade no payload")
	}
}
