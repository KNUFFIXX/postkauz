"""MCP-Probe fuer Abnahme D4, Punkte 7 und 9 (docs/abnahme_D4.md).

Spricht mit `postkauz mcp` ueber stdio (JSON-RPC, eine Zeile je Nachricht),
nur Python-Standardbibliothek (Python 3.8 oder neuer). Gestartet wird sie
von docs/abnahme_D4.ps1; sie laeuft ebenso unter Linux und macOS.

Jede Pruefung ist eine Zeile `OK <Nr> <Beschreibung>` bzw.
`FAIL <Nr> <Beschreibung>`, darunter eingerueckt die Ist-Ausgabe (gekuerzt).
Zeilen `VALUE <name>=<wert>` gibt das aufrufende Skript weiter (z. B. die
Draft-ID fuer `draft abandon`). Exit 0 nur ohne FAIL.

Phasen:
  tools   Punkt 7: Tool-Liste mit reader (9) und assistant (15), keine
          Tools zum Freigeben, Ablehnen, Senden, Loeschen, Verschieben;
          mail_create_draft im Trockenlauf (nichts gespeichert) und echt
          (draft_id, message_id, remote pending); mail_list_drafts.
  closed  Punkt 9: nach `draft abandon` (CLI) liefert mail_update_draft
          denied mit Grund draft_closed.

Geprueft wird an stabilen Merkmalen (Status, Grund, Zaehlungen, Feldnamen),
nicht an ganzen Saetzen: Beschreibungen und Hinweistexte koennen sich
aendern, ohne dass sich das Verhalten aendert.

Schluessel: reader ueber POSTKAUZ_KEY (aus --reader-key-file gelesen),
assistant ueber POSTKAUZ_KEY_FILE (so wie im MCP-Eintrag fuer Punkt 8).
Ein Schluessel erscheint nie in der Ausgabe.
"""

import argparse
import json
import os
import queue
import re
import subprocess
import sys
import threading
import time

FORBIDDEN = ("approve", "reject", "send", "delete", "move", "trash")
KEY_RE = re.compile(r"pk_key_[A-Za-z0-9_-]+")

RESULTS = {"ok": 0, "fail": 0}


def mask(text):
    return KEY_RE.sub("pk_key_********", text)


def short(text, limit=900):
    text = mask(str(text))
    if len(text) > limit:
        text = text[:limit] + " ...[gekuerzt]"
    return text


def check(nr, desc, ok, detail=""):
    RESULTS["ok" if ok else "fail"] += 1
    print(("OK" if ok else "FAIL") + " " + nr + " " + desc)
    if detail:
        for line in short(detail).splitlines():
            print("      " + line)
    sys.stdout.flush()


class Session:
    """Ein Kindprozess `postkauz mcp` mit Lese-Threads fuer stdout/stderr."""

    def __init__(self, args, env_extra, name):
        env = dict(os.environ)
        for k in ("POSTKAUZ_KEY", "POSTKAUZ_KEY_FILE", "POSTKAUZ_DATA_DIR",
                  "POSTKAUZ_CONFIG", "POSTKAUZ_POLICY", "POSTKAUZ_PROFILE"):
            env.pop(k, None)
        env.update(env_extra)
        cmd = [args.exe, "--data-dir", args.data_dir, "--config", args.config, "mcp"]
        flags = 0
        if os.name == "nt":
            # Ohne eigene Konsole (das Skript laeuft unsichtbar): kein Fenster.
            flags = getattr(subprocess, "CREATE_NO_WINDOW", 0x08000000)
        self.p = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                  stderr=subprocess.PIPE, env=env, creationflags=flags)
        self.lines = queue.Queue()
        self.err = []
        self.next_id = 1
        threading.Thread(target=self._read_out, daemon=True).start()
        threading.Thread(target=self._read_err, daemon=True).start()
        self.name = name

    def _read_out(self):
        for raw in iter(self.p.stdout.readline, b""):
            self.lines.put(raw.decode("utf-8", "replace"))
        self.lines.put(None)

    def _read_err(self):
        for raw in iter(self.p.stderr.readline, b""):
            self.err.append(raw.decode("utf-8", "replace"))

    def send(self, obj):
        self.p.stdin.write((json.dumps(obj) + "\n").encode("utf-8"))
        self.p.stdin.flush()

    def request(self, method, params, timeout=60):
        rid = self.next_id
        self.next_id += 1
        self.send({"jsonrpc": "2.0", "id": rid, "method": method, "params": params})
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                line = self.lines.get(timeout=max(0.1, deadline - time.time()))
            except queue.Empty:
                break
            if line is None:
                raise RuntimeError("mcp process ended; stderr: " + "".join(self.err)[-400:])
            line = line.strip()
            if not line.startswith("{"):
                continue
            msg = json.loads(line)
            if msg.get("id") == rid:
                if "error" in msg:
                    raise RuntimeError("json-rpc error: " + json.dumps(msg["error"]))
                return msg.get("result") or {}
        raise RuntimeError("no answer to " + method + " within " + str(timeout) + " s")

    def initialize(self):
        r = self.request("initialize", {
            "protocolVersion": "2025-06-18", "capabilities": {},
            "clientInfo": {"name": "d4-probe", "version": "1"}})
        self.send({"jsonrpc": "2.0", "method": "notifications/initialized"})
        return r

    def call(self, tool, arguments):
        r = self.request("tools/call", {"name": tool, "arguments": arguments})
        data = r.get("structuredContent")
        if data is None:
            try:
                data = json.loads(r["content"][0]["text"])
            except Exception:
                data = {}
        text = ""
        try:
            text = r["content"][0]["text"]
        except Exception:
            text = json.dumps(data, ensure_ascii=False)
        return bool(r.get("isError", False)), data, text

    def close(self):
        try:
            self.p.stdin.close()
        except Exception:
            pass
        try:
            self.p.wait(timeout=10)
        except Exception:
            self.p.kill()


def read_key(path):
    with open(path, encoding="utf-8-sig") as f:
        return f.read().strip()


def tool_names(sess):
    r = sess.request("tools/list", {})
    return sorted(t["name"] for t in r.get("tools", []))


def forbidden_in(names):
    return [n for n in names if any(w in n for w in FORBIDDEN)]


def draft_ids(sess):
    """IDs aller Entwuerfe laut mail_list_drafts (None bei Fehler)."""
    is_err, data, text = sess.call("mail_list_drafts", {})
    if is_err:
        return None, text
    ids = []
    for d in (data.get("drafts") or []):
        if isinstance(d, dict) and "draft_id" in d:
            ids.append(d["draft_id"])
    if not ids:
        # Rueckfall, falls sich die Huelle der Liste aendert.
        ids = [int(m) for m in re.findall(r'"draft_id"\s*:\s*(\d+)', text)]
    return sorted(set(ids)), text


def phase_tools(args):
    # 7.1 reader: weiterhin 9 Tools, nichts zu Entwuerfen.
    s = Session(args, {"POSTKAUZ_KEY": read_key(args.reader_key_file)}, "reader")
    try:
        init = s.initialize()
        names = tool_names(s)
        bad = forbidden_in(names) + [n for n in names if "draft" in n]
        si = init.get("serverInfo") or {}
        check("7.1", "MCP reader (test-agent): 9 Tools, keines zu Entwuerfen, Freigabe, Senden, Loeschen, Verschieben",
              len(names) == 9 and not bad,
              "server " + str(si.get("name")) + " " + str(si.get("version")) + "\n"
              + "tools: " + str(len(names)) + ": " + ", ".join(names) + "\nverboten: " + str(bad))
    finally:
        s.close()

    # 7.2 assistant: 15 Tools, darunter Anlegen und Vorlegen, nichts zum Freigeben.
    s = Session(args, {"POSTKAUZ_KEY_FILE": args.assistant_key_file}, "assistant")
    try:
        init = s.initialize()
        names = tool_names(s)
        bad = forbidden_in(names)
        need = ["mail_create_draft", "mail_request_ready", "mail_update_draft",
                "mail_get_draft", "mail_list_drafts", "mail_get_approval"]
        missing = [n for n in need if n not in names]
        check("7.2", "MCP assistant (test-assistant): 15 Tools mit mail_create_draft und mail_request_ready, keines mit approve/reject/send/delete/move",
              len(names) == 15 and not bad and not missing,
              "tools: " + str(len(names)) + ": " + ", ".join(names)
              + "\nverboten: " + str(bad) + "\nfehlend: " + str(missing))
        instr = (init.get("instructions") or "")
        check("7.3", "Hinweis beim Verbindungsaufbau: Entwuerfe ja, Senden nie (Woerter draft und sent/send)",
              "draft" in instr.lower() and ("never" in instr.lower() or "nothing is sent" in instr.lower()),
              instr)

        before, before_text = draft_ids(s)
        base = {"kind": "reply", "in_reply_to": args.reply_to,
                "body_markdown": "Hallo Frau Beispiel,\n\nder Termin passt. " + args.canary}

        # 7.4 Trockenlauf: Vorschau, nichts gespeichert.
        a = dict(base)
        a["dry_run"] = True
        is_err, data, text = s.call("mail_create_draft", a)
        after, _ = draft_ids(s)
        ok = (not is_err and data.get("dry_run") is True and not data.get("draft_id")
              and before is not None and after == before)
        check("7.4", "mail_create_draft mit dry_run: true -> Vorschau, kein Entwurf gespeichert",
              ok, "isError " + str(is_err) + ", dry_run " + str(data.get("dry_run"))
              + ", draft_id " + str(data.get("draft_id")) + ", Entwuerfe vorher " + str(before)
              + ", nachher " + str(after) + "\n" + text)

        # 7.5 echt: draft_id, message_id, remote pending.
        a = dict(base)
        a["dry_run"] = False
        is_err, data, text = s.call("mail_create_draft", a)
        did = data.get("draft_id")
        ok = (not is_err and isinstance(did, int) and did > 0 and bool(data.get("message_id"))
              and data.get("remote") == "pending" and data.get("status") == "draft")
        check("7.5", "mail_create_draft mit dry_run: false -> draft_id, message_id, status draft, remote pending",
              ok, "isError " + str(is_err) + ", draft_id " + str(did) + ", status " + str(data.get("status"))
              + ", remote " + str(data.get("remote")) + ", source " + str(data.get("source"))
              + ", message_id " + str(data.get("message_id"))
              + "\nremote_note: " + str(data.get("remote_note")))
        if ok:
            print("VALUE draft_id=" + str(did))

        after, after_text = draft_ids(s)
        check("7.6", "mail_list_drafts zeigt den neuen Entwurf",
              after is not None and did in after, "Entwuerfe: " + str(after) + "\n" + after_text)
    finally:
        s.close()


def phase_closed(args):
    s = Session(args, {"POSTKAUZ_KEY_FILE": args.assistant_key_file}, "assistant")
    try:
        s.initialize()
        is_err, data, text = s.call("mail_update_draft", {
            "draft_id": args.draft, "body_markdown": "Hallo,\n\nnoch eine Aenderung.", "dry_run": False})
        status = data.get("status")
        reason = str(data.get("reason") or data.get("code") or "")
        ok = status == "denied" and ("draft_closed" in reason or "draft_closed" in text)
        check("9.2", "mail_update_draft nach draft abandon -> denied (draft_closed)",
              ok, "isError " + str(is_err) + ", status " + str(status) + ", Grund " + reason + "\n" + text)
    finally:
        s.close()


def main():
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("phase", choices=["tools", "closed"])
    ap.add_argument("--exe", required=True)
    ap.add_argument("--data-dir", required=True)
    ap.add_argument("--config", required=True)
    ap.add_argument("--reader-key-file", required=True)
    ap.add_argument("--assistant-key-file", required=True)
    ap.add_argument("--reply-to", type=int, default=0, help="lokale ID der Nachricht fuer die Antwort")
    ap.add_argument("--canary", default="MCPVOGEL", help="Wort im Entwurfstext, das nie im Audit stehen darf")
    ap.add_argument("--draft", type=int, default=0, help="Draft-ID fuer die Phase closed")
    args = ap.parse_args()
    try:
        if args.phase == "tools":
            phase_tools(args)
        else:
            phase_closed(args)
    except Exception as e:  # Abbruch der Probe ist selbst ein FAIL
        check("7/9", "MCP-Probe abgebrochen", False, type(e).__name__ + ": " + str(e))
    print("MCP-Probe: " + str(RESULTS["ok"]) + " OK, " + str(RESULTS["fail"]) + " FAIL")
    sys.exit(0 if RESULTS["fail"] == 0 else 1)


if __name__ == "__main__":
    main()
