#!/bin/sh
# Test-Autorisierungsserver für den Integrationstest von mcp --http mit
# [http] auth = "oauth" (S3.6): Authelia im Docker, nur auf 127.0.0.1,
# eingerichtet aus dem Beispiel deploy/docker/auth/configuration.example.yml
# (prüft so auch das Beispiel). Test-CA, Schlüssel, Geheimnisse und das
# Passwort des Testkontos entstehen zufällig in $WORK, nie im Repository.
#
#   make testauth-up            # WORK=/tmp/postkauz-testauth (Standard)
#   POSTKAUZ_IT=authelia POSTKAUZ_TESTAUTH_DIR=/tmp/postkauz-testauth make integration
#   make testauth-down
set -eu
here=$(cd "$(dirname "$0")/../.." && pwd)
WORK=${WORK:-/tmp/postkauz-testauth}
IMAGE=${AUTHELIA_IMAGE:-authelia/authelia:4.39.28}
NAME=postkauz-testauth
PORT=${TESTAUTH_PORT:-19091}
HOST=auth.postkauz.test
RESOURCE=https://node.postkauz.test/mcp
CALLBACK=33418

docker rm -f "$NAME" >/dev/null 2>&1 || true
rm -rf "$WORK"
mkdir -p "$WORK/tls" "$WORK/data"
cd "$WORK"
rnd() { openssl rand -hex 32; }

# Test-CA und Zertifikat für $HOST (nur dieser Test).
openssl req -x509 -newkey rsa:2048 -nodes -keyout tls/ca.key -out tls/ca.crt -days 2 -subj '/CN=postkauz testauth CA' \
  -addext 'basicConstraints=critical,CA:TRUE' -addext 'keyUsage=critical,keyCertSign,cRLSign' 2>/dev/null
openssl req -newkey rsa:2048 -nodes -keyout tls/auth.key -out tls/auth.csr -subj "/CN=$HOST" 2>/dev/null
printf 'subjectAltName=DNS:%s\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n' "$HOST" > tls/auth.ext
openssl x509 -req -in tls/auth.csr -CA tls/ca.crt -CAkey tls/ca.key -CAcreateserial -out tls/auth.crt -days 2 -extfile tls/auth.ext 2>/dev/null
openssl genrsa -out jwks.pem 2048 2>/dev/null
chmod 644 tls/auth.key jwks.pem

# Konfiguration aus dem Beispiel: Platzhalter ersetzen, TLS dazu.
sed -e "s#<auth-host>#$HOST#g" -e "s#<auth-url>#https://$HOST:$PORT#g" \
    -e "s#<resource>#$RESOURCE#g" \
    -e "s#<callback-port>#$CALLBACK#g" \
    -e "s#<random-jwt>#$(rnd)#" -e "s#<random-session>#$(rnd)#" \
    -e "s#<random-storage>#$(rnd)#" -e "s#<random-hmac>#$(rnd)#" \
    -e "s#address: 'tcp://0.0.0.0:9091/'#address: 'tcp://0.0.0.0:9091/'\n  tls:\n    key: '/config/tls/auth.key'\n    certificate: '/config/tls/auth.crt'#" \
    "$here/deploy/docker/auth/configuration.example.yml" > configuration.yml
if grep -v '^#' configuration.yml | grep -q '<'; then
  echo "setup: placeholder left in configuration.yml" >&2; exit 1
fi

# Testkonto mit Zufallspasswort.
PASSWORD=$(rnd)
hash=$(docker run --rm "$IMAGE" authelia crypto hash generate argon2 --password "$PASSWORD" | sed -n 's/^Digest: //p')
sed -e "s#<name>:#tester:#" -e "s#'<Anzeigename>'#'Test User'#" -e "s#<argon2-hash>#$hash#" -e "s#<name>@#tester@#" \
    "$here/deploy/docker/auth/users.example.yml" > users.yml
chmod 644 users.yml configuration.yml
chmod 777 data

docker run -d --name "$NAME" -p "127.0.0.1:$PORT:9091" -e X_AUTHELIA_CONFIG_FILTERS=template \
  -v "$WORK/configuration.yml:/config/configuration.yml:ro" -v "$WORK/users.yml:/config/users.yml:ro" \
  -v "$WORK/jwks.pem:/config/jwks.pem:ro" -v "$WORK/tls:/config/tls:ro" -v "$WORK/data:/data" "$IMAGE" >/dev/null

cat > env <<ENV
TESTAUTH_ISSUER=https://$HOST:$PORT
TESTAUTH_ADDR=127.0.0.1:$PORT
TESTAUTH_RESOURCE=$RESOURCE
TESTAUTH_CALLBACK=http://localhost:$CALLBACK/callback
TESTAUTH_USER=tester
TESTAUTH_PASSWORD=$PASSWORD
ENV
chmod 600 env
i=0
until curl --noproxy '*' -fsS --cacert tls/ca.crt --resolve "$HOST:$PORT:127.0.0.1" "https://$HOST:$PORT/api/health" >/dev/null 2>&1; do
  i=$((i+1))
  if [ $i -gt 60 ]; then docker logs "$NAME" >&2; echo "setup: authelia did not come up" >&2; exit 1; fi
  sleep 1
done
echo "testauth: ok ($WORK, $(docker inspect -f '{{.Config.Image}}' "$NAME"))"
