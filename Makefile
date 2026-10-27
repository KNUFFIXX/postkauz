# PostKauz (Arbeitstitel). Namen an einer Stelle: BINARY/ALIAS hier,
# Konstanten in internal/brand/brand.go, Modulpfad in go.mod.
BINARY  ?= postkauz
ALIAS   ?= pk
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)
GOFLAGS_BUILD := -trimpath -ldflags "$(LDFLAGS)"
PLATFORMS := linux/amd64 windows/amd64 darwin/arm64
COMPOSE := docker compose -f deploy/testmail/docker-compose.yml

export CGO_ENABLED ?= 0
# ADR-008: nie still eine andere Toolchain nachladen (sonst hebt `go get` die
# go-Zeile in go.mod an). Die Zielversion (Go 1.27.x) muss installiert sein.
# Go-Patchstand (Entscheid Betreiber 21.10.2026, Roadmap Frage 34): Builds mit go1.27.3
# (toolchain-Zeile in go.mod); govulncheck meldete mit 1.27.0 neun stdlib-Schwachstellen.
# GOTOOLCHAIN=local: fehlt die Toolchain, bricht der Build ab, statt still nachzuladen (ADR-008).
export GOTOOLCHAIN ?= local

.PHONY: all build install test race vuln vet fmt fmt-check lint hooks guard pii-check check cross release corpus corpus-load testmail-up testmail-setup testmail-down integration clean

all: check build

build:
	@mkdir -p bin
	go build $(GOFLAGS_BUILD) -o bin/$(BINARY) ./cmd/postkauz
	cp bin/$(BINARY) bin/$(ALIAS)

install: build
	@echo "Copy bin/$(BINARY) and bin/$(ALIAS) to a directory on your PATH."

# Alle Tests, auch die schweren (voller Korpus, Kindprozesse). Für einen
# schnellen Durchlauf beim Entwickeln: go test -short ./... (überspringt
# Korpus- und Subprozess-Tests; nie für check, race oder CI).
test:
	go test ./...

# Race-Detector (P7). Braucht CGO und einen C-Compiler; deshalb ein eigenes
# Ziel und nicht Teil von `make check` (Builds und Releases bleiben CGO-frei).
# Unter -race laufen die Korpus-Tests mehrfach langsamer; der Standard von
# go test (10 Minuten je Paket) reicht dafür knapp nicht, daher 30 Minuten.
# Bewusst ohne -short: Gerade die schweren Tests sollen unter -race laufen.
race:
	CGO_ENABLED=1 go test -race -timeout 30m ./...

# Bekannte Schwachstellen in Abhängigkeiten und Standardbibliothek (A01 N2).
# Version gepinnt (per `go list -m golang.org/x/vuln@latest` aufgelöst, nicht
# geraten); braucht Netz (Modul-Proxy und vuln.go.dev), deshalb eigenes Ziel
# und eigener CI-Job, nicht Teil von `make check`.
GOVULNCHECK_VERSION ?= v1.9.0
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# Pre-Commit-Hook (A12.3) für diesen Klon einschalten: Git nimmt die Hooks
# dann aus .githooks/ (dort ruft pre-commit `make check`). Abschalten mit
# `git config --unset core.hooksPath`. Komfort, die CI bleibt die Sperre.
hooks:
	git config core.hooksPath .githooks
	@echo "hooks: pre-commit runs make check (disable: git config --unset core.hooksPath)"

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# golangci-lint (A12.1) mit der Auswahl aus .golangci.yml (gosec,
# staticcheck, errcheck, bodyclose, gocritic und Standardsatz).
# Version gepinnt: v2.15.0 ist die neueste stabile Version am 27.10.2026
# (Release vom 16.10.2026; ermittelt per `git ls-remote --tags
# https://github.com/golangci/golangci-lint` und `go list -m
# github.com/golangci/golangci-lint/v2@latest`, nicht geraten). Go 1.27
# wird seit v2.13.0 unterstützt (CHANGELOG des Moduls). `go run` baut das
# Werkzeug mit der lokalen Toolchain (go1.27.3), wie bei govulncheck; ein
# Binär-Release, das mit älterem Go gebaut ist, verweigert Code mit
# `go 1.27` in go.mod. Braucht Netz (Modul-Proxy), deshalb eigenes Ziel und
# eigener CI-Job im Berichtsmodus, nicht Teil von `make check`. Befunde des
# Erstlaufs: docs/protokolle/lint_erstlauf.md.
GOLANGCI_LINT_VERSION ?= v2.15.0
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

# Leitplanken des MVP (ADR-002): kein SMTP-Code im Repository.
# Alle Suchen lassen vendor/ aus (A03 Nachtrag 26.10.2026): fremder Code
# darf dort SMTP oder go-imap enthalten; Prüfgegenstand ist nur eigener Code.
guard:
	@if grep -rEn --include='*.go' --exclude-dir=vendor '"net/smtp"|emersion/go-smtp' . ; then \
	  echo "guard: SMTP code is not allowed before slice 4 (ADR-002)"; exit 1; fi
	@# ADR-007: go-imap nur über internal/imapx (Tests dürfen den Fake-Server nutzen).
	@if grep -rlnF --include='*.go' --exclude='*_test.go' '"github.com/emersion/go-imap' internal cmd | grep -v '^internal/imapx/' ; then \
	  echo "guard: go-imap may only be imported by internal/imapx (ADR-007)"; exit 1; fi
	@# ADR-007: go-imap bleibt auf v2.0.0-beta.8 gepinnt; ein Wechsel nur über einen ADR.
	@grep -Eq '^[[:space:]]*github.com/emersion/go-imap/v2 v2\.0\.0-beta\.8$$' go.mod || { \
	  echo "guard: github.com/emersion/go-imap/v2 must stay pinned to v2.0.0-beta.8 (ADR-007)"; exit 1; }
	@# Windows-Skripte (Aufgabe für den Audit-Export, Beispiel-Hooks seit S2.4):
	@# UTF-8 mit BOM (Windows PowerShell 5.1), Inhalt nur ASCII.
	@for f in deploy/windows/*.ps1 deploy/hooks/*.ps1; do \
	  [ "$$(head -c 3 "$$f" | od -An -tx1 | tr -d ' \n')" = "efbbbf" ] || { echo "guard: $$f must be UTF-8 with BOM"; exit 1; }; \
	  if tail -c +4 "$$f" | LC_ALL=C grep -q '[^[:print:][:space:]]'; then echo "guard: $$f must contain ASCII only after the BOM"; exit 1; fi; \
	done
	@# A5.5: Go 1.28 verbietet das Muster "..." bei go list im Module-Modus;
	@# kein Skript, kein Makefile und kein Workflow darf es nutzen.
	@if grep -rEn --exclude-dir=vendor --exclude-dir=.git --include=Makefile --include='*.mk' --include='*.sh' --include='*.ps1' \
	  --include='*.yml' --include='*.yaml' --include=pre-commit 'go[[:space:]]+list([[:space:]]+[^[:space:]]+)*[[:space:]]+(\./)?\.\.\.' \
	  $$(ls -d Makefile scripts deploy docs .github .githooks 2>/dev/null); then \
	  echo "guard: go list must not use the three-dot pattern in scripts (Go 1.28)"; exit 1; fi
	@# T1c (Gegenprüfung K3): keine rohen Bidi-Steuerzeichen (Trojan Source)
	@# in Quelltext, Doku, Skripten und Konfiguration, nur als Escape. Bytes
	@# von U+200E/F, U+202A-E, U+2066-9 und U+061C; testdata/ (Korpus) bleibt
	@# außen vor.
	@if LC_ALL=C grep -rlE --exclude-dir=vendor --exclude-dir=.git --exclude-dir=testdata --exclude-dir=dist \
	  --include='*.go' --include='*.md' --include='*.ps1' --include='*.sh' --include='*.py' --include='*.yml' \
	  --include='*.yaml' --include='*.toml' --include=Makefile --include=pre-commit \
	  "$$(printf '\342\200[\216\217\252-\256]|\342\201[\246-\251]|\330\234')" . ; then \
	  echo "guard: raw bidi control characters found; write them as escapes (e.g. \\u202e)"; exit 1; fi
	@echo "guard: ok"

pii-check:
	@sh scripts/pii-check.sh

# Alles, was vor jedem Commit grün sein muss.
check: fmt-check vet test guard pii-check

# Cross-Compile ohne CGO: linux/amd64, windows/amd64, darwin/arm64.
cross:
	@mkdir -p dist
	@set -e; for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
	  echo "build $$os/$$arch"; \
	  GOOS=$$os GOARCH=$$arch go build $(GOFLAGS_BUILD) -o dist/$(BINARY)-$$os-$$arch$$ext ./cmd/postkauz; \
	done
	@ls -l dist/ | awk 'NR>1 {printf "%-32s %6.1f MB\n", $$NF, $$5/1048576}'

# Release (S1.11): dist/ mit den drei Programmen ohne Symboltabelle und
# Debug-Informationen (-s -w), dem Windows-Skript für den täglichen
# Audit-Export, seit S2.9 den Beispiel-Hooks für die Prüfung vor der
# Freigabe (Entscheid 42: check-example.ps1, check-example.sh), VERSION und
# SHA256SUMS. Beispiel:
#   make release VERSION=0.2.0-rc1
# Den Git-Tag setzt der Betreiber erst nach der Abnahme. Andere Dateien
# in dist/ (Abnahme-Zips) bleiben liegen.
RELEASE_LDFLAGS = -s -w -X main.version=$(VERSION)
SHA256 = $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo "shasum -a 256")
release:
	@case "$(VERSION)" in [0-9]*.[0-9]*.[0-9]*) ;; *) echo "release: give a version, e.g. make release VERSION=0.1.0-rc1 (got $(VERSION))"; exit 1;; esac
	@mkdir -p dist
	@rm -f dist/$(BINARY)-linux-* dist/$(BINARY)-windows-* dist/$(BINARY)-darwin-* dist/SHA256SUMS dist/VERSION dist/*.ps1 dist/check-example.sh
	@set -e; for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
	  echo "build $$os/$$arch ($(VERSION))"; \
	  GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(RELEASE_LDFLAGS)" -o dist/$(BINARY)-$$os-$$arch$$ext ./cmd/postkauz; \
	done
	@{ echo "$(BINARY) $(VERSION)"; \
	  echo "commit $$(git rev-parse HEAD 2>/dev/null || echo unknown)$$(git diff --quiet HEAD 2>/dev/null || echo ' (uncommitted changes)')"; \
	  echo "$$(go env GOVERSION), CGO_ENABLED=$(CGO_ENABLED), -trimpath, -ldflags \"-s -w\""; \
	  echo "built $$(date -u +%Y-%m-%dT%H:%M:%SZ)"; } > dist/VERSION
	@# Das Windows-Skript für den täglichen Audit-Export gehört dazu, mit
	@# Zeilenenden CRLF (wie im Arbeitsbaum unter Windows, .gitattributes).
	@sed 's/\r*$$/\r/' deploy/windows/Register-AuditExport.ps1 > dist/Register-AuditExport.ps1
	@# Beispiel-Hooks (Entscheid 42): PowerShell mit CRLF, Shell mit LF und
	@# ausführbar.
	@sed 's/\r*$$/\r/' deploy/hooks/check-example.ps1 > dist/check-example.ps1
	@sed 's/\r*$$//' deploy/hooks/check-example.sh > dist/check-example.sh && chmod 755 dist/check-example.sh
	@cd dist && $(SHA256) $(BINARY)-linux-* $(BINARY)-windows-* $(BINARY)-darwin-* Register-AuditExport.ps1 check-example.ps1 check-example.sh VERSION > SHA256SUMS
	@cat dist/VERSION; echo; cat dist/SHA256SUMS; echo
	@ls -l dist/ | awk 'NR>1 {printf "%-32s %10d bytes %6.1f MB\n", $$NF, $$5, $$5/1048576}'

# Synthetischer Testkorpus (docs/testkorpus.md): *.eml + corpus.json.
# Lasttest: make corpus CORPUS_DIR=testdata/corpus-large CORPUS_COUNT=50000
CORPUS_DIR   ?= testdata/corpus
CORPUS_COUNT ?= 1000
CORPUS_SEED  ?= 42
corpus:
	go run ./internal/corpus/cmd/gencorpus -out $(CORPUS_DIR) -count $(CORPUS_COUNT) -seed $(CORPUS_SEED)

# Korpus per IMAP APPEND in einen lokalen Testserver laden (Abnahme D1.3).
# Standard: Dovecot, Nutzer tester. Nur localhost; verweigert volle Ordner.
#   make corpus-load LOAD_IMAP=127.0.0.1:13143 LOAD_USER=test      (GreenMail)
LOAD_IMAP     ?= 127.0.0.1:11143
LOAD_USER     ?= tester
LOAD_PASSWORD ?= pass
corpus-load:
	go run ./internal/corpus/cmd/corpusload -imap $(LOAD_IMAP) -user $(LOAD_USER) -password $(LOAD_PASSWORD)

testmail-up:
	$(COMPOSE) up -d
	@echo "Test servers are bound to 127.0.0.1 only; ports: deploy/testmail/README.md"

# Stalwart einrichten (Domain, Konto test/pass); nach jedem testmail-up.
testmail-setup:
	sh deploy/testmail/stalwart-setup.sh

testmail-down:
	$(COMPOSE) down -v

# Integrationstests gegen die Docker-Testserver (nicht in check und CI).
# Auswahl: make integration IT=dovecot,greenmail
IT ?= 1
integration:
	POSTKAUZ_IT=$(IT) go test -count=1 -run Integration -v ./internal/...

clean:
	rm -rf bin dist
