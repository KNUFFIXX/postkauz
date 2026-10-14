#!/bin/sh
# Stalwart v0.11.8 fuer den Spike einrichten (nach `docker compose up -d stalwart`).
# 1) IPv6-Listener auf IPv4 umstellen (Container ohne IPv6 startet sonst keine Listener)
# 2) Neustart
# 3) Domain + Testkonto spike/pass per Admin-API anlegen
set -e
sleep 5
PW=$(docker logs mw-stalwart 2>&1 | sed -n "s/.*with password '\([^']*\)'.*/\1/p" | head -1)
[ -n "$PW" ] || { echo "Admin-Passwort nicht in den Logs gefunden"; exit 1; }
docker exec mw-stalwart sh -c "sed -i 's/\[::\]:/0.0.0.0:/g' /opt/stalwart-mail/etc/config.toml"
docker restart mw-stalwart >/dev/null
sleep 5
curl -s -u "admin:$PW" -X POST http://127.0.0.1:38080/api/principal \
  -d '{"type":"domain","name":"mw.example.test"}'; echo
curl -s -u "admin:$PW" -X POST http://127.0.0.1:38080/api/principal \
  -d '{"type":"individual","name":"spike","secrets":["pass"],"emails":["spike@mw.example.test"],"roles":["user"]}'; echo
echo "Stalwart bereit: IMAPS 127.0.0.1:32993, Nutzer spike / pass (Klartext-Port 32143 erlaubt kein LOGIN)"
