# PostKauz als statisches Image ohne Shell (A03 S3.5): nur die Programmdatei
# aus dist/ (make release), die CA-Zertifikate und ein Nutzer ohne Rechte.
# Kein Go und kein Compiler im Image.
#
#   make release VERSION=<version>
#   docker build -f deploy/docker/Dockerfile -t postkauz:<version> .
#
# Das Image läuft als UID/GID 65532 (nonroot); Daten unter /data
# (POSTKAUZ_DATA_DIR), Konfiguration unter /config (POSTKAUZ_CONFIG).
# Fest auf Version und Digest (Index für alle Plattformen); Stand
# 29.10.2026: 3.22.7 (= 3.22). Update: Digest mit
#   docker buildx imagetools inspect alpine:<tag>
# lesen und beides ersetzen.
FROM alpine:3.22.7@sha256:2e0b81d60597f449baa56dc25d355367869d2dca4cd57e866f1e2b52af3eb55f AS certs
# Das Alpine-Basisimage bringt die CA-Zertifikate schon mit
# (ca-certificates-bundle); kein Paket aus dem Netz.
RUN test -s /etc/ssl/certs/ca-certificates.crt \
 && echo 'nonroot:x:65532:65532:nonroot:/data:/sbin/nologin' > /etc/passwd.min \
 && echo 'nonroot:x:65532:' > /etc/group.min \
 && mkdir -p /data /config && chown 65532:65532 /data && chmod 0700 /data

FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=certs /etc/passwd.min /etc/passwd
COPY --from=certs /etc/group.min /etc/group
COPY --from=certs --chown=65532:65532 /data /data
COPY --from=certs /config /config
COPY dist/postkauz-linux-amd64 /postkauz
USER 65532:65532
ENV POSTKAUZ_DATA_DIR=/data \
    POSTKAUZ_CONFIG=/config/config.toml \
    HOME=/data
VOLUME ["/data"]
ENTRYPOINT ["/postkauz"]
CMD ["doctor"]
