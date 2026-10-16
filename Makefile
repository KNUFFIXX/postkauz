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
export GOTOOLCHAIN ?= local

.PHONY: all build install test race vuln vet fmt fmt-check lint guard pii-check check cross corpus corpus-load testmail-up testmail-setup testmail-down integration clean

all: check build

build:
	@mkdir -p bin
	go build $(GOFLAGS_BUILD) -o bin/$(BINARY) ./cmd/postkauz
	cp bin/$(BINARY) bin/$(ALIAS)

install: build
	@echo "Copy bin/$(BINARY) and bin/$(ALIAS) to a directory on your PATH."

test:
	go test ./...

# Race-Detector (P7). Braucht CGO und einen C-Compiler; deshalb ein eigenes
# Ziel und nicht Teil von `make check` (Builds und Releases bleiben CGO-frei).
race:
	CGO_ENABLED=1 go test -race ./...

# Bekannte Schwachstellen in Abhängigkeiten und Standardbibliothek (A01 N2).
# Version gepinnt (per `go list -m golang.org/x/vuln@latest` aufgelöst, nicht
# geraten); braucht Netz (Modul-Proxy und vuln.go.dev), deshalb eigenes Ziel
# und eigener CI-Job, nicht Teil von `make check`.
GOVULNCHECK_VERSION ?= v1.9.0
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# Platzhalter: golangci-lint ist noch nicht festgelegt. Bis dahin vet + gofmt.
lint: fmt-check vet
	@echo "lint: placeholder (go vet + gofmt only); a linter is chosen in slice 1"

# Leitplanken des MVP (ADR-002): kein SMTP-Code im Repository.
guard:
	@if grep -rEn --include='*.go' '"net/smtp"|emersion/go-smtp' . ; then \
	  echo "guard: SMTP code is not allowed before slice 4 (ADR-002)"; exit 1; fi
	@# ADR-007: go-imap nur über internal/imapx (Tests dürfen den Fake-Server nutzen).
	@if grep -rlnF --include='*.go' --exclude='*_test.go' '"github.com/emersion/go-imap' internal cmd | grep -v '^internal/imapx/' ; then \
	  echo "guard: go-imap may only be imported by internal/imapx (ADR-007)"; exit 1; fi
	@# ADR-007: go-imap bleibt auf v2.0.0-beta.8 gepinnt; ein Wechsel nur über einen ADR.
	@grep -Eq '^[[:space:]]*github.com/emersion/go-imap/v2 v2\.0\.0-beta\.8$$' go.mod || { \
	  echo "guard: github.com/emersion/go-imap/v2 must stay pinned to v2.0.0-beta.8 (ADR-007)"; exit 1; }
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
