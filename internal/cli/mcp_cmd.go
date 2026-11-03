package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/httpmcp"
	"github.com/KNUFFIXX/postkauz/internal/mcp"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/remote"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Umgebungsvariablen des MCP-Servers: der Schlüssel selbst oder eine Datei
// mit dem Schlüssel (Rechte 600). Nie als Argument (Prozessliste).
var (
	EnvKey     = brand.EnvPrefix + "_KEY"
	EnvKeyFile = brand.EnvPrefix + "_KEY_FILE"
)

// mcpScopesText beschreibt, was ein Schlüssel über MCP darf (V15 der
// Nachbesserung T2: seit T2 kann ein Schlüssel mit den passenden Scopes
// markieren, verschieben und den Papierkorb beantragen; TestMCPLongTextFollowsT2).
const mcpScopesText = "The tools follow the scopes of the key: a reader key only reads; a key of the role assistant can also " +
	"create drafts and submit them for approval, tag messages locally (mail:tag) and ask for a sync (mail:sync). " +
	"Marking on the server needs mail:mark, moving mail:move and asking for the trash mail:trash (roles operator and admin); " +
	"changes on the server are made by the sync job, a move to the trash only after a human approved it. " +
	"Only a human approves and sends drafts; nothing is ever sent, and nothing is deleted for good."

// mcpCmd startet den MCP-Server über stdio (S1.9). Er ist kein
// Registerbefehl: Jeder Tool-Aufruf läuft einzeln durch den Executor.
func mcpCmd(a *app) *cobra.Command {
	var httpMode bool
	var listen, remoteName string
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server over stdio for an agent: read mail, write drafts, more as the key's scopes allow; never sends (key from " + EnvKey + ")",
		Long: "Run the MCP server over standard input/output, for Claude Code and other MCP clients.\n" +
			"The API key comes from the environment variable " + EnvKey + " (or a file named in " + EnvKeyFile +
			", readable only by you), never from the command line. Create a key with: key create --name <name> --role reader.\n" +
			mcpScopesText + "\n\n" +
			"With --http it serves MCP over HTTP instead (stateless Streamable HTTP, POST /mcp; settings in [http] of the " +
			"configuration). Then every request brings its own credential as \"Authorization: Bearer ...\": an API key " +
			"(auth = \"key\", loopback and Tailscale addresses only) or an access token of an external authorization server " +
			"(auth = \"oauth\"). The HTTP server knows no mailbox password; TLS and the private network come from in front " +
			"(tailscale serve), see docs/betrieb.md.\n\n" +
			"With --remote <name> it is a bridge for MCP clients that only start local programs (Claude Desktop): " +
			"every message goes unchanged to the MCP endpoint of [remote.<name>] url on another PC (usually mcp --http " +
			"behind tailscale serve), with the API key stored by remote add in the keyring. The bridge opens no database, " +
			"needs no data directory and knows no mailbox password.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name, _ := cmd.Flags().GetString("key"); name != "" {
				return usageError{fmt.Errorf("mcp takes its key from %s or %s, not from --key", EnvKey, EnvKeyFile)}
			}
			if remoteName != "" {
				if httpMode || listen != "" {
					return usageError{errors.New("--remote cannot be combined with --http or --listen")}
				}
				return runMCPRemote(cmd.Context(), a, remoteName)
			}
			if httpMode {
				return runMCPHTTP(cmd.Context(), a, listen)
			}
			if listen != "" {
				return usageError{errors.New("--listen needs --http")}
			}
			plain, err := keyFromEnv()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			a.noCredentials = true
			x, err := a.open(ctx)
			if err != nil {
				return err
			}
			key, err := ops.AuthenticateKeyAudited(ctx, a.st, a.st.AuditWriter(), "stdio", plain, time.Now())
			if err != nil {
				return err
			}
			srv, err := mcp.New(mcp.Options{Executor: x, Key: key, Version: a.version, Log: a.log,
				MaxChars: a.policy.MaxCharsFor(key.Name)})
			if err != nil {
				return err
			}
			a.log.Info("mcp server started", "key", key.ID, "role", key.Role)
			err = srv.Run(ctx, &sdk.IOTransport{Reader: io.NopCloser(a.stdin), Writer: nopWriteCloser{a.stdout}})
			a.log.Info("mcp server stopped", "key", key.ID)
			if err == nil || errors.Is(err, io.EOF) || errors.Is(err, sdk.ErrConnectionClosed) || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		},
	}
	c.Flags().BoolVar(&httpMode, "http", false, "serve MCP over HTTP ([http] in the configuration) instead of stdio")
	c.Flags().StringVar(&listen, "listen", "", "with --http: address and port instead of [http] listen, e.g. 127.0.0.1:8787")
	c.Flags().StringVar(&remoteName, "remote", "", "bridge stdio to the MCP endpoint of [remote.<name>] on another PC (see remote add)")
	return c
}

// runMCPHTTP startet den HTTP-MCP-Server (S3.5, ADR-010) und läuft, bis
// der Prozess beendet wird (Strg+C, Dienst gestoppt).
func runMCPHTTP(ctx context.Context, a *app, listen string) error {
	if os.Getenv(EnvKey) != "" || os.Getenv(EnvKeyFile) != "" {
		fmt.Fprintf(a.stderr, "mcp --http: %s/%s is ignored; every HTTP request brings its own credential\n", EnvKey, EnvKeyFile)
	}
	// Ein Fehler in [http] hält nur diesen Start auf (Nachbesserung T5,
	// Punkt 9h); die übrigen Kommandos laufen trotzdem.
	if a.cfgErr == nil && a.cfg.HTTPErr != nil {
		return a.cfg.HTTPErr
	}
	a.noCredentials = true
	x, err := a.open(ctx)
	if err != nil {
		return err
	}
	h := a.cfg.HTTP
	if listen != "" {
		h.Listen = listen
		if err := config.CheckHTTP(&h); err != nil {
			return usageError{err}
		}
	}
	if err := httpmcp.CheckListen(h); err != nil {
		return usageError{err}
	}
	srv, err := httpmcp.New(httpmcp.Deps{HTTP: h, Executor: x, Keys: a.st, Audit: a.st.AuditWriter(), Version: a.version,
		Log: a.log, MaxChars: a.policy.MaxCharsFor})
	if err != nil {
		return err
	}
	err = srv.ListenAndServe(ctx, func(addr string) {
		a.log.Info("mcp http server started", "listen", addr, "auth", h.Auth, "resource", h.Resource)
		fmt.Fprintf(a.stderr, "mcp --http: listening on %s (auth %s, resource %s); stop with Ctrl+C\n", addr, h.Auth, h.Resource)
	})
	a.log.Info("mcp http server stopped")
	if err == nil || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// runMCPRemote ist die Brücke mcp --remote (A04 Teil 4): stdio auf der
// einen Seite, Streamable HTTP zum Ziel auf der anderen. Keine Datenbank,
// kein Datenverzeichnis, kein Postfachpasswort: nur die Konfiguration und
// der Schlüssel remote/<name> aus dem Schlüsselbund (ohne
// Umgebungsschicht). Warnungen gehen auf stderr (der MCP-Client schreibt
// sie in sein Log), nie mit Schlüssel.
func runMCPRemote(ctx context.Context, a *app, name string) error {
	if a.cfgErr != nil {
		return a.cfgErr
	}
	if err := config.CheckRemoteName(name); err != nil {
		return usageError{err}
	}
	if a.cfg.RemoteErr != nil {
		return a.cfg.RemoteErr
	}
	r, ok := a.cfg.Remote[name]
	if !ok {
		return ops.ErrRemoteNotFound
	}
	if os.Getenv(EnvKey) != "" || os.Getenv(EnvKeyFile) != "" {
		fmt.Fprintf(a.stderr, "mcp --remote: %s/%s is ignored; the key for remote %s comes from the keyring\n", EnvKey, EnvKeyFile, name)
	}
	sec, err := openSecrets(a.cfg.Secrets.Backend, a.paths.SecretsDir)
	if err != nil {
		return err
	}
	key, err := secrets.Stored(sec).Get(remote.KeyRef(name))
	if errors.Is(err, secrets.ErrNotFound) {
		return fmt.Errorf("no key for remote %s in the keyring; store it with: remote add %s --url %s --key-stdin", name, name, r.URL)
	} else if err != nil {
		return fmt.Errorf("reading the key for remote %s from the keyring: %w", name, err)
	}
	log := slog.New(slog.NewTextHandler(a.stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	conn, err := (&sdk.IOTransport{Reader: io.NopCloser(a.stdin), Writer: nopWriteCloser{a.stdout}}).Connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	fmt.Fprintf(a.stderr, "mcp --remote %s: forwarding to %s\n", name, termLine(r.URL))
	b := &remote.Bridge{Name: name, URL: r.URL, Key: key, Log: log}
	if err := b.Run(ctx, conn); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// keyFromEnv liest den Schlüssel aus POSTKAUZ_KEY oder der Datei in
// POSTKAUZ_KEY_FILE (unter Unix nur für den Nutzer lesbar).
func keyFromEnv() (string, error) {
	if v := strings.TrimSpace(os.Getenv(EnvKey)); v != "" {
		return v, nil
	}
	path := os.Getenv(EnvKeyFile)
	if path == "" {
		return "", fmt.Errorf("%w: set %s (or %s) in the MCP client configuration", ops.ErrKeyMissing, EnvKey, EnvKeyFile)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", EnvKeyFile, err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s: %s is readable by others; run: chmod 600 %s", EnvKeyFile, path, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
