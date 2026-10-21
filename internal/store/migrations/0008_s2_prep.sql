-- PostKauz, Migration 0008: Vorarbeiten für Schnitt 2 (A02, Teil A, S2.0).
--
-- A2: Kennung des Datenverzeichnisses. Der Verweis in den Schlüsselbund
-- hieß bis S1 "account/<name>"; zwei Datenverzeichnisse (Profile) mit
-- gleichem Kontonamen teilten sich damit einen Eintrag. Ab jetzt heißt er
-- "account/<datadir-id>/<name>", <datadir-id> sind die ersten 8 Hex dieser
-- Kennung (store.DataDirID). Sie entsteht genau einmal, beim ersten Öffnen
-- mit diesem Schema, und ändert sich danach nie (eine Kopie des
-- Datenverzeichnisses, etwa eine Wiederherstellung, behält sie).
-- accounts.credential_ref ändert diese Datei bewusst NICHT: Den Umzug der
-- Einträge (lesen alt, schreiben neu, Verweis umstellen, löschen alt) macht
-- das Programm beim ersten Öffnen mit Zugriff auf den Schlüsselbund
-- (ops.MoveCredentialRefs); der Verweis wechselt erst, wenn der neue
-- Eintrag geschrieben ist. Bis dahin gilt der alte Verweis weiter.

INSERT INTO meta(key, value) VALUES ('datadir_id', lower(hex(randomblob(16))))
    ON CONFLICT(key) DO NOTHING;
