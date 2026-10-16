package magic

import (
	"fmt"
	"path"
	"strings"
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
}

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
	ext := strings.ToLower(path.Ext(strings.TrimSpace(strings.ReplaceAll(filename, `\`, "/"))))
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
