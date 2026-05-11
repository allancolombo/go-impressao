package services

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/goopedir/go-impressao/internal/models"
	"golang.org/x/text/unicode/norm"
)

// Formatter converte o payload em um texto de comanda pronto para exibição/impressão.
type Formatter struct{}

func NewFormatter() *Formatter {
	return &Formatter{}
}

func (f *Formatter) FormatComandaCozinhaWithCols(r models.ImpressaoCozinhaRequest, printedAt time.Time, cols int) string {
	if cols <= 0 {
		cols = 48
	}
	if cols > 48 {
		cols = 48
	}

	var b strings.Builder

	header := ""
	tipo := sanitizeText(string(r.Tipo))
	if r.Numero == 0 {
		header = fmt.Sprintf("%s", tipo)
	} else {
		header = fmt.Sprintf("%s %d", tipo, r.Numero)
	}
	if runeLen(header) <= cols {
		b.WriteString(centerLine(header, cols))
		b.WriteString("\n")
	} else {
		writeWrappedLine(&b, "", header, cols)
	}

	if r.Numero == 0 && strings.TrimSpace(r.Cliente) != "" {
		b.WriteString(centerLine(sanitizeText(r.Cliente), cols))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	lastProdCat := ""
	for i, p := range r.Produtos {
		if i > 0 {
			b.WriteString(strings.Repeat("-", cols))
			b.WriteString("\n")
		}

		prodCat := sanitizeText(p.Categoria)
		if prodCat != "" && prodCat != lastProdCat {
			writeWrappedLine(&b, "", strings.ToUpper(prodCat), cols)
			lastProdCat = prodCat
		}

		writeWrappedLine(&b, qtyPrefix(p.Quantidade, false), sanitizeText(p.Nome), cols)

		extras := make([]models.Extra, len(p.Extras))
		copy(extras, p.Extras)
		sort.SliceStable(extras, func(i, j int) bool {
			return extraCategoryRank(extras[i].Categoria) < extraCategoryRank(extras[j].Categoria)
		})

		totalSabores := 0
		for _, e := range extras {
			if isSaboresCategory(e.Categoria) {
				totalSabores += e.Quantidade
			}
		}

		lastExtraCat := ""
		for _, e := range extras {
			extraCat := sanitizeText(e.Categoria)
			if extraCat != "" && extraCat != lastExtraCat {
				writeWrappedLine(&b, "", strings.ToUpper(extraCat), cols)
				lastExtraCat = extraCat
			}

			line := sanitizeText(e.Nome)
			if isSaboresCategory(e.Categoria) && totalSabores > 0 {
				line = fmt.Sprintf("%d/%d %s", e.Quantidade, totalSabores, line)
			} else if e.Quantidade > 1 {
				line = fmt.Sprintf("%dUn %s", e.Quantidade, line)
			}
			writeWrappedLine(&b, "  - ", line, cols)
		}

		if p.Observacoes != "" {
			writeWrappedParagraph(&b, sanitizeText(p.Observacoes), cols)
		}
	}

	b.WriteString(strings.Repeat("-", cols))
	b.WriteString("\n")
	writeWrappedLine(&b, "", fmt.Sprintf("%s (%s)", sanitizeText(r.Usuario), sanitizeText(r.Impressora)), cols)
	writeWrappedLine(&b, "", printedAt.Format("02/01/2006 15:04:05"), cols)
	b.WriteString(centerLine("By GooPedir", cols))
	b.WriteString("\n")

	return b.String()
}

// FormatComandaCozinha aplica o layout definido:
// - Primeira linha: Tipo + Número
// - Produtos: Quantidade - Nome
// - Extras: indentados
// - Observações: sem formatação especial
// - Rodapé: Usuário (impressora) e data/hora
func (f *Formatter) FormatComandaCozinha(r models.ImpressaoCozinhaRequest, printedAt time.Time) string {
	return f.FormatComandaCozinhaWithCols(r, printedAt, 48)
}

func writeWrappedParagraph(b *strings.Builder, s string, cols int) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	parts := strings.Split(s, "\n")
	for i, line := range parts {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		writeWrappedLine(b, "", line, cols)
		if i == len(parts)-1 && !strings.HasSuffix(s, "\n") {
			return
		}
	}
}

func writeWrappedLine(b *strings.Builder, prefix string, text string, cols int) {
	prefixLen := runeLen(prefix)
	if cols <= 0 {
		return
	}
	if prefixLen >= cols {
		b.WriteString(prefix)
		b.WriteString(text)
		b.WriteString("\n")
		return
	}

	avail := cols - prefixLen
	lines := wrapWords(text, avail)
	if len(lines) == 0 {
		b.WriteString(prefix)
		b.WriteString("\n")
		return
	}

	b.WriteString(prefix)
	b.WriteString(lines[0])
	b.WriteString("\n")

	indent := strings.Repeat(" ", prefixLen)
	for i := 1; i < len(lines); i++ {
		b.WriteString(indent)
		b.WriteString(lines[i])
		b.WriteString("\n")
	}
}

func qtyPrefix(qty int, indented bool) string {
	base := fmt.Sprintf("%dUn - ", qty)
	if indented {
		return "  " + base
	}
	return base
}

func isSaboresCategory(cat string) bool {
	cat = strings.TrimSpace(strings.ToUpper(sanitizeText(cat)))
	return cat == "SABORES"
}

func extraCategoryRank(cat string) int {
	cat = strings.TrimSpace(strings.ToUpper(sanitizeText(cat)))
	switch {
	case cat == "INGREDIENTES" || cat == "INGREDIENTE":
		return 0
	case strings.HasPrefix(cat, "ADICION"):
		return 1
	case cat == "SABORES" || cat == "SABOR":
		return 2
	case cat == "BORDA" || cat == "BORDAS":
		return 3
	default:
		return 4
	}
}

func centerLine(s string, cols int) string {
	s = strings.TrimSpace(s)
	l := runeLen(s)
	if cols <= 0 || l >= cols {
		return s
	}
	left := (cols - l) / 2
	right := cols - l - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

func wrapWords(s string, width int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if width <= 1 {
		return []string{s}
	}

	words := strings.Fields(s)
	var lines []string
	var cur strings.Builder
	curLen := 0

	flush := func() {
		if curLen > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			curLen = 0
		}
	}

	for _, w := range words {
		wLen := runeLen(w)

		if wLen > width {
			flush()
			for len(w) > 0 {
				part := takeRunes(w, width)
				lines = append(lines, part)
				w = dropRunes(w, runeLen(part))
			}
			continue
		}

		if curLen == 0 {
			cur.WriteString(w)
			curLen = wLen
			continue
		}

		if curLen+1+wLen <= width {
			cur.WriteString(" ")
			cur.WriteString(w)
			curLen += 1 + wLen
			continue
		}

		flush()
		cur.WriteString(w)
		curLen = wLen
	}
	flush()

	return lines
}

func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}

func sanitizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r == '\n' {
			b.WriteByte('\n')
			continue
		}
		if r == '\t' {
			b.WriteByte(' ')
			continue
		}
		if r < 0x20 || r == 0x7F {
			continue
		}
		if r <= 0x7E {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func takeRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	i := 0
	for idx := range s {
		if i == n {
			return s[:idx]
		}
		i++
	}
	return s
}

func dropRunes(s string, n int) string {
	if n <= 0 {
		return s
	}
	i := 0
	for idx := range s {
		if i == n {
			return s[idx:]
		}
		i++
	}
	return ""
}
