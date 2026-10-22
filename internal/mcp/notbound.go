package mcp

// notOverMCP nennt die Operationen des Katalogs, die bewusst kein Tool
// sind, mit Grund (docs/mcp.md, Abschnitt 2). Der Vertragstest verlangt,
// dass jede Operation entweder ein Tool hat oder hier steht.
var notOverMCP = map[string]string{
	"accounts.add":    "admin: credentials, human only (CLI)",
	"accounts.test":   "admin: uses the account password, human only (CLI)",
	"accounts.remove": "admin: human only (CLI)",
	"accounts.set":    "admin, human only (A02 E2): own addresses decide whom reply-all leaves out, and it replaces stored passwords; CLI account set",
	"sync.run":        "mail_sync_now needs the job type sync (the MCP process does not sync itself, docs/architektur.md section 7); later",
	"import.run":      "admin: reads local files (mbox, maildir) into the archive; human only (CLI)",
	"keys.create":     "admin: key management is for humans only",
	"keys.list":       "admin: key management is for humans only",
	"keys.revoke":     "admin: key management is for humans only",
	"audit.export":    "admin: writes files to a second location, human only",
	"audit.verify":    "checks export files on disk; a human task (CLI audit verify)",
	// Entwürfe (S2.3): Die Tools mail_create_draft, mail_update_draft,
	// mail_get_draft, mail_list_drafts, mail_request_ready und
	// mail_get_approval kommen mit Schritt S2.8 (A02); bis dahin nur CLI.
	"drafts.create":        "not yet: the tool mail_create_draft comes with step S2.8 (CLI draft new|reply|forward until then)",
	"drafts.update":        "not yet: the tool mail_update_draft comes with step S2.8 (CLI draft edit until then)",
	"drafts.get":           "not yet: the tool mail_get_draft and the resource mail://{account}/draft/{id} come with step S2.8",
	"drafts.list":          "not yet: the tool mail_list_drafts comes with step S2.8 (CLI draft list until then)",
	"drafts.request_ready": "not yet: the tool mail_request_ready comes with step S2.8 (CLI draft request until then)",
	"approvals.get":        "not yet: the tool mail_get_approval comes with step S2.8 (CLI approval show until then)",
	// Nur für Menschen (A02 E2), dauerhaft ohne Tool: Ein Agent kann einen
	// Entwurf vorlegen, aber nie freigeben, ablehnen, verwerfen oder für
	// gesendet erklären, und er verwaltet keine Absender.
	"drafts.approve":         "human only (A02 E2): an agent may submit a draft but never approve it; CLI approve",
	"drafts.reject":          "human only (A02 E2): the decision on a submitted draft is the human's; CLI reject",
	"drafts.abandon":         "human only (A02 E2): closing a draft is the human's decision; CLI draft abandon",
	"drafts.mark_sent":       "human only (A02 E2): only the human knows what was sent; CLI draft mark-sent",
	"identities.add":         "admin, human only (A02 E2): identities decide the sender of every draft; CLI identity add",
	"identities.list":        "admin, human only (A02 E2); CLI identity list",
	"identities.set_default": "admin, human only (A02 E2); CLI identity set-default",
	"identities.remove":      "admin, human only (A02 E2); CLI identity remove",
	// Kein Tool, aber über MCP erreichbar: Resource (resources.go).
	"mailboxes.overview": "offered as the resource mail://{account}/mailbox/{id}/overview; mail_search covers the same as a tool",
}
