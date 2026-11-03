package sanitize

import "testing"

// A05 Teil 3: Regel für HTML-Quelltext im Klartextteil.
func TestPlainIsHTML(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{`<html xmlns="http://www.w3.org/1999/xhtml"><head><meta charset="utf-8"></head><body>Hallo</body></html>`, true},
		{"\r\n\t <HTML>\n<BODY>Hallo</BODY></HTML>", true},
		{"<!DOCTYPE html PUBLIC \"-//W3C//DTD XHTML 1.0//EN\">\n<html>", true},
		{"<!doctype html><p>x</p>", true},
		{"<head><title>x</title></head>", true},
		{"<body>Hallo</body>", true},
		{"<meta http-equiv=\"Content-Type\" content=\"text/html\">Hallo", true},
		{"<html>", true},
		{`<?xml version="1.0" encoding="utf-8"?>` + "\n<!DOCTYPE html>\n<html>", true},
		{`<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml">`, true},
		// echter Klartext oder anderes XML
		{`<?xml version="1.0"?><rechnung><betrag>12</betrag></rechnung>`, false},
		{`<?xml version="1.0"`, false},
		{"<Anna Beispiel> schrieb:\n> Hallo", false},
		{"<anna@example.org> schrieb am Montag:", false},
		{"Hallo,\n<b>wichtig</b>: bitte bis Freitag.", false},
		{"<b>Wichtig</b>: Termin verschoben.", false},
		{"<header> ist ein Wort", false},
		{"<metadata>", false},
		{"<htmlx", false},
		{"Bitte <html> nicht verwenden.", false},
		{"", false},
		{"   ", false},
	} {
		if got := PlainIsHTML(c.in); got != c.want {
			t.Errorf("PlainIsHTML(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
