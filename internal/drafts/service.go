package drafts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Service führt die Übergänge eines Entwurfs aus: je Aufruf eine kurze
// Schreibtransaktion (BEGIN IMMEDIATE) mit Lesen, Prüfen und Schreiben.
// Mehrere Prozesse (CLI, MCP-Server, sync --watch) dürfen gleichzeitig
// arbeiten; wer zuerst die Schreibsperre hat, gewinnt, der andere sieht
// danach den neuen Zustand.
//
// Wer handelt (Mensch oder Schlüssel) und ob er darf, entscheidet der
// Executor (ab S2.3, HumanOnly für approve/reject/abandon/mark_sent); er
// schreibt auch den Audit-Eintrag der Operation. Der Dienst schreibt selbst
// nur die Einträge der Systemereignisse (Ablauf einer Freigabe, vom Sync
// erkannter Versand), in derselben Transaktion wie den Übergang.
type Service struct {
	Store *store.SQLite
	// Now ist die Uhr (nil = time.Now); Tests setzen eine feste.
	Now func() time.Time
	// Rand ist die Zufallsquelle der Message-ID (nil = crypto/rand).
	Rand io.Reader
	// ApprovalTTL: so lange gilt eine offene oder erteilte Freigabe
	// ([draft] approval_ttl; 0 = DefaultApprovalTTL).
	ApprovalTTL time.Duration
	// PolicyVersion steht in den Audit-Einträgen der Systemereignisse (E8).
	PolicyVersion int64

	// who: Wer handelt; gesetzt über As. Dann schreibt jeder Übergang
	// einen Audit-Eintrag mit diesem Akteur in derselben Transaktion (S2.3).
	who *Who
	// dry: Trockenlauf; jede Transaktion läuft vollständig (alle Prüfungen)
	// und wird am Ende zurückgerollt (S2.3).
	dry bool
}

// Who beschreibt, wer einen Übergang auslöst, für dessen Audit-Eintrag:
// Akteur, Schlüssel ("cli" | "key:<name>"), Scope und Operation wie im
// Eintrag des Executors.
type Who struct {
	Actor     audit.ActorType
	KeyID     string
	Scope     string
	Operation string
}

// As liefert eine Kopie des Dienstes, die jeden Übergang mit who
// auditiert (in derselben Transaktion wie der Übergang) und mit dryRun
// nichts speichert: Die Transaktion läuft mit allen Prüfungen und wird
// zurückgerollt; das Ergebnis zeigt, was geschähe.
func (s *Service) As(who Who, dryRun bool) *Service {
	c := *s
	c.who, c.dry = &who, dryRun
	return &c
}

// DryRun meldet, ob der Dienst im Trockenlauf arbeitet.
func (s *Service) DryRun() bool { return s.dry }

// errDryRun rollt die Transaktion eines Trockenlaufs zurück.
var errDryRun = errors.New("drafts: dry run")

// tx führt fn in einer Schreibtransaktion aus; im Trockenlauf wird sie
// nach erfolgreichem fn zurückgerollt.
func (s *Service) tx(ctx context.Context, fn func(*store.DraftTx) error) error {
	err := s.Store.DraftTx(ctx, func(x *store.DraftTx) error {
		if err := fn(x); err != nil {
			return err
		}
		if s.dry {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		return nil
	}
	return err
}

// ReasonTransition steht als Grund im Audit-Eintrag eines Übergangs, den
// ein Mensch oder Agent auslöst (As); der Eintrag des Executors davor
// trägt die Entscheidung der Policy.
const ReasonTransition = "transition"

// record schreibt den Audit-Eintrag eines Übergangs (nur mit As): Draft-ID,
// Art, Zustand vorher und nachher, Zahl der Empfänger und Anhänge,
// Freigabe, Hook-Ergebnis. Nie Betreff, Adressen oder Text.
func (s *Service) record(x *store.DraftTx, now time.Time, d store.Draft, from, to Status, approvalID int64, hook string) error {
	if s.who == nil {
		return nil
	}
	params := map[string]any{"draft_id": d.ID, "kind": d.Kind, "from": from, "to": to,
		"recipients": len(d.To) + len(d.Cc) + len(d.Bcc), "attachments": len(d.Attachments)}
	if from == "" {
		params["from"] = "new"
	}
	targets := []string{"draft:" + strconv.FormatInt(d.ID, 10)}
	if approvalID > 0 {
		params["approval_id"] = approvalID
		targets = append(targets, "approval:"+strconv.FormatInt(approvalID, 10))
	}
	if hook != "" {
		params["hook"] = hook
	}
	b, _ := json.Marshal(params)
	_, err := x.AppendAudit(audit.Event{TS: now, ActorType: s.who.Actor, KeyID: s.who.KeyID, Scope: s.who.Scope,
		Operation: s.who.Operation, ParamsRedacted: b, Targets: targets, Result: audit.ResultOK, Reason: ReasonTransition,
		PolicyVersion: s.PolicyVersion})
	return err
}

// enqueuePush reiht den Job draft_push ein, wenn die Serverkopie aussteht
// (E1; ausgeführt ab S2.5).
func (s *Service) enqueuePush(x *store.DraftTx, d store.Draft, now time.Time, res *Result) error {
	if d.RemoteState != RemotePending {
		return nil
	}
	id, err := x.EnqueueDraftPush(d.AccountID, now)
	res.PushJobID = id
	return err
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) ttl() time.Duration {
	if s.ApprovalTTL > 0 {
		return s.ApprovalTTL
	}
	return DefaultApprovalTTL
}

// NewDraft ist die Eingabe von Create.
type NewDraft struct {
	AccountID int64
	// IdentityID: 0 = Standardidentität des Kontos; hat das Konto keine,
	// ist der Absender die Kontoadresse ohne Namen (E9).
	IdentityID   int64
	Kind         Kind
	InReplyToMsg int64
	Content
	// CreatedBy: "cli" | "key:<name>" (wie audit_events.api_key_id).
	CreatedBy string
}

// Result ist das Ergebnis eines Aufrufs: der Entwurf danach, der Übergang
// (From == To bei einer Änderung ohne Wirkung) und die Freigabe, wenn eine
// entstand oder entschieden wurde.
type Result struct {
	Draft    store.Draft
	From, To Status
	Approval *store.Approval
	// Changed: Der Inhalt hat sich geändert (Update) bzw. der Entwurf ist neu.
	Changed bool
	// Notes sind Hinweise für die Ausgabe (englisch, ohne Inhalt).
	Notes []string
	// PushJobID: der offene Job draft_push des Kontos, wenn die
	// Serverkopie aussteht (E1; 0 = keiner eingereiht).
	PushJobID int64
}

// NoteNoIdentity: Hinweis, wenn das Konto keine Identität hat (E9).
const NoteNoIdentity = "no identity configured for this account: the sender is the account address without a name (add one with identity add)"

// Create legt einen Entwurf im Zustand draft an: Grenzen geprüft, Absender
// aus der Identität (Kopie), Message-ID vergeben, preview_hash berechnet,
// Serverkopie pending.
func (s *Service) Create(ctx context.Context, in NewDraft) (Result, error) {
	if !ValidKind(in.Kind) {
		return Result{}, fmt.Errorf("drafts: unknown kind %q", in.Kind)
	}
	if err := Validate(in.Content); err != nil {
		return Result{}, err
	}
	now := s.now()
	var res Result
	err := s.tx(ctx, func(x *store.DraftTx) error {
		acc, err := x.Account(in.AccountID)
		if err != nil {
			return err
		}
		d := store.Draft{AccountID: acc.ID, Kind: string(in.Kind), InReplyToMsg: in.InReplyToMsg, Status: string(StatusDraft),
			RemoteState: RemotePending, CreatedBy: in.CreatedBy, CreatedAt: now, UpdatedAt: now}
		switch {
		case in.IdentityID > 0:
			id, err := x.Identity(in.IdentityID)
			if err != nil {
				return err
			}
			if id.AccountID != acc.ID {
				return denied(ReasonIdentityMismatch, "identity %d belongs to another account", id.ID)
			}
			d.IdentityID, d.FromName, d.FromEmail = id.ID, id.Name, id.Email
		default:
			id, ok, err := x.DefaultIdentity(acc.ID)
			if err != nil {
				return err
			}
			if ok {
				d.IdentityID, d.FromName, d.FromEmail = id.ID, id.Name, id.Email
			} else {
				d.FromEmail = acc.Email
				res.Notes = append(res.Notes, NoteNoIdentity)
			}
		}
		if d.MessageIDHeader, err = NewMessageID(s.Rand, d.FromEmail); err != nil {
			return err
		}
		setContent(&d, in.Content)
		d.PreviewHash = PreviewHash(HashOf(d))
		if d.ID, err = x.InsertDraft(d); err != nil {
			return err
		}
		res.Draft, res.From, res.To, res.Changed = d, "", StatusDraft, true
		if err := s.enqueuePush(x, d, now, &res); err != nil {
			return err
		}
		return s.record(x, now, d, "", StatusDraft, 0, "")
	})
	return res, err
}

func setContent(d *store.Draft, c Content) {
	d.To, d.Cc, d.Bcc = c.To, c.Cc, c.Bcc
	d.Subject, d.BodyMarkdown, d.BodyText, d.BodyHTML = c.Subject, c.BodyMarkdown, c.BodyText, c.BodyHTML
	d.HeadersJSON = c.HeadersJSON
	if d.HeadersJSON == "" {
		d.HeadersJSON = "{}"
	}
	d.Attachments = c.Attachments
	d.ComposeOptions = c.ComposeOptions
	if d.ComposeOptions == "" {
		d.ComposeOptions = "{}"
	}
}

// ContentOf liefert den veränderlichen Inhalt eines gespeicherten Entwurfs
// (Ausgangspunkt einer Änderung einzelner Felder).
func ContentOf(d store.Draft) Content {
	return Content{To: d.To, Cc: d.Cc, Bcc: d.Bcc, Subject: d.Subject, BodyMarkdown: d.BodyMarkdown, BodyText: d.BodyText,
		BodyHTML: d.BodyHTML, HeadersJSON: d.HeadersJSON, Attachments: d.Attachments, ComposeOptions: d.ComposeOptions}
}

// sameContent meldet, ob c inhaltlich dem gespeicherten Entwurf gleicht:
// gleicher preview_hash (Empfänger also ohne Rücksicht auf Reihenfolge und
// Schreibweise der Adresse) und gleiche übrige Felder, die der Hash nicht
// abdeckt (Markdown, Kopfzeilen, Name und Typ der Anhänge). Nur dann ist
// eine Änderung keine Änderung.
func sameContent(d store.Draft, c Content) bool {
	n := d
	setContent(&n, c)
	return PreviewHash(HashOf(d)) == PreviewHash(HashOf(n)) && extrasKey(d) == extrasKey(n)
}

func extrasKey(d store.Draft) string {
	var headers any
	_ = json.Unmarshal([]byte(d.HeadersJSON), &headers)
	atts := append([]store.DraftAttachment{}, d.Attachments...)
	sort.Slice(atts, func(i, j int) bool {
		if atts[i].SHA256 != atts[j].SHA256 {
			return atts[i].SHA256 < atts[j].SHA256
		}
		return atts[i].Filename < atts[j].Filename
	})
	b, _ := json.Marshal([]any{d.BodyMarkdown, headers, atts})
	return string(b)
}

// Update ersetzt den Inhalt eines Entwurfs. Ändert sich nichts, bleibt
// alles, wie es ist (auch ready). Sonst: Zustand draft (aus
// pending_approval und ready mit Freigabe superseded), Hook-Ergebnis
// gelöscht, neuer preview_hash, Serverkopie wieder pending (Push nach
// Änderung, S2.5). Abgeschlossen: draft_closed; vom Menschen im Client
// übernommen (E6): draft_taken_over. Ob er übernommen hat, prüft Update
// anhand des Spiegels (zuletzt abgelegte Kopie fehlt oder ist anders); ein
// Befund wird gespeichert (remote_state taken_over bzw. missing, Audit
// system), die Änderung abgelehnt.
//
// expect ist der Fingerprint des Stands, auf dem c beruht (der Aufrufer hat
// einzelne Felder auf ihm zusammengeführt); "" = ohne Prüfung. Hat ein
// anderer Schreiber den Entwurf seitdem geändert, ist das draft_changed
// (ErrDraftChanged), und nichts wird geschrieben: Der Aufrufer liest neu und
// führt erneut zusammen, statt die fremde Änderung zu überschreiben
// (Gegenprüfung S2.3, W4). Die Ablehnung hat den Grund draft_changed.
func (s *Service) Update(ctx context.Context, id int64, c Content, expect string) (Result, error) {
	if err := Validate(c); err != nil {
		return Result{}, err
	}
	now := s.now()
	var res Result
	var deny error // Übernahme erkannt: remote_state wird gespeichert, die Änderung abgelehnt
	err := s.tx(ctx, func(x *store.DraftTx) error {
		d, err := s.draftDue(x, id, now)
		if err != nil {
			return err
		}
		from := Status(d.Status)
		if from.Closed() {
			_, err := Next(from, EvUpdate)
			return err
		}
		if d.RemoteState == RemoteTakenOver || d.RemoteState == RemoteMissing {
			return takenOver(d.ID, d.RemoteState)
		}
		// W1: Kopie eines abgebrochenen Ablageversuchs zuerst übernehmen
		// (sonst hielte die Prüfung sie für eine Fassung des Mailclients).
		if _, err := s.adoptOrphan(x, &d, now); err != nil {
			return err
		}
		// E6: Ist die zuletzt abgelegte Kopie laut Spiegel noch unverändert
		// da? Sonst hat der Mensch im Mailclient übernommen (S2.5).
		if st, err := checkCopy(x, d); err != nil {
			return err
		} else if st != "" {
			res.Draft, res.From, res.To = d, from, from
			deny = takenOver(d.ID, st)
			return s.markRemote(x, &d, st, now)
		}
		if expect != "" && Fingerprint(d) != expect {
			return changed(d.ID)
		}
		res.Draft, res.From, res.To = d, from, from
		if sameContent(d, c) {
			return nil
		}
		t, err := Next(from, EvUpdate)
		if err != nil {
			return err
		}
		if res.Approval, err = applyApproval(x, &d, t.Approval, now, "", ""); err != nil {
			return err
		}
		setContent(&d, c)
		d.Status, d.HookResult, d.PushNote, d.UpdatedAt = string(t.To), "", "", now
		d.PreviewHash = PreviewHash(HashOf(d))
		if d.RemoteState == RemotePushed {
			// Neue Kopie beim nächsten Push; die alte bleibt im
			// Entwürfe-Ordner (kein Expunge im MVP, S2.5).
			d.RemoteState = RemotePending
		}
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		res.Draft, res.To, res.Changed = d, t.To, true
		if err := s.enqueuePush(x, d, now, &res); err != nil {
			return err
		}
		var superseded int64
		if res.Approval != nil {
			superseded = res.Approval.ID
		}
		return s.record(x, now, d, from, t.To, superseded, "")
	})
	if err == nil && deny != nil {
		return res, deny
	}
	return res, err
}

// draftDue liest den Entwurf in der Transaktion; ist seine Freigabe
// abgelaufen, fällt er zuerst zurück (wie beim Lesen, E7), in derselben
// Transaktion. So sieht auch ein Trockenlauf, der den Rückfall nicht
// speichert, den Stand danach (K8).
func (s *Service) draftDue(x *store.DraftTx, id int64, now time.Time) (store.Draft, error) {
	d, err := x.Draft(id)
	if err != nil || d.ApprovalID == 0 || (d.Status != string(StatusPendingApproval) && d.Status != string(StatusReady)) {
		return d, err
	}
	a, err := x.Approval(d.ApprovalID)
	if err != nil {
		return d, err
	}
	if a.ExpiresAt.IsZero() || now.Before(a.ExpiresAt) || (a.Decision != DecisionPending && a.Decision != DecisionApproved) {
		return d, nil
	}
	if err := s.expireOne(x, id, now); err != nil {
		return d, err
	}
	return x.Draft(id)
}

// changed ist die Ablehnung draft_changed (ReasonOf(err) == ReasonDraftChanged).
func changed(id int64) error {
	return denied(ReasonDraftChanged, "draft %d was changed by another writer in the meantime; read it again", id)
}

// Checked ist, was ein Aufrufer vor der Vorlage geprüft hat: der Stand
// (Fingerprint) und die daraus gebaute Nachricht (compose.MessageHash).
type Checked struct {
	// Fingerprint des Entwurfs, den Hook und Nachrichtenbau gesehen haben
	// ("" = ohne Prüfung; nur Tests und Werkzeuge ohne Hook).
	Fingerprint string
	// MessageHash der gebauten Nachricht; die Freigabe bindet ihn (W1).
	MessageHash string
}

// RequestReady legt den Entwurf zur Freigabe vor. hook ist das Ergebnis des
// Prüf-Hooks (S2.4; ohne Hook "pass"). Besteht er nicht, bleibt der
// Entwurf draft, nur hook_result wird gesetzt (kein Fehler: Result.To ist
// draft). Sonst entsteht eine Freigabe (pending, gültig bis jetzt +
// approval_ttl) mit dem preview_hash des Entwurfs, dem message_hash der
// gebauten Nachricht und einer Beschreibung ohne Mailinhalt, und der
// Entwurf wird pending_approval.
//
// Hook und Nachrichtenbau laufen vor dieser Transaktion (der Hook darf 30 s
// dauern, die Schreibsperre nicht). Hat ein anderer Schreiber den Entwurf
// inzwischen geändert (Fingerprint weicht ab), ist das draft_changed und
// nichts wird geschrieben: Sonst bände die Freigabe einen Stand, den weder
// Hook noch Nachrichtenbau gesehen haben (Gegenprüfung S2.3, W2).
func (s *Service) RequestReady(ctx context.Context, id int64, requestedBy, hook string, chk Checked) (Result, error) {
	switch hook {
	case HookPass, HookFail, HookTimeout, HookError:
	default:
		return Result{}, fmt.Errorf("drafts: unknown hook result %q", hook)
	}
	now := s.now()
	var res Result
	err := s.tx(ctx, func(x *store.DraftTx) error {
		d, err := s.draftDue(x, id, now)
		if err != nil {
			return err
		}
		from := Status(d.Status)
		t, err := Next(from, EvRequestReady)
		if err != nil {
			return err
		}
		if chk.Fingerprint != "" && Fingerprint(d) != chk.Fingerprint {
			return changed(d.ID)
		}
		res.From = from
		d.HookResult, d.UpdatedAt = hook, now
		if d.PushNote == PushNoteApprovalStale {
			d.PushNote = "" // neu vorgelegt: der Hinweis des Pushs ist erledigt
		}
		if hook != HookPass {
			if err := x.UpdateDraft(d); err != nil {
				return err
			}
			res.Draft, res.To = d, from
			return s.record(x, now, d, from, from, 0, hook)
		}
		acc, err := x.Account(d.AccountID)
		if err != nil {
			return err
		}
		a := store.Approval{SubjectType: "draft", SubjectID: d.ID, Description: describe(acc.Name, d), RequestedBy: requestedBy,
			RequestedAt: now, PreviewHash: d.PreviewHash, ExpiresAt: now.Add(s.ttl()), Decision: DecisionPending, MessageHash: chk.MessageHash}
		if a.ID, err = x.InsertApproval(a); err != nil {
			return err
		}
		d.Status, d.ApprovalID = string(t.To), a.ID
		repush := staleCopy(&d, chk.MessageHash)
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		res.Draft, res.To, res.Approval = d, t.To, &a
		if repush {
			if err := s.enqueuePush(x, d, now, &res); err != nil {
				return err
			}
		}
		return s.record(x, now, d, from, t.To, a.ID, hook)
	})
	return res, err
}

// staleCopy prüft bei Vorlage und Freigabe, ob die Kopie im Entwürfe-Ordner
// noch die Nachricht ist, die jetzt vorgelegt bzw. freigegeben wird
// (Gegenprüfung S2.4/S2.5, W2): Hat sich seit dem Push etwas geändert, das
// in den Nachrichtenbau eingeht, aber nicht im Entwurf steht (Identität
// entfernt, Signatur, [draft] quote_line), läge sonst eine andere Fassung im
// Ordner, als der Mensch freigibt. Dann wird die Kopie wieder pending (der
// nächste Push legt die neue Fassung ab, die alte bleibt im Ordner wie nach
// jeder Änderung); true = Job einreihen. Eine Kopie aus Schema 13 (ohne
// Nachrichten-Hash) gilt als abweichend. messageHash "" (ohne Prüfung) =
// nichts tun.
func staleCopy(d *store.Draft, messageHash string) bool {
	if messageHash == "" || d.RemoteState != RemotePushed || d.PushedMessageHash == messageHash {
		return false
	}
	d.RemoteState = RemotePending
	return true
}

// describe ist die Beschreibung einer Freigabe: Kontoname, Art, Zahl der
// Empfänger und Anhänge, Draft-ID; nie Betreff, Adressen oder Text.
func describe(account string, d store.Draft) string {
	return fmt.Sprintf("draft %d (%s) in account %s: %d recipients, %d attachments", d.ID, d.Kind, account,
		len(d.To)+len(d.Cc)+len(d.Bcc), len(d.Attachments))
}

// Approve gibt eine Freigabe frei (nur Mensch, ab S2.3 HumanOnly). Der
// Entwurf muss pending_approval sein und denselben preview_hash haben wie
// die Freigabe; sonst approval_stale. Eine abgelaufene Freigabe fällt dabei
// zurück (approval_expired, Systemereignis im Audit). messageHash ist der
// Hash der jetzt neu gebauten Nachricht (compose.MessageHash); weicht er
// vom gespeicherten der Vorlage ab (Identität, Signatur, Reply-To, Zitat
// oder Anhänge haben sich geändert) oder fehlt dieser (Freigabe von vor
// Schema 12), fällt der Entwurf auf draft zurück, die Freigabe wird
// superseded (gespeichert, auditiert) und das Ergebnis ist approval_stale
// (Gegenprüfung S2.3, W1). Danach ist der Entwurf ready, die Freigabe
// approved und gilt bis jetzt + approval_ttl.
func (s *Service) Approve(ctx context.Context, approvalID int64, decidedBy, messageHash string) (Result, error) {
	return s.decide(ctx, approvalID, EvApprove, decidedBy, "", messageHash)
}

// Reject lehnt eine offene Freigabe ab (nur Mensch); der Entwurf wird draft.
// reason ist der Grund des Menschen; er steht nur in approvals.reason.
func (s *Service) Reject(ctx context.Context, approvalID int64, decidedBy, reason string) (Result, error) {
	return s.decide(ctx, approvalID, EvReject, decidedBy, reason, "")
}

func (s *Service) decide(ctx context.Context, approvalID int64, ev Event, decidedBy, reason, messageHash string) (Result, error) {
	now := s.now()
	var res Result
	var deny error // Ablehnung nach einem Übergang, der trotzdem gespeichert wird
	err := s.tx(ctx, func(x *store.DraftTx) error {
		a, err := x.Approval(approvalID)
		if err != nil {
			return err
		}
		if a.SubjectType != "draft" {
			return denied(ReasonInvalidTransition, "approval %d is not for a draft", a.ID)
		}
		d, err := x.Draft(a.SubjectID)
		if err != nil {
			return err
		}
		from := Status(d.Status)
		res.Draft, res.From, res.To, res.Approval = d, from, from, &a
		if from.Closed() {
			_, err := Next(from, ev)
			return err
		}
		switch a.Decision {
		case DecisionPending:
		case DecisionSuperseded, DecisionExpired:
			return denied(ReasonApprovalStale, "approval %d is %s; the draft changed or the approval ran out, request a new one", a.ID, a.Decision)
		default:
			return denied(ReasonApprovalDecided, "approval %d was already %s", a.ID, a.Decision)
		}
		if d.ApprovalID != a.ID || d.PreviewHash != a.PreviewHash || from != StatusPendingApproval {
			return denied(ReasonApprovalStale, "draft %d no longer matches approval %d; request a new one", d.ID, a.ID)
		}
		if !a.ExpiresAt.IsZero() && !now.Before(a.ExpiresAt) {
			if err := s.expireOne(x, d.ID, now); err != nil {
				return err
			}
			if res.Draft, err = x.Draft(d.ID); err != nil {
				return err
			}
			if na, err := x.Approval(a.ID); err == nil {
				res.Approval = &na
			}
			res.To = Status(res.Draft.Status)
			deny = denied(ReasonApprovalExpired, "approval %d ran out at %s", a.ID, a.ExpiresAt.Format(time.RFC3339))
			return nil
		}
		if ev == EvApprove && (a.MessageHash == "" || a.MessageHash != messageHash) {
			// Die fertige Nachricht ist nicht mehr die vorgelegte: wie eine
			// Änderung (pending_approval → draft, Freigabe superseded).
			t, err := Next(from, EvUpdate)
			if err != nil {
				return err
			}
			if res.Approval, err = applyApproval(x, &d, t.Approval, now, "", ""); err != nil {
				return err
			}
			d.Status, d.HookResult, d.UpdatedAt = string(t.To), "", now
			if err := x.UpdateDraft(d); err != nil {
				return err
			}
			res.Draft, res.To = d, t.To
			deny = denied(ReasonApprovalStale, "the message built from draft %d differs from the one submitted with approval %d "+
				"(identity, signature, reply-to, quoted original or attachments changed); submit it again", d.ID, a.ID)
			return s.record(x, now, d, from, t.To, a.ID, "")
		}
		t, err := Next(from, ev)
		if err != nil {
			return err
		}
		if res.Approval, err = applyApproval(x, &d, t.Approval, now, decidedBy, reason); err != nil {
			return err
		}
		if t.Approval == ApprovalApprove {
			res.Approval.ExpiresAt = now.Add(s.ttl())
			if err := x.UpdateApproval(*res.Approval); err != nil {
				return err
			}
		}
		d.Status, d.UpdatedAt = string(t.To), now
		repush := ev == EvApprove && staleCopy(&d, messageHash)
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		res.Draft, res.To = d, t.To
		if repush {
			if err := s.enqueuePush(x, d, now, &res); err != nil {
				return err
			}
		}
		return s.record(x, now, d, from, t.To, a.ID, "")
	})
	if err == nil && deny != nil {
		return res, deny
	}
	return res, err
}

// applyApproval wendet die Wirkung eines Übergangs auf die Freigabe des
// Entwurfs an (nicht bei ApprovalCreate; das macht RequestReady). Liefert
// die geänderte Freigabe oder nil.
func applyApproval(x *store.DraftTx, d *store.Draft, eff ApprovalEffect, now time.Time, by, reason string) (*store.Approval, error) {
	if eff == ApprovalNone || eff == ApprovalCreate || d.ApprovalID == 0 {
		return nil, nil
	}
	a, err := x.Approval(d.ApprovalID)
	if err != nil {
		return nil, err
	}
	open := a.Decision == DecisionPending
	live := open || a.Decision == DecisionApproved
	switch eff {
	case ApprovalApprove:
		if !open {
			return nil, denied(ReasonApprovalStale, "approval %d is %s", a.ID, a.Decision)
		}
		a.Decision, a.DecidedBy, a.DecidedAt = DecisionApproved, by, now
	case ApprovalReject:
		if !open {
			return nil, denied(ReasonApprovalStale, "approval %d is %s", a.ID, a.Decision)
		}
		a.Decision, a.DecidedBy, a.DecidedAt, a.Reason = DecisionRejected, by, now, reason
	case ApprovalSupersede:
		if !live {
			return nil, nil
		}
		a.Decision = DecisionSuperseded
	case ApprovalExpire:
		if !live {
			return nil, nil
		}
		a.Decision = DecisionExpired
	case ApprovalCloseOpen:
		if !open {
			return nil, nil
		}
		a.Decision = DecisionSuperseded
	default:
		return nil, fmt.Errorf("drafts: unknown approval effect %q", eff)
	}
	if err := x.UpdateApproval(a); err != nil {
		return nil, err
	}
	return &a, nil
}

// Abandon verwirft einen Entwurf (nur Mensch): abandoned, closed_at gesetzt,
// offene oder erteilte Freigabe superseded. Die Serverkopie bleibt (kein
// Expunge im MVP).
func (s *Service) Abandon(ctx context.Context, id int64) (Result, error) {
	return s.close(ctx, id, EvAbandon, 0, "")
}

// MarkSent erklärt einen Entwurf für gesendet (nur Mensch, `draft
// mark-sent`): sent_by_human, closed_at gesetzt, eine noch offene Freigabe
// superseded (eine erteilte bleibt approved).
func (s *Service) MarkSent(ctx context.Context, id int64) (Result, error) {
	return s.close(ctx, id, EvMarkSent, 0, "")
}

// SentDetected schließt einen Entwurf, dessen Versand der Sync erkannt hat
// (E5, S2.6): sent_by_human mit der gesendeten Nachricht; reason ist
// message_id oder fallback. Der Audit-Eintrag (Akteur system) steht in
// derselben Transaktion.
func (s *Service) SentDetected(ctx context.Context, id, sentMessageID int64, reason string) (Result, error) {
	if reason != DetectMessageID && reason != DetectFallback {
		return Result{}, fmt.Errorf("drafts: unknown detection reason %q", reason)
	}
	return s.close(ctx, id, EvSentDetected, sentMessageID, reason)
}

func (s *Service) close(ctx context.Context, id int64, ev Event, sentMessageID int64, reason string) (Result, error) {
	now := s.now()
	var res Result
	err := s.tx(ctx, func(x *store.DraftTx) error {
		d, err := x.Draft(id)
		if err != nil {
			return err
		}
		from := Status(d.Status)
		t, err := Next(from, ev)
		if err != nil {
			return err
		}
		if res.Approval, err = applyApproval(x, &d, t.Approval, now, "", ""); err != nil {
			return err
		}
		d.Status, d.UpdatedAt, d.ClosedAt = string(t.To), now, now
		if sentMessageID > 0 {
			d.SentMessageID = sentMessageID
		}
		if err := x.UpdateDraft(d); err != nil {
			return err
		}
		res.Draft, res.From, res.To = d, from, t.To
		if ev == EvSentDetected {
			// Nur IDs: die lokale ID der gesendeten Nachricht, nie ihr Inhalt.
			_, err := x.AppendAudit(s.systemEvent(now, OpSentDetected, reason, d, from, t.To, map[string]any{"sent_message_id": sentMessageID}))
			return err
		}
		var aid int64
		if res.Approval != nil {
			aid = res.Approval.ID
		}
		return s.record(x, now, d, from, t.To, aid, "")
	})
	return res, err
}

// OpSentDetected ist die Operation im Audit, wenn der Sync den Versand
// eines Entwurfs erkannt hat (E5; Grund message_id oder fallback).
const OpSentDetected = "drafts.sent_detected"

// systemEvent baut den Audit-Eintrag eines Systemereignisses: Akteur
// system, nur IDs und Zustände, nie Inhalt.
func (s *Service) systemEvent(now time.Time, op, reason string, d store.Draft, from, to Status, extra ...map[string]any) audit.Event {
	params := map[string]any{"draft_id": d.ID, "from": from, "to": to}
	for _, e := range extra {
		for k, v := range e {
			params[k] = v
		}
	}
	targets := []string{"draft:" + strconv.FormatInt(d.ID, 10)}
	if d.ApprovalID > 0 {
		params["approval_id"] = d.ApprovalID
		targets = append(targets, "approval:"+strconv.FormatInt(d.ApprovalID, 10))
	}
	b, _ := json.Marshal(params)
	return audit.Event{TS: now, ActorType: audit.ActorSystem, KeyID: "system", Operation: op, ParamsRedacted: b,
		Targets: targets, Result: audit.ResultOK, Reason: reason, PolicyVersion: s.PolicyVersion}
}

// ExpireApprovals setzt alle Entwürfe (accountID 0 = aller Konten), deren
// offene oder erteilte Freigabe abgelaufen ist, nach draft zurück und
// markiert die Freigabe expired; je Entwurf ein Audit-Eintrag (Akteur
// system, Grund approval_expired) in derselben Transaktion. Liefert die
// IDs der zurückgesetzten Entwürfe. Aufgerufen beim Lesen (Get, List) und
// ab S2.6 beim Sync.
func (s *Service) ExpireApprovals(ctx context.Context, accountID int64) ([]int64, error) {
	now := s.now()
	var out []int64
	err := s.tx(ctx, func(x *store.DraftTx) error {
		ids, err := x.ExpiredDraftIDs(now, accountID)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := s.expireOne(x, id, now); err != nil {
				return err
			}
		}
		out = ids
		return nil
	})
	return out, err
}

func (s *Service) expireOne(x *store.DraftTx, id int64, now time.Time) error {
	d, err := x.Draft(id)
	if err != nil {
		return err
	}
	from := Status(d.Status)
	t, err := Next(from, EvExpire)
	if err != nil {
		return err
	}
	if _, err := applyApproval(x, &d, t.Approval, now, "", ""); err != nil {
		return err
	}
	d.Status, d.UpdatedAt = string(t.To), now
	if err := x.UpdateDraft(d); err != nil {
		return err
	}
	_, err = x.AppendAudit(s.systemEvent(now, "drafts.expire", ReasonApprovalExpired, d, from, t.To))
	return err
}

// Get liest einen Entwurf; eine abgelaufene Freigabe fällt vorher zurück
// (E7: „beim nächsten Lesen"). Im Trockenlauf (As mit dryRun) wird der
// Rückfall nur berechnet, nicht gespeichert: Das Ergebnis zeigt den Stand
// nach dem Rückfall, Datenbank und Audit bleiben unverändert (K8).
func (s *Service) Get(ctx context.Context, id int64) (store.Draft, error) {
	d, err := s.Store.Draft(ctx, id)
	if err != nil {
		return d, err
	}
	if d.Status != string(StatusPendingApproval) && d.Status != string(StatusReady) {
		return d, nil
	}
	if s.dry {
		now := s.now()
		err := s.tx(ctx, func(x *store.DraftTx) error {
			ids, err := x.ExpiredDraftIDs(now, d.AccountID)
			if err != nil {
				return err
			}
			for _, e := range ids {
				if e == id {
					if err := s.expireOne(x, id, now); err != nil {
						return err
					}
				}
			}
			d, err = x.Draft(id)
			return err
		})
		return d, err
	}
	n, err := s.ExpireApprovals(ctx, d.AccountID)
	if err != nil || len(n) == 0 {
		return d, err
	}
	return s.Store.Draft(ctx, id)
}

// List liest Entwürfe nach Filter; abgelaufene Freigaben fallen vorher zurück.
func (s *Service) List(ctx context.Context, f store.DraftFilter) ([]store.Draft, error) {
	if _, err := s.ExpireApprovals(ctx, f.AccountID); err != nil {
		return nil, err
	}
	return s.Store.ListDrafts(ctx, f)
}

// OwnAddresses liefert die eigenen Adressen eines Kontos (E9): Kontoadresse,
// Anmeldename (wenn Adresse), Identitäten samt Reply-To und Aliasse; klein,
// ohne Dubletten. „Allen antworten" lässt sie weg (S2.2), die
// Versanderkennung nutzt sie (S2.6).
func (s *Service) OwnAddresses(ctx context.Context, accountID int64) ([]string, error) {
	return s.Store.OwnAddresses(ctx, accountID)
}

// IsNotFound meldet, ob err ein unbekannter Entwurf, eine unbekannte
// Freigabe oder Identität ist.
func IsNotFound(err error) bool {
	return errors.Is(err, store.ErrDraftNotFound) || errors.Is(err, store.ErrApprovalNotFound) ||
		errors.Is(err, store.ErrIdentityNotFound) || errors.Is(err, store.ErrAccountNotFound)
}
