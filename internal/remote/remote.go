// Package remote ist die Brücke `postkauz mcp --remote <name>` (A04 Teil 4):
// ein MCP-Server über stdio, der jede JSON-RPC-Nachricht 1:1 an einen
// entfernten MCP-Endpunkt über Streamable HTTP weiterreicht, in der Regel
// `mcp --http` eines anderen Rechners hinter tailscale serve (Modus key).
//
// Zweck: Claude Desktop bindet in claude_desktop_config.json nur
// stdio-Server ein; seine Remote-Connectoren laufen über die Cloud des
// Anbieters und erreichen ein Tailnet nicht. Statt einer fremden Brücke
// (npx mcp-remote: Lieferkette, Schlüssel im Klartext in der
// Konfiguration) bringt PostKauz sie selbst mit.
//
//   - Die Brücke öffnet keine Datenbank, braucht kein Datenverzeichnis und
//     liest kein Postfachpasswort. Sie kennt nur die Adresse aus
//     [remote.<name>] url und den Schlüssel aus dem Schlüsselbund (Verweis
//     remote/<name>), den sie als "Authorization: Bearer" mitschickt.
//   - Weitergereicht wird ohne Deutung der Inhalte: Anfragen und
//     Benachrichtigungen per POST, Antworten als JSON oder SSE zurück auf
//     stdout, Server-Nachrichten über einen GET-Strom, wenn der Server einen
//     anbietet (sonst 405), notifications/cancelled durchgereicht und die
//     zugehörige HTTP-Anfrage abgebrochen. Mcp-Session-Id und
//     MCP-Protocol-Version übernimmt die Brücke aus den Antworten; ab
//     Protokoll 2026-07-28 setzt sie auch Mcp-Method und Mcp-Name.
//   - Nur https; http nur zu einer Loopback-Adresse (config.CheckRemoteURL).
//     Zertifikate prüft die Standardbibliothek wie immer; Umleitungen werden
//     nicht verfolgt (der Schlüssel ginge sonst an eine andere Adresse).
//   - Fehler der Verbindung kommen als JSON-RPC-Fehler mit verständlichem
//     Text an den Client (Codes unten). Eine abgelaufene Sitzung (404 mit
//     Session-ID) initialisiert die Brücke einmal neu, mit der gemerkten
//     initialize-Anfrage des Clients, und wiederholt die Anfrage.
//   - Der Schlüssel steht nie in einer Ausgabe, einem Log oder Fehlertext.
//
// Warum nicht der Client-Transport des SDK (StreamableClientTransport):
// Er setzt MCP-Protocol-Version und startet den GET-Strom erst, wenn der
// SDK-Client die Sitzung selbst führt (sessionUpdated ist nicht
// exportiert); als 1:1-Brücke fehlten genau diese Teile. Genutzt werden aus
// dem SDK die JSON-RPC-Typen (jsonrpc) und auf der stdio-Seite
// mcp.IOTransport; `remote test` spricht als SDK-Client durch dieselbe
// Brücke (Test). Keine neue Abhängigkeit.
package remote

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// KeyRef ist der Verweis des Schlüssels im Schlüsselbund.
func KeyRef(name string) string { return "remote/" + name }

// JSON-RPC-Fehlercodes der Brücke (Bereich für Server-Fehler).
const (
	// CodeUnreachable: Server nicht erreichbar (Netz, DNS, TLS, Frist).
	CodeUnreachable int64 = -32010
	// CodeUnauthorized: 401, Schlüssel ungültig.
	CodeUnauthorized int64 = -32011
	// CodeForbidden: 403, Schlüssel widerrufen, abgelaufen oder ohne Recht.
	CodeForbidden int64 = -32012
	// CodeSessionExpired: 404 mit Session-ID, Neuanmeldung gescheitert.
	CodeSessionExpired int64 = -32013
	// CodeRateLimited: 429.
	CodeRateLimited int64 = -32014
	// CodeUnavailable: 502, 503, 504.
	CodeUnavailable int64 = -32015
	// CodeBadResponse: alles andere (falsche Adresse, Umleitung, unbekannte
	// Antwort).
	CodeBadResponse int64 = -32016
)

// Grenzen.
const (
	// DefaultMaxMessageBytes: größte einzelne Nachricht vom Server (wie
	// mcp.DefaultMaxLineLength auf der stdio-Seite).
	DefaultMaxMessageBytes = 16 << 20
	// DefaultCallTimeout: so lange wartet die Brücke auf die Antwort einer
	// Anfrage (der Server bricht nach 60 s selbst ab).
	DefaultCallTimeout = 5 * time.Minute
	// notifyTimeout: Frist für Benachrichtigungen und Antworten an den
	// Server.
	notifyTimeout = 30 * time.Second
	// closeTimeout: Frist für das abschließende DELETE einer Sitzung.
	closeTimeout = 5 * time.Second
	// drainTimeout: so lange warten laufende Anfragen nach dem Ende von
	// stdin noch auf ihre Antwort.
	drainTimeout = 10 * time.Second
	// maxListenRetries: Wiederverbinden des GET-Stroms ohne Fortschritt.
	maxListenRetries = 5
)

// NewHTTPClient ist der ausgehende Client: Proxy aus der Umgebung,
// Zertifikatsprüfung der Standardbibliothek (mindestens TLS 1.2), Fristen
// für Verbindung und Kopf, keine Umleitungen.
func NewHTTPClient() *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 2 * time.Minute,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
