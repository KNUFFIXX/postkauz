package magic

import (
	"fmt"
	"path"
	"strings"
	"unicode"
)

// Result ist der Vergleich von Angabe und Inhalt.
type Result struct {
	Detected Type
	// Mismatch: Content-Type oder Dateiendung behaupten einen anderen Typ,
	// als der Inhalt ist. Reason nennt beides (ohne Inhalt).
	Mismatch bool
	Reason   string
	// Executable: Inhalt, Dateiendung oder Content-Type sind ein Programm
	// oder Skript (beim Speichern bekommt so ein Anhang später die Endung
	// .blocked, S3).
	Executable bool
}

// generic sind Content-Types ohne Aussage über den Inhalt.
var generic = map[string]bool{
	"": true, OctetStream: true, "application/unknown": true, "application/binary": true, "binary/octet-stream": true,
	"application/x-download": true, "application/force-download": true, "application/x-unknown": true,
	"application/download": true,
}

// aliases vereinheitlichen gleichbedeutende Angaben.
var aliases = map[string]string{
	"image/jpg": "image/jpeg", "image/pjpeg": "image/jpeg", "image/x-png": "image/png",
	"application/x-pdf": PDF, "application/acrobat": PDF, "application/x-zip-compressed": ZIP, "application/x-zip": ZIP,
	"application/x-gzip": GZIP, "application/x-rar-compressed": RAR, "application/x-rar": RAR,
	"application/x-msdos-program": EXE, "application/x-dosexec": EXE, "application/exe": EXE, "application/x-exe": EXE,
	"application/msword": OLE, "application/vnd.ms-excel": OLE, "application/vnd.ms-powerpoint": OLE,
	"application/vnd.ms-outlook": OLE, "application/x-msi": OLE, "application/x-ole-storage": OLE,
	"text/rtf": RTF, "application/x-rtf": RTF, "application/x-7z-compressed": SevenZip,
	"application/x-sh": Script, "application/x-shellscript": Script,
}

// byExt ist der Typ, den eine Dateiendung behauptet.
var byExt = map[string]string{
	".pdf": PDF, ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".jpe": "image/jpeg", ".png": "image/png", ".gif": "image/gif",
	".bmp": "image/bmp", ".webp": "image/webp", ".zip": ZIP, ".docx": DOCX, ".xlsx": XLSX, ".pptx": PPTX,
	".docm": DOCX, ".xlsm": XLSX, ".odt": "application/vnd.oasis.opendocument.text",
	".ods": "application/vnd.oasis.opendocument.spreadsheet", ".odp": "application/vnd.oasis.opendocument.presentation",
	".doc": OLE, ".xls": OLE, ".ppt": OLE, ".msg": OLE, ".msi": OLE, ".rtf": RTF, ".gz": GZIP, ".tgz": GZIP,
	".7z": SevenZip, ".rar": RAR, ".cab": CAB, ".xz": XZ, ".bz2": BZIP2, ".zst": ZSTD, ".iso": ISO,
	".txt": TextPlain, ".csv": TextPlain, ".log": TextPlain, ".md": TextPlain, ".ics": TextPlain, ".vcf": TextPlain,
	".json": TextPlain, ".xml": TextPlain, ".htm": TextHTML, ".html": TextHTML, ".eml": RFC822,
	".exe": EXE, ".dll": EXE, ".scr": EXE, ".com": EXE, ".cpl": EXE, ".sys": EXE, ".lnk": Shortcut, ".jar": JAR,
	".sh": Script,
}

// riskyExt sind Endungen, die Windows, macOS oder eine Shell ausführen.
var riskyExt = map[string]bool{
	".exe": true, ".dll": true, ".scr": true, ".com": true, ".cpl": true, ".sys": true, ".bat": true, ".cmd": true,
	".ps1": true, ".psm1": true, ".vbs": true, ".vbe": true, ".js": true, ".jse": true, ".wsf": true, ".wsh": true,
	".hta": true, ".msi": true, ".msp": true, ".lnk": true, ".reg": true, ".jar": true, ".sh": true, ".app": true,
	".pif": true, ".scf": true, ".url": true, ".iso": true, ".img": true, ".vhd": true, ".vhdx": true, ".docm": true,
	".xlsm": true, ".pptm": true, ".apk": true, ".appx": true, ".msix": true, ".chm": true, ".gadget": true,
	// Seit S3.3 (A14): weitere Endungen, die Windows oder macOS ausführen
	// oder die Office mit Makros öffnet.
	".msc": true, ".inf": true, ".ws": true, ".wsc": true, ".sct": true, ".shb": true, ".ps1xml": true, ".psd1": true,
	".xll": true, ".xlam": true, ".ppam": true, ".dotm": true, ".xltm": true, ".potm": true, ".ppsm": true,
	".command": true, ".appref-ms": true, ".settingcontent-ms": true,
	// Seit der Nachbesserung T3 (Punkt 11b): OneNote, Remotedesktop,
	// ClickOnce, Access, Internet-Verknüpfungen, Bibliotheks- und
	// Suchverknüpfungen, Diagnosepakete, XAML-Browseranwendungen,
	// WinHelp und Monad-Skripte.
	".one": true, ".rdp": true, ".application": true, ".accde": true, ".mde": true, ".website": true,
	".library-ms": true, ".search-ms": true, ".diagcab": true, ".xbap": true, ".hlp": true,
	".msh": true, ".msh1": true, ".msh2": true, ".mshxml": true, ".msh1xml": true, ".msh2xml": true,
	// Seit der zweiten Nachbesserung T3 (Punkt 10): Rest der Sperrliste von
	// Outlook (Level 1): Access-Projekte und -Datenbanken, App-Installer
	// und App-Bündel, Office-Add-ins (VSTO), PowerShell-Formate und
	// -Konsolen, Java Web Start, Windows Sandbox, Zertifikate (Import in
	// den Zertifikatspeicher), Internet-Einstellungen, Installer-
	// Transformationen, Exchange-Verknüpfungen, Visual Basic und
	// Unix-Shell-Skripte.
	".ade": true, ".adp": true, ".mdb": true, ".accdb": true, ".mda": true,
	".appinstaller": true, ".appxbundle": true, ".msixbundle": true, ".vsto": true,
	".ps2xml": true, ".psc1": true, ".jnlp": true, ".wsb": true,
	".cer": true, ".crt": true, ".der": true, ".ins": true, ".isp": true, ".mst": true, ".xnk": true,
	".vb": true, ".bas": true, ".ksh": true, ".csh": true,
}

// EffectiveExt liefert die Dateiendung, nach der Windows einen Namen
// ausführt (klein geschrieben, mit Punkt; "" ohne Endung). Vorher fällt
// weg, was Windows beim Anlegen der Datei selbst entfernt oder was nur
// täuscht (A14, S3.3):
//
//   - alles bis zum letzten Pfadtrenner ("/" oder "\"),
//   - ein NTFS-Datenstrom ab dem ersten ":" ("x.bat::$DATA" und
//     "x.bat:strom" sind beide x.bat),
//   - Steuer- und Formatzeichen (etwa U+202E, das die Schreibrichtung
//     umkehrt und "rechnung\u202Efdp.exe" als „rechnungexe.pdf“ zeigt),
//   - Punkte und Leerzeichen am Ende ("x.bat." und "x.bat " sind x.bat).
//
// Maßgeblich ist die letzte Endung: "rechnung.pdf.exe" ist .exe.
func EffectiveExt(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i]
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". \t")
	return strings.ToLower(path.Ext(name))
}

// ExecutableName meldet, ob ein Dateiname nach seiner wirksamen Endung
// (EffectiveExt) ein Programm oder Skript ist.
func ExecutableName(name string) bool { return riskyExt[EffectiveExt(name)] }

// family ordnet verwandte Typen zusammen (ein Wert je Familie).
func family(t string) string {
	switch {
	case strings.HasPrefix(t, "text/"), strings.HasPrefix(t, "message/"), t == "application/json", t == "application/xml",
		t == "application/javascript", t == "application/ecmascript", t == "application/x-javascript":
		if t == Script {
			return Script
		}
		return "text"
	case t == ZIP:
		return "zip"
	}
	return t
}

// verifiable: Für diesen Typ kennt Detect Magic Bytes; stimmt der Inhalt
// nicht, ist das ein Widerspruch. Andere Typen (Audio, Video, Schriften,
// Exoten) lassen sich nicht prüfen und gelten nicht als Widerspruch.
func verifiable(t string) bool {
	switch t {
	case PDF, ZIP, DOCX, XLSX, PPTX, JAR, OLE, EXE, RTF, SevenZip, RAR, GZIP, CAB, XZ, BZIP2, ZSTD, Shortcut,
		"image/jpeg", "image/png", "image/gif", "image/bmp", "image/webp",
		"application/vnd.oasis.opendocument.text", "application/vnd.oasis.opendocument.spreadsheet",
		"application/vnd.oasis.opendocument.presentation":
		return true
	}
	return family(t) == "text"
}

// compatible: Der Inhalt passt zur Behauptung.
func compatible(claim string, d Type) bool {
	if claim == d.MIME || family(claim) == family(d.MIME) {
		return true
	}
	switch claim {
	case ZIP: // DOCX, ODF, JAR sind ZIP-Dateien
		return d.Archive || d.MIME == DOCX || d.MIME == XLSX || d.MIME == PPTX ||
			strings.HasPrefix(d.MIME, "application/vnd.oasis.opendocument.")
	}
	if family(claim) == "text" && d.Text {
		return true
	}
	return false
}

// Normalize vereinheitlicht einen Content-Type (klein, ohne Parameter,
// Aliase aufgelöst).
func Normalize(ct string) string {
	ct, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(ct)), ";")
	ct = strings.TrimSpace(ct)
	if a, ok := aliases[ct]; ok {
		return a
	}
	return ct
}

// Check vergleicht den erkannten Typ mit Content-Type und Dateiname.
func Check(declared, filename string, d Type) Result {
	r := Result{Detected: d, Executable: d.Executable}
	decl := Normalize(declared)
	ext := EffectiveExt(filename)
	if riskyExt[ext] || decl == EXE || decl == Script || decl == Shortcut || decl == JAR {
		r.Executable = true
	}
	type claim struct{ label, mime string }
	var claims []claim
	if !generic[decl] {
		claims = append(claims, claim{"declared " + decl, decl})
	}
	if t, ok := byExt[ext]; ok {
		claims = append(claims, claim{"extension " + ext, t})
	}
	for _, c := range claims {
		switch {
		case d.MIME == Empty:
			// leerer Teil (z. B. abgeschnittene Nachricht): nichts zu prüfen
			continue
		case compatible(c.mime, d):
			continue
		case d.Executable:
			// Ein Programm unter fremder Flagge ist immer ein Widerspruch.
		case !verifiable(c.mime):
			// Angabe nicht prüfbar (Audio, Video, Exoten): kein Widerspruch.
			continue
		}
		r.Mismatch = true
		if r.Reason == "" {
			r.Reason = fmt.Sprintf("%s, content is %s", c.label, d.MIME)
		}
	}
	return r
}
