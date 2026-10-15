# testdata

Testmaterial für PostKauz. Regeln (ausführlich: [`docs/testkorpus.md`](../docs/testkorpus.md)):

1. **Nur erfundene Daten im Repository.** Personen, Organisationen, Nummern und Texte sind synthetisch; Adressen stammen ausschließlich aus `example.com`, `example.org`, `example.net` oder `*.invalid`.
2. **Nie echte Mails**, auch nicht „nur ein bisschen gekürzt". Echte Mails werden nur anonymisiert und nur außerhalb des Repositorys verwendet (`testdata/private/` ist per `.gitignore` ausgeschlossen und kein Ablageort für Freigegebenes).
3. **Deterministisch erzeugen.** Der Korpus entsteht aus einem Generator mit festem Zufallssamen (`make corpus`, ab Schnitt 1) und einem Manifest mit den erwarteten Ergebnissen. Handgeschriebene Einzelfälle sind erlaubt, wenn sie ein konkretes Verhalten belegen und im Manifest stehen.
4. **Groß bleibt draußen.** Lasttest-Korpora (`testdata/corpus-large/`) werden lokal erzeugt und nicht eingecheckt.
5. **PII-Gate:** `make pii-check` muss vor jedem Commit grün sein.
6. **Schädliche Fälle** (Zip-Bomben, Skript-Anhänge, Injektionstexte) werden zur Laufzeit erzeugt oder klein und harmlos gehalten; es liegt nie ausführbare Schadsoftware im Repository.

Aktuell enthält das Verzeichnis nur diese Datei.
