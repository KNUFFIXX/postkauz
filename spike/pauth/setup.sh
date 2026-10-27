#!/bin/sh
# Spike P-AUTH: erzeugt in $WORK eine Test-CA, Zertifikate, Zufallsgeheimnisse,
# die Authelia-Konfiguration und ein Testkonto. Nur für den Container der Sitzung.
# Aufruf: WORK=/pfad sh setup.sh   (danach: . $WORK/env)
set -eu
here=$(cd "$(dirname "$0")" && pwd)
: "${WORK:?WORK must point to a scratch directory}"
mkdir -p "$WORK/tls"
cd "$WORK"
rnd() { openssl rand -hex 32; }

# Namen auflösen (nur in diesem Container)
grep -q 'pauth.test' /etc/hosts || echo '127.0.0.1 auth.pauth.test dex.pauth.test mcp.pauth.test other.pauth.test' >> /etc/hosts

# Test-CA und Serverzertifikate
openssl req -x509 -newkey rsa:2048 -nodes -keyout tls/ca.key -out tls/ca.crt -days 2 -subj '/CN=pauth spike CA' 2>/dev/null
for n in auth dex mcp; do
  openssl req -newkey rsa:2048 -nodes -keyout tls/$n.key -out tls/$n.csr -subj "/CN=$n.pauth.test" 2>/dev/null
  printf 'subjectAltName=DNS:%s.pauth.test\n' "$n" > tls/$n.ext
  openssl x509 -req -in tls/$n.csr -CA tls/ca.crt -CAkey tls/ca.key -CAcreateserial -out tls/$n.crt -days 2 -extfile tls/$n.ext 2>/dev/null
done
chmod 644 tls/*.key

# Signaturschlüssel für Authelia und Konfiguration
openssl genrsa -out jwk.pem 2048 2>/dev/null
pem=$(sed 's/^/          /' jwk.pem)
python3 - "$here/authelia/configuration.yml.tmpl" "$WORK/configuration.yml" "$pem" "$(rnd)" "$(rnd)" "$(rnd)" "$(rnd)" <<'PY'
import sys
src, dst, pem, a, b, c, d = sys.argv[1:]
t = open(src).read()
t = t.replace('__JWK_PEM__', pem).replace('__JWT_SECRET__', a).replace('__SESSION_SECRET__', b)
t = t.replace('__STORAGE_KEY__', c).replace('__HMAC_SECRET__', d)
open(dst, 'w').write(t)
PY

# Testkonto mit Zufallspasswort
PAUTH_PASSWORD=$(rnd)
hash=$(docker run --rm authelia/authelia:latest authelia crypto hash generate argon2 --password "$PAUTH_PASSWORD" | sed -n 's/^Digest: //p')
cat > users.yml <<YML
users:
  tester:
    displayname: 'Test User'
    password: '$hash'
    email: 'tester@example.com'
    groups: []
YML

# Dex: gleiche Testperson als statisches Passwort (bcrypt)
dexhash=$(cd "$here" && go run ./cmd/bcrypt "$PAUTH_PASSWORD")
sed -e "s#__BCRYPT__#$dexhash#" -e "s#__WORK__#$WORK#g" "$here/dex/config.yaml.tmpl" > dex.yaml

cat > env <<ENV
export PAUTH_PASSWORD=$PAUTH_PASSWORD
export SSL_CERT_FILE=$WORK/tls/ca.crt
export NO_PROXY="$NO_PROXY,.pauth.test" no_proxy="$no_proxy,.pauth.test"
ENV
echo "setup: ok ($WORK)"
