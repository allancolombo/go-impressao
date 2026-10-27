//go:build windows

package printer

import (
	"strings"
	"time"

	"github.com/goopedir/go-impressao/internal/models"
)

func cupomBorderLine(cols int) string {
	return strings.TrimRight(strings.Repeat("- ", (cols+1)/2), " ")
}

func cupomChamadaCodigo(c *models.ConferenciaCupom) string {
	site := sanitizeOneLineASCII(c.Site)
	if site == "" {
		return "Use o codigo no site:"
	}
	return "Use o codigo no site " + site + ":"
}

func cupomValidadeLinhas(c *models.ConferenciaCupom) []string {
	validade := formatCupomValidade(c.Validade)
	if validade == "" {
		return nil
	}
	return []string{"Oferta valida no seu proximo pedido", "ate " + validade}
}

func formatCupomValidade(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range []string{"2006-01-02", "2006-01-02 15:04:05", "2006-01-02T15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("02/01/2006")
		}
	}
	return sanitizeOneLineASCII(s)
}

func cupomQRModuleSize(cols int) byte {
	if cols < 40 {
		return 4
	}
	return 5
}

func appendCenteredWrapped(buf *[]byte, text string, cols int) {
	for _, l := range wrapTextASCII(sanitizeTextASCII(text), cols) {
		*buf = append(*buf, encodeCP850(centerASCII(l, cols))...)
		*buf = append(*buf, '\n')
	}
}

func writeCenteredWrapped(b *strings.Builder, text string, cols int) {
	for _, l := range wrapTextASCII(sanitizeTextASCII(text), cols) {
		b.WriteString(centerASCII(l, cols))
		b.WriteString("\n")
	}
}

// appendEscPOSCupom imprime o cupom dentro de uma área tracejada, centralizado.
func appendEscPOSCupom(buf *[]byte, c *models.ConferenciaCupom, cols int, fontStyle conferenciaFontStyle) {
	if !c.Ativo() {
		return
	}
	*buf = append(*buf, '\n')
	*buf = append(*buf, 0x1B, 0x61, 0x01)
	*buf = append(*buf, encodeCP850(cupomBorderLine(cols))...)
	*buf = append(*buf, '\n', '\n')

	if titulo := strings.ToUpper(sanitizeOneLineASCII(c.Titulo)); titulo != "" {
		*buf = append(*buf, 0x1B, 0x45, 0x01)
		*buf = append(*buf, 0x1D, 0x21, 0x01)
		appendCenteredWrapped(buf, titulo, cols)
		appendConferenciaFont(buf, fontStyle)
		*buf = append(*buf, 0x1B, 0x45, 0x00)
	}
	if c.Descricao != "" {
		*buf = append(*buf, 0x1B, 0x45, 0x01)
		appendCenteredWrapped(buf, c.Descricao, cols)
		*buf = append(*buf, 0x1B, 0x45, 0x00)
	}
	*buf = append(*buf, '\n')

	appendCenteredWrapped(buf, cupomChamadaCodigo(c), cols)
	*buf = append(*buf, 0x1B, 0x45, 0x01)
	*buf = append(*buf, 0x1D, 0x21, 0x11)
	appendCenteredWrapped(buf, strings.ToUpper(sanitizeOneLineASCII(c.Codigo)), cols/2)
	appendConferenciaFont(buf, fontStyle)
	*buf = append(*buf, 0x1B, 0x45, 0x00)

	if qr := sanitizeOneLineASCII(c.QRCode); qr != "" {
		*buf = append(*buf, '\n')
		appendCenteredWrapped(buf, "Aponte a camera para o QR Code:", cols)
		appendEscPOSQRCode(buf, qr, cupomQRModuleSize(cols))
		*buf = append(*buf, '\n')
	}

	*buf = append(*buf, '\n')
	for _, l := range cupomValidadeLinhas(c) {
		appendCenteredWrapped(buf, l, cols)
	}
	appendCenteredWrapped(buf, "Confira as regras no site.", cols)

	if c.Mensagem != "" {
		*buf = append(*buf, '\n')
		*buf = append(*buf, 0x1B, 0x45, 0x01)
		for _, block := range strings.Split(sanitizeTextASCII(c.Mensagem), "\n") {
			appendCenteredWrapped(buf, block, cols)
		}
		*buf = append(*buf, 0x1B, 0x45, 0x00)
	}

	*buf = append(*buf, '\n')
	*buf = append(*buf, encodeCP850(cupomBorderLine(cols))...)
	*buf = append(*buf, '\n')
	*buf = append(*buf, 0x1B, 0x61, 0x00)
}

// writeFallbackCupom é a versão em texto puro (preview/histórico); o QR vira o link.
func writeFallbackCupom(b *strings.Builder, c *models.ConferenciaCupom, cols int) {
	if !c.Ativo() {
		return
	}
	b.WriteString("\n")
	b.WriteString(cupomBorderLine(cols))
	b.WriteString("\n\n")
	if titulo := strings.ToUpper(sanitizeOneLineASCII(c.Titulo)); titulo != "" {
		writeCenteredWrapped(b, titulo, cols)
	}
	if c.Descricao != "" {
		writeCenteredWrapped(b, c.Descricao, cols)
	}
	b.WriteString("\n")
	writeCenteredWrapped(b, cupomChamadaCodigo(c), cols)
	writeCenteredWrapped(b, strings.ToUpper(sanitizeOneLineASCII(c.Codigo)), cols)
	if qr := sanitizeOneLineASCII(c.QRCode); qr != "" {
		b.WriteString("\n")
		writeCenteredWrapped(b, "QR Code:", cols)
		writeCenteredWrapped(b, qr, cols)
	}
	b.WriteString("\n")
	for _, l := range cupomValidadeLinhas(c) {
		writeCenteredWrapped(b, l, cols)
	}
	writeCenteredWrapped(b, "Confira as regras no site.", cols)
	if c.Mensagem != "" {
		b.WriteString("\n")
		for _, block := range strings.Split(sanitizeTextASCII(c.Mensagem), "\n") {
			writeCenteredWrapped(b, block, cols)
		}
	}
	b.WriteString("\n")
	b.WriteString(cupomBorderLine(cols))
	b.WriteString("\n")
}
