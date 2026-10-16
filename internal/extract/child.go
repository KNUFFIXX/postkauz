package extract

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Kindprozess: Das init schaltet das Programm in den Extraktor-Modus, wenn
// POSTKAUZ_INTERNAL_EXTRACT gesetzt ist. Er liest den Anhang von stdin,
// schreibt Text nach stdout und endet; er öffnet keine Datenbank und kein
// Netz. Hier (und nur hier) wird ein Archiv geöffnet: DOCX ist ZIP.
func init() {
	if mode := os.Getenv(childEnv); mode != "" {
		os.Exit(childMain(mode, os.Stdin, os.Stdout, os.Stderr))
	}
}

// Grenzen im Kindprozess (zusätzlich zu Zeit und GOMEMLIMIT).
const (
	childMaxInput    = 64 << 20 // gelesene Bytes von stdin
	childMaxEntry    = 32 << 20 // entpackte Größe von word/document.xml
	childMaxRatio    = 200      // höchstens 200-fache Packrate
	childMaxTextOut  = 4 << 20
	docxDocumentPath = "word/document.xml"
)

func childMain(mode string, stdin io.Reader, stdout, stderr io.Writer) int {
	var err error
	switch mode {
	case string(KindDOCX):
		err = docxText(stdin, stdout)
	default:
		err = fmt.Errorf("unknown extractor mode %q", mode)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// docxHiddenMarker steht vor dem Text, wenn versteckter Text ausgelassen
// wurde. NUL kommt in XML-Text nie vor (XML 1.0), die Marke ist eindeutig.
const docxHiddenMarker = "\x00hidden\n"

// docxText liest den Text aus word/document.xml: Inhalt von <w:t>,
// Absätze (<w:p>) als Zeilen, <w:tab> und <w:br> als Tab bzw. Umbruch.
// Ohne <w:t> (vereinfachte Dateien) zählt aller Zeichentext. Text in
// Läufen mit <w:vanish/> (in Word ausgeblendet) fehlt; dann steht
// docxHiddenMarker vor dem Text. Nicht ausgewertet: Ausblenden über
// Formatvorlagen (styles.xml) und Feldfunktionen.
func docxText(stdin io.Reader, stdout io.Writer) error {
	data, err := io.ReadAll(io.LimitReader(stdin, childMaxInput+1))
	if err != nil {
		return err
	}
	if len(data) > childMaxInput {
		return errors.New("input too large")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return errors.New("not a valid DOCX (zip) file")
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == docxDocumentPath {
			doc = f
			break
		}
	}
	if doc == nil {
		return errors.New("DOCX has no word/document.xml")
	}
	if doc.UncompressedSize64 > childMaxEntry || (doc.CompressedSize64 > 0 && doc.UncompressedSize64/doc.CompressedSize64 > childMaxRatio) {
		return errors.New("word/document.xml unpacks too large (zip bomb guard)")
	}
	rc, err := doc.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	lr := &io.LimitedReader{R: rc, N: childMaxEntry + 1}
	dec := xml.NewDecoder(lr)
	dec.Strict = false
	var out, all strings.Builder
	inT, sawT := false, false
	// inPPr: Absatzeigenschaften (dort blendet vanish nur die
	// Absatzmarke aus, nicht den Text); runHidden: aktueller Lauf ist
	// ausgeblendet; hidden: es wurde Text ausgelassen.
	inPPr, runHidden, hidden := false, false, false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if lr.N <= 0 {
				return errors.New("word/document.xml unpacks too large (zip bomb guard)")
			}
			return errors.New("unreadable word/document.xml")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inT, sawT = true, true
			case "r":
				runHidden = false
			case "pPr":
				inPPr = true
			case "vanish":
				if !inPPr && onOff(t) {
					runHidden = true
				}
			case "tab":
				if !runHidden {
					out.WriteByte('\t')
				}
			case "br", "cr":
				if !runHidden {
					out.WriteByte('\n')
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "r":
				runHidden = false
			case "pPr":
				inPPr = false
			case "p":
				out.WriteByte('\n')
			}
		case xml.CharData:
			if inT && runHidden {
				if len(bytes.TrimSpace(t)) > 0 {
					hidden = true
				}
			} else if inT {
				out.Write(t)
			}
			if all.Len() < childMaxTextOut {
				all.Write(t)
			}
		}
		if out.Len() > childMaxTextOut {
			break
		}
	}
	text := out.String()
	if !sawT {
		text = all.String()
	}
	if hidden {
		if _, err := io.WriteString(stdout, docxHiddenMarker); err != nil {
			return err
		}
	}
	_, err = io.WriteString(stdout, strings.TrimSpace(text)+"\n")
	return err
}

// onOff liest einen OOXML-Schalter wie <w:vanish/> oder
// <w:vanish w:val="false"/>: ohne val an, sonst je nach Wert.
func onOff(t xml.StartElement) bool {
	for _, a := range t.Attr {
		if a.Name.Local == "val" {
			switch strings.ToLower(strings.TrimSpace(a.Value)) {
			case "0", "false", "off":
				return false
			}
		}
	}
	return true
}
