"""HTTP-MCP-Probe fuer Abnahme D8 (docs/abnahme_D8.md).

Spricht mit `postkauz mcp --http` ueber Streamable HTTP (zustandslos,
JSON-Antworten), nur Python-Standardbibliothek (Python 3.8 oder neuer).
Gestartet von docs/abnahme_D8.ps1 (Windows) bzw. docs/abnahme_D8_cloud.sh
(Cloud-Sitzung); laeuft ebenso unter Linux und macOS.

Jede Pruefung ist eine Zeile `OK <Nr> <Beschreibung>`, `FAIL <Nr> ...` oder
`SKIP <Nr> <Grund>`, darunter eingerueckt die Ist-Ausgabe (gekuerzt).
Letzte Zeile `probe: <n> OK, <m> FAIL, <k> SKIP`. Exit 0 nur ohne FAIL.
Geprueft wird an stabilen Merkmalen (Status, Kopfzeilen, Toolnamen, Felder),
nicht an ganzen Saetzen. Schluessel und Token erscheinen nie in der Ausgabe
(maskiert, auch in Fehlermeldungen).

Unterkommandos:
  key    Modus key: Bearer = PostKauz-Schluessel (aus --key-file).
  oauth  Modus oauth: Authorization Code mit PKCE S256 beim
         Autorisierungsserver aus den Metadaten; der Mensch meldet sich im
         Browser an. --wait-expiry wartet den Ablauf ab und erneuert.
         Ohne Terminal (Cloud-Sitzung): --login-url-file (Anmelde-URL nur
         dorthin und auf /dev/tty) und --paste-file (Rueckleitungs-URL aus
         einer Datei statt von der Standardeingabe).
  revoked  nach `key revoke`: gleicher Zugang -> 403 denied (key_revoked).

Proxy: urllib nimmt HTTPS_PROXY/HTTP_PROXY aus der Umgebung (in der
Cloud-Sitzung der lokale Proxy von tailscaled, abnahme_D8_cloud.sh).
"""

import argparse
import base64
import hashlib
import http.server
import json
import os
import re
import secrets
import socket
import ssl
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

KEY_RE = re.compile(r"pk_key_[A-Za-z0-9_-]+")
JWT_RE = re.compile(r"eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+")
SECRETS = []
RESULTS = {"ok": 0, "fail": 0, "skip": 0}
PROTOCOL = "2025-11-25"


def mask(text):
    text = KEY_RE.sub("pk_key_********", str(text))
    text = JWT_RE.sub("<token>", text)
    for s in SECRETS:
        if s and len(s) >= 8:
            text = text.replace(s, "<geheim>")
    return text


def short(text, limit=900):
    text = mask(text)
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
    return ok


def skip(nr, why):
    RESULTS["skip"] += 1
    print("SKIP " + nr + " " + why)
    sys.stdout.flush()


class Resp:
    def __init__(self, status, headers, body):
        self.status = status
        self.headers = headers
        self.body = body

    def header(self, name):
        for k, v in self.headers:
            if k.lower() == name.lower():
                return v
        return ""

    def json(self):
        try:
            return json.loads(self.body or "null")
        except ValueError:
            return None

    def show(self):
        keep = [k + ": " + v for k, v in self.headers
                if k.lower() in ("www-authenticate", "content-type", "mcp-session-id", "server", "retry-after", "allow")]
        return "status " + str(self.status) + "\n" + "\n".join(keep) + "\n" + (self.body or "")[:600]


def request(method, url, body=None, headers=None, host=None, timeout=30):
    hdr = dict(headers or {})
    if host:
        hdr["Host"] = host
    data = None
    if body is not None:
        data = body if isinstance(body, bytes) else body.encode("utf-8")
    req = urllib.request.Request(url, data=data, method=method, headers=hdr)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return Resp(r.status, list(r.headers.items()), r.read().decode("utf-8", "replace"))
    except urllib.error.HTTPError as e:
        return Resp(e.code, list(e.headers.items()), e.read().decode("utf-8", "replace"))
    except (urllib.error.URLError, socket.timeout, ConnectionError, ssl.SSLError) as e:
        return Resp(0, [], "error: " + type(e).__name__ + " " + str(getattr(e, "reason", e)))


class Session:
    """Ein MCP-Client ueber zustandsloses Streamable HTTP."""

    def __init__(self, url, bearer, host=None):
        self.url = url
        self.bearer = bearer
        self.host = host
        self.next_id = 1
        self.headers_seen = []

    def call(self, method, params=None):
        msg = {"jsonrpc": "2.0", "id": self.next_id, "method": method}
        self.next_id += 1
        if params is not None:
            msg["params"] = params
        hdr = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream",
               "MCP-Protocol-Version": PROTOCOL}
        if self.bearer:
            hdr["Authorization"] = "Bearer " + self.bearer
        r = request("POST", self.url, json.dumps(msg), hdr, self.host)
        self.headers_seen.append(r)
        return r

    def rpc(self, method, params=None):
        r = self.call(method, params)
        j = r.json() if r.status == 200 else None
        if not isinstance(j, dict):
            return r, None
        return r, j

    def initialize(self):
        return self.rpc("initialize", {"protocolVersion": PROTOCOL, "capabilities": {},
                                       "clientInfo": {"name": "abnahme-d8", "version": "1"}})

    def tool(self, name, args):
        r, j = self.rpc("tools/call", {"name": name, "arguments": args})
        if not j or "result" not in j:
            return r, None
        res = j["result"]
        text = "".join(c.get("text", "") for c in res.get("content", []) if c.get("type") == "text")
        try:
            out = json.loads(text)
        except ValueError:
            out = None
        return r, {"is_error": bool(res.get("isError")), "text": text, "json": out}


def origin(url):
    u = urllib.parse.urlsplit(url)
    return u.scheme + "://" + u.netloc


def common_checks(nr, url, host, wrong_bearer):
    """401 ohne Bearer, 401 mit falschem Bearer, 405 fuer GET, PRM."""
    base = origin(url)
    r = request("GET", base + "/healthz", host=host)
    check(nr + ".1", "GET /healthz -> 204 ohne Inhalt", r.status == 204 and not r.body, r.show())
    path = urllib.parse.urlsplit(url).path.rstrip("/")
    r = request("GET", base + "/.well-known/oauth-protected-resource" + path, host=host)
    meta = r.json() if r.status == 200 else None
    check(nr + ".2", "Metadaten (RFC 9728) mit resource", isinstance(meta, dict) and bool(meta.get("resource")), r.show())
    hdr = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
    init = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": PROTOCOL,
                       "capabilities": {}, "clientInfo": {"name": "abnahme-d8", "version": "1"}}})
    r = request("POST", url, init, hdr, host)
    wa = r.header("WWW-Authenticate")
    check(nr + ".3", "POST /mcp ohne Bearer -> 401 mit WWW-Authenticate resource_metadata",
          r.status == 401 and "resource_metadata=" in wa, r.show())
    h2 = dict(hdr)
    h2["Authorization"] = "Bearer " + wrong_bearer
    r = request("POST", url, init, h2, host)
    check(nr + ".4", "POST /mcp mit falschem Zugang -> 401", r.status == 401, r.show())
    r = request("GET", url, None, {"Authorization": "Bearer " + wrong_bearer}, host)
    check(nr + ".5", "GET /mcp -> 405", r.status == 405, r.show())
    return meta


def session_checks(nr, s, expect_tools, principal, oauth_sub=None):
    r, j = s.initialize()
    ok = j is not None and "result" in j and not r.header("Mcp-Session-Id") and not r.header("Server")
    check(nr + ".1", "initialize -> 200, keine Session-ID, keine Server-Kennung", ok, r.show())
    r, j = s.rpc("tools/list")
    names = sorted(t.get("name", "") for t in (j or {}).get("result", {}).get("tools", []))
    want = expect_tools
    check(nr + ".2", "tools/list -> " + str(want) + " Tools (Rolle des Schluessels)", len(names) == want and "mail_search" in names,
          "tools: " + ", ".join(names))
    bad = [n for n in names if n in ("mail_send", "mail_delete", "mail_approve")]
    check(nr + ".3", "kein Tool zum Senden, Loeschen oder Freigeben", not bad, ", ".join(bad))
    r, out = s.tool("mail_search", {"query": "a", "limit": 3})
    ok = out is not None and not out["is_error"] and isinstance(out["json"], dict) and out["json"].get("untrusted") is True
    check(nr + ".4", "mail_search -> Ergebnis in der Untrusted-Huelle", ok, (out or {}).get("text", r.show())[:400])
    r, out = s.tool("mail_audit_log", {"since": "10m", "operation": "messages.search"})
    events = ((out or {}).get("json") or {}).get("events", [])
    via = [e.get("params", {}).get("via", {}) for e in events]
    ok = any(v.get("transport") == "http" and v.get("principal") == principal for v in via)
    if oauth_sub is not None:
        ok = ok and any(v.get("sub") == oauth_sub and v.get("iss") for v in via)
    check(nr + ".5", "mail_audit_log nennt transport http und Principal " + principal + (" mit iss/sub" if oauth_sub else ""),
          ok, json.dumps(via[-3:]))
    text = json.dumps([x.body for x in s.headers_seen])
    leaked = s.bearer and (s.bearer in text or s.bearer.split(".")[-1] in text)
    check(nr + ".6", "Zugang (Schluessel/Token) in keiner Antwort", not leaked)


def read_secret(path):
    with open(path, "r", encoding="utf-8") as f:
        v = f.read().strip()
    SECRETS.append(v)
    return v


# ---------------------------------------------------------------- key


def cmd_key(a):
    key = read_secret(a.key_file)
    common_checks(a.nr, a.url, a.host, "pk_key_" + "x" * 43)
    session_checks(a.nr + "s", Session(a.url, key, a.host), a.expect_tools, a.principal)


# ---------------------------------------------------------------- oauth


def b64url(b):
    return base64.urlsafe_b64encode(b).rstrip(b"=").decode("ascii")


def jwt_claims(tok):
    try:
        part = tok.split(".")[1]
        part += "=" * (-len(part) % 4)
        return json.loads(base64.urlsafe_b64decode(part))
    except (IndexError, ValueError):
        return {}


def as_metadata(issuer):
    u = urllib.parse.urlsplit(issuer)
    for m in (u.scheme + "://" + u.netloc + "/.well-known/oauth-authorization-server" + u.path.rstrip("/"),
              issuer.rstrip("/") + "/.well-known/openid-configuration"):
        r = request("GET", m)
        j = r.json() if r.status == 200 else None
        if isinstance(j, dict) and j.get("issuer") == issuer:
            return j
    return None


def wait_for_code(port, timeout):
    got = {}

    class H(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            q = urllib.parse.parse_qs(urllib.parse.urlsplit(self.path).query)
            got.update({k: v[0] for k, v in q.items()})
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.end_headers()
            self.wfile.write("Abnahme D8: Anmeldung empfangen, dieses Fenster kann zu.".encode("utf-8"))

        def log_message(self, *args):
            pass

    srv = http.server.HTTPServer(("127.0.0.1", port), H)
    srv.timeout = 1
    end = time.time() + timeout
    while not got and time.time() < end:
        srv.handle_request()
    srv.server_close()
    return got


def wait_for_file(path, timeout):
    """Wartet auf eine Datei mit einer vollstaendigen Zeile (Weiter-Signal
    ohne Standardeingabe, etwa aus einer Cloud-Sitzung). Liest sie und
    loescht sie danach (sie enthaelt den Anmeldecode)."""
    end = time.time() + timeout
    while time.time() < end:
        try:
            with open(path, "r", encoding="utf-8") as f:
                text = f.read()
        except FileNotFoundError:
            text = ""
        if text.endswith("\n"):
            try:
                os.remove(path)
            except OSError:
                pass
            return text.strip()
        time.sleep(2)
    return ""


def token_request(meta, form):
    r = request("POST", meta["token_endpoint"], urllib.parse.urlencode(form),
                {"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"})
    j = r.json() if r.status == 200 else None
    for k in ("access_token", "refresh_token", "id_token"):
        if isinstance(j, dict) and j.get(k):
            SECRETS.append(j[k])
    return r, j


def login(a, meta, resource):
    verifier = b64url(secrets.token_bytes(32))
    challenge = b64url(hashlib.sha256(verifier.encode("ascii")).digest())
    state = b64url(secrets.token_bytes(16))
    redirect = "http://localhost:" + str(a.callback_port) + "/callback"
    q = {"response_type": "code", "client_id": a.client_id, "redirect_uri": redirect, "scope": a.scope,
         "state": state, "code_challenge": challenge, "code_challenge_method": "S256", "resource": resource}
    url = meta["authorization_endpoint"] + "?" + urllib.parse.urlencode(q)
    print("")
    if a.login_url_file:
        # Ohne Filter nur in die Datei (Rechte 600) und, falls vorhanden, auf
        # das Terminal; die Standardausgabe geht maskiert ins Protokoll und
        # machte die URL unbrauchbar.
        fd = os.open(a.login_url_file, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0), 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write(url + "\n")
        on_tty = False
        try:
            with open("/dev/tty", "w", encoding="utf-8") as t:
                t.write("\n[Du] Bitte diese Adresse im Browser oeffnen, anmelden und zustimmen:\n      " + url + "\n")
            on_tty = True
        except OSError:
            pass
        print("[Du] Anmelde-Adresse steht in " + a.login_url_file + (" und auf dem Terminal" if on_tty else "")
              + ": im Browser oeffnen, anmelden und zustimmen.")
    else:
        print("[Du] Bitte diese Adresse im Browser oeffnen, anmelden und zustimmen:")
        print("      " + url)
    if a.paste_file:
        print("[Du] Danach die Adresse aus der Adresszeile (beginnt mit " + redirect + ") als eine Zeile mit Zeilenende in "
              + a.paste_file + " ablegen (erst " + a.paste_file + ".tmp schreiben, dann umbenennen).")
        print("      (die Probe wartet hoechstens " + str(a.login_timeout) + " s auf die Datei)")
        sys.stdout.flush()
        line = wait_for_file(a.paste_file, a.login_timeout)
        got = {k: v[0] for k, v in urllib.parse.parse_qs(urllib.parse.urlsplit(line).query).items()}
    elif a.paste:
        print("[Du] Danach die Adresse aus der Adresszeile (beginnt mit " + redirect + ") hier einfuegen und Enter:")
        sys.stdout.flush()
        line = sys.stdin.readline().strip()
        got = {k: v[0] for k, v in urllib.parse.parse_qs(urllib.parse.urlsplit(line).query).items()}
    else:
        print("      (die Probe wartet hoechstens " + str(a.login_timeout) + " s auf die Rueckleitung an " + redirect + ")")
        sys.stdout.flush()
        got = wait_for_code(a.callback_port, a.login_timeout)
    if got.get("state") != state or not got.get("code"):
        return None, "keine Anmeldung empfangen oder state falsch (" + ", ".join(sorted(got)) + ")"
    r, j = token_request(meta, {"grant_type": "authorization_code", "code": got["code"], "redirect_uri": redirect,
                                "client_id": a.client_id, "code_verifier": verifier, "resource": resource})
    if not j or not j.get("access_token"):
        return None, r.show()
    return j, ""


def cmd_oauth(a):
    meta_rs = common_checks(a.nr, a.url, a.host, "eyJhbGciOiJub25lIn0.eyJ4IjoxfQ.x")
    servers = (meta_rs or {}).get("authorization_servers") or []
    if not check(a.nr + ".6", "Metadaten nennen authorization_servers", len(servers) == 1, json.dumps(meta_rs)):
        return
    issuer = servers[0]
    meta = as_metadata(issuer)
    if not check(a.nr + ".7", "Metadaten des Autorisierungsservers (RFC 8414) mit PKCE S256", isinstance(meta, dict)
                 and "S256" in (meta.get("code_challenge_methods_supported") or []), json.dumps(meta)[:500] if meta else issuer):
        return
    if a.key_file:
        key = read_secret(a.key_file)
        s = Session(a.url, key, a.host)
        r, _ = s.initialize()
        check(a.nr + ".8", "PostKauz-Schluessel als Bearer im Modus oauth -> 401", r.status == 401, r.show())
    else:
        skip(a.nr + ".8", "kein --key-file")
    if a.other_token_file:
        s = Session(a.url, read_secret(a.other_token_file), a.host)
        r, _ = s.initialize()
        check(a.nr + ".9", "Token fuer eine andere Audience -> 401", r.status == 401, r.show())
    else:
        skip(a.nr + ".9", "kein --other-token-file (Token eines zweiten Clients fuer eine andere Resource)")
    tok, why = login(a, meta, meta_rs["resource"])
    if not check(a.nr + ".10", "Anmeldung (Authorization Code mit PKCE S256) -> Access- und Refresh-Token",
                 tok is not None and bool(tok.get("refresh_token")), why):
        return
    claims = jwt_claims(tok["access_token"])
    check(a.nr + ".11", "Access-Token: aud enthaelt die Resource, kurze Lebensdauer",
          meta_rs["resource"].rstrip("/") in [x.rstrip("/") for x in (claims.get("aud") if isinstance(claims.get("aud"), list) else [claims.get("aud")])]
          and 0 < claims.get("exp", 0) - claims.get("iat", 0) <= 3600,
          "aud " + json.dumps(claims.get("aud")) + ", Lebensdauer " + str(claims.get("exp", 0) - claims.get("iat", 0)) + " s")
    if tok.get("id_token"):
        r, _ = Session(a.url, tok["id_token"], a.host).initialize()
        check(a.nr + ".12", "ID-Token als Bearer -> 401", r.status == 401, r.show())
    else:
        skip(a.nr + ".12", "kein ID-Token")
    s = Session(a.url, tok["access_token"], a.host)
    r, j = s.initialize()
    if not check(a.nr + ".s0", "Subjekt des Tokens ist einem Schluessel zugeordnet (sonst 403)", r.status != 403,
                 "403: in [http.principals] eintragen: \"" + str(claims.get("sub")) + "\" = \"" + a.principal + "\", Dienst neu starten"):
        return
    session_checks(a.nr + "s", s, a.expect_tools, a.principal, claims.get("sub"))
    if a.token_file:
        fd = os.open(a.token_file, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump({"refresh_token": tok["refresh_token"], "token_endpoint": meta["token_endpoint"],
                       "client_id": a.client_id, "resource": meta_rs["resource"]}, f)
    if not a.wait_expiry:
        skip(a.nr + ".13", "Ablauf nicht abgewartet (--wait-expiry)")
        return
    wait = max(0, claims.get("exp", 0) - int(time.time())) + 40
    print("      warte " + str(wait) + " s auf den Ablauf des Tokens (Lebensdauer plus 30 s Uhrabweichung) ...")
    sys.stdout.flush()
    time.sleep(wait)
    r, _ = Session(a.url, tok["access_token"], a.host).initialize()
    check(a.nr + ".13", "abgelaufenes Token -> 401", r.status == 401, r.show())
    r, j = token_request(meta, {"grant_type": "refresh_token", "refresh_token": tok["refresh_token"], "client_id": a.client_id,
                                "resource": meta_rs["resource"]})
    ok = bool(j and j.get("access_token"))
    if ok:
        r2, _ = Session(a.url, j["access_token"], a.host).initialize()
        ok = r2.status == 200
        if a.token_file and j.get("refresh_token"):
            with open(a.token_file, "w", encoding="utf-8") as f:
                json.dump({"refresh_token": j["refresh_token"], "token_endpoint": meta["token_endpoint"],
                           "client_id": a.client_id, "resource": meta_rs["resource"]}, f)
    check(a.nr + ".14", "Refresh -> neues Token -> 200", ok, r.show())


def cmd_revoked(a):
    if a.key_file:
        s = Session(a.url, read_secret(a.key_file), a.host)
    else:
        with open(a.token_file, "r", encoding="utf-8") as f:
            t = json.load(f)
        SECRETS.append(t["refresh_token"])
        r, j = token_request({"token_endpoint": t["token_endpoint"]}, {"grant_type": "refresh_token", "refresh_token": t["refresh_token"],
                                                                        "client_id": t["client_id"], "resource": t["resource"]})
        if not check(a.nr + ".1", "Refresh beim Autorisierungsserver", bool(j and j.get("access_token")), r.show()):
            return
        s = Session(a.url, j["access_token"], a.host)
    r, _ = s.initialize()
    body = r.json() or {}
    check(a.nr + ".2", "Zugang mit widerrufenem Schluessel -> 403 denied (key_revoked)",
          r.status == 403 and body.get("status") == "denied" and body.get("code") == "key_revoked", r.show())


def main():
    p = argparse.ArgumentParser(description="HTTP-MCP-Probe fuer Abnahme D8")
    sub = p.add_subparsers(dest="cmd", required=True)
    for name in ("key", "oauth", "revoked"):
        sp = sub.add_parser(name)
        sp.add_argument("--url", required=True, help="Resource-URL, z. B. https://<knoten>/mcp")
        sp.add_argument("--host", default=None, help="Host-Kopf (Standard: aus --url)")
        sp.add_argument("--nr", default="D8", help="Praefix der Pruefnummern")
        sp.add_argument("--key-file", default="", help="Datei mit dem PostKauz-Schluessel")
        sp.add_argument("--token-file", default="", help="Datei fuer den Refresh-Token (wird mit Rechten 600 angelegt)")
        sp.add_argument("--expect-tools", type=int, default=12, help="Tools der Rolle (reader: 12 seit T2)")
        sp.add_argument("--principal", default="cloud-reader")
        sp.add_argument("--client-id", default="claude-code")
        sp.add_argument("--callback-port", type=int, default=33418)
        sp.add_argument("--scope", default="openid offline_access")
        sp.add_argument("--paste", action="store_true", help="Rueckleitungs-URL einfuegen statt lokal empfangen (ohne Browser am Rechner)")
        sp.add_argument("--paste-file", default="", help="Rueckleitungs-URL aus dieser Datei lesen (wartet bis --login-timeout; ohne Standardeingabe)")
        sp.add_argument("--login-url-file", default="", help="Anmelde-URL nur in diese Datei (Rechte 600) und auf /dev/tty, nicht auf die Standardausgabe")
        sp.add_argument("--login-timeout", type=int, default=300)
        sp.add_argument("--other-token-file", default="")
        sp.add_argument("--wait-expiry", action="store_true")
    a = p.parse_args()
    try:
        {"key": cmd_key, "oauth": cmd_oauth, "revoked": cmd_revoked}[a.cmd](a)
    except Exception as e:  # nie mit Geheimnis abbrechen
        check("D8.x", "Probe ohne Ausnahme", False, type(e).__name__ + ": " + str(e))
    print("probe: %d OK, %d FAIL, %d SKIP" % (RESULTS["ok"], RESULTS["fail"], RESULTS["skip"]))
    sys.exit(1 if RESULTS["fail"] else 0)


if __name__ == "__main__":
    main()
