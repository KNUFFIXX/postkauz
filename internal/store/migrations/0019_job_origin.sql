-- PostKauz, Migration 0019 (Schnitt 3, Teil T1b; A03 A8): Jobs mit
-- Herkunft.
--
-- key_id: wer den Job eingereiht hat, als Text wie audit_events.api_key_id
-- ("cli" | "key:<name>" | "system"); audit_id: der Start-Eintrag des
-- Aufrufs, der ihn einreihte (NULL bei Jobs der Trigger und älteren Jobs).
-- Je Art und Konto ist höchstens ein Job offen; ein späterer Aufruf, der
-- denselben offenen Job meint, ändert die Herkunft nicht (der erste zählt).
-- Wer den Job ausführt, prüft vor jeder Wirkung den Schlüssel, für den sie
-- geschieht (Widerruf, Ablauf, Scope; ops.DraftPusher je Entwurf).
--
-- Nur neue Spalten (ALTER TABLE ADD COLUMN), kein Umbau; bestehende Zeilen
-- behalten alle Werte.

ALTER TABLE jobs ADD COLUMN key_id TEXT;
ALTER TABLE jobs ADD COLUMN audit_id INTEGER;
