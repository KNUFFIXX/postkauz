#!/usr/bin/env python3
"""Synthetischer Mailkorpus fuer den PostKauz-Spike S0.

Erzeugt N (Standard 1000) .eml-Dateien plus manifest.json mit den
erwarteten Eigenschaften jeder Mail (Ordner, Message-ID, Thread-Schluessel,
Charset, dekodierter Betreff, Anhangsnamen, Kaputt-Typ).

Nur fiktive Namen und Adressen unter *.example.test. Deterministisch (Seed).

Aufruf:  python3 gen_corpus.py --out ../corpus [--count 1000] [--seed 42]
"""
import argparse
import io
import json
import os
import random
import zipfile
from datetime import datetime, timedelta, timezone
from email import policy
from email.header import Header
from email.message import EmailMessage, Message
from email.mime.multipart import MIMEMultipart
from email.mime.text import MIMEText
from email.utils import format_datetime

DOMAIN = "corpus.example.test"

# Fiktive Personen (bewusst Umlaute, keine realen Personen)
PEOPLE = [
    ("Jürgen Testmann", "juergen.testmann@mieter.example.test"),
    ("Bärbel Beispiel", "baerbel@beispiel.example.test"),
    ("Ömer Platzhalter", "oemer.p@hausverwaltung.example.test"),
    ("Grete Größe", "grete@groesse.example.test"),
    ("Ännchen Übermut", "aennchen@uebermut.example.test"),
    ("Sören Straßburger", "soeren@strasse.example.test"),
    ("Hans-Jörg Fiktiv", "hj.fiktiv@handwerk.example.test"),
    ("Zoë Musterfrau", "zoe@muster.example.test"),
    ("Malte Mängel", "malte@maengel.example.test"),
    ("Dörte Dummy", "doerte@dummy.example.test"),
]
ME = ("Spike Postfach", "spike@mw.example.test")

SUBJECTS = [
    "Heizungsablesung im Erdgeschoß",
    "Schlüsselübergabe für Wohnung 3 links",
    "Nebenkostenabrechnung – Rückfrage zur Grundsteuer",
    "Wasserschaden im Badezimmer (dringend!)",
    "Kündigung des Stellplatzes zum Monatsende",
    "Rauchwarnmelder-Wartung: Terminvorschläge",
    "Treppenhausreinigung – Beschwerde über Lärm",
    "Angebot für Fassadenanstrich „Südseite“",
    "Mieterhöhung gemäß § 558 BGB – Zustimmung",
    "Müllgebühren und Biotonne – Änderungen ab Januar",
    "Fenster schließt nicht – Reparatur nötig",
    "Übergabeprotokoll und Zählerstände",
]

SENTENCES = [
    "Sehr geehrte Damen und Herren, ich wende mich wegen der Heizkörper an Sie.",
    "Die Thermostatventile im Wohnzimmer lassen sich nicht mehr drehen.",
    "Können Sie bitte einen Handwerker schicken? Ich bin werktags ab 15 Uhr erreichbar.",
    "Anbei übersende ich Ihnen die Zählerstände vom Übergabetermin.",
    "Die Grundsteuer ist im Vergleich zum Vorjahr um 12,5 % gestiegen – warum?",
    "Im Keller riecht es muffig; vermutlich ist die Lüftung defekt.",
    "Für Rückfragen stehe ich gern zur Verfügung. Mit freundlichen Grüßen",
    "Der Gärtner hat die Hecke zu stark gekürzt, die Nachbarn beschweren sich.",
    "Bitte bestätigen Sie den Eingang dieser Nachricht kurz per E-Mail.",
    "Die Rechnung über 1.234,56 € lege ich als PDF bei.",
    "Äpfel, Öfen und Übergrößen: Umlaute müssen überall korrekt ankommen.",
    "Straßenreinigung und Winterdienst sind laut Vertrag umlagefähig.",
    "Die Waschmaschine im Gemeinschaftsraum ist seit Dienstag außer Betrieb.",
    "Ich bitte um Zusendung einer Kopie des Mietvertrags inklusive Anlagen.",
]

ATTACH_NAMES = [
    "Übergabeprotokoll Wohnung 3.pdf",
    "Zählerstände_März.pdf",
    "Mietvertrag Änderung.docx",
    "Schadensfoto Küche.jpg",
    "Kostenübersicht 2025 – Grundsteuer und Müllgebühren für das gesamte Gebäude.pdf",
    "Fotodokumentation_Wasserschaden_Bäder.jpg",
    "Angebot Fassade „Süd“.docx",
]

NEWSLETTERS = [
    ("Mieterbund-Rundbrief", "rundbrief.verein.example.test", "Neues aus dem Mietrecht – Ausgabe {n}"),
    ("Energie-Tipps", "news.energie.example.test", "Heizkosten sparen: {n} Tipps für den Winter"),
    ("Handwerker-Börse", "liste.handwerk.example.test", "Wochenübersicht KW {n}: Angebote für Hausverwaltungen"),
]


def body_text(rng, n=None):
    n = n or rng.randint(2, 6)
    return "\n".join(rng.choice(SENTENCES) for _ in range(n)) + "\n"


def fake_pdf(rng):
    return (b"%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj << /Type /Catalog >> endobj\n"
            + bytes(rng.getrandbits(8) for _ in range(rng.randint(200, 2000)))
            + b"\n%%EOF\n")


def fake_docx(rng):
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr("[Content_Types].xml", '<?xml version="1.0"?><Types/>')
        z.writestr("word/document.xml", "<w:document>" + body_text(rng) + "</w:document>")
    return buf.getvalue()


def fake_jpg(rng):
    return (b"\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"
            + bytes(rng.getrandbits(8) for _ in range(rng.randint(300, 3000)))
            + b"\xff\xd9")


def attachment_for(rng, name):
    if name.endswith(".pdf"):
        return fake_pdf(rng), "application", "pdf"
    if name.endswith(".docx"):
        return fake_docx(rng), "application", "vnd.openxmlformats-officedocument.wordprocessingml.document"
    return fake_jpg(rng), "image", "jpeg"


def charset_for(rng):
    return rng.choices(["utf-8", "iso-8859-1", "windows-1252"], weights=[60, 25, 15])[0]


def encodable(text, charset):
    """Text so anpassen, dass er im Ziel-Charset darstellbar ist."""
    if charset == "iso-8859-1":
        text = (text.replace("€", "EUR").replace("„", '"').replace("“", '"')
                .replace("–", "-").replace("ë", "e"))
    if charset == "windows-1252":
        text = text.replace("ë", "e")
    text.encode(charset)
    return text


def fmt_addr(name, addr, charset):
    # Anzeigename als RFC-2047-encoded-word im gewaehlten Charset
    return f"{Header(name, charset).encode()} <{addr}>"


def build_message(rng, *, idx, charset, subject, sender, to, date, msgid,
                  in_reply_to=None, references=None, html=False,
                  attachments=(), newsletter=None, q_encoding=False):
    subject = encodable(subject, charset)
    text = encodable(body_text(rng), charset)
    text_part = MIMEText(text, "plain", charset)
    if html:
        html_body = "<html><body>" + "".join(f"<p>{l}</p>" for l in text.splitlines()) + "</body></html>"
        alt = MIMEMultipart("alternative")
        alt.attach(text_part)
        alt.attach(MIMEText(html_body, "html", charset))
        main = alt
    else:
        main = text_part
    if attachments:
        outer = MIMEMultipart("mixed")
        outer.attach(main)
        for name in attachments:
            data, maj, sub = attachment_for(rng, name)
            from email.mime.base import MIMEBase
            from email import encoders
            part = MIMEBase(maj, sub)
            part.set_payload(data)
            encoders.encode_base64(part)
            # RFC 2231 (filename*=utf-8''...) inkl. Fortsetzungen bei langen Namen
            part.add_header("Content-Disposition", "attachment", filename=("utf-8", "", name))
            part.set_param("name", ("utf-8", "", name))
            outer.attach(part)
        msg = outer
    else:
        msg = main
    if q_encoding:
        from email.charset import Charset, QP
        cs = Charset(charset)
        cs.header_encoding = QP
        msg["Subject"] = Header(subject, cs).encode()
    else:
        msg["Subject"] = Header(subject, charset).encode()
    msg["From"] = fmt_addr(sender[0], sender[1], charset)
    msg["To"] = fmt_addr(to[0], to[1], charset)
    msg["Date"] = format_datetime(date)
    msg["Message-ID"] = msgid
    if in_reply_to:
        msg["In-Reply-To"] = in_reply_to
    if references:
        msg["References"] = " ".join(references)
    if newsletter:
        lname, ldomain = newsletter
        msg["List-Id"] = f"{Header(lname, charset).encode()} <{ldomain}>"
        msg["List-Unsubscribe"] = f"<mailto:unsubscribe@{ldomain}>, <https://{ldomain}/abmelden?id={idx}>"
        msg["List-Unsubscribe-Post"] = "List-Unsubscribe=One-Click"
        msg["Precedence"] = "bulk"
    return msg, subject


def to_bytes(msg):
    # policy.compat32 + linesep CRLF: so wie es ein IMAP-Server erwartet
    return msg.as_bytes(policy=policy.compat32.clone(linesep="\r\n"))


# ---- absichtlich kaputte Mails --------------------------------------------

def broken_message(rng, kind, idx, date, msgid):
    sender, to = rng.choice(PEOPLE), ME
    subject = rng.choice(SUBJECTS)
    if kind == "deep_nesting":
        inner = MIMEText(body_text(rng), "plain", "utf-8")
        for depth in range(40):
            mp = MIMEMultipart("mixed")
            mp.attach(inner)
            inner = mp
        msg = inner
    else:
        msg = MIMEText(body_text(rng), "plain", "utf-8")
    msg["Subject"] = Header(subject, "utf-8").encode()
    msg["From"] = fmt_addr(sender[0], sender[1], "utf-8")
    msg["To"] = fmt_addr(to[0], to[1], "utf-8")
    msg["Date"] = format_datetime(date)
    msg["Message-ID"] = msgid
    raw = to_bytes(msg)
    expected_mid = msgid
    if kind == "missing_date":
        raw = b"\r\n".join(l for l in raw.split(b"\r\n") if not l.startswith(b"Date:"))
    elif kind == "invalid_message_id":
        expected_mid = f"kaputt-ohne-klammern-{idx}"
        raw = raw.replace(f"Message-ID: {msgid}".encode(), f"Message-ID: {expected_mid}".encode())
    elif kind == "raw_8bit_header":
        # Betreff roh in latin-1 bzw. utf-8, ohne RFC 2047
        enc = rng.choice(["latin-1", "utf-8"])
        lines = raw.split(b"\r\n")
        lines = [(b"Subject: " + f"Roh: {subject}".encode(enc, "replace")) if l.startswith(b"Subject:") else l
                 for l in lines]
        # Folgezeilen eines gefalteten Betreffs entfernen
        out, skip = [], False
        for l in lines:
            if l.startswith(b"Subject:"):
                out.append(l); skip = True; continue
            if skip and l[:1] in (b" ", b"\t"):
                continue
            skip = False
            out.append(l)
        raw = b"\r\n".join(out)
        subject = f"Roh: {subject}"
    elif kind == "overlong_line":
        head, _, body = raw.partition(b"\r\n\r\n")
        body = body + ("Überlange Zeile " * 2500).encode("utf-8") + b"\r\n"
        raw = head.replace(b"Content-Transfer-Encoding: base64", b"Content-Transfer-Encoding: 8bit") + b"\r\n\r\n" + body
    elif kind == "tz_without_sign":
        d = date.strftime("%a, %d %b %Y %H:%M:%S") + " 0100"
        lines = raw.split(b"\r\n")
        raw = b"\r\n".join((b"Date: " + d.encode()) if l.startswith(b"Date:") else l for l in lines)
    return raw, subject, expected_mid


BROKEN_KINDS = ["missing_date", "invalid_message_id", "raw_8bit_header",
                "deep_nesting", "overlong_line", "tz_without_sign"]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--count", type=int, default=1000)
    ap.add_argument("--threads", type=int, default=60)
    ap.add_argument("--seed", type=int, default=42)
    args = ap.parse_args()
    rng = random.Random(args.seed)
    os.makedirs(args.out, exist_ok=True)

    n = args.count
    n_broken = n * 5 // 100
    n_news = 45
    manifest = []
    base = datetime(2024, 1, 3, 8, 0, tzinfo=timezone(timedelta(hours=1)))

    def folder_for(date):
        if date.year == 2024:
            return "Archiv/2024"
        if date.year == 2025:
            return "Archiv/2025"
        return "INBOX"

    def rand_date():
        return base + timedelta(minutes=rng.randint(0, 60 * 24 * 1000))

    idx = 0
    plans = []
    # 1) Threads: 60 Stuecke mit 2..8 Nachrichten
    for t in range(args.threads):
        size = rng.randint(2, 8)
        start = rand_date()
        subj = rng.choice(SUBJECTS) + f" (Vorgang {t + 1})"
        a, b = rng.sample(PEOPLE, 1)[0], ME
        chain = []
        for k in range(size):
            date = start + timedelta(hours=rng.randint(1, 48) * (k + 1))
            plans.append(dict(kind="thread", thread=f"T{t + 1:03d}", pos=k, subject=subj,
                              sender=a if k % 2 == 0 else b, to=b if k % 2 == 0 else a, date=date))
    # 2) Newsletter
    for k in range(n_news):
        nl = NEWSLETTERS[k % len(NEWSLETTERS)]
        plans.append(dict(kind="newsletter", nl=nl, n=k + 1, date=rand_date()))
    # 3) kaputte
    for k in range(n_broken):
        plans.append(dict(kind="broken", broken=BROKEN_KINDS[k % len(BROKEN_KINDS)], date=rand_date()))
    # 4) Rest: Einzelmails
    while len(plans) < n:
        plans.append(dict(kind="single", date=rand_date()))
    plans = plans[:n]

    thread_ids = {}  # thread -> Liste Message-IDs
    # Threads in Positionsreihenfolge erzeugen, damit References stimmen
    plans.sort(key=lambda p: (p.get("thread", "~"), p.get("pos", 0)))
    drafts_left = 20
    for p in plans:
        idx += 1
        msgid = f"<s0.{idx:04d}.{rng.getrandbits(32):08x}@{DOMAIN}>"
        charset = charset_for(rng)
        entry = dict(file=f"{idx:04d}.eml", message_id=msgid, charset=charset,
                     thread=None, broken=None, newsletter=False, attachments=[], date=p["date"].isoformat())
        if p["kind"] == "broken":
            raw, subject, mid = broken_message(rng, p["broken"], idx, p["date"], msgid)
            entry.update(broken=p["broken"], charset="utf-8", subject=subject, message_id=mid)
        else:
            attachments = []
            if rng.random() < 0.18:
                attachments = rng.sample(ATTACH_NAMES, rng.randint(1, 2))
            html = rng.random() < 0.35
            kw = {}
            if p["kind"] == "thread":
                chain = thread_ids.setdefault(p["thread"], [])
                subj = p["subject"] if p["pos"] == 0 else rng.choice(["AW: ", "Re: ", "WG: AW: "]) + p["subject"]
                if chain:
                    refs = list(chain)
                    # Variation: manche Clients kuerzen References auf die letzten 2
                    if rng.random() < 0.2 and len(refs) > 2:
                        refs = refs[-2:]
                    kw["references"] = refs
                    # Variation: manche Antworten ohne In-Reply-To
                    if rng.random() > 0.15:
                        kw["in_reply_to"] = chain[-1]
                chain.append(msgid)
                entry["thread"] = p["thread"]
                sender, to = p["sender"], p["to"]
            elif p["kind"] == "newsletter":
                lname, ldom, tmpl = p["nl"]
                subj = tmpl.format(n=p["n"])
                sender, to = (lname, f"news@{ldom}"), ME
                kw["newsletter"] = (lname, ldom)
                html = True
                entry["newsletter"] = True
            else:
                subj = rng.choice(SUBJECTS)
                sender, to = rng.choice(PEOPLE), ME
            msg, subject = build_message(rng, idx=idx, charset=charset, subject=subj, sender=sender,
                                         to=to, date=p["date"], msgid=msgid, html=html,
                                         attachments=attachments, q_encoding=rng.random() < 0.3, **kw)
            raw = to_bytes(msg)
            entry.update(subject=subject, attachments=attachments, html=html)
        folder = folder_for(p["date"])
        if folder == "INBOX" and p["kind"] == "single" and drafts_left > 0:
            folder, drafts_left = "Drafts", drafts_left - 1
        entry["folder"] = folder
        entry["flags"] = []
        if folder == "Drafts":
            entry["flags"].append("\\Draft")
        elif folder != "INBOX" or rng.random() < 0.4:
            entry["flags"].append("\\Seen")
        if rng.random() < 0.05:
            entry["flags"].append("\\Flagged")
        with open(os.path.join(args.out, entry["file"]), "wb") as f:
            f.write(raw)
        manifest.append(entry)

    with open(os.path.join(args.out, "manifest.json"), "w", encoding="utf-8") as f:
        json.dump(manifest, f, ensure_ascii=False, indent=1)
    by_folder = {}
    for e in manifest:
        by_folder[e["folder"]] = by_folder.get(e["folder"], 0) + 1
    print(json.dumps(dict(total=len(manifest), threads=len(thread_ids),
                          thread_messages=sum(len(v) for v in thread_ids.values()),
                          broken=n_broken, newsletters=n_news, by_folder=by_folder,
                          with_attachments=sum(1 for e in manifest if e["attachments"])), ensure_ascii=False))


if __name__ == "__main__":
    main()
