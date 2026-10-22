package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Prüfung auf umgeleitete Pfade (A7, Befund D3.3c): Ein Programm, das aus
// einem Windows-App-Paket (MSIX) heraus läuft (z. B. aus der
// Claude-Desktop-App gestartet), sieht neue Ordner unter %LOCALAPPDATA% und
// %APPDATA% in Wahrheit unter %LOCALAPPDATA%\Packages\<Paket>\LocalCache\
// Local bzw. …\Roaming; die Aufgabenplanung und jedes andere Programm sehen
// dort einen anderen (leeren) Ordner. Windows zeigt die Umleitung weder im
// Pfad noch über GetFinalPathNameByHandle, und der Prozess hat dabei nicht
// unbedingt eine Paket-Identität (gemessen auf dem Büro-PC, 22.10.2026).
// Verlässlich ist die Datei-Identität: Der Ordner unter %LOCALAPPDATA% und
// sein Gegenstück im Paketordner sind dieselbe Datei (os.SameFile:
// Volume-Seriennummer und Datei-Index). Auf Linux und macOS gibt es das
// nicht; dort prüft CheckRedirects nichts.

// redirectCheck: nur unter Windows (in Tests einschaltbar).
var redirectCheck = runtime.GOOS == "windows"

// maxProbeEntries begrenzt die Einträge je Paketordner, die
// packageView vergleicht (doctor soll schnell bleiben).
const maxProbeEntries = 64

// Redirects ist das Ergebnis von CheckRedirects.
type Redirects struct {
	// Package: Name des Paketordners (unter %LOCALAPPDATA%\Packages), dessen
	// Umleitung dieser Prozess sieht ("" = keine, der Normalfall).
	Package string
	// DataDirReal bzw. ConfigReal: der echte Pfad, wenn Windows das
	// Datenverzeichnis bzw. die Konfigurationsdatei in einen Paketordner
	// umleitet ("" = nicht umgeleitet).
	DataDirReal string
	ConfigReal  string
	// Warnings: fertige Texte für doctor (Englisch).
	Warnings []string
}

// CheckRedirects prüft Datenverzeichnis und (falls vorhanden)
// Konfigurationsdatei. Es legt nichts an und ändert nichts.
func CheckRedirects(p Paths) Redirects {
	var r Redirects
	if !redirectCheck {
		return r
	}
	local, roaming := os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")
	advice := fmt.Sprintf("use a folder outside %%LOCALAPPDATA%% and %%APPDATA%%, e.g. --data-dir C:\\PostKauz\\data or %s (README, \"Daily audit export\")", EnvDataDir)
	if rp, pkg, ok := redirectTarget(p.DataDir, local, roaming); ok {
		r.DataDirReal, r.Package = rp, pkg
		r.Warnings = append(r.Warnings, fmt.Sprintf("data directory is redirected by Windows to %s (this program runs inside the app package %s); the Task Scheduler and other programs see a different folder; %s", rp, pkg, advice))
	}
	if p.ConfigExists {
		if rp, pkg, ok := redirectTarget(p.ConfigFile, local, roaming); ok {
			r.ConfigReal, r.Package = rp, pkg
			r.Warnings = append(r.Warnings, fmt.Sprintf("configuration file is redirected by Windows to %s (this program runs inside the app package %s); the Task Scheduler and other programs do not see it; move it outside %%LOCALAPPDATA%% and %%APPDATA%% and point to it with --config or %s", rp, pkg, EnvConfig))
		}
	}
	if r.DataDirReal == "" && (underBase(p.DataDir, local) || underBase(p.DataDir, roaming)) {
		if _, err := os.Stat(p.DataDir); err != nil {
			if pkg := packageView(local); pkg != "" {
				r.Package = pkg
				r.Warnings = append(r.Warnings, fmt.Sprintf("this program runs inside the app package %s: a data directory created under %%LOCALAPPDATA%% or %%APPDATA%% would be redirected into the package folder and stay invisible to the Task Scheduler; %s", pkg, advice))
			}
		}
	}
	return r
}

// redirectTarget: Liegt path unter %LOCALAPPDATA% bzw. %APPDATA% und ist er
// dieselbe Datei wie sein Gegenstück in einem Paketordner, liefert es dieses
// Gegenstück und den Paketnamen.
func redirectTarget(path, local, roaming string) (target, pkg string, ok bool) {
	if path == "" || local == "" || isPackageCachePath(path) {
		return "", "", false
	}
	var sub, rel string
	switch {
	case underBase(path, local):
		sub, rel = "Local", relTo(local, path)
	case underBase(path, roaming):
		sub, rel = "Roaming", relTo(roaming, path)
	default:
		return "", "", false
	}
	if rel == "" || rel == "." {
		return "", "", false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", "", false
	}
	pkgs, err := os.ReadDir(filepath.Join(local, "Packages"))
	if err != nil {
		return "", "", false
	}
	for _, d := range pkgs {
		if !d.IsDir() {
			continue
		}
		cand := filepath.Join(local, "Packages", d.Name(), "LocalCache", sub, rel)
		if ci, err := os.Stat(cand); err == nil && os.SameFile(fi, ci) {
			return cand, d.Name(), true
		}
	}
	return "", "", false
}

// packageView: Sieht dieser Prozess die Umleitung eines Pakets? Dann ist ein
// Eintrag unter %LOCALAPPDATA% dieselbe Datei wie der gleichnamige im
// LocalCache des Pakets. Liefert den Paketnamen oder "".
func packageView(local string) string {
	if local == "" {
		return ""
	}
	pkgs, err := os.ReadDir(filepath.Join(local, "Packages"))
	if err != nil {
		return ""
	}
	for _, d := range pkgs {
		if !d.IsDir() {
			continue
		}
		cache := filepath.Join(local, "Packages", d.Name(), "LocalCache", "Local")
		entries, err := os.ReadDir(cache)
		if err != nil {
			continue
		}
		for i, e := range entries {
			if i >= maxProbeEntries {
				break
			}
			if strings.EqualFold(e.Name(), "Packages") {
				continue
			}
			ci, err1 := os.Stat(filepath.Join(cache, e.Name()))
			vi, err2 := os.Stat(filepath.Join(local, e.Name()))
			if err1 == nil && err2 == nil && os.SameFile(ci, vi) {
				return d.Name()
			}
		}
	}
	return ""
}

// isPackageCachePath: …\Packages\<Paket>\LocalCache\… (Groß-/Kleinschreibung
// egal, beide Trennzeichen).
func isPackageCachePath(path string) bool {
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(path, `\`, "/")), "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "packages" && parts[i+1] != "" && parts[i+2] == "localcache" {
			return true
		}
	}
	return false
}

// underBase: liegt path unter base (ohne Rücksicht auf Groß-/Kleinschreibung,
// wie unter Windows)?
func underBase(path, base string) bool {
	if base == "" || path == "" {
		return false
	}
	low := strings.ToLower(filepath.Clean(path))
	b := strings.ToLower(filepath.Clean(base))
	return low == b || strings.HasPrefix(low, b+string(filepath.Separator))
}

// relTo: path relativ zu base (base ist per underBase geprüft).
func relTo(base, path string) string {
	cp, cb := filepath.Clean(path), filepath.Clean(base)
	if len(cp) <= len(cb) {
		return ""
	}
	return strings.TrimPrefix(cp[len(cb):], string(filepath.Separator))
}
