package mcp

// notOverMCP nennt die Operationen des Katalogs, die bewusst kein Tool
// sind, mit Grund (docs/mcp.md, Abschnitt 2). Der Vertragstest verlangt,
// dass jede Operation entweder ein Tool hat oder hier steht.
var notOverMCP = map[string]string{
	"accounts.add":    "admin: credentials, human only (CLI)",
	"accounts.test":   "admin: uses the account password, human only (CLI)",
	"accounts.remove": "admin: human only (CLI)",
	"sync.run":        "mail_sync_now needs the job type sync (the MCP process does not sync itself, docs/architektur.md section 7); later",
	"keys.create":     "admin: key management is for humans only",
	"keys.list":       "admin: key management is for humans only",
	"keys.revoke":     "admin: key management is for humans only",
	"audit.export":    "admin: writes files to a second location, human only",
	"audit.verify":    "checks export files on disk; a human task (CLI audit verify)",
	// Kein Tool, aber über MCP erreichbar: Resource (resources.go).
	"mailboxes.overview": "offered as the resource mail://{account}/mailbox/{id}/overview; mail_search covers the same as a tool",
}
