package cli

import "runtime"

// mdName setzt in einem Maildir-Testnamen das Trennzeichen der Flags: „:2,“,
// unter Windows „!2,“ (ein „:“ legte dort einen NTFS-Datenstrom an statt
// einer Datei; A12.4). Der Importer liest beide.
func mdName(name string) string {
	if runtime.GOOS == "windows" {
		return windowsInfo(name)
	}
	return name
}

func windowsInfo(name string) string {
	out := []byte(name)
	for i := 0; i+2 < len(out); i++ {
		if out[i] == ':' && out[i+1] == '2' && out[i+2] == ',' {
			out[i] = '!'
		}
	}
	return string(out)
}
