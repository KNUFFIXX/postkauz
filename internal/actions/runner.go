package actions

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Ausführung der Aktionen (Job mailbox_action, E1): nur im Prozess mit
// Zugangsdaten und Kontosperre (postkauz sync zu Beginn jedes Laufs,
// sync --watch zusätzlich alle --jobs-interval). Je Aktion, älteste zuerst:
//
//  1. Herkunft prüfen (A8): Gilt der Schlüssel, der sie angelegt hat, noch
//     (Widerruf, Ablauf, Scope)? Lassen die Regeln der Policy, die dieser
//     Prozess geladen hat ([move], [delete], [keys.<name>]), sie noch zu
//     (V3)? Sonst scheitert sie mit dem Grund, ohne Kommando an den
//     Server. Hat ein Mensch einen Antrag freigegeben, hält ein inzwischen
//     abgelaufener (nicht widerrufener) Schlüssel ihn nicht auf (wie T2
//     Punkt 7 für Entwürfe); die Policy gilt trotzdem.
//  2. Ziele prüfen: Der Zielordner von move und trash muss unter seiner ID
//     noch denselben Pfad und dieselbe Rolle haben, ein move endet nie im
//     Papierkorb (sonst alle Ziele stale, target_changed). Die Serverkopie muss im Spiegel noch unter Ordner, UID
//     und UIDVALIDITY liegen (sonst stale). Hat PostKauz den Inhalt dieser
//     Kopie nie selbst geladen (nur per Message-ID und Größe verknüpft, T2
//     Punkt 3), holt der Job ihren Kopf (BODY.PEEK[HEADER]) und vergleicht
//     ihn mit dem lokalen; weicht er ab, ist das Ziel stale, und nichts
//     passiert mit ihr.
//  3. Je Quellordner ein Kommando (STORE bzw. MOVE); Ergebnis je Ziel als
//     Code, nie Servertext.
//
// Ein Verbindungs- oder Anmeldefehler bricht den Lauf ab; die Aktion bleibt
// running und wird beim nächsten Lauf fortgesetzt (STORE ist idempotent,
// MOVE einer nicht mehr vorhandenen UID tut nichts und zählt als
// not_found, V11).

// Remote ist der Teil des Providers, den die Ausführung braucht.
type Remote interface {
	FetchHeaders(ctx context.Context, mailbox string, uidValidity uint32, uids []provider.UID) ([]provider.Header, error)
	SetFlags(ctx context.Context, mailbox string, uidValidity uint32, uids []provider.UID, patch provider.FlagPatch) error
	Move(ctx context.Context, from string, uidValidity uint32, uids []provider.UID, to string) (provider.MoveResult, error)
}

// RunStore ist der Teil des Stores für die Ausführung.
type RunStore interface {
	ExpireActions(ctx context.Context, now time.Time) (int, error)
	QueuedActions(ctx context.Context, accountID int64) ([]store.Action, error)
	StartAction(ctx context.Context, id int64, now time.Time) error
	FinishAction(ctx context.Context, id int64, results []store.TargetResult, errCode string, now time.Time) (string, error)
	SaveTargetResults(ctx context.Context, id int64, results []store.TargetResult, now time.Time) error
	ServerCopies(ctx context.Context, messageID int64) ([]store.ServerCopy, error)
	Mailboxes(ctx context.Context, accountID int64) ([]store.Mailbox, error)
	MessageHeaderBlob(ctx context.Context, messageID int64) (string, error)
	MarkContentLoaded(ctx context.Context, messageID, mailboxID int64) error
}

// Blobs liest Rohmails.
type Blobs interface {
	Get(sha256 string) (io.ReadCloser, error)
}

// Origin beschreibt eine Aktion für die Prüfung vor der Ausführung.
type Origin struct {
	// KeyID: wer sie angelegt hat ("cli" | "key:<name>" | "system").
	KeyID string
	// Scope: den braucht der Schlüssel für diese Art von Aktion.
	Scope policy.Scope
	// Approved: Ein Mensch hat die Aktion freigegeben.
	Approved bool
	// Facts: was die Aktion anrichtet, für die Eingabeprüfung der Policy
	// (V3): Art, Zahl der Nachrichten, confirm_count, Rolle des Zielordners
	// heute.
	Facts policy.Facts
}

// OriginCheck prüft den Schlüssel hinter einer Aktion (A8) und die Regeln
// der Policy des ausführenden Prozesses (V3): "" = gültig, sonst der Grund
// (key_revoked, key_expired, key_invalid, missing_scope, policy_forbids,
// target_role_human_only, …).
type OriginCheck func(ctx context.Context, o Origin) (string, error)

// Runner führt die Aktionen eines Kontos aus.
type Runner struct {
	Store  RunStore
	Blobs  Blobs
	Origin OriginCheck
	Log    *slog.Logger
	Now    func() time.Time
}

// Report fasst einen Lauf zusammen (nur Zahlen).
type Report struct {
	Actions int `json:"actions"`
	OK      int `json:"ok"`
	Failed  int `json:"failed"`
	Stale   int `json:"stale"`
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Runner) log() *slog.Logger {
	if r.Log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return r.Log
}

// Pending meldet, ob das Konto ausführbare Aktionen hat.
func (r *Runner) Pending(ctx context.Context, accountID int64) (bool, error) {
	if _, err := r.Store.ExpireActions(ctx, r.now()); err != nil {
		return false, err
	}
	q, err := r.Store.QueuedActions(ctx, accountID)
	return len(q) > 0, err
}

// scopeOf ist der Scope, den der Schlüssel für eine Art von Aktion braucht.
func scopeOf(kind string) policy.Scope {
	switch kind {
	case store.ActionMove:
		return policy.ScopeMailMove
	case store.ActionTrash:
		return policy.ScopeMailTrash
	case store.ActionMark:
		return policy.ScopeMailMark
	}
	return policy.ScopeMailTag // keywords (Tags als IMAP-Keywords)
}

// factsOf beschreibt eine gespeicherte Aktion für policy.CheckInput (V3),
// so wie der Antrag sie beschrieb: Zahl der Nachrichten (target_count zählt
// übersprungene nicht mit; confirm_count war beim Antrag gleich der Zahl
// aller IDs, deshalb das größere von beiden), confirm_count und die Rolle,
// die der Zielordner heute im Spiegel hat (sonst die beim Anlegen).
// keywords ist für die Policy keine Postfach-Aktion (nur die Zahl zählt).
func factsOf(a store.Action, boxes map[int64]store.Mailbox) policy.Facts {
	f := policy.Facts{Targets: a.TargetCount, ConfirmCount: a.ConfirmCount}
	if a.ConfirmCount != nil {
		f.Targets = max(f.Targets, *a.ConfirmCount)
	}
	switch a.Kind {
	case store.ActionMove:
		f.Action = policy.ActionMove
	case store.ActionTrash:
		f.Action = policy.ActionTrash
	case store.ActionMark:
		f.Action = policy.ActionMark
	}
	if a.Kind == store.ActionMove {
		f.TargetRole = a.Params.TargetRole
		if d, ok := boxes[a.Params.TargetMailboxID]; ok {
			f.TargetRole = d.Role
		}
	}
	return f
}

// Run führt alle ausführbaren Aktionen des Kontos aus. Ein Fehler kommt
// nur bei Abbruch, Verbindungs-, Anmelde- oder Datenbankfehlern zurück.
func (r *Runner) Run(ctx context.Context, accountID int64, remote Remote) (Report, error) {
	var rep Report
	if _, err := r.Store.ExpireActions(ctx, r.now()); err != nil {
		return rep, err
	}
	queue, err := r.Store.QueuedActions(ctx, accountID)
	if err != nil {
		return rep, err
	}
	if len(queue) == 0 {
		return rep, nil
	}
	boxes, err := r.Store.Mailboxes(ctx, accountID)
	if err != nil {
		return rep, err
	}
	byID := map[int64]store.Mailbox{}
	for _, b := range boxes {
		byID[b.ID] = b
	}
	for _, a := range queue {
		rep.Actions++
		results, unsaved, code, err := r.runOne(ctx, a, byID, remote)
		for _, t := range results {
			switch t.Result {
			case store.TargetOK, store.TargetSkipped:
				rep.OK++
			case store.TargetStale:
				rep.Stale++
			default:
				rep.Failed++
			}
		}
		if err != nil {
			return rep, err // Aktion bleibt running; der nächste Lauf macht den Rest
		}
		st, err := r.Store.FinishAction(context.WithoutCancel(ctx), a.ID, unsaved, code, r.now())
		if err != nil {
			return rep, err
		}
		r.log().Info("mailbox action done", "action_id", a.ID, "kind", a.Kind, "state", st, "targets", len(results), "error", code)
	}
	return rep, nil
}

// fatal: Fehler, nach denen der Lauf endet (die Aktion bleibt running).
func fatal(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, provider.ErrConnection) || errors.Is(err, provider.ErrAuth)
}

// runOne führt eine Aktion aus. results: alle Ergebnisse dieses Laufs;
// unsaved: die davon, die noch nicht gespeichert sind (die Ergebnisse der
// Kommandos speichert runOne je Quellordner sofort, V4). Bricht der Lauf
// ab (err), sind results die schon gespeicherten Ergebnisse, damit der
// Bericht sie zählt (Nachprüfung zu fd70f19, Punkt 6).
func (r *Runner) runOne(ctx context.Context, a store.Action, boxes map[int64]store.Mailbox, remote Remote) (results, unsaved []store.TargetResult, code string, err error) {
	open := []store.ActionTarget{}
	for _, t := range a.Targets {
		if t.Result == "" {
			open = append(open, t)
		}
	}
	if r.Origin != nil {
		reason, err := r.Origin(ctx, Origin{KeyID: a.RequestedBy, Scope: scopeOf(a.Kind), Approved: a.ApprovalID > 0, Facts: factsOf(a, boxes)})
		if err != nil {
			return nil, nil, "", err
		}
		if reason != "" {
			for _, t := range open {
				results = append(results, store.TargetResult{MessageID: t.MessageID, MailboxID: t.MailboxID, Result: store.TargetFailed})
			}
			return results, results, reason, nil
		}
	}
	if err := r.Store.StartAction(ctx, a.ID, r.now()); err != nil {
		return nil, nil, "", err
	}
	dest, hasDest := boxes[a.Params.TargetMailboxID]
	if (a.Kind == store.ActionMove || a.Kind == store.ActionTrash) && !hasDest {
		for _, t := range open {
			results = append(results, store.TargetResult{MessageID: t.MessageID, MailboxID: t.MailboxID, Result: store.TargetNotFound})
		}
		return results, results, "target_not_found", nil
	}
	// V1: Die ID allein bindet das Ziel nicht (mailboxes.id ohne
	// AUTOINCREMENT wird nach zwei Ordnerabgleichen neu vergeben, und ein
	// Ordner kann die Rolle wechseln). Pfad und Rolle müssen noch passen,
	// und move landet nie im Papierkorb; sonst geht kein Kommando an den
	// Server.
	if (a.Kind == store.ActionMove || a.Kind == store.ActionTrash) && targetChanged(a, dest) {
		r.log().Warn("target mailbox changed since the action was created; nothing done (stale)", "action_id", a.ID)
		for _, t := range open {
			results = append(results, store.TargetResult{MessageID: t.MessageID, MailboxID: t.MailboxID, Result: store.TargetStale})
		}
		return results, results, "target_changed", nil
	}
	// Ziele je Quellordner, nach der Prüfung von Spiegel und Kopf. Die
	// Köpfe nie selbst geladener Kopien holt der Job je Ordner mit einem
	// FETCH (V21), nicht je UID.
	groups := map[int64][]store.ActionTarget{}
	var order []int64
	add := func(t store.ActionTarget) {
		if _, seen := groups[t.MailboxID]; !seen {
			order = append(order, t.MailboxID)
		}
		groups[t.MailboxID] = append(groups[t.MailboxID], t)
	}
	refuse := func(res store.TargetResult, why string) {
		results = append(results, res)
		unsaved = append(unsaved, res)
		if code == "" {
			code = why
		}
	}
	headers := map[int64][]store.ActionTarget{}
	var headerOrder []int64
	for _, t := range open {
		res, why, st, err := r.checkMirror(ctx, t, boxes)
		if err != nil {
			return nil, nil, "", err
		}
		switch st {
		case checkRefused:
			refuse(res, why)
		case checkHeader:
			if _, seen := headers[t.MailboxID]; !seen {
				headerOrder = append(headerOrder, t.MailboxID)
			}
			headers[t.MailboxID] = append(headers[t.MailboxID], t)
		default:
			add(t)
		}
	}
	for _, mbID := range headerOrder {
		passed, refused, why, err := r.checkHeaders(ctx, boxes[mbID], headers[mbID], remote)
		if err != nil {
			return nil, nil, "", err
		}
		for _, res := range refused {
			refuse(res, why[res.MessageID])
		}
		for _, t := range passed {
			add(t)
		}
	}
	var saved []store.TargetResult
	for _, mbID := range order {
		ts := groups[mbID]
		mb := boxes[mbID]
		uids := make([]provider.UID, len(ts))
		for i, t := range ts {
			uids[i] = provider.UID(t.UID)
		}
		res, err := r.apply(ctx, a, mb, dest, ts, uids, remote)
		if err != nil && fatal(ctx, err) {
			return saved, nil, "", err
		}
		if err := r.Store.SaveTargetResults(context.WithoutCancel(ctx), a.ID, res, r.now()); err != nil {
			return saved, nil, "", err
		}
		saved = append(saved, res...)
		results = append(results, res...)
	}
	return results, unsaved, code, nil
}

// targetChanged meldet, ob der Zielordner unter der gespeicherten ID nicht
// mehr der beim Anlegen ist (Pfad oder Rolle anders) oder ob ein move im
// Papierkorb enden würde.
func targetChanged(a store.Action, dest store.Mailbox) bool {
	switch {
	case dest.Path != a.Params.TargetMailbox:
		return true
	case a.Params.TargetRole != "" && dest.Role != a.Params.TargetRole:
		return true
	case a.Kind == store.ActionMove && dest.Role == string(provider.RoleTrash):
		return true
	}
	return false
}

// Ergebnis von checkMirror.
type checkState int

const (
	checkOK      checkState = iota // ausführen
	checkHeader                    // erst den Kopf vom Server vergleichen
	checkRefused                   // nicht ausführen (Ergebnis steht fest)
)

// Gründe, die eine Aktion trägt, wenn Ziele bei der Prüfung ausfallen
// (das Ergebnis je Ziel bleibt stale: die Tabelle action_targets erlaubt nur
// feste Ergebnisse, Migration 0020).
const (
	// ReasonSourceChanged: Der Quellordner hat unter seiner ID nicht mehr
	// den Pfad, den er beim Anlegen des Ziels hatte (Quellpfad je Ziel,
	// Nachprüfung zu fd70f19).
	ReasonSourceChanged = "source_changed"
	// ReasonNoLocalCopy: Eine nie selbst geladene Serverkopie ließ sich
	// nicht prüfen, weil die lokale Rohmail fehlt oder unlesbar ist (V17).
	ReasonNoLocalCopy = "no_local_copy"
)

// checkMirror prüft ein Ziel gegen den Spiegel: Ordner da, UIDVALIDITY
// gleich, Pfad des Ordners wie beim Anlegen (source_path; NULL bei
// Altbestand = nicht geprüft), Kopie unter Ordner und UID vorhanden. Bei
// nie geladenem Inhalt folgt der Kopfvergleich (checkHeaders).
func (r *Runner) checkMirror(ctx context.Context, t store.ActionTarget, boxes map[int64]store.Mailbox) (store.TargetResult, string, checkState, error) {
	res := store.TargetResult{MessageID: t.MessageID, MailboxID: t.MailboxID, Result: store.TargetStale}
	mb, ok := boxes[t.MailboxID]
	if !ok || mb.UIDValidity != t.UIDValidity {
		return res, "", checkRefused, nil
	}
	if t.SourcePath != "" && mb.Path != t.SourcePath {
		r.log().Warn("source mailbox changed since the action was created; nothing done for this message (stale)",
			"message_id", t.MessageID, "mailbox_id", t.MailboxID)
		return res, ReasonSourceChanged, checkRefused, nil
	}
	cs, err := r.Store.ServerCopies(ctx, t.MessageID)
	if err != nil {
		return res, "", checkRefused, err
	}
	for _, c := range cs {
		if c.MailboxID == t.MailboxID && c.UID == t.UID && c.UIDValidity == t.UIDValidity {
			if c.ContentLoaded {
				return res, "", checkOK, nil
			}
			return res, "", checkHeader, nil
		}
	}
	return res, "", checkRefused, nil
}

// checkHeaders vergleicht die Köpfe nie selbst geladener Kopien eines
// Ordners mit den lokalen Rohmails: ein FETCH für alle (V21). Ohne lokale
// Rohmail fragt es den Server gar nicht erst (stale, no_local_copy, V17).
// passed: gleich (gilt danach als geladen); refused: Ergebnis je Ziel
// (stale, not_found, failed) mit Grund je Nachricht in why. err nur bei
// Abbruch, Verbindungs-, Anmelde- oder Datenbankfehlern.
func (r *Runner) checkHeaders(ctx context.Context, mb store.Mailbox, ts []store.ActionTarget, remote Remote) (passed []store.ActionTarget,
	refused []store.TargetResult, why map[int64]string, err error) {
	why = map[int64]string{}
	result := func(t store.ActionTarget, code string) store.TargetResult {
		return store.TargetResult{MessageID: t.MessageID, MailboxID: t.MailboxID, Result: code}
	}
	locals := map[provider.UID][]byte{}
	var ask []store.ActionTarget
	var uids []provider.UID
	for _, t := range ts {
		local, err := r.localHeader(ctx, t.MessageID)
		if err != nil {
			return nil, nil, nil, err
		}
		if local == nil {
			refused = append(refused, result(t, store.TargetStale))
			why[t.MessageID] = ReasonNoLocalCopy
			continue
		}
		locals[provider.UID(t.UID)] = local
		ask = append(ask, t)
		uids = append(uids, provider.UID(t.UID))
	}
	if len(ask) == 0 {
		return nil, refused, why, nil
	}
	hs, err := remote.FetchHeaders(ctx, mb.Path, mb.UIDValidity, uids)
	switch {
	case err != nil && fatal(ctx, err):
		return nil, nil, nil, err
	case err != nil:
		code := store.TargetFailed
		if errors.Is(err, provider.ErrUIDValidityChanged) {
			code = store.TargetStale
		}
		for _, t := range ask {
			refused = append(refused, result(t, code))
		}
		return nil, refused, why, nil
	}
	byUID := map[provider.UID]provider.Header{}
	for _, h := range hs {
		byUID[h.UID] = h
	}
	for _, t := range ask {
		h, found := byUID[provider.UID(t.UID)]
		switch {
		case !found:
			refused = append(refused, result(t, store.TargetNotFound))
		case h.HeaderTooLarge || !bytes.Equal(normHeader(locals[provider.UID(t.UID)]), normHeader(h.RawHeader)):
			r.log().Warn("server copy differs from the local message; action skipped for it (stale)", "message_id", t.MessageID, "uid", t.UID)
			refused = append(refused, result(t, store.TargetStale))
		default:
			if err := r.Store.MarkContentLoaded(ctx, t.MessageID, t.MailboxID); err != nil {
				return nil, nil, nil, err
			}
			passed = append(passed, t)
		}
	}
	return passed, refused, why, nil
}

// maxLocalHeader begrenzt das Lesen des lokalen Kopfes (wie FetchMeta).
const maxLocalHeader = 1 << 20

// localHeader liest den Kopf der lokalen Rohmail einer Nachricht; nil, wenn
// es keine gibt oder sie unlesbar ist (V17). err nur bei Datenbankfehlern.
func (r *Runner) localHeader(ctx context.Context, messageID int64) ([]byte, error) {
	sha, err := r.Store.MessageHeaderBlob(ctx, messageID)
	if err != nil {
		return nil, err
	}
	if sha == "" || r.Blobs == nil {
		return nil, nil
	}
	rc, err := r.Blobs.Get(sha)
	if err != nil {
		return nil, nil
	}
	defer rc.Close()
	local, err := readHeader(rc)
	if err != nil {
		return nil, nil
	}
	return local, nil
}

// readHeader liest den Kopf einer Rohmail bis zur ersten Leerzeile.
func readHeader(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(io.LimitReader(r, maxLocalHeader+1))
	var out []byte
	for {
		line, err := br.ReadBytes('\n')
		out = append(out, line...)
		if len(bytes.TrimRight(line, "\r\n")) == 0 && len(line) > 0 {
			return out, nil
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if len(out) > maxLocalHeader {
			return nil, fmt.Errorf("header larger than %d bytes", maxLocalHeader)
		}
	}
}

// normHeader: CRLF zu LF, Leerzeilen am Ende weg.
func normHeader(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return bytes.TrimRight(b, "\n")
}

// apply führt eine Aktion für die Ziele eines Quellordners aus.
func (r *Runner) apply(ctx context.Context, a store.Action, mb, dest store.Mailbox, ts []store.ActionTarget, uids []provider.UID,
	remote Remote) ([]store.TargetResult, error) {
	all := func(code string) []store.TargetResult {
		out := make([]store.TargetResult, len(ts))
		for i, t := range ts {
			out[i] = store.TargetResult{MessageID: t.MessageID, MailboxID: t.MailboxID, Result: code}
		}
		return out
	}
	byErr := func(err error) string {
		switch {
		case errors.Is(err, provider.ErrUIDValidityChanged):
			return store.TargetStale
		case errors.Is(err, provider.ErrUnsupported):
			return store.TargetUnsupported
		case errors.Is(err, provider.ErrNotFound):
			return store.TargetNotFound
		}
		return store.TargetFailed
	}
	switch a.Kind {
	case store.ActionMark, store.ActionKeywords:
		var patch provider.FlagPatch
		if m := a.Params.Mark; m != nil {
			patch.Seen, patch.Flagged = m.Seen, m.Flagged
		}
		patch.AddKeywords, patch.RemoveKeywords = a.Params.AddKeywords, a.Params.RemoveKeywords
		if err := remote.SetFlags(ctx, mb.Path, mb.UIDValidity, uids, patch); err != nil {
			if fatal(ctx, err) {
				return nil, err
			}
			r.log().Warn("mailbox action failed for a mailbox", "action_id", a.ID, "kind", a.Kind, "error_code", byErr(err))
			return all(byErr(err)), nil
		}
		return all(store.TargetOK), nil
	case store.ActionMove, store.ActionTrash:
		if mb.ID == dest.ID {
			return all(store.TargetSkipped), nil
		}
		mr, err := remote.Move(ctx, mb.Path, mb.UIDValidity, uids, dest.Path)
		if err != nil {
			if fatal(ctx, err) {
				return nil, err
			}
			r.log().Warn("mailbox action failed for a mailbox", "action_id", a.ID, "kind", a.Kind, "error_code", byErr(err))
			return all(byErr(err)), nil
		}
		// V11: Eine UID, die es im Quellordner nicht mehr gab, hat der
		// Server nicht verschoben (COPYUID nennt sie nicht, bzw. kein
		// EXPUNGE): not_found statt ok; der Spiegel bleibt, der nächste
		// Sync trägt die Kopie aus.
		missing := map[provider.UID]bool{}
		for _, u := range mr.Missing {
			missing[u] = true
		}
		out := make([]store.TargetResult, len(ts))
		for i, t := range ts {
			if missing[provider.UID(t.UID)] {
				out[i] = store.TargetResult{MessageID: t.MessageID, MailboxID: t.MailboxID, Result: store.TargetNotFound}
				continue
			}
			out[i] = store.TargetResult{MessageID: t.MessageID, MailboxID: t.MailboxID, Result: store.TargetOK,
				DestMailboxID: dest.ID, DestUIDValidity: mr.UIDValidity, NewUID: uint32(mr.UIDs[provider.UID(t.UID)])}
		}
		return out, nil
	}
	return all(store.TargetFailed), nil
}
