package ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// Fehlermeldungen der Schemaprüfung (Gegenprüfung S2.3, Befund B1): Die
// Bibliothek nennt in ihren Meldungen den geprüften Wert ("maxLength:
// \"<ganzer Betreff>\" contains …", "pattern: \"<Kontoname>\" does not
// match …", "unexpected additional properties [\"<Schlüssel>\"]"). Eine
// Eingabe ist Daten des Aufrufers und darf weder in einer Fehlermeldung
// noch im Log oder in --json-Fehlern erscheinen (docs/sicherheit.md,
// Abschnitt 5). schemaError kürzt die Meldung deshalb auf Feldpfad und
// Regel; Zahlen und erlaubte Werte stammen aus dem Schema, nie aus der
// Eingabe. Unbekannte Feldnamen werden nicht genannt (sie stammen vom
// Aufrufer).

// ruleRE liest das Schlüsselwort am Anfang der innersten Meldung.
var ruleRE = regexp.MustCompile(`^[a-zA-Z]+`)

// schemaError übersetzt einen Fehler von Resolved.Validate in eine
// InputError-Meldung ohne Eingabewert.
func schemaError(rs *jsonschema.Resolved, err error) error {
	path, rule := schemaErrorParts(err)
	field := fieldOf(path)
	sub := subSchema(rs, path)
	var what string
	switch rule {
	case "maxLength":
		what = "too long" + limitText(sub, "maxLength", "at most %s characters")
	case "minLength":
		what = "too short" + limitText(sub, "minLength", "at least %s characters")
	case "pattern":
		what = "does not have the expected form"
	case "enum", "const":
		what = "not an allowed value" + enumText(sub)
	case "type":
		what = "wrong type" + limitText(sub, "type", "want %s")
	case "minimum", "exclusiveMinimum":
		what = "too small" + limitText(sub, rule, "minimum %s")
	case "maximum", "exclusiveMaximum":
		what = "too large" + limitText(sub, rule, "maximum %s")
	case "maxItems":
		what = "too many entries" + limitText(sub, "maxItems", "at most %s")
	case "minItems":
		what = "too few entries" + limitText(sub, "minItems", "at least %s")
	case "additionalProperties":
		what = "unknown field (see the input schema of the operation)"
	case "required":
		what = "required field missing" + requiredText(sub)
	case "":
		what = "does not match the input schema"
	default:
		what = "violates " + rule
	}
	if field == "" {
		return inputErr("invalid input: %s", what)
	}
	return inputErr("invalid input: %s: %s", field, what)
}

// schemaErrorParts liefert den Schema-Pfad der tiefsten Prüfung (z. B.
// "/properties/to/items") und das Schlüsselwort der Regel. Der Pfad stammt
// aus den Hüllen "validating <pfad>: …" (Schema-Orte, keine Eingabe), die
// Regel aus dem Anfang der innersten Meldung.
func schemaErrorParts(err error) (path, rule string) {
	for e := err; e != nil; e = errors.Unwrap(e) {
		inner := errors.Unwrap(e)
		if inner == nil {
			msg := e.Error()
			if strings.HasPrefix(msg, "unexpected additional properties") {
				return path, "additionalProperties"
			}
			return path, ruleRE.FindString(msg)
		}
		prefix, ok := strings.CutSuffix(e.Error(), ": "+inner.Error())
		if p, isLoc := strings.CutPrefix(prefix, "validating "); ok && isLoc && !strings.ContainsAny(p, " \"") {
			path = p
		}
	}
	return path, ""
}

// fieldOf macht aus einem Schema-Pfad einen lesbaren Feldnamen:
// "/properties/attachments/items/properties/file" → "attachments[].file".
func fieldOf(path string) string {
	if path == "" || path == "root" {
		return ""
	}
	var b strings.Builder
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i := 0; i < len(parts); i++ {
		switch parts[i] {
		case "properties":
			if i+1 < len(parts) {
				if b.Len() > 0 {
					b.WriteString(".")
				}
				b.WriteString(parts[i+1])
				i++
			}
		case "items":
			b.WriteString("[]")
		}
	}
	return b.String()
}

// subSchema liefert das Teilschema am Pfad als JSON-Objekt (nil, wenn
// nicht auffindbar).
func subSchema(rs *jsonschema.Resolved, path string) map[string]any {
	if rs == nil {
		return nil
	}
	b, err := json.Marshal(rs.Schema())
	if err != nil {
		return nil
	}
	var cur any
	if json.Unmarshal(b, &cur) != nil {
		return nil
	}
	if path != "" && path != "root" {
		for _, tok := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
			m, ok := cur.(map[string]any)
			if !ok {
				return nil
			}
			cur = m[tok]
		}
	}
	m, _ := cur.(map[string]any)
	return m
}

func limitText(sub map[string]any, key, format string) string {
	v, ok := sub[key]
	if !ok {
		return ""
	}
	var s string
	switch x := v.(type) {
	case float64:
		s = strconv.FormatFloat(x, 'f', -1, 64)
	case string:
		s = x
	default:
		return ""
	}
	return " (" + fmt.Sprintf(format, s) + ")"
}

func enumText(sub map[string]any) string {
	list, ok := sub["enum"].([]any)
	if !ok || len(list) == 0 || len(list) > 20 {
		return ""
	}
	var s []string
	for _, v := range list {
		if x, ok := v.(string); ok {
			s = append(s, x)
		}
	}
	if len(s) == 0 {
		return ""
	}
	return " (one of: " + strings.Join(s, ", ") + ")"
}

func requiredText(sub map[string]any) string {
	list, ok := sub["required"].([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	var s []string
	for _, v := range list {
		if x, ok := v.(string); ok {
			s = append(s, x)
		}
	}
	return " (required: " + strings.Join(s, ", ") + ")"
}
