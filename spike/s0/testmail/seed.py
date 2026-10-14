#!/usr/bin/env python3
"""Korpus per IMAP APPEND in einen Testserver laden (protokollneutral).

Aufruf:
  python3 seed.py --port 31143 --user spike --password pass --corpus ../corpus
  python3 seed.py --port 32993 --tls --user spike --password pass --corpus ../corpus

--tls nutzt ein unverifiziertes TLS-Kontext-Objekt; NUR fuer die lokalen
Docker-Testserver mit selbstsigniertem Zertifikat gedacht.
"""
import argparse
import imaplib
import json
import os
import re
import ssl
import time
from datetime import datetime


def connect(a):
    if a.tls:
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE  # nur Docker-Testserver
        m = imaplib.IMAP4_SSL(a.host, a.port, ssl_context=ctx)
    else:
        m = imaplib.IMAP4(a.host, a.port)
    m.login(a.user, a.password)
    return m


def list_mailboxes(m):
    typ, data = m.list()
    out = {}
    sep = "/"
    for line in data:
        if not line:
            continue
        s = line.decode()
        mm = re.match(r'\((.*?)\) "(.*?)" (.*)$', s)
        if not mm:
            continue
        attrs, sep, name = mm.groups()
        out[name.strip('"')] = attrs
    return out, sep


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, required=True)
    ap.add_argument("--tls", action="store_true")
    ap.add_argument("--user", required=True)
    ap.add_argument("--password", required=True)
    ap.add_argument("--corpus", required=True)
    a = ap.parse_args()

    manifest = json.load(open(os.path.join(a.corpus, "manifest.json"), encoding="utf-8"))
    m = connect(a)
    boxes, sep = list_mailboxes(m)
    drafts = next((n for n, at in boxes.items() if "\\Drafts" in at), None) or "Drafts"
    target = {"INBOX": "INBOX", "Drafts": drafts,
              "Archiv/2024": f"Archiv{sep}2024", "Archiv/2025": f"Archiv{sep}2025"}
    for name in ["Archiv", target["Archiv/2024"], target["Archiv/2025"], drafts]:
        if name not in boxes:
            typ, data = m.create(f'"{name}"')
            print("CREATE", name, typ, data)
    t0 = time.time()
    counts = {}
    for e in manifest:
        raw = open(os.path.join(a.corpus, e["file"]), "rb").read()
        try:
            dt = datetime.fromisoformat(e["date"])
            idate = imaplib.Time2Internaldate(dt)
        except Exception:
            idate = None
        flags = "(" + " ".join(e["flags"]) + ")" if e["flags"] else None
        box = target[e["folder"]]
        typ, data = m.append(f'"{box}"', flags, idate, raw)
        if typ != "OK":
            print("APPEND-Fehler", e["file"], typ, data)
        counts[box] = counts.get(box, 0) + 1
    dur = time.time() - t0
    print(json.dumps(dict(appended=sum(counts.values()), by_mailbox=counts,
                          seconds=round(dur, 2), per_second=round(sum(counts.values()) / dur, 1)),
                     ensure_ascii=False))
    m.logout()


if __name__ == "__main__":
    main()
