package printer

import (
	"fmt"
	"strings"
)

func WrapHTMLWithPaper(html string, paper string) string {
	trimmed := strings.TrimSpace(html)
	if trimmed == "" {
		return html
	}

	width := paperWidthCSS(paper)
	if width == "" {
		return html
	}

	style := fmt.Sprintf("<style>html,body{margin:0;padding:0;width:%s;}</style>", width)
	if strings.Contains(trimmed, "</head>") {
		return strings.Replace(trimmed, "</head>", style+"</head>", 1)
	}
	if strings.Contains(trimmed, "<html") {
		return strings.Replace(trimmed, "<html", "<html", 1) + style
	}
	return "<!doctype html><html><head>" + style + "</head><body>" + trimmed + "</body></html>"
}

func paperWidthCSS(paper string) string {
	switch strings.TrimSpace(paper) {
	case "80", "80mm":
		return "80mm"
	case "58", "58mm":
		return "58mm"
	default:
		return ""
	}
}
