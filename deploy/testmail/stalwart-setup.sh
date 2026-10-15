#!/bin/sh
# Stalwart v0.11.8 für die lokalen Integrationstests einrichten (nach
# `make testmail-up`; aufgerufen von `make testmail-setup`). Wiederholbar.
#   1. IPv6-Listener auf IPv4 umstellen (Container ohne IPv6 startet sonst
#      keine Listener) und Stalwart neu starten.
#   2. Domain example.org und Testkonto test / pass per Verwaltungs-API anlegen.
# NUR FÜR LOKALE TESTS. Zugangsdaten sind öffentlich bekannte Standardwerte.
set -eu
C=postkauz-testmail-stalwart-1
API=http://127.0.0.1:18080

i=0
until PW=$(docker logs "$C" 2>&1 | sed -n "s/.*with password '\([^']*\)'.*/\1/p" | head -1) && [ -n "$PW" ]; do
  i=$((i + 1)); [ "$i" -gt 30 ] && { echo "stalwart-setup: admin password not found in the logs"; exit 1; }
  sleep 1
done

if docker exec "$C" grep -q '\[::\]:' /opt/stalwart-mail/etc/config.toml; then
  docker exec "$C" sh -c "sed -i 's/\[::\]:/0.0.0.0:/g' /opt/stalwart-mail/etc/config.toml"
  docker restart "$C" >/dev/null
fi

i=0
until curl -s -o /dev/null -u "admin:$PW" "$API/api/principal?limit=1"; do
  i=$((i + 1)); [ "$i" -gt 30 ] && { echo "stalwart-setup: API not reachable"; exit 1; }
  sleep 1
done

curl -s -u "admin:$PW" -X POST "$API/api/principal" -d '{"type":"domain","name":"example.org"}' >/dev/null
curl -s -u "admin:$PW" -X POST "$API/api/principal" \
  -d '{"type":"individual","name":"test","secrets":["pass"],"emails":["test@example.org"],"roles":["user"]}' >/dev/null
echo "stalwart-setup: ok (IMAPS 127.0.0.1:12993, user test / pass; plain port 12143 refuses LOGIN)"
