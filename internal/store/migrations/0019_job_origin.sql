-- PostKauz, Migration 0019 (Schnitt 3, Teil T1b; A03 A8): Jobs und
-- Entwürfe mit Herkunft.
--
-- key_id: wer den Job eingereiht hat, als Text wie audit_events.api_key_id
-- ("cli" | "key:<name>" | "system"); audit_id: der Start-Eintrag des
-- Aufrufs, der ihn einreihte (NULL bei Jobs der Trigger und älteren Jobs).
-- Je Art und Konto ist höchstens ein Job offen; ein späterer Aufruf, der
-- denselben offenen Job meint, ändert die Herkunft nicht (der erste zählt).
-- Wer den Job ausführt, prüft vor jeder Wirkung den Schlüssel, für den sie
-- geschieht (Widerruf, Ablauf, Scope; ops.DraftPusher je Entwurf).
--
-- drafts.updated_by (Nachbesserung T1b, A-F3): wer den Inhalt zuletzt
-- geändert hat ("cli" | "key:<name>"; Anlegen und drafts.update). Der Push
-- prüft diesen Schlüssel wie created_by. NULL bei älteren Entwürfen (dann
-- gilt created_by).
--
-- drafts.push_hold (A-F3): Grund, warum der Push einen Entwurf des Menschen
-- zurückhält (key_revoked, key_expired, key_invalid, missing_scope): Ein
-- Agent hat den Inhalt zuletzt geändert, sein Schlüssel gilt nicht mehr.
-- Der Entwurf bleibt draft, wird aber nicht abgelegt, bis der Mensch ihn
-- ändert oder selbst vorlegt. Eigene Spalte statt eines Codes in push_note,
-- weil dessen CHECK-Liste nur per Neubau änderbar ist.
--
-- Nur neue Spalten (ALTER TABLE ADD COLUMN), kein Umbau; bestehende Zeilen
-- behalten alle Werte.

ALTER TABLE jobs ADD COLUMN key_id TEXT;
ALTER TABLE jobs ADD COLUMN audit_id INTEGER;
ALTER TABLE drafts ADD COLUMN updated_by TEXT;
ALTER TABLE drafts ADD COLUMN push_hold TEXT;
