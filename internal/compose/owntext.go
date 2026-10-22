package compose

import (
	"bytes"
	"strings"

	"github.com/KNUFFIXX/postkauz/internal/sanitize"
)

// OwnText liefert den Klartext des eigenen Texts eines Entwurfs: nur
// body_markdown bzw. body_html, so umgesetzt wie in Build (goldmark,
// Allowlist, HTML zu Text), aber ohne Signatur, Zitat und
// Weiterleitungskopf. Die Versanderkennung (S2.6, A02 E5) vergleicht ihn
// im Rückfall-Abgleich mit dem zitatfreien Text der gesendeten Nachricht.
// Ein leerer oder nicht umsetzbarer Text ergibt "".
func OwnText(bodyMarkdown, bodyHTML string) string {
	src := bodyHTML
	if strings.TrimSpace(src) == "" && strings.TrimSpace(bodyMarkdown) != "" {
		var buf bytes.Buffer
		if err := markdown.Convert([]byte(bodyMarkdown), &buf); err != nil {
			return ""
		}
		src = buf.String()
	}
	if strings.TrimSpace(src) == "" {
		return ""
	}
	return sanitize.HTML(sanitize.CleanHTML(src).HTML).Text
}

// SignatureText liefert den Klartext einer Signatur wie Build sie anhängt:
// HTML hat Vorrang (bereinigt, als Text), sonst der Text.
func SignatureText(signatureText, signatureHTML string) string {
	if strings.TrimSpace(signatureHTML) != "" {
		return sanitize.HTML(sanitize.CleanHTML(signatureHTML).HTML).Text
	}
	return sanitize.Text(strings.TrimSpace(signatureText))
}
