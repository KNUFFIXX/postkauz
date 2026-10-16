//go:build race

package importer

// raceEnabled: Unter dem Race-Detector (make race) läuft der Test mit der
// großen Datei kleiner; die 200 MB prüft make check.
const raceEnabled = true
