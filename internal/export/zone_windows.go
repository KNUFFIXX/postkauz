//go:build windows

package export

import "os"

// zoneIdentifier ist der Inhalt des Datenstroms Zone.Identifier: Zone 3
// heißt „aus dem Internet“ (Mark of the Web).
const zoneIdentifier = "[ZoneTransfer]\r\nZoneId=3\r\n"

// markFromInternet gibt der Datei path den NTFS-Datenstrom
// ":Zone.Identifier" mit Zone 3, wie ein Browser einem Download: Windows
// warnt vor dem Ausführen (SmartScreen), Office öffnet in der geschützten
// Ansicht und sperrt Makros (Nachbesserung T3, Punkt 11c). Über os.Root
// geht das nicht (Go lehnt ":" dort ab); deshalb über den vollen Pfad,
// nachdem die Datei über os.Root angelegt und geschrieben ist.
// Dateisysteme ohne Datenströme (FAT32, exFAT, manche Netzlaufwerke)
// lehnen ab; der Aufrufer warnt dann nur.
func markFromInternet(path string) error {
	return os.WriteFile(path+":Zone.Identifier", []byte(zoneIdentifier), 0o600)
}
