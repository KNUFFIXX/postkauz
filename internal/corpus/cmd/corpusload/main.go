// corpusload legt den synthetischen Testkorpus in einem lokalen
// Testserver ab (Abnahme D1.3) oder markiert für D1.6 eine Nachricht als
// gelesen. NUR für die Docker-Testserver auf diesem Rechner: andere Hosts
// werden verweigert.
//
//	go run ./internal/corpus/cmd/corpusload -imap 127.0.0.1:11143 -user tester -password pass
//	go run ./internal/corpus/cmd/corpusload -imap 127.0.0.1:11143 -user tester -password pass -mark-seen
//
// Ohne -dir wird der Standardkorpus (1.000 Nachrichten, Samen 42) im
// Speicher erzeugt; er ist identisch mit dem von `make corpus`.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/corpus/imapload"
	"github.com/KNUFFIXX/postkauz/internal/imapx"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

func main() {
	addr := flag.String("imap", "127.0.0.1:11143", "test server as host:port (localhost only)")
	user := flag.String("user", "tester", "login name on the test server")
	password := flag.String("password", "", "password of the test server account (test servers only)")
	tlsMode := flag.String("tls", "", "none | tls (default: tls for ports ending in 993, else none)")
	dir := flag.String("dir", "", "corpus directory written by `make corpus` (default: generate the standard corpus in memory)")
	count := flag.Int("count", 1000, "messages to generate when -dir is empty")
	seed := flag.Uint64("seed", 42, "random seed when -dir is empty")
	prefix := flag.String("prefix", "", "create all folders below this folder")
	force := flag.Bool("force", false, "load even if the target folders already contain messages")
	markSeen := flag.Bool("mark-seen", false, "instead of loading: mark the first unread message in -mailbox as read (acceptance D1.6)")
	mailbox := flag.String("mailbox", "INBOX", "mailbox for -mark-seen")
	flag.Parse()

	if err := run(*addr, *user, *password, *tlsMode, *dir, *count, *seed, *prefix, *force, *markSeen, *mailbox); err != nil {
		fmt.Fprintln(os.Stderr, "corpusload:", err)
		os.Exit(1)
	}
}

func run(addr, user, password, tlsMode, dir string, count int, seed uint64, prefix string, force, markSeen bool, mailbox string) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid -imap %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("invalid port %q", portStr)
	}
	if ip := net.ParseIP(host); !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return errors.New("only local test servers (localhost, 127.0.0.1) are allowed; never use real mailboxes")
	}
	if password == "" {
		return errors.New("-password is required (the test server password, see deploy/testmail/README.md)")
	}
	if tlsMode == "" {
		tlsMode = "none"
		if port%1000 == 993 {
			tlsMode = "tls"
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	c, err := imapx.Dial(ctx, imapx.Config{Host: host, Port: port, TLS: imapx.TLSMode(tlsMode), InsecureTestServer: true,
		CommandTimeout: 30 * time.Second})
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Login(user, secrets.Secret(password)); err != nil {
		return err
	}
	if markSeen {
		return markFirstUnseen(c, mailbox)
	}

	var cp *corpus.Corpus
	if dir != "" {
		if cp, err = corpus.Read(dir); err != nil {
			return fmt.Errorf("read corpus (run `make corpus` first): %w", err)
		}
	} else if cp, err = corpus.Generate(corpus.Options{Count: count, Seed: seed}); err != nil {
		return err
	}
	start := time.Now()
	last := time.Now()
	counts, err := imapload.Load(ctx, c, cp, imapload.Options{Prefix: prefix, Force: force, Progress: func(done, total int) {
		if time.Since(last) > 2*time.Second || done == total {
			last = time.Now()
			fmt.Fprintf(os.Stderr, "  %d/%d\n", done, total)
		}
	}})
	if err != nil {
		return err
	}
	folders := make([]string, 0, len(counts))
	total := 0
	for f, n := range counts {
		folders = append(folders, f)
		total += n
	}
	sort.Strings(folders)
	for _, f := range folders {
		fmt.Printf("%-24s %5d\n", f, counts[f])
	}
	fmt.Printf("%d messages loaded in %.1fs\n", total, time.Since(start).Seconds())
	return nil
}

// markFirstUnseen setzt \Seen an der ersten ungelesenen Nachricht und
// nennt ihre Message-ID (für `postkauz show <Message-ID> --headers`).
func markFirstUnseen(c *imapx.Client, mailbox string) error {
	if _, err := c.Select(mailbox, imapx.SelectOptions{}); err != nil {
		return err
	}
	var uid uint32
	if err := c.FetchFlags(1, 0, imapx.FlagsOptions{}, func(f imapx.FlagsData) {
		if uid == 0 && !slices.ContainsFunc(f.Flags, func(s string) bool { return strings.EqualFold(s, `\Seen`) }) {
			uid = f.UID
		}
	}); err != nil {
		return err
	}
	if uid == 0 {
		return fmt.Errorf("no unread message in %s", mailbox)
	}
	meta, err := c.FetchMeta([]uint32{uid}, imapx.MetaOptions{Header: true})
	if err != nil || len(meta) != 1 {
		return fmt.Errorf("read message %d: %v", uid, err)
	}
	if err := c.StoreFlags([]uint32{uid}, []string{`\Seen`}, true); err != nil {
		return err
	}
	mid := mimeparse.HeaderMessageID(meta[0].HeaderFields)
	fmt.Printf("marked as read: %s uid %d\nmessage-id: %s\n", mailbox, uid, mid)
	return nil
}
