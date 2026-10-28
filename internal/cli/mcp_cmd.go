package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/mcp"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// Umgebungsvariablen des MCP-Servers: der Schlüssel selbst oder eine Datei
// mit dem Schlüssel (Rechte 600). Nie als Argument (Prozessliste).
var (
	EnvKey     = brand.EnvPrefix + "_KEY"
	EnvKeyFile = brand.EnvPrefix + "_KEY_FILE"
)

// mcpCmd startet den MCP-Server über stdio (S1.9). Er ist kein
// Registerbefehl: Jeder Tool-Aufruf läuft einzeln durch den Executor.
func mcpCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server over stdio for an agent: read mail, write drafts; never sends (key from " + EnvKey + ")",
		Long: "Run the MCP server over standard input/output, for Claude Code and other MCP clients.\n" +
			"The API key comes from the environment variable " + EnvKey + " (or a file named in " + EnvKeyFile +
			", readable only by you), never from the command line. Create a key with: key create --name <name> --role reader.\n" +
			"The tools follow the scopes of the key: a reader key only reads; a key with mail:draft (role assistant) can also " +
			"create drafts and submit them for approval. Only a human approves and sends; nothing is ever sent, deleted or moved.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name, _ := cmd.Flags().GetString("key"); name != "" {
				return usageError{fmt.Errorf("mcp takes its key from %s or %s, not from --key", EnvKey, EnvKeyFile)}
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
