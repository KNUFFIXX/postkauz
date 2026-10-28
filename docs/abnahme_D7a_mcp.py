"""MCP-Probe fuer Abnahme D7a, Teil A Schritt 3 (docs/abnahme_D7a.md).

Spricht mit `postkauz mcp` ueber stdio (JSON-RPC, eine Zeile je Nachricht),
nur Python-Standardbibliothek (Python 3.8 oder neuer). Gestartet wird sie
von docs/abnahme_D7a.ps1; sie laeuft ebenso unter Linux und macOS.

Jede Pruefung ist eine Zeile `OK <Nr> <Beschreibung>` bzw.
`FAIL <Nr> <Beschreibung>`, darunter eingerueckt die Ist-Ausgabe (gekuerzt).
Exit 0 nur ohne FAIL.

Pruefungen (A03 Teil D, D7 Schritt 3):
  A3.10 assistant sieht mail_tag, mail_mark, mail_sync_now, aber weder
        mail_move noch mail_trash.
  A3.11 operator sieht mail_move und mail_trash (und die drei oben).
  A3.12 mail_trash mit 51 IDs ohne confirm_count (Schluessel operator)
        -> status denied, Grund confirm_count_required; nichts angelegt
        (mail_list_actions bzw. die Zahl der Aktionen bleibt gleich).

Geprueft wird an stabilen Merkmalen (Toolnamen, Status, Grund), nicht an
ganzen Saetzen.

Schluessel: assistant ueber POSTKAUZ_KEY_FILE, operator ueber
POSTKAUZ_KEY_FILE (je eine Datei, vom aufrufenden Skript angelegt). Ein
Schluessel erscheint nie in der Ausgabe.
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

    def __init__(self, args, env_extra):
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
            "clientInfo": {"name": "d7a-probe", "version": "1"}})
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


def tool_names(sess):
    r = sess.request("tools/list", {})
    return sorted(t["name"] for t in r.get("tools", []))


def action_count(sess):
    """Zahl der Aktionen laut mail_list_actions (None, wenn es das Tool nicht gibt)."""
    try:
        is_err, data, text = sess.call("mail_list_actions", {})
    except Exception:
        return None
    if is_err:
        return None
    return len(data.get("actions") or [])


def run(args):
    want = ["mail_tag", "mail_mark", "mail_sync_now"]
    s = Session(args, {"POSTKAUZ_KEY_FILE": args.assistant_key_file})
    try:
        init = s.initialize()
        names = tool_names(s)
        missing = [n for n in want if n not in names]
        extra = [n for n in ("mail_move", "mail_trash") if n in names]
        si = init.get("serverInfo") or {}
        check("A3.10", "MCP assistant: mail_tag, mail_mark, mail_sync_now ja; mail_move, mail_trash nein",
              not missing and not extra,
              "server " + str(si.get("name")) + " " + str(si.get("version")) + "\n"
              + "tools: " + str(len(names)) + ": " + ", ".join(names)
              + "\nfehlend: " + str(missing) + "\nzu viel: " + str(extra))
    finally:
        s.close()

    s = Session(args, {"POSTKAUZ_KEY_FILE": args.operator_key_file})
    try:
        s.initialize()
        names = tool_names(s)
        missing = [n for n in want + ["mail_move", "mail_trash"] if n not in names]
        check("A3.11", "MCP operator: mail_move und mail_trash vorhanden (dazu mail_tag, mail_mark, mail_sync_now)",
              not missing,
              "tools: " + str(len(names)) + ": " + ", ".join(names) + "\nfehlend: " + str(missing))

        before = action_count(s)
        ids = list(range(1, 52))
        is_err, data, text = s.call("mail_trash", {"ids": ids, "dry_run": False})
        after = action_count(s)
        status = data.get("status")
        reason = str(data.get("reason") or data.get("code") or "")
        ok = (status == "denied" and reason == "confirm_count_required"
              and not data.get("action_id") and before == after)
        check("A3.12", "mail_trash mit 51 IDs ohne confirm_count -> denied (confirm_count_required), kein Antrag",
              ok, "isError " + str(is_err) + ", status " + str(status) + ", Grund " + reason
              + ", action_id " + str(data.get("action_id"))
              + ", Aktionen vorher " + str(before) + ", nachher " + str(after) + "\n" + text)
    finally:
        s.close()


def main():
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--exe", required=True)
    ap.add_argument("--data-dir", required=True)
    ap.add_argument("--config", required=True)
    ap.add_argument("--assistant-key-file", required=True)
    ap.add_argument("--operator-key-file", required=True)
    args = ap.parse_args()
    try:
        run(args)
    except Exception as e:  # Abbruch der Probe ist selbst ein FAIL
        check("A3.x", "MCP-Probe abgebrochen", False, type(e).__name__ + ": " + str(e))
    print("MCP-Probe: " + str(RESULTS["ok"]) + " OK, " + str(RESULTS["fail"]) + " FAIL")
    sys.exit(0 if RESULTS["fail"] == 0 else 1)


if __name__ == "__main__":
    main()
