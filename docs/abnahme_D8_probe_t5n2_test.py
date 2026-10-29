"""Regressionstests fuer abnahme_D8_probe.py (A03 T5, zweite Nachbesserung).

Nur Python-Standardbibliothek, ohne Netz (keine Anfrage verlaesst den
Prozess). Aufruf aus dem Repository:

    python3 -m unittest docs/abnahme_D8_probe_t5n2_test.py

oder aus docs/: python3 -m unittest abnahme_D8_probe_t5n2_test

Hostnamen und Subjekte sind synthetisch (example.org).
"""

import base64
import contextlib
import importlib.util
import io
import json
import os
import re
import subprocess
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
PROBE = os.path.join(HERE, "abnahme_D8_probe.py")


def load_probe():
    # Laden wie ein Modul: main() laeuft nur unter __name__ == "__main__",
    # das Laden selbst hat keine Seiteneffekte.
    spec = importlib.util.spec_from_file_location("t5n2_probe", PROBE)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def b64(d):
    return base64.urlsafe_b64encode(json.dumps(d, separators=(",", ":")).encode()).rstrip(b"=").decode()


def t5n2_token(extra):
    """Access-Token in der Form von Authelia (RS256, at+jwt) mit einer
    Nutzlast, deren Laenge `extra` steuert."""
    hdr = b64({"alg": "RS256", "kid": "a" * 64, "typ": "at+jwt"})
    claims = {"aud": ["https://pc.tailnet.example.org/mcp"], "client_id": "claude-code", "exp": 1761740000,
              "iat": 1761736400, "iss": "https://auth.tailnet.example.org:8443", "jti": "0" * 36, "nbf": 1761736400,
              "scp": ["openid", "offline_access"], "sub": "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0",
              "groups": ["x" * extra] if extra else []}
    pay = b64(claims)
    return hdr, pay, hdr + "." + pay + "." + "S" * 342


class TestT5N2MaskBeforeCut(unittest.TestCase):
    """Punkt 2: Resp.show() kuerzt erst nach dem Maskieren."""

    def setUp(self):
        self.p = load_probe()

    def test_t5n2_access_token_never_partly_visible(self):
        rt = "authelia_rt_" + "R" * 80
        # 0 bis 300: der Schnitt bei 600 Zeichen faellt vor, in und hinter
        # die Nutzlast (vor dem Fix ab etwa 582 Zeichen Kopf+Nutzlast offen).
        for extra in (0, 100, 200, 300, 400):
            with self.subTest(extra=extra):
                hdr, pay, at = t5n2_token(extra)
                body = json.dumps({"access_token": at, "expires_in": 3599, "refresh_token": rt, "token_type": "bearer"})
                self.p.SECRETS[:] = [at, rt]
                out = self.p.short(self.p.Resp(200, [("Content-Type", "application/json")], body).show())
                self.assertNotIn("eyJ", out)
                self.assertNotIn(hdr[:20], out)
                self.assertNotIn(pay[:20], out)
                self.assertNotIn(pay[-20:], out)
                self.assertNotIn("RRRRRRRR", out)
                self.assertIn("<token>", out)

    def test_t5n2_refresh_token_cut_at_600(self):
        # Refresh-Token ueber der Schnittstelle 600: vorher blieb sein Anfang
        # stehen (der Vergleich mit SECRETS braucht das ganze Token).
        rt = "authelia_rt_" + "R" * 80
        for pad in (540, 560, 580, 595):
            with self.subTest(pad=pad):
                body = json.dumps({"error_description": "x" * pad, "refresh_token": rt})
                self.p.SECRETS[:] = [rt]
                out = self.p.Resp(400, [], body).show()
                self.assertNotIn("authelia_rt_", out)
                self.assertNotIn("RRRRRRRR", out)

    def test_t5n2_show_masks_without_short(self):
        # show() allein ist schon maskiert (es geht auch ungekuerzt in check()).
        _, _, at = t5n2_token(300)
        self.p.SECRETS[:] = []
        out = self.p.Resp(200, [], json.dumps({"access_token": at})).show()
        self.assertNotIn("eyJ", out)
        self.assertLessEqual(len(out.split("\n", 2)[-1]), 600)


FINAL_RE = re.compile(r"^probe: (\d+) OK, (\d+) FAIL, (\d+) SKIP$")


def t5n2_run(*args):
    """Startet die Probe als eigenen Prozess (wie die Abnahme-Skripte)."""
    return subprocess.run([sys.executable, "-I", PROBE] + list(args), stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, universal_newlines=True, timeout=60)


class TestT5N2AbortCountsAsFail(unittest.TestCase):
    """Punkt 3: Jeder Abbruch endet mit FAIL-Zeile, Schlusszeile und Exit 1."""

    def assert_failed_cleanly(self, out, code, nr):
        lines = [x for x in out.splitlines() if x.strip()]
        self.assertTrue(lines, out)
        self.assertRegex(lines[-1], FINAL_RE)
        self.assertEqual(FINAL_RE.match(lines[-1]).group(2), "1", out)
        self.assertTrue(any(x.startswith("FAIL " + nr + ".x ") for x in lines), out)
        self.assertEqual(code, 1)

    def test_t5n2_missing_url(self):
        # Vor dem Fix: argparse beendete mit Exit 2 ohne FAIL-Zeile.
        r = t5n2_run("key", "--nr", "Q7")
        self.assert_failed_cleanly(r.stdout, r.returncode, "Q7")
        self.assertIn("--url", r.stderr)

    def test_t5n2_no_subcommand(self):
        r = t5n2_run()
        self.assert_failed_cleanly(r.stdout, r.returncode, "D8")

    def test_t5n2_exception_in_command(self):
        # Fehlende Schluesseldatei: Ausnahme vor der ersten Anfrage (kein Netz).
        r = t5n2_run("key", "--url", "http://127.0.0.1:9/mcp", "--key-file", os.path.join(HERE, "t5n2-fehlt", "k.txt"),
                     "--nr", "Z1")
        self.assert_failed_cleanly(r.stdout, r.returncode, "Z1")

    def test_t5n2_help_is_not_a_failure(self):
        r = t5n2_run("--help")
        self.assertEqual(r.returncode, 0)
        self.assertNotIn("FAIL", r.stdout)

    def test_t5n2_base_exception_in_command(self):
        # SystemExit und andere BaseException aus einem Unterkommando zaehlen
        # als FAIL (vorher fing main() nur Exception).
        for exc in (SystemExit(3), GeneratorExit("t5n2")):
            with self.subTest(exc=type(exc).__name__):
                p = load_probe()

                def boom(a, exc=exc):
                    raise exc

                p.cmd_key = boom
                buf = io.StringIO()
                with contextlib.redirect_stdout(buf), self.assertRaises(SystemExit) as cm:
                    p.main(["key", "--url", "http://127.0.0.1:9/mcp", "--nr", "B9"])
                self.assert_failed_cleanly(buf.getvalue(), cm.exception.code, "B9")

    def test_t5n2_keyboard_interrupt_propagates(self):
        # Strg+C bricht ab (ohne Schlusszeile; die Skripte werten das als FAIL).
        p = load_probe()

        def boom(a):
            raise KeyboardInterrupt

        p.cmd_key = boom
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf), self.assertRaises(KeyboardInterrupt):
            p.main(["key", "--url", "http://127.0.0.1:9/mcp"])
        self.assertNotIn("probe: ", buf.getvalue())


if __name__ == "__main__":
    unittest.main()
