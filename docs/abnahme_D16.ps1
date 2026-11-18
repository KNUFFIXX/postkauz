<#
.SYNOPSIS
    Abnahme D16 (PostKauz, Auftrag A10 "Geplant ist nicht gesendet"):
    Umstellung des Dauerbetriebs von 0.3.0-rc7 auf 0.3.0-rc8 mit zwei
    vollstaendigen Laeufen und Rueckweg (Teil A) und fuenf Proben zum
    geplanten Senden im eigenen Gmail-Konto (Teil B).

.DESCRIPTION
    Laeuft auf dem Buero-PC (Windows 11, Windows PowerShell 5.1) am
    Live-Datenverzeichnis des Dauerbetriebs, ohne Go und ohne Docker.
    Anleitung: docs/abnahme_D16.md. Claude fuehrt das Skript aus.
    Muster: abnahme_D15.ps1 (Tausch, zwei Laeufe, Rueckweg, Teil B ueber den
    MCP-Server), dazu die Punkte aus der Gegenpruefung von D15 (Auftrag A10,
    Teil 0 (c); abnahme_D16.md, Abschnitt 8).

    Teil A "Umstellung" (Standard):
      Teil 0  Voraussetzungen: beide Programme (Version, Pruefsumme),
              Konten (-Accounts genau wie accounts list), Aufgaben (-Tasks
              ist Pflicht), NEU 0.6b: keine Sicherungsaufgabe in -Tasks
              (Aufgabe, deren Aktion postkauz backup aufruft; FAIL),
              NEU 0.8: je Konto mailbox list; hat ein Konto einen Ordner mit
              Rolle all, aber keinen mit Rolle sent, steht ein Hinweis (WARN)
              im Protokoll (A10 Teil 1 c). Ein FAIL in Teil 0 aendert nichts
              (Zeile "nichts geaendert"); darauf beruht der Lesetest.
      Teil 1  Mit dem alten Programm (rc7): Aufgaben anhalten und
              abschalten (mit -KeepStdioMcp laeuft der stdio-MCP der
              Claude-App weiter), doctor vorher (database ok, Schema 22),
              offene Entwuerfe vorher, Marke im Audit, Sicherung nur mit
              -BackupTo.
      Teil 2  Programm tauschen: altes als postkauz-rc7.exe aufheben
              (umbenennen statt ueberschreiben mit -KeepStdioMcp), neues an
              seine Stelle; doctor nachher: Schema unveraendert, keine neue
              Warnung, Zeile attach_guard da.
      Teil 3  ZWEI vollstaendige Laeufe mit rc8 (je Konto sync; ein Fehler
              ist nur WARN, wenn ein sofortiger zweiter Versuch ohne Fehler
              endet); nach jedem Lauf alle seit der Marke geschlossenen
              Entwuerfe mit Status und Grund aus dem Audit (neu in A10: Grund
              scheduled); geplante Entwuerfe (remote scheduled) mit Termin;
              nach einem FAIL im ersten Lauf kein zweiter; doctor ohne neue
              Warnung, Schema 22; audit verify ok.
      Teil 4  Aufgaben wieder einschalten und starten, nur wenn Teil 3
              vollstaendig und ohne FAIL lief. Sonst kommt nach dem Tausch
              zuerst das alte Programm zurueck und die Aufgaben laufen mit
              ihm an. Strg+C ist im Abschluss abgeschaltet.

    Teil B (-TeilB -SelfAccount <konto>, Mitmach, nach Teil A):
      B.0  Probe-Entwurf an sich selbst; der Mensch sendet ihn in Gmail
           gewoehnlich: sent_by_human, und die gesendete Nachricht
           (sent_message_id) liegt laut show in einem Ordner mit Rolle sent
           (A10 Teil 1).
      B.1  Neuer Probe-Entwurf; der Mensch plant ihn in Gmail fuer + 3 Tage:
           remote scheduled, scheduled_for, Status offen, nie sent_by_human;
           Audit drafts.scheduled_detected; draft edit (Trockenlauf) und mit
           -AgentKeyFile mail_update_draft (Trockenlauf): denied
           (draft_scheduled); nach einem weiteren Lauf weiter geplant.
      B.4a-c (zwischen B.1 und B.2) search "is:scheduled" --json, show
           --json und mit -AgentKeyFile mail_search: die geplante Nachricht
           mit roles (all, nicht sent) und scheduled_for.
      B.2  Der Mensch bricht den Plan in Gmail ab: remote taken_over, Status
           offen, Audit drafts.remote_check Grund schedule_cancelled; nach
           einem weiteren Lauf weiter taken_over.
      B.4d-e (nach B.2) search "is:draft" --json (und mail_search): die
           Nachricht im Entwuerfe-Ordner mit Rolle drafts in roles.
      B.3  Der Mensch plant denselben Entwurf fuer + 5 Minuten und laesst ihn
           senden: sent_by_human; Grund aus drafts.sent_detected (erwartet
           scheduled; fallback, thread oder subject nur INFO: Gmail hat beim
           Versand eine neue Nachricht angelegt); Kopie mit Rolle sent.
      Nach einem unerwarteten Status folgt kein Schritt, der davon abhaengt
      (B.4, B.2 und B.3 brauchen B.1; B.4d/e und B.3 brauchen B.2); sie
      stehen dann als SKIP im Protokoll. B.0 und B.1 sind unabhaengig. Die
      Aufgaben laufen dabei weiter. Mit -AgentKeyFile laufen Anlegen,
      mail_update_draft und mail_search ueber den MCP-Server mit dem
      Schluessel eines Agenten.

    Modus "Rueckweg" (-Rueckweg): A10 hat keine Migration (Schema 22). Das
      alte Programm (postkauz-rc7.exe) kommt zurueck und laeuft weiter.

    Jede Pruefung ist eine Zeile "OK <Nr> ...", "FAIL <Nr> ...", "WARN
    <Nr> ...", "INFO <Nr> ..." oder "SKIP <Nr> ...". Betreff, Adressen,
    Mailtext, Ordnernamen, Konto- und Aufgabennamen stehen nie im Protokoll
    (Konten als "Konto <n>", Aufgaben als "Aufgabe <n>", auch in
    doctor-Warnungen und Ausnahmetexten); nur IDs, Zustaende, Gruende,
    Zaehler, Rollen und Termine. Letzte Zeile: "D16: <n> OK, <m> FAIL, <w>
    WARN, <s> SKIP". Exit 0 nur ohne FAIL. Ein vorhandenes Protokoll wird
    unter einem Namen mit Zeitstempel beiseitegelegt.

.PARAMETER Exe
    Das Programm des Dauerbetriebs (Standard C:\PostKauz\postkauz.exe).

.PARAMETER NewExe
    Das neue Programm (postkauz-windows-amd64.exe aus dem Build 0.3.0-rc8).
    Neben ihm muss SHA256SUMS liegen. Nur fuer Teil A.

.PARAMETER DataDir
    Live-Datenverzeichnis (Standard C:\PostKauz\data).

.PARAMETER Config
    Live-Konfiguration (Standard C:\PostKauz\config.toml).

.PARAMETER BackupTo
    Optional, empfohlen: Ordner einer Sicherung vor dem Tausch (betrieb.md
    2.2/2.3) auf einem verschluesselten Datentraeger. A10 hat keine
    Migration; ohne -BackupTo keine Sicherung (INFO 1.4).

.PARAMETER Accounts
    Alle Konten (mit Komma); Teil 3 und der Rueckweg syncen je Konto, 4.1
    prueft je Konto ein sync --watch, 0.8 liest je Konto mailbox list.

.PARAMETER Tasks
    Pflicht in Teil A und im Rueckweg: alle geplanten Aufgaben, die
    PostKauz dauerhaft betreiben (sync --watch, mcp --http, audit export),
    mit Komma getrennt (Namen nicht raten). NICHT die Sicherungsaufgabe
    (postkauz backup): 0.6b und R.0f pruefen das (FAIL).

.PARAMETER ExpectSha256
    SHA-256 der neuen Programmdatei (aus dem Protokoll des Builds).

.PARAMETER ExpectVersion
    Version des neuen Programms (Standard 0.3.0-rc8).

.PARAMETER OldVersion
    Version des alten Programms (Standard 0.3.0-rc7). Fuer den Lesetest
    0.0.0-probe: dann ist 0.4 FAIL, und das Skript aendert nichts.

.PARAMETER Rueckweg
    Statt der Umstellung den Rueckweg auf das alte Programm pruefen.

.PARAMETER KeepStdioMcp
    Der stdio-MCP der Claude-App darf weiterlaufen (Tausch per Umbenennen;
    nur ohne Migration moeglich, A10 hat keine).

.PARAMETER TeilB
    Teil B statt Teil A ausfuehren (braucht -SelfAccount).

.PARAMETER SelfAccount
    Teil B: das eigene Gmail-Konto, in dem die Probe-Entwuerfe entstehen
    (nie das geteilte Postfach ohne Anweisung des Betreibers).

.PARAMETER SelfAddress
    Teil B, optional: eigene Adresse des Kontos (Empfaenger der Proben).
    Ohne Angabe die Adresse aus accounts list. Steht nie im Protokoll.

.PARAMETER AgentKeyFile
    Teil B, optional: Datei mit dem API-Schluessel eines Agenten (pk_key_...,
    etwa der Schluessel der Claude-App). Dann laufen Anlegen,
    mail_update_draft und mail_search ueber den MCP-Server (postkauz mcp,
    stdio) mit diesem Schluessel. Schluessel und Dateiname stehen nie im
    Protokoll.

.PARAMETER WaitMinutes
    Teil B: so lange wartet das Skript je Schritt auf den Menschen im
    Webmail (B.0 senden, B.1 planen, B.2 Plan abbrechen, B.3 erneut planen;
    Standard 20). B.3 wartet danach bis zum Termin + 20 Minuten.

.PARAMETER LogFile
    Protokolldatei. Standard: D16_windows.txt (Teil B: D16_teilB.txt)
    neben dem Programm. Fuer den Lesetest eine eigene Datei angeben.

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d16\abnahme_D16.ps1 -Exe C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data -Config C:\PostKauz\config.toml -NewExe C:\PostKauz\d16\postkauz-windows-amd64.exe -Accounts a,b,c -Tasks "PostKauz sync watch a,PostKauz sync watch b,PostKauz sync watch c,PostKauz audit export,PostKauz mcp http" -KeepStdioMcp -BackupTo E:\Sicherung\d16

.EXAMPLE
    Lesetest vor dem Tausch (aendert nichts; erwartet nur 0.4 und E.2 als FAIL):
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d16\abnahme_D16.ps1 -Exe C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data -Config C:\PostKauz\config.toml -NewExe C:\PostKauz\d16\postkauz-windows-amd64.exe -Accounts a,b,c -Tasks "PostKauz sync watch a,PostKauz sync watch b,PostKauz sync watch c,PostKauz audit export,PostKauz mcp http" -KeepStdioMcp -OldVersion 0.0.0-probe -LogFile C:\PostKauz\d16\D16_lesetest.txt

.EXAMPLE
    Rueckweg:
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d16\abnahme_D16.ps1 -Rueckweg -Exe C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data -Config C:\PostKauz\config.toml -Accounts a,b,c -Tasks "PostKauz sync watch a,PostKauz sync watch b,PostKauz sync watch c,PostKauz audit export,PostKauz mcp http" -KeepStdioMcp

.EXAMPLE
    Teil B:
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d16\abnahme_D16.ps1 -TeilB -Exe C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data -Config C:\PostKauz\config.toml -SelfAccount a -AgentKeyFile C:\PostKauz\d16\agent.key
#>
param(
    [string] $Exe = 'C:\PostKauz\postkauz.exe',
    [string] $NewExe = '',
    [string] $DataDir = 'C:\PostKauz\data',
    [string] $Config = 'C:\PostKauz\config.toml',
    [string] $BackupTo = '',
    [string[]] $Accounts = @(),
    [string[]] $Tasks = @(),
    [string] $ExpectSha256 = '',
    [string] $ExpectVersion = '0.3.0-rc8',
    [string] $OldVersion = '0.3.0-rc7',
    [switch] $Rueckweg,
    [switch] $KeepStdioMcp,
    [switch] $TeilB,
    [string] $SelfAccount = '',
    [string] $SelfAddress = '',
    [string] $AgentKeyFile = '',
    [int] $WaitMinutes = 20,
    [string] $LogFile = ''
)

$ErrorActionPreference = 'Stop'
# Mit powershell.exe -File kommen Listen ("a,b") in PS 5.1 als ein Text an:
# selbst trennen.
function Split-List {
    param([string[]] $L)
    return @($L | ForEach-Object { $_ -split ',' } | ForEach-Object { $_.Trim() } | Where-Object { $_ })
}
$Accounts = Split-List $Accounts
$Tasks = Split-List $Tasks
$SelfAccount = $SelfAccount.Trim()
$script:Utf8 = New-Object System.Text.UTF8Encoding $false
$script:NOk = 0
$script:NFail = 0
$script:NWarn = 0
$script:NSkip = 0
$script:Log = $null
$script:LogBroken = $false
$script:Fails = New-Object System.Collections.Generic.List[string]
$script:WasEnabled = @{}
$script:Started = Get-Date
$script:Mcp = $null
$script:McpIn = $null
$script:McpDead = ''
# Marke im Audit (wie D13): nur Abschluesse mit hoeherer ID zaehlen; -1 =
# keine Marke, dann ab $script:AuditSince (Zeitstempel, RFC 3339).
$script:AuditMark = [int64] -1
$script:AuditSince = ''
# Teil 3 vollstaendig durchgelaufen: nur dann ist ein Ergebnis ohne FAIL
# eine bestandene Abnahme.
$script:Complete = $false
# Abschlussmerker fuer Teil B (alle Schritte B.0 bis B.3 sind gelaufen;
# ein abgebrochener oder wegen eines FAIL ausgelassener Schritt zaehlt als
# gelaufen, der FAIL steht schon im Protokoll).
$script:CompleteB = $false
# Aufgaben am Ende nicht wieder einschalten (Rueckweg gescheitert).
$script:KeepTasksOff = $false
# Teil 1 begonnen (Aufgaben angehalten): ohne das hat das Skript nichts
# geaendert (Zeile "nichts geaendert" am Ende, Lesetest).
$script:Changed = $false
# Teil B: Ergebnisse der Schritte fuer die abhaengigen Schritte. B1Ok: der
# Entwurf aus B.1 ist geplant (B.4a-c, B.2 und B.3 haengen davon ab); B2Ok:
# der Plan ist abgebrochen und der Entwurf taken_over (B.4d-e und B.3).
$script:B0Draft = ''
$script:B1Draft = ''
$script:B1Subject = ''
$script:B1Msg = ''
$script:B1At = $null
$script:B1Ok = $false
$script:B2Ok = $false
# 0g (f): Konto- und Aufgabennamen werden in JEDER Protokollzeile ersetzt
# (auch doctor-Warnungen und Ausnahmetexte in X, 4.1, E.1): Name
# (kleingeschrieben) -> "Konto <n>" bzw. "Aufgabe <n>".
$script:NameLabels = @{}
$script:NamePattern = ''
$script:NAccLabel = 0
try { [Console]::OutputEncoding = $script:Utf8 } catch { }

# ---------------------------------------------------------------- Hilfen

# Maskiert API-Schluessel und Adressen (zweite Sicherung; das Protokoll
# enthaelt ohnehin nur Zaehler, IDs und Zustaende).
function Hide-Secrets {
    param([string] $Text)
    if ($null -eq $Text) { return '' }
    $t = [regex]::Replace($Text, 'pk_key_[A-Za-z0-9_-]+', 'pk_key_********')
    $t = [regex]::Replace($t, '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}', '<adresse>')
    return $t
}

# Nachtrag 2, N8 (D13): Aufgaben stehen im Protokoll nur als laufende
# Nummer in der Reihenfolge von -Tasks (ihre Namen koennen Kontonamen
# tragen).
function Get-TaskLabel {
    param([string] $Name)
    $i = [array]::IndexOf([string[]] $Tasks, $Name)
    return ('Aufgabe ' + ($i + 1))
}

# Namen aus -Tasks in einer Fehlermeldung (Disable-ScheduledTask nennt sie
# mitunter) durch die Nummer ersetzen.
function Hide-TaskNames {
    param([string] $Text)
    if ($null -eq $Text) { return '' }
    $t = $Text
    foreach ($tn in @($Tasks | Sort-Object { $_.Length } -Descending)) {
        if ($tn) { $t = [regex]::Replace($t, [regex]::Escape($tn), (Get-TaskLabel $tn), 'IgnoreCase') }
    }
    return $t
}

# 0g (f): Muster aus allen bekannten Konto- und Aufgabennamen, laengste
# zuerst, nur als ganzes Wort (davor und danach kein Buchstabe, keine
# Ziffer, kein "_", ".", "@" oder "-").
function Update-NamePattern {
    $keys = @($script:NameLabels.Keys | Sort-Object { $_.Length } -Descending | ForEach-Object { [regex]::Escape([string] $_) })
    if ($keys.Count -eq 0) { $script:NamePattern = ''; return }
    $script:NamePattern = '(?<![A-Za-z0-9_.@-])(?:' + ($keys -join '|') + ')(?![A-Za-z0-9_.@-])'
}

function Register-TaskNames {
    foreach ($tn in $Tasks) {
        if (-not $tn) { continue }
        $k = $tn.ToLowerInvariant()
        if (-not $script:NameLabels.ContainsKey($k)) { $script:NameLabels[$k] = Get-TaskLabel $tn }
    }
    Update-NamePattern
}

function Register-AccountNames {
    param([string[]] $Names)
    foreach ($n in $Names) {
        if (-not $n) { continue }
        $k = $n.Trim().ToLowerInvariant()
        if (-not $k -or $script:NameLabels.ContainsKey($k)) { continue }
        $script:NAccLabel++
        $script:NameLabels[$k] = 'Konto ' + $script:NAccLabel
    }
    Update-NamePattern
}

# Ersetzt Konto- und Aufgabennamen in einem Text in einem Durchgang. Gelingt
# das nicht, kommt die Zeile nicht ins Protokoll (fail closed).
function Hide-Names {
    param([string] $Text)
    if ($null -eq $Text) { return '' }
    if (-not $script:NamePattern) { return $Text }
    try {
        $eval = [System.Text.RegularExpressions.MatchEvaluator] { param($m) [string] $script:NameLabels[$m.Value.ToLowerInvariant()] }
        return [regex]::Replace($Text, $script:NamePattern, $eval, [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)
    } catch {
        return '[Zeile nicht maskierbar, weggelassen]'
    }
}

# Schreiben ins Protokoll mit try/catch (Sammelpunkt 0e): Ein voller
# Datentraeger oder eine gesperrte Datei bricht die Abnahme nicht mitten im
# Tausch ab; die Zeile steht dann nur auf dem Bildschirm, und die
# Schlusszeile nennt den Fehler. Seit D15 ohne Konto- und Aufgabennamen.
function Write-Log {
    param([string] $Text)
    $t = Hide-Names (Hide-Secrets $Text)
    if ($script:Log) {
        try { [System.IO.File]::AppendAllText($script:Log, $t + "`r`n", $script:Utf8) } catch { $script:LogBroken = $true }
    }
    try { Write-Host $t } catch { }
}

function Add-Warn {
    param([string] $Nr, [string] $Desc, [string] $Detail = '')
    $script:NWarn++
    Write-Log ('WARN ' + $Nr + ' ' + $Desc)
    if ($Detail) {
        foreach ($l in ((Limit-Text $Detail) -split "`r?`n")) { Write-Log ('      ' + $l) }
    }
}

function Limit-Text {
    param([string] $Text, [int] $MaxLines = 25, [int] $MaxChars = 1800)
    if ($null -eq $Text) { return '' }
    $t = $Text.TrimEnd()
    $cut = $false
    $lines = $t -split "`r?`n"
    if ($lines.Count -gt $MaxLines) { $t = ($lines[0..($MaxLines - 1)] -join "`n"); $cut = $true }
    if ($t.Length -gt $MaxChars) { $t = $t.Substring(0, $MaxChars); $cut = $true }
    if ($cut) { $t = $t + "`n...[gekuerzt]" }
    return $t
}

function Add-Check {
    param([string] $Nr, [string] $Desc, $Ok, [string] $Detail = '')
    if ($Ok) { $script:NOk++; $tag = 'OK' } else { $script:NFail++; $tag = 'FAIL'; $script:Fails.Add($Nr + ' ' + $Desc) }
    Write-Log ($tag + ' ' + $Nr + ' ' + $Desc)
    if ($Detail) {
        foreach ($l in ((Limit-Text $Detail) -split "`r?`n")) { Write-Log ('      ' + $l) }
    }
}

function Add-Info {
    param([string] $Nr, [string] $Desc, [string] $Detail = '')
    Write-Log ('INFO ' + $Nr + ' ' + $Desc)
    if ($Detail) {
        foreach ($l in ((Limit-Text $Detail) -split "`r?`n")) { Write-Log ('      ' + $l) }
    }
}

function Add-Skip {
    param([string] $Nr, [string] $Desc)
    $script:NSkip++
    Write-Log ('SKIP ' + $Nr + ' ' + $Desc)
}

# Argumente nach den Regeln der Windows-C-Laufzeit quoten (Go liest sie so).
function ConvertTo-ArgString {
    param([string[]] $List)
    $parts = New-Object System.Collections.Generic.List[string]
    foreach ($a in $List) {
        if ($null -eq $a -or $a -eq '') { $parts.Add('""'); continue }
        if ($a -notmatch '[\s"]') { $parts.Add($a); continue }
        $s = '"'
        $bs = 0
        foreach ($ch in $a.ToCharArray()) {
            if ($ch -eq [char]92) { $bs++; continue }
            if ($ch -eq [char]34) { $s += ('\' * (2 * $bs + 1)) + '"'; $bs = 0; continue }
            if ($bs -gt 0) { $s += ('\' * $bs); $bs = 0 }
            $s += $ch
        }
        $s += ('\' * (2 * $bs)) + '"'
        $parts.Add($s)
    }
    return ($parts -join ' ')
}

# postkauz.exe ($Prog) ohne Fenster, mit --data-dir und --config, stdout und
# stderr als UTF-8, mit Zeitgrenze; ohne Umgebungsvariablen von PostKauz.
# Text enthaelt die Argumente; er kommt nur dort ins Protokoll, wo keine
# Adressen oder Betreffs in den Argumenten stehen.
function Invoke-Pk {
    param([string] $Prog, [string[]] $A, [int] $TimeoutSec = 1800, [switch] $NoDirs)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $Prog
    $all = $A
    if (-not $NoDirs) { $all = @('--data-dir', $DataDir, '--config', $Config) + $A }
    $psi.Arguments = ConvertTo-ArgString $all
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.StandardOutputEncoding = $script:Utf8
    $psi.StandardErrorEncoding = $script:Utf8
    foreach ($k in @('POSTKAUZ_KEY', 'POSTKAUZ_KEY_FILE', 'POSTKAUZ_DATA_DIR', 'POSTKAUZ_CONFIG', 'POSTKAUZ_POLICY', 'POSTKAUZ_PROFILE')) {
        if ($psi.EnvironmentVariables.ContainsKey($k)) { $psi.EnvironmentVariables.Remove($k) }
    }
    $p = [System.Diagnostics.Process]::Start($psi)
    $p.StandardInput.Close()
    $to = $p.StandardOutput.ReadToEndAsync()
    $te = $p.StandardError.ReadToEndAsync()
    $timedOut = $false
    if (-not $p.WaitForExit($TimeoutSec * 1000)) { $timedOut = $true; try { $p.Kill() } catch { } }
    $p.WaitForExit()
    [void] $to.Wait(10000)
    [void] $te.Wait(10000)
    $code = -1
    if (-not $timedOut) { $code = $p.ExitCode }
    $out = [string] $to.Result
    $err = [string] $te.Result
    $text = '> postkauz ' + ($A -join ' ') + "`n" + $out
    if ($err) { $text = $text.TrimEnd() + "`n[stderr] " + $err }
    if ($timedOut) { $text = $text.TrimEnd() + "`n[Zeitgrenze erreicht, Prozess beendet]" }
    $text = $text.TrimEnd() + "`n[exit " + $code + ']'
    return New-Object PSObject -Property @{ Exit = $code; Out = $out; Err = $err; Text = $text }
}

function Get-Json {
    param([string] $Text)
    try { return ($Text.Trim() | ConvertFrom-Json) } catch { return $null }
}

function Get-Sha256 {
    param([string] $Path)
    $sha = [System.Security.Cryptography.SHA256]::Create()
    $fs = [System.IO.File]::OpenRead($Path)
    try { return (($sha.ComputeHash($fs) | ForEach-Object { $_.ToString('x2') }) -join '') } finally { $fs.Dispose() }
}

# Grund einer fachlichen Ablehnung (Exit 3), etwa invalid_address; nur der
# Code, nie die Meldung.
function Get-DenyReason {
    param($R)
    $m = [regex]::Match(($R.Out + "`n" + $R.Err), 'denied \(([a-z_]+)\)')
    if ($m.Success) { return $m.Groups[1].Value }
    $m = [regex]::Match(($R.Out + "`n" + $R.Err), '"code"\s*:\s*"([a-z_]+)"')
    if ($m.Success) { return $m.Groups[1].Value }
    if ($R.Out + $R.Err -match 'invalid_address') { return 'invalid_address' }
    return ''
}

# Ein Fehler von Get-CimInstance ist ein Fehler, nicht "0 Prozesse"
# (Sammelpunkt 0e): Abbruch mit Grund.
function Get-PkProcesses {
    try {
        return @(Get-CimInstance Win32_Process -Filter "Name = 'postkauz.exe'" -OperationTimeoutSec 15 -ErrorAction Stop)
    } catch {
        throw ('D16-ABBRUCH: Prozessliste nicht lesbar (Get-CimInstance): ' + $_.Exception.Message)
    }
}

function Test-StdioMcp {
    param($P)
    $c = [string] $P.CommandLine
    return ($c -match '(?i)\smcp(\s|$)' -and $c -notmatch '(?i)--http')
}

function Get-BlockingPk {
    $all = Get-PkProcesses
    if ($KeepStdioMcp) { return @($all | Where-Object { -not (Test-StdioMcp $_) }) }
    return $all
}

# Liegt das Programm des Prozesses im Ordner von -Exe? Nur solche beendet
# das Skript (Nachbesserung 1); andere nennt es nur.
function Test-OwnPk {
    param($P)
    $path = [string] $P.ExecutablePath
    if (-not $path) { return $false }
    return ([string] (Split-Path -Parent $path)).TrimEnd('\') -ieq $script:ExeDir.TrimEnd('\')
}

function Test-AuditTask {
    param([string] $Name)
    return ($Name -match '(?i)audit export')
}

# Gehoert ein Hilfsskript (run-sync-watch.ps1) zu diesem Dauerbetrieb? Ja,
# wenn seine Befehlszeile das Datenverzeichnis, die Konfiguration, das
# Programm oder den Ordner von -Exe nennt oder in einer Aufgabe aus -Tasks
# steht (Nachbesserung 2; Buero-PC: C:\PostKauz\run-sync-watch.ps1 -Konto <name>).
function Test-OwnHelper {
    param($P, [string[]] $TaskArgs)
    $c = ([string] $P.CommandLine).ToLowerInvariant()
    foreach ($k in @($DataDir, $Config, $script:ExeFull, $script:ExeDir)) {
        if ($k -and (Test-PathIn $c ([string] $k).ToLowerInvariant())) { return $true }
    }
    foreach ($a in $TaskArgs) {
        $m = [regex]::Match($a, '(?i)[^\s"]*run-?sync-?watch\.ps1')
        if ($m.Success -and (Test-PathIn $c $m.Value.ToLowerInvariant())) { return $true }
    }
    return $false
}

# Steht der Pfad $K in der Befehlszeile $C als ganzer Pfad (Nachtrag 1, A7)?
# Davor Anfang, Leerzeichen, Anfuehrungszeichen oder "="; danach Ende,
# Leerzeichen, Anfuehrungszeichen oder "\" (ein Unterordner). So gehoert
# C:\PostKauz-Test nicht zu C:\PostKauz.
function Test-PathIn {
    param([string] $C, [string] $K)
    $K = $K.TrimEnd('\')
    if (-not $K) { return $false }
    $i = $C.IndexOf($K)
    while ($i -ge 0) {
        $before = ($i -eq 0) -or (' "''='.Contains([string] $C[$i - 1]))
        $j = $i + $K.Length
        $after = ($j -ge $C.Length) -or (' "''\'.Contains([string] $C[$j]))
        if ($before -and $after) { return $true }
        $i = $C.IndexOf($K, $i + 1)
    }
    return $false
}

function Stop-Tasks {
    param([string] $Nr)
    $taskArgs = @()
    foreach ($tn in $Tasks) {
        $t = Get-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue
        if ($null -ne $t) { foreach ($ac in $t.Actions) { $taskArgs += ([string] $ac.Execute + ' ' + [string] $ac.Arguments) } }
    }
    foreach ($tn in $Tasks) {
        $t = Get-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue
        if ($null -eq $t) { Add-Info $Nr ((Get-TaskLabel $tn) + ' (Nummer in -Tasks) nicht vorhanden'); continue }
        if (-not $script:WasEnabled.ContainsKey($tn)) { $script:WasEnabled[$tn] = ($t.State -ne 'Disabled') }
        try { Stop-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue } catch { }
        try {
            Disable-ScheduledTask -TaskName $tn -ErrorAction Stop | Out-Null
        } catch {
            # Meist fehlende Rechte (Aufgabe "mit hoechsten Privilegien" oder
            # von einem anderen Benutzer angelegt): Abschnitt 3 der Anleitung.
            Add-Check $Nr ((Get-TaskLabel $tn) + ' (Nummer in -Tasks) abschalten') $false (Hide-TaskNames $_.Exception.Message)
        }
    }
    try {
        $allHelpers = @(Get-CimInstance Win32_Process -Filter "Name = 'powershell.exe' OR Name = 'pwsh.exe'" -OperationTimeoutSec 15 -ErrorAction Stop |
            Where-Object { $_.ProcessId -ne $PID -and ([string] $_.CommandLine) -match '(?i)run-?sync-?watch\.ps1' })
    } catch {
        throw ('D16-ABBRUCH: Prozessliste nicht lesbar (Get-CimInstance): ' + $_.Exception.Message)
    }
    $helpers = @($allHelpers | Where-Object { Test-OwnHelper $_ $taskArgs })
    $otherHelpers = @($allHelpers | Where-Object { -not (Test-OwnHelper $_ $taskArgs) } | ForEach-Object { 'Hilfsskript PID ' + $_.ProcessId })
    foreach ($h in $helpers) { try { Stop-Process -Id $h.ProcessId -Force -ErrorAction Stop } catch { } }
    $isService = { param($P) ([string] $P.CommandLine) -match '(?i)\s(sync|audit)(\s|$)|--http' }
    $deadline = (Get-Date).AddSeconds(20)
    while (@(Get-PkProcesses | Where-Object { & $isService $_ }).Count -gt 0 -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 2 }
    $killed = 0
    $foreign = @()
    foreach ($p in @(Get-PkProcesses | Where-Object { & $isService $_ })) {
        if (-not (Test-OwnPk $p)) { $foreign += ('PID ' + $p.ProcessId + ': ' + [string] $p.ExecutablePath); continue }
        try { Stop-Process -Id $p.ProcessId -Force -ErrorAction Stop; $killed++ } catch { }
    }
    $detail = ''
    if ($foreign.Count + $otherHelpers.Count -gt 0) { $detail = "nicht beendet (nicht dieser Dauerbetrieb):`n" + ((@($foreign) + @($otherHelpers)) -join "`n") }
    Add-Info $Nr ('Hilfsskripte beendet: ' + $helpers.Count + '; postkauz.exe (sync, --http, audit) aus dem Ordner von -Exe nach 20 s beendet: ' + $killed) $detail
    $deadline = (Get-Date).AddSeconds(30)
    while ((Get-BlockingPk).Count -gt 0 -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 2 }
    $block = @(Get-BlockingPk)
    $n = $block.Count
    $others = @($block | ForEach-Object { 'PID ' + $_.ProcessId + ': ' + [string] $_.ExecutablePath })
    if ($KeepStdioMcp) {
        $stdio = @(Get-PkProcesses | Where-Object { Test-StdioMcp $_ }).Count
        Add-Check $Nr 'Aufgaben angehalten und abgeschaltet, ausser dem stdio-MCP der Claude-App laeuft kein postkauz.exe' ($n -eq 0) ('uebrige postkauz.exe: ' + $n + ', stdio-MCP (darf laufen): ' + $stdio + "`n" + ($others -join "`n"))
    } else {
        Add-Check $Nr 'Aufgaben angehalten und abgeschaltet, kein postkauz.exe laeuft (Claude-App vorher ganz beenden)' ($n -eq 0) ('laufende postkauz.exe: ' + $n + "`n" + ($others -join "`n"))
    }
}

# Befehlszeile in Woerter zerlegen ("..." als ein Wort, ohne die
# Anfuehrungszeichen); das erste Wort ist das Programm.
function Split-CommandLine {
    param([string] $C)
    $toks = @()
    foreach ($m in [regex]::Matches([string] $C, '"([^"]*)"|(\S+)')) {
        if ($m.Groups[1].Success) { $toks += $m.Groups[1].Value } else { $toks += $m.Groups[2].Value }
    }
    return , $toks
}

# Konten eines sync-Aufrufs aus den Optionen -a <konto>, -a=<konto>,
# -a<konto>, --account <konto>, --account=<konto>; "*" fuer --all
# (Nachtrag 2, N6: nur diese Optionen, kein Teilwort-Vergleich; --mailbox
# shared ist nicht das Konto shared).
function Get-WatchAccounts {
    param([string[]] $Toks)
    $acc = @()
    for ($i = 1; $i -lt $Toks.Count; $i++) {
        $t = $Toks[$i]
        if ($t -ieq '--all' -or $t -ieq '--all=true') { $acc += '*'; continue }
        if ($t -ieq '-a' -or $t -ieq '--account') {
            if ($i + 1 -lt $Toks.Count) { $acc += $Toks[$i + 1]; $i++ }
            continue
        }
        $m = [regex]::Match($t, '^(?i)(?:--account=|-a=)(.*)$')
        if ($m.Success) { $acc += $m.Groups[1].Value; continue }
        $m = [regex]::Match($t, '^-a([^-=].*)$')
        if ($m.Success) { $acc += $m.Groups[1].Value }
    }
    return , $acc
}

# Laufende sync --watch genau mit dem Programm -Exe (Nachtrag 2, N6:
# ExecutablePath gleich -Exe; postkauz-rc7.exe im selben Ordner zaehlt
# nicht). Je Prozess: Pid und Konten (Get-WatchAccounts).
function Get-OwnWatch {
    $list = @()
    foreach ($p in @(Get-PkProcesses)) {
        if (([string] $p.ExecutablePath) -ine $script:ExeFull) { continue }
        $toks = Split-CommandLine ([string] $p.CommandLine)
        $rest = @($toks | Select-Object -Skip 1)
        if (-not ($rest -icontains 'sync' -and ($rest -icontains '--watch' -or $rest -icontains '--watch=true'))) { continue }
        $list += New-Object PSObject -Property @{ Pid = $p.ProcessId; Accounts = (Get-WatchAccounts $toks) }
    }
    return $list
}

# Aufgaben aus -Tasks wieder einschalten und (ausser audit export)
# starten; jede Aufgabe einzeln mit try und eigener Pruefung. Seit D13
# (Sammelpunkt 0e) reicht "Running" nicht: Nach dem Start muss die Aufgabe
# nach 20 s noch laufen, und je Konto aus -Accounts muss ein sync --watch
# genau mit dem Programm -Exe laufen, das die erwartete Version meldet
# ($Version). Liefert $true, wenn alles in Ordnung ist.
function Start-Tasks {
    param([string] $Nr, [string] $Version)
    $hint = 'Claude-App danach wieder starten'
    if ($KeepStdioMcp) { $hint = 'Claude-App nach D16 neu starten, damit ihr MCP das aktuelle Programm nimmt' }
    $all = $true
    $todo = @()
    foreach ($tn in $Tasks) {
        if ($null -eq (Get-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue)) { continue }
        if ($script:WasEnabled.ContainsKey($tn) -and -not $script:WasEnabled[$tn]) { continue }
        $err = ''
        try {
            Enable-ScheduledTask -TaskName $tn -ErrorAction Stop | Out-Null
            if (-not (Test-AuditTask $tn)) { Start-ScheduledTask -TaskName $tn -ErrorAction Stop }
        } catch { $err = Hide-TaskNames $_.Exception.Message }
        $todo += New-Object PSObject -Property @{ Name = $tn; Err = $err }
    }
    if ($todo.Count -eq 0) {
        Add-Info $Nr 'keine Aufgabe wieder eingeschaltet; von Hand starten (betrieb.md 1.3)'
        return $true
    }
    $deadline = (Get-Date).AddSeconds(30)
    $states = @{}
    foreach ($x in $todo) {
        $state = ''
        if (-not $x.Err) {
            do {
                $state = [string] (Get-ScheduledTask -TaskName $x.Name -ErrorAction SilentlyContinue).State
                if ((Test-AuditTask $x.Name) -or $state -eq 'Running') { break }
                Start-Sleep -Seconds 2
            } while ((Get-Date) -lt $deadline)
        }
        $states[$x.Name] = $state
    }
    # Laufen sie nach 20 s noch? (Eine Aufgabe, deren Programm sofort
    # endet, steht kurz auf Running und danach auf Ready.)
    Start-Sleep -Seconds 20
    foreach ($x in $todo) {
        $audit = Test-AuditTask $x.Name
        $state = $states[$x.Name]
        $later = [string] (Get-ScheduledTask -TaskName $x.Name -ErrorAction SilentlyContinue).State
        $ok = (-not $x.Err) -and (($audit -and $later -and $later -ne 'Disabled') -or ($state -eq 'Running' -and $later -eq 'Running'))
        if (-not $ok) { $all = $false }
        $what = ' wieder eingeschaltet, gestartet und nach 20 s noch Running'
        if ($audit) { $what = ' wieder eingeschaltet' }
        $detail = 'Zustand ' + $state + ', nach 20 s ' + $later
        if ($x.Err) { $detail += ', Fehler: ' + $x.Err }
        Add-Check $Nr ((Get-TaskLabel $x.Name) + ' (Nummer in -Tasks)' + $what + ' (' + $hint + ')') $ok $detail
    }
    # Nachtrag 2, N6: die Pruefung je Konto haengt nicht an den Namen der
    # Aufgaben; sie laeuft, sobald eine Aufgabe ausser audit export gestartet
    # wurde.
    $syncTasks = @($todo | Where-Object { -not (Test-AuditTask $_.Name) }).Count
    if ($syncTasks -gt 0) {
        $watch = @(Get-OwnWatch)
        $without = 0
        foreach ($acc in $Accounts) {
            $has = @($watch | Where-Object { @($_.Accounts) -contains '*' -or @($_.Accounts) -icontains $acc }).Count
            if ($has -eq 0) { $without++ }
        }
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        $ok = ($Accounts.Count -gt 0 -and $without -eq 0 -and $v.Exit -eq 0 -and $v.Out -match [regex]::Escape($Version))
        if (-not $ok) { $all = $false }
        Add-Check $Nr ('je Konto (-a, --account oder --all) laeuft ein sync --watch genau mit ' + $script:ExeFull + ' (meldet ' + $Version + ')') $ok ('sync --watch: ' + $watch.Count + ', Konten: ' + $Accounts.Count + ', davon ohne sync --watch: ' + $without + ' (gestartete Aufgaben: ' + $syncTasks + ")`n" + $v.Out.Trim())
    }
    return $all
}

# Programm an $script:ExeFull durch $From ersetzen; das bisherige bleibt als
# $Keep (mit -KeepStdioMcp per Umbenennen, sonst per Kopie).
function Install-Program {
    param([string] $From, [string] $Keep)
    if (Test-Path $Keep) {
        $aside = Get-FreeName ([System.IO.Path]::GetFileNameWithoutExtension($Keep))
        Rename-Item -LiteralPath $Keep -NewName (Split-Path -Leaf $aside) -ErrorAction Stop
        Add-Info '2.0' ('vorhandene ' + (Split-Path -Leaf $Keep) + ' beiseitegelegt als ' + (Split-Path -Leaf $aside))
    }
    if ($KeepStdioMcp) {
        Rename-Item -LiteralPath $script:ExeFull -NewName (Split-Path -Leaf $Keep) -ErrorAction Stop
        $script:Swapped = $true
        Copy-Item -LiteralPath $From -Destination $script:ExeFull -Force -ErrorAction Stop
    } else {
        Copy-Item -LiteralPath $script:ExeFull -Destination $Keep -ErrorAction Stop
        $script:Swapped = $true
        Copy-Item -LiteralPath $From -Destination $script:ExeFull -Force -ErrorAction Stop
    }
}

# Freier Name im Ordner von -Exe: <Basis>.exe, sonst mit Zeitstempel (eine
# vorhandene Datei wird nie geloescht oder ueberschrieben).
function Get-FreeName {
    param([string] $Base)
    $n = Join-Path $script:ExeDir ($Base + '.exe')
    if (Test-Path $n) { $n = Join-Path $script:ExeDir ($Base + '-' + (Get-Date -Format 'yyyyMMddHHmmss') + '.exe') }
    return $n
}

function Get-NewCopyName { return (Get-FreeName 'postkauz-rc8') }

# vcs.revision aus der Build-Info einer Go-Programmdatei. "" = nicht gefunden.
function Get-VcsRevision {
    param([string] $Path)
    try {
        $text = [System.Text.Encoding]::ASCII.GetString([System.IO.File]::ReadAllBytes($Path))
        $m = [regex]::Match($text, 'vcs\.revision=([0-9a-f]{40})')
        if ($m.Success) { return $m.Groups[1].Value }
    } catch { }
    return ''
}

# Version des Programms an $Path ("" = fehlt oder meldet nichts); fuer die
# Stand-Zeile nach einem gescheiterten E.0 (Sammelpunkt 0e).
function Get-ProgramVersion {
    param([string] $Path)
    if (-not (Test-Path $Path)) { return '' }
    try {
        $v = Invoke-Pk $Path @('version') -NoDirs -TimeoutSec 60
        $m = [regex]::Match($v.Out, '0\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?')
        if ($m.Success) { return $m.Value }
    } catch { }
    return ''
}

# Das alte Programm $Keep wieder an $script:ExeFull: das neue wird
# umbenannt (eine laufende Exe laesst sich umbenennen, nicht loeschen), das
# alte zurueckbenannt (-KeepStdioMcp) bzw. kopiert.
function Restore-Program {
    param([string] $Keep, [string] $Nr)
    $err = ''
    $aside = ''
    try {
        if (Test-Path $script:ExeFull) {
            $aside = Get-NewCopyName
            Rename-Item -LiteralPath $script:ExeFull -NewName (Split-Path -Leaf $aside) -ErrorAction Stop
        }
        if ($KeepStdioMcp) {
            Rename-Item -LiteralPath $Keep -NewName (Split-Path -Leaf $script:ExeFull) -ErrorAction Stop
        } else {
            Copy-Item -LiteralPath $Keep -Destination $script:ExeFull -Force -ErrorAction Stop
        }
    } catch {
        $err = $_.Exception.Message
    }
    $v = $null
    if (Test-Path $script:ExeFull) { $v = Invoke-Pk $script:ExeFull @('version') -NoDirs }
    $ok = (-not $err -and $null -ne $v -and $v.Out -match [regex]::Escape($OldVersion))
    if ($ok) { $script:Swapped = $false }
    $detail = 'neues Programm beiseite: ' + $aside
    if ($null -ne $v) { $detail += "`n" + $v.Out.Trim() }
    if ($err) { $detail += "`nFehler: " + $err }
    Add-Check $Nr ('altes Programm zurueck: ' + $script:ExeFull + ' meldet ' + $OldVersion) $ok $detail
    return $ok
}

# audit [--operation <op>] --since <since> [--after-id <n>] --limit 1000
# --json (NDJSON), seitenweise ueber --after-id (aufsteigende IDs); ist nach
# $MaxPages Seiten noch keine Seite kuerzer als 1000, gilt die Abfrage als
# nicht lesbar (Exit 99). --since ist immer gesetzt (ohne gilt 24h).
function Get-AuditPaged {
    param([string] $Prog, [string] $Op, [string] $Since, [int64] $AfterId = 0, [switch] $Raw, [int] $MaxPages = 50)
    $events = New-Object System.Collections.Generic.List[object]
    $after = $AfterId
    $code = 99
    for ($page = 0; $page -lt $MaxPages; $page++) {
        $a = @('audit', '--since', $Since, '--limit', '1000', '--json')
        if ($Op) { $a += @('--operation', $Op) }
        if ($Raw) { $a += '--raw' }
        if ($after -gt 0) { $a += @('--after-id', [string] $after) }
        $r = Invoke-Pk $Prog $a
        if ($r.Exit -ne 0) { $code = $r.Exit; break }
        $n = 0
        foreach ($line in ($r.Out -split "`r?`n")) {
            $t = $line.Trim()
            if (-not $t.StartsWith('{')) { continue }
            $o = Get-Json $t
            if ($null -eq $o -or $null -eq $o.id) { continue }
            $n++
            if ([int64] $o.id -gt $after) { $after = [int64] $o.id }
            if ($null -ne $o.operation) { $events.Add($o) }
        }
        if ($n -lt 1000) { $code = 0; break }
    }
    return New-Object PSObject -Property @{ Exit = $code; Events = $events }
}

# Zeitpunkt als RFC 3339 in UTC (fuer --since).
function Format-Rfc3339 {
    param([DateTimeOffset] $T)
    return $T.ToUniversalTime().ToString("yyyy-MM-dd'T'HH:mm:ss'Z'", [System.Globalization.CultureInfo]::InvariantCulture)
}

# Marke im Audit setzen (wie D13, Nachtrag 2, N2): die hoechste ID (alle
# Zeilen, --raw) der letzten 15 Minuten; die eigene Abfrage steht schon
# darin. Ohne Marke gilt der Zeitpunkt $At als Grenze.
function Set-AuditMark {
    param([string] $Prog, [string] $Nr, [DateTimeOffset] $At)
    $script:AuditMark = [int64] -1
    $script:AuditSince = Format-Rfc3339 $At
    $au = Get-AuditPaged $Prog '' '15m' 0 -Raw
    if ($au.Exit -eq 0) {
        foreach ($ev in $au.Events) { if ([int64] $ev.id -gt $script:AuditMark) { $script:AuditMark = [int64] $ev.id } }
    }
    if ($script:AuditMark -gt 0) {
        Add-Info $Nr ('Marke im Audit: ID ' + $script:AuditMark + '; als Abschluss zaehlt nur, was danach im Audit steht')
    } else {
        $script:AuditMark = [int64] -1
        Add-Warn $Nr ('keine Marke im Audit lesbar (exit ' + $au.Exit + '); Grenze ist der Zeitpunkt ' + $script:AuditSince + ' (UTC)')
    }
}

# Audit-Eintraege zu einer Operation seit der Marke.
function Get-AuditOp {
    param([string] $Prog, [string] $Op)
    if ($script:AuditMark -gt 0) { return Get-AuditPaged $Prog $Op '36500d' $script:AuditMark }
    $since = $script:AuditSince
    if (-not $since) { $since = Format-Rfc3339 ([DateTimeOffset] $script:Started.AddMinutes(-10)) }
    return Get-AuditPaged $Prog $Op $since
}

# Grund je Entwurf aus den Systemereignissen der Versanderkennung
# (sent_detected, replaced_detected, copy_gone_detected) und aus
# drafts.supersede (nur der Abschluss mit Grund superseded_by_draft). Seit
# D15 (A09 Teil 1) auch recipients_added aus drafts.sent_detected (nur die
# Zahl, nie Adressen): Map Added (ID -> Zahl).
function Get-CloseReasons {
    param([string] $Prog)
    $reasons = @{}
    $added = @{}
    $ok = $true
    foreach ($op in @('drafts.sent_detected', 'drafts.replaced_detected', 'drafts.copy_gone_detected', 'drafts.supersede')) {
        $au = Get-AuditOp $Prog $op
        if ($au.Exit -ne 0) { $ok = $false }
        foreach ($ev in $au.Events) {
            if ($op -eq 'drafts.supersede' -and [string] $ev.reason -ne 'superseded_by_draft') { continue }
            foreach ($tg in $ev.targets) {
                if ([string] $tg -match '^draft:([0-9]+)$') {
                    $id = [int64] $Matches[1]
                    # drafts.supersede nennt alten und neuen Entwurf; der alte
                    # ist draft_id der Parameter.
                    if ($op -eq 'drafts.supersede' -and $null -ne $ev.params -and [int64] $ev.params.draft_id -ne $id) { continue }
                    $reasons[$id] = [string] $ev.reason
                    if ($op -eq 'drafts.sent_detected' -and $null -ne $ev.params -and $null -ne $ev.params.recipients_added) {
                        $added[$id] = [int] $ev.params.recipients_added
                    }
                }
            }
        }
    }
    return New-Object PSObject -Property @{ Ok = $ok; Reasons = $reasons; Added = $added }
}

function Get-LineCount {
    param([string] $Text)
    if (-not $Text -or -not $Text.Trim()) { return 0 }
    return @($Text.Trim() -split "`r?`n").Count
}

function Get-Doctor {
    param([string] $Prog)
    $r = Invoke-Pk $Prog @('doctor', '--json')
    $j = Get-Json $r.Out
    $w = @()
    $db = ''
    $schema = -1
    if ($null -ne $j) {
        if ($null -ne $j.warnings) { $w = @($j.warnings | ForEach-Object { [string] $_ }) }
        $db = [string] $j.database
        if ($null -ne $j.schema_version) { $schema = [int] $j.schema_version }
    }
    return New-Object PSObject -Property @{ Exit = $r.Exit; Warnings = $w; Database = $db; Schema = $schema; Text = $r.Text }
}

function Get-WarningKey {
    param([string] $W)
    return [regex]::Replace($W, '[0-9]+', '#')
}

function Format-Ids {
    param($L)
    if (@($L).Count -eq 0) { return '(keine)' }
    return ((@($L) | Sort-Object) -join ', ')
}

# Ein sync -a <Konto> --json; liefert Objekt mit Exit, Json, Line.
function Invoke-SyncOnce {
    param([string] $Acc)
    $s = Invoke-Pk $script:ExeFull @('sync', '-a', $Acc, '--json') -TimeoutSec 3600
    $j = Get-Json $s.Out
    $line = 'exit ' + $s.Exit + ', stderr ' + (Get-LineCount $s.Err) + ' Zeilen'
    if ($null -ne $j) {
        $line += ', neu ' + $j.new + ', Fehler ' + $j.errors
        $dc = $j.draft_check
        if ($null -ne $dc) {
            $line += ', sent_by_message_id ' + [int] $dc.sent_by_message_id + ', sent_by_fallback ' + [int] $dc.sent_by_fallback +
                ', sent_by_thread ' + [int] $dc.sent_by_thread + ', sent_by_subject ' + [int] $dc.sent_by_subject +
                ', replaced_in_thread ' + [int] $dc.replaced_in_thread + ', copy_gone ' + [int] $dc.copy_gone +
                ', missing ' + [int] $dc.missing + ', taken_over ' + [int] $dc.taken_over +
                ', scheduled ' + [int] $dc.scheduled + ', sent_by_scheduled ' + [int] $dc.sent_by_scheduled +
                ', schedule_cancelled ' + [int] $dc.schedule_cancelled + ', scheduled_gone ' + [int] $dc.scheduled_gone
            # A10 Teil 2: geplante Entwuerfe dieses Laufs (nur IDs).
            $sd = @(@($dc.scheduled_draft_ids) | Where-Object { $null -ne $_ })
            if ($sd.Count -gt 0) { $line += ' (geplant: draft ' + ($sd -join ', ') + ')' }
        }
    }
    $clean = ($s.Exit -eq 0 -and $null -ne $j -and [int] $j.errors -eq 0)
    return New-Object PSObject -Property @{ Exit = $s.Exit; Json = $j; Line = $line; Clean = $clean }
}

# sync je Konto (ein vollstaendiger Lauf). Sammelpunkt 0e: "errors 0" ist
# streng, aber ein voruebergehender Serverfehler ist nur WARN, wenn ein
# sofortiger zweiter Versuch fuer dasselbe Konto ohne Fehler endet; sonst
# FAIL.
function Invoke-SyncRun {
    param([string] $Nr, [string] $Label, [string] $Version)
    foreach ($acc in $Accounts) {
        $r = Invoke-SyncOnce $acc
        if ($r.Clean) {
            Add-Check $Nr ('sync -a <Konto> (' + $Label + '): Exit 0 und errors 0') $true $r.Line
            continue
        }
        $again = Invoke-SyncOnce $acc
        if ($again.Clean) {
            Add-Warn $Nr ('sync -a <Konto> (' + $Label + '): erster Versuch mit Fehler, sofortige Wiederholung ohne Fehler (voruebergehend)') ("erster Versuch: " + $r.Line + "`nWiederholung: " + $again.Line)
        } else {
            Add-Check $Nr ('sync -a <Konto> (' + $Label + '): Exit 0 und errors 0 (auch die Wiederholung scheiterte)') $false ("erster Versuch: " + $r.Line + "`nWiederholung: " + $again.Line)
        }
    }
}

# Entwuerfe in diesen Zustaenden (ID, Status, Art, remote), ohne Betreff.
function Get-Drafts {
    param([string] $Prog, [string[]] $Status)
    $a = @('draft', 'list', '--json', '--limit', '500')
    foreach ($st in $Status) { $a += @('--status', $st) }
    $r = Invoke-Pk $Prog $a
    $j = Get-Json $r.Out
    $list = @()
    $parsed = ($null -ne $j -and $null -ne $j.drafts)
    if ($parsed) {
        # Seit D16 (A10 Teil 2) mit scheduled_for (remote scheduled).
        foreach ($d in $j.drafts) { $list += New-Object PSObject -Property @{ Id = [int64] $d.draft_id; Status = [string] $d.status; Remote = [string] $d.remote; Kind = [string] $d.kind; ScheduledFor = $d.scheduled_for } }
    }
    return New-Object PSObject -Property @{ Exit = $r.Exit; List = $list; Parsed = $parsed; Text = $r.Text }
}

function Format-Drafts {
    param($List)
    if ($List.Count -eq 0) { return '(keine)' }
    return (($List | Sort-Object Id | ForEach-Object {
        $t = 'draft ' + $_.Id + ': ' + $_.Status + ', ' + $_.Kind + ', remote ' + $_.Remote
        if ($_.Remote -eq 'scheduled') { $t += ' (Termin ' + (Format-At (ConvertTo-Dto $_.ScheduledFor)) + ')' }
        $t
    }) -join "`n")
}

# draft show <id> --json als Objekt (oder $null); kommt nie ins Protokoll.
function Get-DraftDetail {
    param([string] $Prog, [string] $Id)
    $r = Invoke-Pk $Prog @('draft', 'show', $Id, '--json')
    if ($r.Exit -ne 0) { return $null }
    return Get-Json $r.Out
}

# Alle seit der Marke geschlossenen Entwuerfe (vorher offen, jetzt nicht
# mehr) mit Status, Grund und recipients_added; jeder einzeln mit draft
# show. Abschluesse im Audit fuer Entwuerfe, die vorher nicht offen waren,
# stehen in Foreign.
function Get-Closed {
    param([string] $Prog)
    $after = Get-Drafts $Prog @('draft', 'pending_approval', 'ready')
    $cr = Get-CloseReasons $Prog
    $closed = @()
    $vanished = @()
    $openAfterIds = @($after.List | ForEach-Object { $_.Id })
    foreach ($o in $script:Before.List) {
        if ($openAfterIds -contains $o.Id) { continue }
        $dd = Get-DraftDetail $Prog ([string] $o.Id)
        if ($null -ne $dd -and @('sent_by_human', 'abandoned') -contains [string] $dd.status) {
            $r = $cr.Reasons[[int64] $o.Id]
            if (-not $r) { $r = '' }
            $n = 0
            if ($cr.Added.ContainsKey([int64] $o.Id)) { $n = [int] $cr.Added[[int64] $o.Id] }
            $closed += New-Object PSObject -Property @{ Id = $o.Id; Status = [string] $dd.status; Reason = $r; Added = $n }
        } else {
            $vanished += $o.Id
        }
    }
    $beforeIds = @($script:Before.List | ForEach-Object { $_.Id })
    $foreign = @($cr.Reasons.Keys | Where-Object { $beforeIds -notcontains $_ } | ForEach-Object { [int64] $_ })
    $sent = @(); $withAdded = @(); $bySched = @(); $replaced = @(); $gone = @(); $superseded = @(); $odd = @(); $lines = @()
    foreach ($f in $foreign) { $lines += ('draft ' + $f + ': vorher nicht offen, Grund ' + $cr.Reasons[$f]) }
    foreach ($c in $closed) {
        $r = $c.Reason
        $shown = $r
        if (-not $shown) { $shown = '(kein Audit-Eintrag des Systems)' }
        $line = 'draft ' + $c.Id + ': ' + $c.Status + ', Grund ' + $shown
        if ($c.Added -gt 0) { $line += ', recipients_added ' + $c.Added }
        $lines += $line
        # Seit D16 (A10 Teil 2) auch Grund scheduled: die geplante Nachricht
        # liegt in Gesendet.
        if ($c.Status -eq 'sent_by_human' -and @('message_id', 'fallback', 'thread', 'subject', 'scheduled') -contains $r) {
            $sent += $c.Id
            if ($c.Added -gt 0) { $withAdded += $c.Id }
            if ($r -eq 'scheduled') { $bySched += $c.Id }
        }
        elseif ($c.Status -eq 'abandoned' -and $r -eq 'replaced') { $replaced += $c.Id }
        elseif ($c.Status -eq 'abandoned' -and $r -eq 'copy_gone') { $gone += $c.Id }
        elseif ($c.Status -eq 'abandoned' -and $r -eq 'superseded_by_draft') { $superseded += $c.Id }
        else { $odd += $c.Id }
    }
    return New-Object PSObject -Property @{ After = $after; AuditOk = $cr.Ok; Closed = $closed; Vanished = $vanished; Sent = $sent;
        WithAdded = $withAdded; BySched = $bySched; Replaced = $replaced; Gone = $gone; Superseded = $superseded; Odd = $odd; Foreign = $foreign; Lines = $lines }
}

# Nach jedem Lauf: Liste aller Abschluesse; FAIL, wenn einer ohne passenden
# Grund im Audit ist, ein vorher offener weder offen noch geschlossen
# lesbar ist oder das Audit einen Abschluss eines vorher nicht offenen
# Entwurfs nennt. Ohne Erwartungslisten (anders als D13): Der Betreiber
# bestaetigt die Liste in 3.5.
function Test-Closed {
    param([string] $Nr, [string] $Label)
    $g = Get-Closed $script:ExeFull
    $ok = ($g.After.Exit -eq 0 -and $g.After.Parsed -and $g.After.List.Count -lt 500 -and $g.AuditOk -and $g.Odd.Count -eq 0 -and
        $g.Vanished.Count -eq 0 -and $g.Foreign.Count -eq 0)
    Add-Check $Nr ('nach dem ' + $Label + ': alle seit der Marke geschlossenen Entwuerfe mit Grund aus dem Audit (' + $g.Closed.Count + ')') $ok ((@($g.Lines) -join "`n") + "`nohne passenden Grund: " + (Format-Ids $g.Odd) + "`nvorher offen, jetzt weder offen noch geschlossen lesbar: " + (Format-Ids $g.Vanished) + "`nim Audit, aber vorher nicht offen: " + (Format-Ids $g.Foreign))
    return $g
}

# Abbruch mit Grund; der catch am Ende erkennt das Praefix.
function Stop-D16 {
    param([string] $Why)
    throw ('D16-ABBRUCH: ' + $Why)
}

# Zeile "attach_guard: <n> dir rules, <m> file rules" aus doctor (Text);
# "" = fehlt.
function Get-GuardLine {
    param([string] $Prog)
    $r = Invoke-Pk $Prog @('doctor')
    $m = [regex]::Match($r.Out, '(?m)^attach_guard: [0-9]+ dir rules, [0-9]+ file rules\s*$')
    if ($m.Success) { return $m.Value.Trim() }
    return ''
}

# Konfiguration mit [draft] attach_deny_dirs/attach_deny_files? Seit rc6
# erlaubt (A08); D16 nennt es nur.
function Test-DenyKeys {
    $cfgText = ''
    if (Test-Path $Config) { $cfgText = [System.IO.File]::ReadAllText($Config) }
    return ($cfgText -match '(?m)^\s*attach_deny_(dirs|files)\s*=')
}

# 0g (f): Strg+C im Abschluss (finally) abschalten, damit 4.1, E.0 und E.1
# nicht mittendrin abbrechen. Ohne Konsole (Fehler) bleibt es beim
# Verhalten von D14; $true = abgeschaltet.
function Set-CtrlCGuard {
    param([bool] $On)
    try { [Console]::TreatControlCAsInput = $On; return $true } catch { return $false }
}


# ---------------------------------------------------------------- neu in D16

# Bezeichnung eines Kontos im Protokoll ("Konto <n>"), nie der Name.
function Get-AccLabel {
    param([string] $Name)
    $k = $Name.Trim().ToLowerInvariant()
    if ($script:NameLabels.ContainsKey($k)) { return [string] $script:NameLabels[$k] }
    return 'Konto ?'
}

# mailbox list -a <Konto> --json (ohne Serverkontakt): Ordner mit role und
# last_sync_at. Pfade und Namen der Ordner kommen nie ins Protokoll.
function Get-MailboxList {
    param([string] $Prog, [string] $Acc)
    $r = Invoke-Pk $Prog @('mailbox', 'list', '-a', $Acc, '--json') -TimeoutSec 300
    $j = $null
    if ($r.Exit -eq 0) { $j = Get-Json $r.Out }
    $boxes = @()
    $ok = ($null -ne $j -and $null -ne $j.mailboxes)
    if ($ok) { $boxes = @(@($j.mailboxes) | Where-Object { $null -ne $_ }) }
    return New-Object PSObject -Property @{ Ok = $ok; Exit = $r.Exit; Boxes = $boxes }
}

# A10 Teil 1 (c): Hat ein Konto einen Ordner mit Rolle all, aber keinen mit
# Rolle sent (Gesendet fuer IMAP ausgeblendet), zaehlt dort weiter "Alle
# Nachrichten" als gesendet (mit eigenem Absender, ohne Termin in der
# Zukunft), und das Rennen aus B5 bleibt moeglich. Nur ein Hinweis (WARN),
# kein FAIL. Liest nur; laeuft schon in Teil 0 (auch im Lesetest).
function Test-MailboxRoles {
    param([string] $Prog, [string] $Nr)
    foreach ($acc in $Accounts) {
        $label = Get-AccLabel $acc
        $m = Get-MailboxList $Prog $acc
        if (-not $m.Ok) {
            Add-Warn $Nr ($label + ': mailbox list --json nicht lesbar; Rollen nicht geprueft') ('exit ' + $m.Exit)
            continue
        }
        $roles = @($m.Boxes | ForEach-Object { [string] $_.role })
        $hasAll = ($roles -contains 'all')
        $hasSent = ($roles -contains 'sent')
        $detail = 'Ordner: ' + $m.Boxes.Count + ', Rollen: ' + ((@($roles | Where-Object { $_ } | Sort-Object -Unique)) -join ', ')
        if ($hasAll -and -not $hasSent) {
            Add-Warn $Nr ($label + ': Rolle all ohne Rolle sent: Gesendet ist fuer IMAP ausgeblendet; dort zaehlt weiter "Alle Nachrichten" (A10 Teil 1 c). Abhilfe nur nach Entscheid des Betreibers: in Gmail unter Einstellungen, Labels, bei "Gesendet" "In IMAP anzeigen".') $detail
        } else {
            Add-Info $Nr ($label + ': Rolle all ' + $(if ($hasAll) { 'ja' } else { 'nein' }) + ', Rolle sent ' + $(if ($hasSent) { 'ja' } else { 'nein' }) + ' (in Ordnung)') $detail
        }
    }
}

# Befehlszeilen der Aktionen einer Aufgabe (Execute und Arguments) und, wenn
# eine Aktion ein Skript (.ps1, .cmd, .bat) nennt, dessen Pfad.
function Get-TaskActionLines {
    param([string] $Name)
    $t = Get-ScheduledTask -TaskName $Name -ErrorAction SilentlyContinue
    $lines = @()
    if ($null -eq $t) { return , $lines }
    foreach ($ac in @($t.Actions)) {
        $lines += New-Object PSObject -Property @{ Cmd = ([string] $ac.Execute + ' ' + [string] $ac.Arguments); Dir = [string] $ac.WorkingDirectory }
    }
    return , $lines
}

# Teil 0 (c) aus A10: Ist die Aufgabe eine Sicherungsaufgabe, also ruft ihre
# Aktion postkauz backup auf? Ja, wenn das Wort "backup" allein in der
# Befehlszeile steht (postkauz.exe backup create ..., auch in -Command "...")
# oder in einem dort genannten Skript eine Zeile ohne "#" am Anfang
# postkauz und das Wort backup nennt. Liefert "action", "script" oder "".
function Test-BackupTask {
    param([string] $Name)
    foreach ($l in (Get-TaskActionLines $Name)) {
        if ($l.Cmd -match '(?i)(^|[\s"''&;])backup($|[\s"'';])') { return 'action' }
        foreach ($tok in (Split-CommandLine $l.Cmd)) {
            $p = ([string] $tok).Trim("'")
            if ($p -notmatch '(?i)\.(ps1|cmd|bat)$') { continue }
            if (-not [System.IO.Path]::IsPathRooted($p) -and $l.Dir) { $p = Join-Path $l.Dir $p }
            try {
                if (-not (Test-Path -LiteralPath $p)) { continue }
                foreach ($line in [System.IO.File]::ReadAllLines($p)) {
                    $s = $line.Trim()
                    if ($s.StartsWith('#') -or $s.StartsWith('::') -or $s -match '^(?i)rem\s') { continue }
                    if ($s -match '(?i)postkauz' -and $s -match '(?i)(^|[\s"''&;])backup($|[\s"'';])') { return 'script' }
                }
            } catch { }
        }
    }
    return ''
}

# 0.6b (Teil A) und R.0f (Rueckweg): keine Sicherungsaufgabe in -Tasks. Am
# Ende startet das Skript jede Aufgabe aus -Tasks (ausser audit export); eine
# Sicherungsaufgabe liefe dann ausserhalb ihres Plans an. FAIL, wenn eine
# Aktion postkauz backup aufruft; WARN, wenn nur der Name nach Sicherung
# klingt.
function Test-NoBackupTasks {
    param([string] $Nr)
    $found = @()
    $named = @()
    foreach ($tn in $Tasks) {
        $kind = Test-BackupTask $tn
        if ($kind) { $found += ((Get-TaskLabel $tn) + ' (' + $(if ($kind -eq 'action') { 'Aktion' } else { 'Skript der Aktion' }) + ' ruft postkauz backup auf)') }
        elseif ($tn -match '(?i)backup|sicherung') { $named += (Get-TaskLabel $tn) }
    }
    Add-Check $Nr 'keine Sicherungsaufgabe in -Tasks (Aktion ruft postkauz backup auf; sie gehoert nicht in -Tasks)' ($found.Count -eq 0) ('Aufgaben: ' + $Tasks.Count + ', Sicherungsaufgaben: ' + $found.Count + "`n" + ($found -join "`n"))
    if ($named.Count -gt 0) {
        Add-Warn $Nr ('Name klingt nach Sicherung, Aktion ruft aber kein postkauz backup auf: ' + ($named -join ', ') + ' (Claude prueft die Aufgabe von Hand)')
    }
}

# ---------------------------------------------------------------- Teil A

function Invoke-Umstellung {
    Write-Log '--- Teil 0: Voraussetzungen'
    $newFull = ''
    if ($NewExe) { $newFull = [System.IO.Path]::GetFullPath($NewExe) }
    Add-Check '0.1' 'neues Programm angegeben und vorhanden' ($newFull -and (Test-Path $newFull)) $newFull
    if (-not ($newFull -and (Test-Path $newFull))) { Stop-D16 'neues Programm fehlt' }
    $sums = Join-Path (Split-Path -Parent $newFull) 'SHA256SUMS'
    $hash = Get-Sha256 $newFull
    $want = ''
    if (Test-Path $sums) {
        foreach ($l in [System.IO.File]::ReadAllLines($sums)) { if ($l -match '^([0-9a-f]{64})\s+\*?postkauz-windows-amd64\.exe$') { $want = $Matches[1] } }
    }
    Add-Check '0.2' 'Pruefsumme des neuen Programms passt zu SHA256SUMS' ($want -and $hash -eq $want) ('sha256 ' + $hash + "`nSHA256SUMS " + $want)
    $rev = Get-VcsRevision $newFull
    if ($ExpectSha256) {
        Add-Check '0.2b' 'Pruefsumme des neuen Programms gleich -ExpectSha256' ($hash -eq $ExpectSha256.Trim().ToLowerInvariant()) ('sha256 ' + $hash + "`nerwartet " + $ExpectSha256 + "`nvcs.revision " + $rev)
    } else {
        Add-Info '0.2b' ('vcs.revision des neuen Programms: ' + $rev + ' (ohne -ExpectSha256)')
    }
    $v = Invoke-Pk $newFull @('version') -NoDirs
    Add-Check '0.3' ('neues Programm meldet ' + $ExpectVersion) ($v.Exit -eq 0 -and $v.Out -match [regex]::Escape($ExpectVersion)) $v.Text
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    Add-Check '0.4' ('altes Programm meldet ' + $OldVersion) ($v.Exit -eq 0 -and $v.Out -match [regex]::Escape($OldVersion)) $v.Text
    $al = Invoke-Pk $script:ExeFull @('accounts', 'list', '--json')
    $aj = Get-Json $al.Out
    $known = @()
    if ($null -ne $aj -and $null -ne $aj.accounts) { $known = @($aj.accounts | ForEach-Object { [string] $_.name }) }
    Register-AccountNames $known
    $unknown = @($Accounts | Where-Object { $known -notcontains $_ })
    $notGiven = @($known | Where-Object { $Accounts -notcontains $_ })
    Add-Check '0.5' '-Accounts entspricht genau den konfigurierten Konten (accounts list)' ($Accounts.Count -gt 0 -and $al.Exit -eq 0 -and $unknown.Count -eq 0 -and $notGiven.Count -eq 0) ('angegeben: ' + $Accounts.Count + ', konfiguriert: ' + $known.Count + ', nicht konfiguriert: ' + $unknown.Count + ', konfiguriert, aber nicht angegeben: ' + $notGiven.Count)
    $missingTasks = @($Tasks | Where-Object { $null -eq (Get-ScheduledTask -TaskName $_ -ErrorAction SilentlyContinue) })
    Add-Check '0.6' '-Tasks angegeben (Pflicht) und jede Aufgabe vorhanden' ($Tasks.Count -gt 0 -and $missingTasks.Count -eq 0) ('Aufgaben: ' + $Tasks.Count + ', nicht gefunden: ' + $missingTasks.Count)
    # A10 Teil 0 (c): keine Sicherungsaufgabe in -Tasks (4.1 startete sie).
    Test-NoBackupTasks '0.6b'
    # A08-Schluessel bleiben erlaubt: rc7 und rc8 kennen sie.
    Add-Info '0.7' ('[draft] attach_deny_dirs/attach_deny_files in der Konfiguration: ' + $(if (Test-DenyKeys) { 'ja' } else { 'nein' }) + ' (rc7 und rc8 kennen sie; kein Hindernis fuer den Rueckweg)')
    # A10 Teil 1 (c): je Konto Rolle all nur mit Rolle sent (sonst Hinweis).
    Test-MailboxRoles $script:ExeFull '0.8'
    if ($script:NFail -gt 0) { Stop-D16 'Voraussetzungen fehlen; nichts geaendert' }

    Write-Log '--- Teil 1: Stand vorher (altes Programm)'
    $script:Changed = $true
    Stop-Tasks '1.1'
    if ($script:NFail -gt 0) { Stop-D16 'es laeuft noch ein postkauz.exe (Claude-App oder Aufgabe) oder eine Aufgabe liess sich nicht abschalten; nichts getauscht' }
    $script:DocBefore = Get-Doctor $script:ExeFull
    Add-Check '1.2' 'doctor vorher: database ok, Schema 22' ($script:DocBefore.Database -eq 'ok' -and $script:DocBefore.Schema -eq 22) ('database ' + $script:DocBefore.Database + ', schema ' + $script:DocBefore.Schema + "`n" + (($script:DocBefore.Warnings) -join "`n"))
    $script:Before = Get-Drafts $script:ExeFull @('draft', 'pending_approval', 'ready')
    $beforeAt = [DateTimeOffset]::Now
    Add-Check '1.3' ('draft list vorher: ' + $script:Before.List.Count + ' offene Entwuerfe (unter der Grenze von 500)') ($script:Before.Exit -eq 0 -and $script:Before.Parsed -and $script:Before.List.Count -lt 500) ('JSON lesbar: ' + $script:Before.Parsed + "`n" + (Format-Drafts $script:Before.List))
    # Wie D13: Marke nach der Vorher-Liste; nur spaetere Abschluesse zaehlen.
    Set-AuditMark $script:ExeFull '1.3a' $beforeAt
    if ($script:NFail -gt 0) { Stop-D16 'Stand vorher nicht lesbar; nichts getauscht' }
    if ($BackupTo) {
        $b = Invoke-Pk $script:ExeFull @('backup', 'create', '--to', $BackupTo) -TimeoutSec 7200
        Add-Check '1.4' 'Sicherung geschrieben (betrieb.md 2.2): Exit 0 und "backup written to"' ($b.Exit -eq 0 -and $b.Out -match 'backup written to') $b.Text
        if ($script:NFail -gt 0) { Stop-D16 'Sicherung gescheitert; nichts getauscht' }
    } else {
        Add-Info '1.4' 'keine Sicherung (ohne -BackupTo, empfohlen): A10 hat keine Migration; ein Abschluss in Teil 3 bleibt auch nach dem Rueckweg bestehen'
    }

    Write-Log '--- Teil 2: Programm tauschen'
    $why = ''
    try {
        Install-Program $newFull $OldCopy
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Check '2.1' ('getauscht: ' + $script:ExeFull + ' meldet ' + $ExpectVersion + ', altes Programm als postkauz-rc7.exe aufgehoben') ($v.Out -match [regex]::Escape($ExpectVersion) -and (Test-Path $OldCopy)) $v.Text
        $d = Get-Doctor $script:ExeFull
        $old = @{}
        foreach ($w in $script:DocBefore.Warnings) { $old[(Get-WarningKey $w)] = $true }
        $new = @($d.Warnings | Where-Object { -not $old.ContainsKey((Get-WarningKey $_)) })
        Add-Check '2.2' ('doctor nachher: database ok, Schema unveraendert ' + $script:DocBefore.Schema + ', keine neue Warnung') ($d.Database -eq 'ok' -and $d.Schema -eq $script:DocBefore.Schema -and $new.Count -eq 0) ('database ' + $d.Database + ', schema ' + $d.Schema + "`nneu: " + (($new) -join "`n"))
        $gl = Get-GuardLine $script:ExeFull
        Add-Check '2.3' 'doctor nachher zeigt die Zeile attach_guard (Anhang-Sperre aus A08 weiter da)' ([bool] $gl) $gl
        if ($script:NFail -gt 0) { $why = 'Tausch nicht in Ordnung' }
    } catch {
        Add-Check '2.x' ('Fehler beim Tausch: ' + $_.Exception.Message) $false
        $why = 'Fehler beim Tausch'
    }
    if ($why) { Stop-D16 ($why + '; kein sync mit dem neuen Programm') }
    if ($KeepStdioMcp) {
        Add-Info '2.4' 'Der stdio-MCP der Claude-App laeuft bis zum naechsten Neustart der App mit dem alten Programm (postkauz-rc7.exe).'
    }

    Write-Log '--- Teil 3: zwei vollstaendige Laeufe mit dem neuen Programm'
    $failsBefore = $script:NFail
    Invoke-SyncRun '3.1' 'erster Lauf' $ExpectVersion
    [void] (Test-Closed '3.2' 'ersten Lauf')
    # Wie D13 (Nachtrag 1, A3): Nach einem FAIL im ersten Lauf kein zweiter,
    # damit er nicht weitere Entwuerfe endgueltig schliesst.
    if ($script:NFail -gt $failsBefore) { Stop-D16 'FAIL nach dem ersten Lauf; kein zweiter Lauf mit dem neuen Programm' }
    Invoke-SyncRun '3.3' 'zweiter Lauf' $ExpectVersion
    $g = Test-Closed '3.4' 'zweiten Lauf'
    Add-Info '3.4c' 'Summe nach zwei Laeufen (A10 Teil 1: gesendet zaehlt nur aus Gesendet; Teil 2: Grund scheduled, wenn eine geplante Nachricht gesendet ist)' ('sent_by_human: ' + (Format-Ids $g.Sent) + "`n  davon Grund scheduled: " + (Format-Ids $g.BySched) + "`n  davon mit ergaenzten Empfaengern: " + (Format-Ids $g.WithAdded) + "`nersetzt: " + (Format-Ids $g.Replaced) + "`ncopy_gone: " + (Format-Ids $g.Gone) + "`nsupersede: " + (Format-Ids $g.Superseded))
    # A10 Teil 2: offene Entwuerfe, die im Mailprogramm geplant sind (nur ID
    # und Termin).
    $sched = @($g.After.List | Where-Object { $_.Remote -eq 'scheduled' })
    Add-Info '3.4d' ('offene Entwuerfe mit remote scheduled (im Mailprogramm geplant, noch nicht gesendet): ' + $sched.Count) (Format-Drafts $sched)
    Add-Info '3.5' 'Betreiber: Stimmen die Abschluesse (draft show <id> zeigt Betreff und Grund nur am Bildschirm)? Noch offen:' (Format-Drafts $g.After.List)
    $docAfter = Get-Doctor $script:ExeFull
    $old = @{}
    foreach ($w in $script:DocBefore.Warnings) { $old[(Get-WarningKey $w)] = $true }
    $new = @($docAfter.Warnings | Where-Object { -not $old.ContainsKey((Get-WarningKey $_)) })
    Add-Check '3.6' 'doctor nach den Laeufen: database ok, Schema 22, keine neue Warnung' ($docAfter.Database -eq 'ok' -and $docAfter.Schema -eq 22 -and $new.Count -eq 0) ('database ' + $docAfter.Database + ', schema ' + $docAfter.Schema + "`nneu: " + (($new) -join "`n") + "`nalle: " + (($docAfter.Warnings) -join "`n"))
    $av = Invoke-Pk $script:ExeFull @('audit', 'verify', '--json') -TimeoutSec 1800
    $avj = Get-Json $av.Out
    Add-Check '3.7' 'audit verify: ok' ($av.Exit -eq 0 -and $null -ne $avj -and $avj.status -eq 'ok') ('exit ' + $av.Exit + ', status ' + [string] $avj.status)
    if ($script:NFail -gt $failsBefore) {
        Add-Info '3.8' 'FAIL in Teil 3: Die geschlossenen Entwuerfe bleiben geschlossen (Liste in 3.2/3.4); das alte Programm kommt vor dem Neustart der Aufgaben zurueck (E.0).'
    }
    $script:Complete = $true
}

# ---------------------------------------------------------------- Teil B

# ---- MCP (stdio) fuer -AgentKeyFile (wie D13, Nachtrag 1, A8)

# Startet postkauz mcp mit dem Schluessel aus $KeyFile (Umgebung
# POSTKAUZ_KEY_FILE) und meldet die Sitzung an. Der Schluessel steht nie im
# Protokoll; Fehler nennen nur Codes.
function Start-Mcp {
    param([string] $KeyFile)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $script:ExeFull
    $psi.Arguments = ConvertTo-ArgString @('--data-dir', $DataDir, '--config', $Config, 'mcp')
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.StandardOutputEncoding = $script:Utf8
    $psi.StandardErrorEncoding = $script:Utf8
    foreach ($k in @('POSTKAUZ_KEY', 'POSTKAUZ_KEY_FILE', 'POSTKAUZ_DATA_DIR', 'POSTKAUZ_CONFIG', 'POSTKAUZ_POLICY', 'POSTKAUZ_PROFILE')) {
        if ($psi.EnvironmentVariables.ContainsKey($k)) { $psi.EnvironmentVariables.Remove($k) }
    }
    $psi.EnvironmentVariables['POSTKAUZ_KEY_FILE'] = [System.IO.Path]::GetFullPath($KeyFile)
    $p = [System.Diagnostics.Process]::Start($psi)
    $script:McpErr = $p.StandardError.ReadToEndAsync()
    $script:Mcp = $p
    $script:McpId = 0
    $script:McpDead = ''
    # Nachtrag 2, N9 (D13): eigener Schreiber auf den Rohstrom, UTF-8 ohne
    # BOM und mit LF; $p.StandardInput selbst wird nie benutzt und nie
    # geschlossen (sein Flush schriebe ein BOM nach).
    $script:McpIn = New-Object System.IO.StreamWriter($p.StandardInput.BaseStream, $script:Utf8)
    $script:McpIn.NewLine = "`n"
    $script:McpIn.AutoFlush = $true
    $init = Send-Mcp 'initialize' @{ protocolVersion = '2025-06-18'; capabilities = @{}; clientInfo = @{ name = 'abnahme-d16'; version = '1' } }
    if ($null -eq $init -or $null -ne $init.error) { return $false }
    $line = (@{ jsonrpc = '2.0'; method = 'notifications/initialized' } | ConvertTo-Json -Compress -Depth 10)
    $script:McpIn.WriteLine($line)
    return $true
}

# Eine JSON-RPC-Anfrage; liefert die Antwort mit derselben id oder $null
# (Zeitgrenze 60 s). Nach einer Zeitgrenze liest das Skript nicht noch
# einmal; $script:McpDead nennt den Grund, und jeder weitere Aufruf liefert
# sofort $null.
function Send-Mcp {
    param([string] $Method, $Params)
    if ($script:McpDead) { return $null }
    $script:McpId++
    $id = $script:McpId
    $line = (@{ jsonrpc = '2.0'; id = $id; method = $Method; params = $Params } | ConvertTo-Json -Compress -Depth 10)
    try { $script:McpIn.WriteLine($line) } catch { $script:McpDead = 'MCP-Server nimmt keine Anfrage mehr an (' + $Method + ')'; return $null }
    $deadline = (Get-Date).AddSeconds(60)
    while ($true) {
        $left = [int] ($deadline - (Get-Date)).TotalMilliseconds
        if ($left -le 0) { $script:McpDead = 'keine Antwort des MCP-Servers in 60 s (' + $Method + ')'; return $null }
        $t = $script:Mcp.StandardOutput.ReadLineAsync()
        if (-not $t.Wait($left)) { $script:McpDead = 'keine Antwort des MCP-Servers in 60 s (' + $Method + ')'; return $null }
        if ($null -eq $t.Result) { $script:McpDead = 'MCP-Server hat die Verbindung beendet (' + $Method + ')'; return $null }
        $o = Get-Json ([string] $t.Result)
        if ($null -ne $o -and $null -ne $o.id -and [int] $o.id -eq $id) { return $o }
    }
}

# Ein Tool aufrufen; liefert das Ergebnis (structuredContent) oder ein
# Objekt mit den Feldern code, status und reason (nie die Meldung). 0g (f):
# needs_approval kommt vom Server OHNE isError und OHNE structuredContent;
# Status und Grund stehen nur in content[].text. Darum wird content[].text
# immer gelesen, wenn structuredContent fehlt; ein Status denied,
# needs_approval oder error ist dann ein Fehlerobjekt (code = code des
# Servers, sonst der Status).
function Invoke-McpTool {
    param([string] $Tool, $Arguments)
    $r = Send-Mcp 'tools/call' @{ name = $Tool; arguments = $Arguments }
    if ($null -eq $r) { return New-Object PSObject -Property @{ code = 'no_answer'; status = 'error'; reason = $script:McpDead } }
    if ($null -ne $r.error) { return New-Object PSObject -Property @{ code = ('rpc_' + [string] $r.error.code); status = 'error'; reason = '' } }
    if (-not [bool] $r.result.isError -and $null -ne $r.result.structuredContent) { return $r.result.structuredContent }
    $txt = ''
    foreach ($c in @($r.result.content)) { if ($null -ne $c) { $txt += [string] $c.text } }
    $e = Get-Json $txt
    if (-not [bool] $r.result.isError -and $null -ne $e -and @('denied', 'needs_approval', 'error') -notcontains [string] $e.status) {
        # Erfolg ohne structuredContent: das Ergebnis aus dem Text.
        return $e
    }
    $code = 'error'; $status = ''; $reason = ''
    if ($null -ne $e) {
        if ($e.status) { $status = [string] $e.status; $code = $status }
        if ($e.code) { $code = [string] $e.code }
        if ($e.reason) { $reason = [string] $e.reason }
    } elseif (-not [bool] $r.result.isError) {
        $code = 'no_content'
    }
    return New-Object PSObject -Property @{ code = $code; status = $status; reason = $reason }
}

# Fehler eines MCP-Aufrufs fuer das Protokoll: Code, Status, Grund.
function Format-McpError {
    param($R)
    if ($null -eq $R) { return 'keine Antwort' }
    if (-not $R.code) { return 'kein Fehler' }
    return ('Fehler ' + [string] $R.code + ', Status ' + [string] $R.status + ', Grund ' + [string] $R.reason)
}

# Nach einer Zeitgrenze oder einem Ende des Servers Teil B sauber beenden:
# FAIL mit Grund, dann Abbruch (alle weiteren Schritte brauchen den Server).
function Test-McpAlive {
    param([string] $Nr)
    if (-not $script:McpDead) { return }
    Add-Check $Nr ('MCP-Sitzung: ' + $script:McpDead) $false
    Stop-D16 ('MCP-Server antwortet nicht; Teil B beendet (' + $script:McpDead + ')')
}

function Stop-Mcp {
    if ($null -eq $script:Mcp) { return }
    try { if ($null -ne $script:McpIn) { $script:McpIn.Close() } } catch { }
    $script:McpIn = $null
    if ($script:McpDead -or -not $script:Mcp.WaitForExit(15000)) { try { $script:Mcp.Kill() } catch { } }
    $script:Mcp = $null
}

# ---- Schrittsteuerung und Probe-Entwurf (aus D15)

# 0g (f): Abbruch NUR des laufenden Schritts (B.0, B.1, B.4a, B.2, B.4d
# oder B.3) nach einem FAIL; die abhaengigen Pruefungen dieses Schritts
# laufen nicht mehr. Seit D16 (A10 Teil 0 c) lassen auch die folgenden
# Schritte, die davon abhaengen, ihre Pruefungen aus (B1Ok, B2Ok; SKIP).
function Stop-Step {
    param([string] $Why)
    throw ('D16-SCHRITT: ' + $Why)
}

function Invoke-Step {
    param([string] $Nr, [scriptblock] $Body)
    try {
        & $Body
    } catch {
        $msg = [string] $_.Exception.Message
        if ($msg.StartsWith('D16-SCHRITT: ')) {
            Write-Log ('Schritt ' + $Nr + ' abgebrochen: ' + $msg.Substring(13))
        } else {
            throw
        }
    }
}

# Den laufenden sync --watch des Kontos um einen Lauf bitten (sync request,
# R1); er spiegelt dann den Entwuerfe-Ordner, ohne auf --interval (5 min) zu
# warten. Fehler zaehlen nicht (der naechste regulaere Lauf kommt ohnehin).
function Request-Sync {
    try { [void] (Invoke-Pk $script:ExeFull @('sync', 'request', '-a', $SelfAccount, '--json') -TimeoutSec 60) } catch { }
}

# Liegt eine Ordnerkopie mit UID $Uid der Nachricht mit Message-ID $MsgId im
# Spiegel (nicht Papierkorb, nicht Import)? show <Message-ID> -a <Konto>
# --json, Feld mailboxes (Rolle, UID). Teilen mehrere Nachrichten die
# Message-ID ("ambiguous", IDs in der Meldung), jede einzeln mit ihrer ID.
function Test-CopyInMirror {
    param([string] $MsgId, [int64] $Uid)
    $r = Invoke-Pk $script:ExeFull @('show', $MsgId, '-a', $SelfAccount, '--json')
    $objs = @()
    if ($r.Exit -eq 0) {
        $j = Get-Json $r.Out
        if ($null -ne $j) { $objs += $j }
    } else {
        $m = [regex]::Match(($r.Out + "`n" + $r.Err), 'ambiguous message_id[^(]*\(ids ([0-9, ]+)')
        if ($m.Success) {
            foreach ($n in ($m.Groups[1].Value -split ',')) {
                $n = $n.Trim()
                if (-not $n) { continue }
                $s = Invoke-Pk $script:ExeFull @('show', $n, '--json')
                if ($s.Exit -eq 0) {
                    $sj = Get-Json $s.Out
                    if ($null -ne $sj) { $objs += $sj }
                }
            }
        }
    }
    foreach ($o in $objs) {
        foreach ($l in @($o.mailboxes)) {
            if ($null -eq $l) { continue }
            if (@('trash', 'import') -contains [string] $l.role) { continue }
            if ([int64] $l.uid -eq $Uid) { return $true }
        }
    }
    return $false
}

# 0g (f): Wartet bis $Minutes, bis die Kopie des Entwurfs $Id IM SPIEGEL
# liegt: draft show meldet remote pushed mit remote_uid, UND die Nachricht
# mit der Message-ID des Entwurfs hat im Spiegel eine Ordnerkopie mit dieser
# UID. Erst dann findet drafts.supersede die Kopie (D13: B.6/B.7 endeten
# copy_missing, weil das Skript nur auf pushed wartete). Liefert ein Objekt
# mit Ok und Why (ohne Betreff und Adressen).
function Wait-MirrorCopy {
    param([string] $Id, [int] $Minutes)
    $deadline = (Get-Date).AddMinutes($Minutes)
    $why = 'draft show liefert nichts'
    $lastRequest = [DateTime]::MinValue
    while ($true) {
        $d = Get-DraftDetail $script:ExeFull $Id
        if ($null -ne $d) {
            $remote = [string] $d.remote
            $uid = [int64] 0
            if ($null -ne $d.remote_uid) { $uid = [int64] $d.remote_uid }
            if ($remote -eq 'pushed' -and $uid -gt 0 -and [string] $d.message_id) {
                if (Test-CopyInMirror ([string] $d.message_id) $uid) {
                    return New-Object PSObject -Property @{ Ok = $true; Why = ('remote pushed, Kopie im Spiegel (uid ' + $uid + ')') }
                }
                $why = 'remote pushed (uid ' + $uid + '), aber die Kopie ist noch nicht im Spiegel'
                if (((Get-Date) - $lastRequest).TotalSeconds -ge 120) { Request-Sync; $lastRequest = Get-Date }
            } else {
                $why = 'remote ' + $remote
            }
        }
        if ((Get-Date) -ge $deadline) { break }
        Start-Sleep -Seconds 10
    }
    return New-Object PSObject -Property @{ Ok = $false; Why = ($why + ' (nach ' + $Minutes + ' Minuten)') }
}

function New-ProbeDraft {
    param([string] $Subject, [string] $Text, [string] $Nr)
    if ($AgentKeyFile) {
        $r = Invoke-McpTool 'mail_create_draft' @{ account = $SelfAccount; kind = 'new'; to = @($SelfAddress); subject = $Subject; body_markdown = $Text; dry_run = $false }
        Test-McpAlive $Nr
        $id = ''
        if ($null -ne $r -and -not $r.code -and $null -ne $r.draft_id) { $id = [string] $r.draft_id }
        $why = 'draft ' + $id
        if (-not $id) { $why = Format-McpError $r }
        Add-Check $Nr 'Probe-Entwurf ueber MCP mit dem Agentenschluessel angelegt (neue Mail an sich selbst)' ([bool] $id) $why
        return $id
    }
    $body = Join-Path ([System.IO.Path]::GetTempPath()) ('d16-teilb-' + [guid]::NewGuid().ToString('N') + '.md')
    [System.IO.File]::WriteAllText($body, $Text, $script:Utf8)
    try {
        $c = Invoke-Pk $script:ExeFull @('draft', 'new', '-a', $SelfAccount, '--to', $SelfAddress, '--subject', $Subject, '--body-file', $body, '--json')
    } finally {
        Remove-Item -LiteralPath $body -Force -ErrorAction SilentlyContinue
    }
    $cj = Get-Json $c.Out
    $id = ''
    if ($null -ne $cj -and $null -ne $cj.draft_id) { $id = [string] $cj.draft_id }
    Add-Check $Nr 'Probe-Entwurf angelegt (neue Mail an sich selbst)' ($c.Exit -eq 0 -and $id) ('exit ' + $c.Exit + ', draft ' + $id + ', Grund ' + (Get-DenyReason $c))
    return $id
}

# ---- Hilfen fuer A10 (geplantes Senden)

# Zeitpunkt aus JSON (RFC 3339) als DateTimeOffset oder $null. PS 5.1 laesst
# Zeitangaben als Text, neuere Fassungen machen DateTime daraus: beides
# annehmen.
function ConvertTo-Dto {
    param($V)
    if ($null -eq $V) { return $null }
    if ($V -is [DateTimeOffset]) { return $V }
    if ($V -is [DateTime]) { return [DateTimeOffset] $V }
    $s = ([string] $V).Trim()
    if (-not $s) { return $null }
    $t = [DateTimeOffset]::MinValue
    if ([DateTimeOffset]::TryParse($s, [System.Globalization.CultureInfo]::InvariantCulture, [System.Globalization.DateTimeStyles]::AssumeUniversal, [ref] $t)) { return $t }
    return $null
}

# Termin fuer das Protokoll (UTC, RFC 3339) oder "(keiner)".
function Format-At {
    param($T)
    if ($null -eq $T) { return '(keiner)' }
    return (Format-Rfc3339 $T)
}

# Rollen einer Liste (roles eines Treffers oder mailboxes[].role) ohne
# Leereintraege, sortiert.
function Get-RoleList {
    param($L)
    return @(@($L) | Where-Object { $_ } | ForEach-Object { [string] $_ } | Sort-Object)
}

function Format-Roles {
    param($L)
    $r = @(Get-RoleList $L)
    if ($r.Count -eq 0) { return '(keine)' }
    return ($r -join ', ')
}

# Geplant laut Regel A10 Teil 2 (a): in einem Ordner mit Rolle all und in
# keinem mit Rolle inbox, sent, drafts, trash oder junk.
function Test-ScheduledRoles {
    param($L)
    $r = @(Get-RoleList $L)
    if ($r -notcontains 'all') { return $false }
    foreach ($x in @('inbox', 'sent', 'drafts', 'trash', 'junk')) { if ($r -contains $x) { return $false } }
    return $true
}

# Audit-Eintraege einer Operation seit der Marke, die den Entwurf $Id als
# Ziel nennen (draft:<id>); Ok = Audit lesbar.
function Get-DraftAudit {
    param([string] $Op, [string] $Id)
    $au = Get-AuditOp $script:ExeFull $Op
    $list = @()
    foreach ($ev in $au.Events) {
        foreach ($tg in @($ev.targets)) {
            if ([string] $tg -eq ('draft:' + $Id)) { $list += $ev; break }
        }
    }
    return New-Object PSObject -Property @{ Ok = ($au.Exit -eq 0); Events = $list }
}

# Zeitpunkt der letzten Spiegelung des Entwuerfe-Ordners (Rolle drafts) von
# -SelfAccount laut mailbox list (last_sync_at) oder $null. Daran erkennt
# das Skript, dass nach einer Beobachtung ein weiterer Lauf kam.
function Get-DraftsBoxSync {
    $m = Get-MailboxList $script:ExeFull $SelfAccount
    if (-not $m.Ok) { return $null }
    $best = $null
    foreach ($b in $m.Boxes) {
        if ([string] $b.role -ne 'drafts') { continue }
        $t = ConvertTo-Dto $b.last_sync_at
        if ($null -ne $t -and ($null -eq $best -or $t -gt $best)) { $best = $t }
    }
    return $best
}

# Wartet bis $Minutes auf einen weiteren vollstaendigen Lauf von sync
# --watch (sync request alle 2 Minuten; last_sync_at des Entwuerfe-Ordners
# wird neuer), danach 30 s fuer die Pruefung der Entwuerfe nach dem
# Spiegeln. Liefert Ok und Why.
function Wait-NextRun {
    param([int] $Minutes)
    $t0 = Get-DraftsBoxSync
    $deadline = (Get-Date).AddMinutes($Minutes)
    Request-Sync
    $lastRequest = Get-Date
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 15
        $t = Get-DraftsBoxSync
        if ($null -ne $t -and ($null -eq $t0 -or $t -gt $t0)) {
            Start-Sleep -Seconds 30
            return New-Object PSObject -Property @{ Ok = $true; Why = ('weiterer Lauf: Entwuerfe-Ordner gespiegelt ' + (Format-At $t) + ' (vorher ' + (Format-At $t0) + ')') }
        }
        if (((Get-Date) - $lastRequest).TotalSeconds -ge 120) { Request-Sync; $lastRequest = Get-Date }
    }
    return New-Object PSObject -Property @{ Ok = $false; Why = ('kein weiterer Lauf in ' + $Minutes + ' Minuten erkennbar (last_sync_at des Entwuerfe-Ordners ' + (Format-At $t0) + ')') }
}

# Wartet bis $Minutes auf einen Zustand des Entwurfs $Id: $Until ist
# "closed" (sent_by_human oder abandoned), "scheduled" oder "taken_over"
# (remote). Ein geschlossener Entwurf beendet das Warten immer. Bittet
# sync --watch alle 2 Minuten um einen Lauf. Liefert das letzte draft show
# (Detail, kann $null sein) und ob der Zustand erreicht ist.
function Wait-Draft {
    param([string] $Id, [string] $Until, [int] $Minutes)
    $deadline = (Get-Date).AddMinutes($Minutes)
    $lastRequest = Get-Date
    $d = $null
    $reached = $false
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 15
        $d = Get-DraftDetail $script:ExeFull $Id
        if ($null -ne $d) {
            $st = [string] $d.status
            $closed = (@('sent_by_human', 'abandoned') -contains $st)
            if ($Until -eq 'closed' -and $closed) { $reached = $true; break }
            if ($closed) { break }
            if ($Until -ne 'closed' -and [string] $d.remote -eq $Until) { $reached = $true; break }
        }
        if (((Get-Date) - $lastRequest).TotalSeconds -ge 120) { Request-Sync; $lastRequest = Get-Date }
    }
    return New-Object PSObject -Property @{ Detail = $d; Reached = $reached }
}

# Kurzbeschreibung eines draft show fuer das Protokoll (ohne Inhalt).
function Format-DraftState {
    param($D)
    if ($null -eq $D) { return 'draft show liefert nichts' }
    $s = 'Status ' + [string] $D.status + ', remote ' + [string] $D.remote
    if ($null -ne $D.scheduled_for) { $s += ', scheduled_for ' + (Format-At (ConvertTo-Dto $D.scheduled_for)) }
    if ($null -ne $D.scheduled_message_id) { $s += ', scheduled_message_id ' + [string] $D.scheduled_message_id }
    if ($null -ne $D.sent_message_id) { $s += ', sent_message_id ' + [string] $D.sent_message_id }
    return $s
}

function Test-OpenStatus {
    param([string] $St)
    return (@('draft', 'pending_approval', 'ready') -contains $St)
}

# Rollen und Termin einer Nachricht laut show <id> --json (mailboxes[].role,
# scheduled_for).
function Get-MessagePlace {
    param([string] $Id)
    $r = Invoke-Pk $script:ExeFull @('show', $Id, '--json')
    $j = $null
    if ($r.Exit -eq 0) { $j = Get-Json $r.Out }
    $roles = @()
    $at = $null
    if ($null -ne $j) {
        $roles = @(Get-RoleList (@($j.mailboxes) | Where-Object { $null -ne $_ } | ForEach-Object { $_.role }))
        $at = ConvertTo-Dto $j.scheduled_for
    }
    return New-Object PSObject -Property @{ Ok = ($null -ne $j); Exit = $r.Exit; Roles = $roles; At = $at }
}

# search "<q>" -a <SelfAccount> --json --limit 100 (NDJSON, eine Zeile je
# Treffer); liefert die Treffer als Objekte (Betreff bleibt im Speicher).
function Get-CliHits {
    param([string] $Query)
    $r = Invoke-Pk $script:ExeFull @('search', $Query, '-a', $SelfAccount, '--limit', '100', '--json')
    $hits = @()
    foreach ($line in ($r.Out -split "`r?`n")) {
        $t = $line.Trim()
        if (-not $t.StartsWith('{')) { continue }
        $o = Get-Json $t
        if ($null -ne $o -and $null -ne $o.id) { $hits += $o }
    }
    return New-Object PSObject -Property @{ Exit = $r.Exit; Hits = $hits }
}

# mail_search ueber MCP (nur mit -AgentKeyFile).
function Get-McpHits {
    param([string] $Query, [string] $Nr)
    $j = Invoke-McpTool 'mail_search' @{ query = $Query; account = $SelfAccount; limit = 100 }
    Test-McpAlive $Nr
    $hits = @()
    $err = ''
    if ($null -eq $j -or $j.code) { $err = Format-McpError $j } else { $hits = @(@($j.hits) | Where-Object { $null -ne $_ }) }
    return New-Object PSObject -Property @{ Err = $err; Hits = $hits }
}

# Den Treffer mit der lokalen ID $MsgId suchen; ohne ihn (etwa neue lokale
# ID nach dem Abbruch des Plans) den mit dem Betreff der Probe (Vergleich nur
# im Speicher, nie im Protokoll). Liefert den Treffer oder $null.
function Find-Hit {
    param($Hits, [string] $MsgId, [string] $Subject)
    foreach ($h in @($Hits)) { if ([string] $h.id -eq $MsgId) { return $h } }
    if ($Subject) {
        foreach ($h in @($Hits)) { if ([string] $h.subject -eq $Subject) { return $h } }
    }
    return $null
}

# Betreff einer Probe: synthetisch, ASCII, mit Zeitstempel; steht nie im
# Protokoll (die Anleitung nennt nur den Anfang "D16 Probe B.x").
function Get-ProbeSubject {
    param([string] $Step)
    return ('D16 Probe ' + $Step + ' ' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
}

# ---- Schritte

# B.0: gewoehnlich senden; die gesendete Nachricht hat die Rolle sent
# (A10 Teil 1: gesendet ist nur, was in Gesendet liegt).
function Invoke-B0 {
    Write-Log '--- B.0: Probe im Webmail gewoehnlich senden (gesendet ist, was in Gesendet liegt)'
    $id = New-ProbeDraft (Get-ProbeSubject 'B.0') "Hallo,`n`ndas ist Abnahme D16 Teil B.0. Bitte im Webmail unveraendert gewoehnlich senden (nicht planen).`n`nViele Gruesse" 'B.0a'
    if (-not $id) { Stop-Step 'kein Probe-Entwurf' }
    $script:B0Draft = $id
    $w = Wait-MirrorCopy $id 12
    Add-Check 'B.0b' ('Entwurf ' + $id + ': Kopie im Spiegel des Entwuerfe-Ordners (im Webmail sichtbar)') $w.Ok $w.Why
    if (-not $w.Ok) { Stop-Step 'Kopie nicht im Spiegel; im Webmail nichts tun, Entwurf mit draft abandon schliessen' }
    Add-Info 'B.0c' ('JETZT: Entwurf ' + $id + ' (Betreff "D16 Probe B.0 ...") im Webmail im Browser oeffnen und unveraendert SENDEN (gewoehnlich, nicht planen). Das Skript wartet bis zu ' + $WaitMinutes + ' Minuten.')
    $r = Wait-Draft $id 'closed' $WaitMinutes
    $d = $r.Detail
    $st = ''
    if ($null -ne $d) { $st = [string] $d.status }
    Add-Check 'B.0d' ('Entwurf ' + $id + ' als gesendet erkannt (sent_by_human)') ($st -eq 'sent_by_human') (Format-DraftState $d)
    if ($st -ne 'sent_by_human') { Stop-Step 'nicht als gesendet erkannt; B.0e und B.0f entfallen' }
    $au = Get-DraftAudit 'drafts.sent_detected' $id
    $reason = ''
    if (@($au.Events).Count -gt 0) { $reason = [string] (@($au.Events)[-1]).reason }
    Add-Check 'B.0e' 'Audit drafts.sent_detected: Grund message_id, fallback, thread oder subject' ($au.Ok -and @('message_id', 'fallback', 'thread', 'subject') -contains $reason) ('Audit lesbar: ' + $au.Ok + ', Eintraege: ' + @($au.Events).Count + ', Grund ' + $reason)
    $sent = ''
    if ($null -ne $d.sent_message_id) { $sent = [string] $d.sent_message_id }
    if (-not $sent) {
        Add-Check 'B.0f' 'draft show --json nennt sent_message_id' $false (Format-DraftState $d)
        return
    }
    $p = Get-MessagePlace $sent
    Add-Check 'B.0f' ('gesendete Nachricht ' + $sent + ' liegt in einem Ordner mit Rolle sent (show --json, mailboxes[].role), ohne scheduled_for') ($p.Ok -and $p.Roles -contains 'sent' -and $null -eq $p.At) ('exit ' + $p.Exit + ', Rollen: ' + (Format-Roles $p.Roles) + ', scheduled_for ' + (Format-At $p.At))
}

# B.1: in Gmail fuer + 3 Tage geplant -> remote scheduled, nie
# sent_by_human (Fehler B4 aus A10).
function Invoke-B1 {
    Write-Log '--- B.1: Probe im Webmail fuer + 3 Tage planen (geplant ist nicht gesendet)'
    $script:B1Subject = Get-ProbeSubject 'B.1'
    $id = New-ProbeDraft $script:B1Subject "Hallo,`n`ndas ist Abnahme D16 Teil B.1. Bitte im Webmail zum Senden planen (in 3 Tagen), nicht sofort senden.`n`nViele Gruesse" 'B.1a'
    if (-not $id) { Stop-Step 'kein Probe-Entwurf; B.4, B.2 und B.3 entfallen' }
    $script:B1Draft = $id
    $w = Wait-MirrorCopy $id 12
    Add-Check 'B.1b' ('Entwurf ' + $id + ': Kopie im Spiegel des Entwuerfe-Ordners (im Webmail sichtbar)') $w.Ok $w.Why
    if (-not $w.Ok) { Stop-Step 'Kopie nicht im Spiegel; im Webmail nichts tun, Entwurf mit draft abandon schliessen' }
    Add-Info 'B.1c' ('JETZT: Entwurf ' + $id + ' (Betreff "D16 Probe B.1 ...") im Webmail oeffnen, beim Pfeil neben "Senden" "Senden planen" waehlen, "Datum und Uhrzeit auswaehlen": in 3 Tagen, Uhrzeit beliebig; NICHT sofort senden. Das Skript wartet bis zu ' + $WaitMinutes + ' Minuten.')
    $r = Wait-Draft $id 'scheduled' $WaitMinutes
    $d = $r.Detail
    $st = ''; $remote = ''; $note = ''
    if ($null -ne $d) { $st = [string] $d.status; $remote = [string] $d.remote; $note = [string] $d.remote_note }
    if ($st -eq 'sent_by_human') {
        Add-Check 'B.1d' ('Entwurf ' + $id + ': geplant, NICHT sent_by_human (Fehler B4 ist noch da)') $false (Format-DraftState $d)
        Stop-Step 'geplanter Entwurf als gesendet geschlossen; im Webmail den Plan abbrechen (Ordner "Geplant"), B.4, B.2 und B.3 entfallen'
    }
    $at = $null
    $msg = ''
    if ($null -ne $d) {
        $at = ConvertTo-Dto $d.scheduled_for
        if ($null -ne $d.scheduled_message_id) { $msg = [string] $d.scheduled_message_id }
    }
    $hours = ''
    if ($null -ne $at) { $hours = [string] [math]::Round(($at - [DateTimeOffset]::Now).TotalHours, 1) }
    $okD = ($r.Reached -and (Test-OpenStatus $st) -and $remote -eq 'scheduled' -and $null -ne $at -and $at -gt [DateTimeOffset]::Now.AddHours(1) -and
        [int64] ('0' + $msg) -gt 0 -and $note -match '^scheduled in the mail client for .+; not sent yet$')
    Add-Check 'B.1d' ('Entwurf ' + $id + ': remote scheduled, scheduled_for mehr als 1 Stunde voraus, scheduled_message_id, remote_note "scheduled in the mail client for ...; not sent yet", Status offen') $okD ((Format-DraftState $d) + ', Termin in ' + $hours + ' Stunden (erwartet etwa 72), remote_note passt: ' + ($note -match '^scheduled in the mail client for .+; not sent yet$'))
    if (-not $okD) { Stop-Step 'Entwurf nicht als geplant erkannt; im Webmail den Plan abbrechen (Ordner "Geplant"), B.4, B.2 und B.3 entfallen' }
    $script:B1Msg = $msg
    $script:B1At = $at
    $script:B1Ok = $true
    if ($null -ne $at -and [math]::Abs(($at - [DateTimeOffset]::Now).TotalHours - 72) -gt 24) {
        Add-Info 'B.1d2' ('Termin nicht etwa + 3 Tage (' + $hours + ' Stunden); fuer die Probe genuegt ein Termin in der Zukunft')
    }
    # draft list --json: derselbe Stand in der Liste.
    $l = Get-Drafts $script:ExeFull @('draft', 'pending_approval', 'ready')
    $row = @($l.List | Where-Object { [string] $_.Id -eq $id })
    $okL = ($l.Parsed -and $row.Count -eq 1 -and $row[0].Remote -eq 'scheduled' -and $null -ne (ConvertTo-Dto $row[0].ScheduledFor))
    $rowText = '(nicht in der Liste)'
    if ($row.Count -eq 1) { $rowText = 'remote ' + $row[0].Remote + ', scheduled_for ' + (Format-At (ConvertTo-Dto $row[0].ScheduledFor)) }
    Add-Check 'B.1e' ('draft list --json: Entwurf ' + $id + ' mit remote scheduled und scheduled_for') $okL $rowText
    # Audit drafts.scheduled_detected (Akteur system).
    $au = Get-DraftAudit 'drafts.scheduled_detected' $id
    $ev = $null
    if (@($au.Events).Count -gt 0) { $ev = @($au.Events)[-1] }
    $evMsg = ''; $evAt = $null; $evReason = ''; $evActor = ''
    if ($null -ne $ev) {
        $evReason = [string] $ev.reason
        $evActor = [string] $ev.actor
        if ($null -ne $ev.params) { $evMsg = [string] $ev.params.message_id; $evAt = ConvertTo-Dto $ev.params.scheduled_for }
    }
    Add-Check 'B.1f' 'Audit drafts.scheduled_detected: Akteur system, Grund scheduled, message_id und scheduled_for wie draft show' ($au.Ok -and $null -ne $ev -and $evActor -eq 'system' -and $evReason -eq 'scheduled' -and $evMsg -eq $msg -and $null -ne $evAt) ('Audit lesbar: ' + $au.Ok + ', Eintraege: ' + @($au.Events).Count + ', Akteur ' + $evActor + ', Grund ' + $evReason + ', message_id ' + $evMsg + ', scheduled_for ' + (Format-At $evAt))
    # draft edit als Trockenlauf (--dry-run, global): wird abgelehnt, bevor
    # etwas gebaut wird; auch ohne Ablehnung aenderte ein Trockenlauf nichts.
    $e = Invoke-Pk $script:ExeFull @('draft', 'edit', $id, '--quote=true', '--dry-run', '--json')
    $er = Get-DenyReason $e
    Add-Check 'B.1g' ('draft edit ' + $id + ' --dry-run: Exit 3, denied (draft_scheduled)') ($e.Exit -eq 3 -and $er -eq 'draft_scheduled') ('exit ' + $e.Exit + ', Grund ' + $er)
    if ($AgentKeyFile) {
        $m = Invoke-McpTool 'mail_update_draft' @{ draft_id = [int64] $id; quote = $true; dry_run = $true }
        Test-McpAlive 'B.1h'
        $mOk = ($null -ne $m -and ([string] $m.code -eq 'draft_scheduled' -or [string] $m.reason -eq 'draft_scheduled'))
        Add-Check 'B.1h' ('mail_update_draft ' + $id + ' (Trockenlauf, Agentenschluessel): abgelehnt mit draft_scheduled') $mOk (Format-McpError $m)
    } else {
        Add-Info 'B.1h' 'ohne -AgentKeyFile: mail_update_draft nicht geprueft'
    }
    # Ein weiterer Lauf: bleibt geplant, wird nicht gesendet.
    $nr = Wait-NextRun 10
    if (-not $nr.Ok) { Add-Warn 'B.1i' 'kein weiterer Lauf erkennbar; Zustand trotzdem geprueft' $nr.Why }
    $d2 = Get-DraftDetail $script:ExeFull $id
    $st2 = ''
    if ($null -ne $d2) { $st2 = [string] $d2.status }
    $ok2 = ($null -ne $d2 -and (Test-OpenStatus $st2) -and [string] $d2.remote -eq 'scheduled')
    Add-Check 'B.1i' ('nach einem weiteren Lauf: Entwurf ' + $id + ' weiter offen und remote scheduled (nie sent_by_human)') $ok2 ((Format-DraftState $d2) + "`n" + $nr.Why)
    if (-not $ok2) {
        $script:B1Ok = $false
        Stop-Step 'Entwurf nach einem weiteren Lauf nicht mehr geplant; im Webmail den Plan abbrechen, B.4, B.2 und B.3 entfallen'
    }
}

# B.4a bis B.4c: die geplante Nachricht in search, show und mail_search
# (A10 Teil 3), solange sie geplant ist.
function Invoke-B4Scheduled {
    Write-Log '--- B.4a-c: Suche nach der geplanten Nachricht (is:scheduled, roles, scheduled_for)'
    if (-not $script:B1Ok) { Add-Skip 'B.4a' 'haengt von B.1 ab (Entwurf nicht als geplant erkannt)'; return }
    $msg = $script:B1Msg
    # Der Suchindex kann einen Augenblick hinterher sein: bis zu 5 Minuten.
    $deadline = (Get-Date).AddMinutes(5)
    $h = $null
    $res = $null
    do {
        $res = Get-CliHits 'is:scheduled'
        $h = Find-Hit $res.Hits $msg ''
        if ($null -ne $h) { break }
        Request-Sync
        Start-Sleep -Seconds 30
    } while ((Get-Date) -lt $deadline)
    $at = $null; $roles = @()
    if ($null -ne $h) { $at = ConvertTo-Dto $h.scheduled_for; $roles = @(Get-RoleList $h.roles) }
    $okA = ($res.Exit -eq 0 -and $null -ne $h -and (Test-ScheduledRoles $roles) -and $null -ne $at -and $at -eq $script:B1At)
    Add-Check 'B.4a' ('search "is:scheduled" --json trifft die geplante Nachricht ' + $msg + ': roles mit all (ohne inbox, sent, drafts, trash, junk), scheduled_for wie draft show') $okA ('exit ' + $res.Exit + ', Treffer: ' + @($res.Hits).Count + ', gefunden: ' + ($null -ne $h) + ', roles: ' + (Format-Roles $roles) + ', scheduled_for ' + (Format-At $at) + ' (draft show ' + (Format-At $script:B1At) + ')')
    $p = Get-MessagePlace $msg
    Add-Check 'B.4b' ('show ' + $msg + ' --json: mailboxes[].role mit all (nicht sent, nicht drafts), scheduled_for gesetzt') ($p.Ok -and (Test-ScheduledRoles $p.Roles) -and $null -ne $p.At) ('exit ' + $p.Exit + ', Rollen: ' + (Format-Roles $p.Roles) + ', scheduled_for ' + (Format-At $p.At))
    if ($AgentKeyFile) {
        $m = Get-McpHits 'is:scheduled' 'B.4c'
        $mh = Find-Hit $m.Hits $msg ''
        $mAt = $null; $mRoles = @()
        if ($null -ne $mh) { $mAt = ConvertTo-Dto $mh.scheduled_for; $mRoles = @(Get-RoleList $mh.roles) }
        Add-Check 'B.4c' ('mail_search "is:scheduled" (Agentenschluessel) trifft die Nachricht ' + $msg + ' mit roles und scheduled_for') (-not $m.Err -and $null -ne $mh -and (Test-ScheduledRoles $mRoles) -and $null -ne $mAt -and $mAt -eq $script:B1At) ('Fehler: ' + $(if ($m.Err) { $m.Err } else { '(keiner)' }) + ', Treffer: ' + @($m.Hits).Count + ', gefunden: ' + ($null -ne $mh) + ', roles: ' + (Format-Roles $mRoles) + ', scheduled_for ' + (Format-At $mAt))
    } else {
        Add-Info 'B.4c' 'ohne -AgentKeyFile: mail_search nicht geprueft'
    }
}

# B.2: Plan in Gmail abgebrochen -> taken_over, offen (A10 Teil 2 (ii)).
function Invoke-B2 {
    Write-Log '--- B.2: Plan im Webmail abbrechen (wieder ein Entwurf, taken_over)'
    if (-not $script:B1Ok) { Add-Skip 'B.2' 'haengt von B.1 ab (Entwurf nicht als geplant erkannt)'; return }
    $id = $script:B1Draft
    Add-Info 'B.2a' ('JETZT: im Webmail den Ordner "Geplant" oeffnen, die Nachricht zu Entwurf ' + $id + ' (Betreff "D16 Probe B.1 ...") oeffnen und "Senden abbrechen" waehlen; danach NICHT senden, nicht loeschen und nicht aendern. Das Skript wartet bis zu ' + $WaitMinutes + ' Minuten.')
    $r = Wait-Draft $id 'taken_over' $WaitMinutes
    $d = $r.Detail
    $st = ''
    if ($null -ne $d) { $st = [string] $d.status }
    $okB = ($r.Reached -and (Test-OpenStatus $st) -and [string] $d.remote -eq 'taken_over' -and $null -eq $d.scheduled_for)
    Add-Check 'B.2b' ('Entwurf ' + $id + ': remote taken_over, Status offen, kein scheduled_for') $okB (Format-DraftState $d)
    if (-not $okB) { Stop-Step 'Abbruch des Plans nicht erkannt; B.4d, B.4e und B.3 entfallen (im Webmail nachsehen, ob die Nachricht noch geplant ist)' }
    $au = Get-DraftAudit 'drafts.remote_check' $id
    $hit = @(@($au.Events) | Where-Object { [string] $_.reason -eq 'schedule_cancelled' })
    Add-Check 'B.2c' 'Audit drafts.remote_check mit Grund schedule_cancelled' ($au.Ok -and $hit.Count -ge 1) ('Audit lesbar: ' + $au.Ok + ', Eintraege: ' + @($au.Events).Count + ', Gruende: ' + ((@($au.Events) | ForEach-Object { [string] $_.reason }) -join ', '))
    $nr = Wait-NextRun 10
    if (-not $nr.Ok) { Add-Warn 'B.2d' 'kein weiterer Lauf erkennbar; Zustand trotzdem geprueft' $nr.Why }
    $d2 = Get-DraftDetail $script:ExeFull $id
    $st2 = ''
    if ($null -ne $d2) { $st2 = [string] $d2.status }
    $ok2 = ($null -ne $d2 -and (Test-OpenStatus $st2) -and [string] $d2.remote -eq 'taken_over')
    Add-Check 'B.2d' ('nach einem weiteren Lauf: Entwurf ' + $id + ' weiter offen und taken_over') $ok2 ((Format-DraftState $d2) + "`n" + $nr.Why)
    if (-not $ok2) { Stop-Step 'Zustand nach einem weiteren Lauf nicht taken_over; B.4d, B.4e und B.3 entfallen' }
    $script:B2Ok = $true
}

# B.4d und B.4e: is:draft trifft die Nachricht im Entwuerfe-Ordner (ohne
# \Draft, Befund B3).
function Invoke-B4Draft {
    Write-Log '--- B.4d-e: Suche nach dem Entwurf im Entwuerfe-Ordner (is:draft)'
    if (-not $script:B2Ok) { Add-Skip 'B.4d' 'haengt von B.2 ab (Abbruch des Plans nicht erkannt)'; return }
    $msg = $script:B1Msg
    $deadline = (Get-Date).AddMinutes(5)
    $h = $null
    $res = $null
    do {
        $res = Get-CliHits 'is:draft'
        $h = Find-Hit $res.Hits $msg $script:B1Subject
        if ($null -ne $h -and @(Get-RoleList $h.roles) -contains 'drafts') { break }
        Request-Sync
        Start-Sleep -Seconds 30
    } while ((Get-Date) -lt $deadline)
    $roles = @(); $hid = ''
    if ($null -ne $h) { $roles = @(Get-RoleList $h.roles); $hid = [string] $h.id }
    Add-Check 'B.4d' ('search "is:draft" --json trifft die Nachricht im Entwuerfe-Ordner (roles mit drafts, ohne scheduled_for)') ($res.Exit -eq 0 -and $null -ne $h -and $roles -contains 'drafts' -and $null -eq $h.scheduled_for) ('exit ' + $res.Exit + ', Treffer: ' + @($res.Hits).Count + ', Nachricht ' + $hid + ' (aus B.1: ' + $msg + '), roles: ' + (Format-Roles $roles))
    if ($AgentKeyFile) {
        $m = Get-McpHits 'is:draft' 'B.4e'
        $mh = Find-Hit $m.Hits $msg $script:B1Subject
        $mRoles = @(); $mid = ''
        if ($null -ne $mh) { $mRoles = @(Get-RoleList $mh.roles); $mid = [string] $mh.id }
        Add-Check 'B.4e' 'mail_search "is:draft" (Agentenschluessel) trifft die Nachricht mit Rolle drafts in roles' (-not $m.Err -and $null -ne $mh -and $mRoles -contains 'drafts') ('Fehler: ' + $(if ($m.Err) { $m.Err } else { '(keiner)' }) + ', Treffer: ' + @($m.Hits).Count + ', Nachricht ' + $mid + ', roles: ' + (Format-Roles $mRoles))
    } else {
        Add-Info 'B.4e' 'ohne -AgentKeyFile: mail_search nicht geprueft'
    }
}

# B.3: erneut fuer + 5 Minuten planen und senden lassen -> sent_by_human,
# Grund aus dem Audit (A10 Teil 2 (i), am echten Server nicht geprueft).
function Invoke-B3 {
    Write-Log '--- B.3: erneut fuer + 5 Minuten planen und senden lassen'
    if (-not $script:B2Ok) { Add-Skip 'B.3' 'haengt von B.2 ab (Abbruch des Plans nicht erkannt)'; return }
    $id = $script:B1Draft
    Add-Info 'B.3a' ('JETZT: im Webmail den Entwurf ' + $id + ' (Betreff "D16 Probe B.1 ...", wieder unter "Entwuerfe") oeffnen, unveraendert "Senden planen", "Datum und Uhrzeit auswaehlen": heute, in 5 Minuten. Danach nichts mehr tun. Das Skript wartet bis zu ' + $WaitMinutes + ' Minuten auf den Plan und danach bis zum Termin + 20 Minuten.')
    $start = Get-Date
    $deadline = $start.AddMinutes($WaitMinutes + 25)
    $cap = $start.AddMinutes(120)
    $lastRequest = $start
    $seenAt = $null
    $seenMsg = ''
    $d = $null
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 15
        $d = Get-DraftDetail $script:ExeFull $id
        if ($null -ne $d) {
            if (@('sent_by_human', 'abandoned') -contains [string] $d.status) { break }
            if ([string] $d.remote -eq 'scheduled' -and $null -ne $d.scheduled_for) {
                $at = ConvertTo-Dto $d.scheduled_for
                if ($null -ne $at -and ($null -eq $seenAt -or $at -ne $seenAt)) {
                    $seenAt = $at
                    if ($null -ne $d.scheduled_message_id) { $seenMsg = [string] $d.scheduled_message_id }
                    # Frist: Termin + 20 Minuten (hoechstens 2 Stunden nach dem Start von B.3).
                    $deadline = $at.LocalDateTime.AddMinutes(20)
                    if ($deadline -gt $cap) { $deadline = $cap }
                    Add-Info 'B.3b' ('erneut geplant erkannt: Termin ' + (Format-At $at) + ', Nachricht ' + $seenMsg + '; Frist bis Termin + 20 Minuten')
                }
            }
        }
        if (((Get-Date) - $lastRequest).TotalSeconds -ge 120) { Request-Sync; $lastRequest = Get-Date }
    }
    if ($null -eq $seenAt) { Add-Info 'B.3b' 'erneut geplant: nicht beobachtet (zwischen zwei Laeufen geplant und gesendet, oder nicht geplant)' }
    $st = ''
    if ($null -ne $d) { $st = [string] $d.status }
    Add-Check 'B.3c' ('Entwurf ' + $id + ' als gesendet erkannt (sent_by_human)') ($st -eq 'sent_by_human') (Format-DraftState $d)
    if ($st -ne 'sent_by_human') { Stop-Step 'nicht als gesendet erkannt; B.3d und B.3e entfallen (im Webmail nachsehen, ob die Nachricht gesendet ist)' }
    $au = Get-DraftAudit 'drafts.sent_detected' $id
    $reason = ''
    if (@($au.Events).Count -gt 0) { $reason = [string] (@($au.Events)[-1]).reason }
    if ($au.Ok -and $reason -eq 'scheduled') {
        Add-Check 'B.3d' 'Audit drafts.sent_detected: Grund scheduled (die geplante Nachricht liegt in Gesendet)' $true ('Grund ' + $reason)
    } elseif ($au.Ok -and @('fallback', 'thread', 'subject', 'message_id') -contains $reason) {
        Add-Info 'B.3d' ('Audit drafts.sent_detected: Grund ' + $reason + ' (nicht scheduled): Gmail hat beim Versand eine neue Nachricht angelegt; als gesendet erkannt, in Ordnung')
    } else {
        Add-Check 'B.3d' 'Audit drafts.sent_detected: Grund scheduled (oder fallback, thread, subject)' $false ('Audit lesbar: ' + $au.Ok + ', Eintraege: ' + @($au.Events).Count + ', Grund ' + $reason)
    }
    $sent = ''
    if ($null -ne $d.sent_message_id) { $sent = [string] $d.sent_message_id }
    if (-not $sent) {
        Add-Check 'B.3e' 'draft show --json nennt sent_message_id' $false (Format-DraftState $d)
        return
    }
    $p = Get-MessagePlace $sent
    Add-Check 'B.3e' ('gesendete Nachricht ' + $sent + ' liegt in einem Ordner mit Rolle sent, ohne scheduled_for') ($p.Ok -and $p.Roles -contains 'sent' -and $null -eq $p.At) ('exit ' + $p.Exit + ', Rollen: ' + (Format-Roles $p.Roles) + ', scheduled_for ' + (Format-At $p.At))
    $same = 'nicht beobachtet'
    if ($seenMsg) { $same = [string] ($seenMsg -eq $sent) }
    Add-Info 'B.3f' ('sent_message_id ' + $sent + ' gleich der zuletzt geplanten Nachricht: ' + $same + ' (B.1: ' + $script:B1Msg + ')')
}

function Invoke-TeilB {
    Write-Log '--- Teil B: Proben zum geplanten Senden im eigenen Gmail-Konto (Mitmach)'
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    Add-Check 'BV.1' ('Programm meldet ' + $ExpectVersion) ($v.Exit -eq 0 -and $v.Out -match [regex]::Escape($ExpectVersion)) $v.Text
    $al = Invoke-Pk $script:ExeFull @('accounts', 'list', '--json')
    $aj = Get-Json $al.Out
    $known = @()
    if ($null -ne $aj -and $null -ne $aj.accounts) {
        $known = @($aj.accounts | ForEach-Object { [string] $_.name })
        Register-AccountNames $known
        foreach ($a in @($aj.accounts)) {
            if (-not $SelfAddress -and [string] $a.name -eq $SelfAccount) { $script:SelfAddressFound = [string] $a.email }
        }
    }
    if (-not $SelfAddress -and $script:SelfAddressFound) { $script:SelfAddress = $script:SelfAddressFound }
    Add-Check 'BV.2' '-SelfAccount ist ein konfiguriertes Konto, eigene Adresse bekannt (-SelfAddress oder accounts list)' ($SelfAccount -and ($known -contains $SelfAccount) -and [bool] $SelfAddress) ('Konten: ' + $known.Count + ', Konto gefunden: ' + ($known -contains $SelfAccount) + ', Adresse bekannt: ' + [bool] $SelfAddress)
    # A10 Teil 1: Teil B braucht ein Konto mit Gesendet (Rolle sent) und
    # "Alle Nachrichten" (Rolle all).
    $mb = Get-MailboxList $script:ExeFull $SelfAccount
    $roles = @()
    if ($mb.Ok) { $roles = @($mb.Boxes | ForEach-Object { [string] $_.role }) }
    Add-Check 'BV.3' 'mailbox list -a <Konto> --json: Ordner mit Rolle all, sent und drafts vorhanden (Gmail mit Gesendet ueber IMAP)' ($mb.Ok -and $roles -contains 'all' -and $roles -contains 'sent' -and $roles -contains 'drafts') ('lesbar: ' + $mb.Ok + ', Ordner: ' + @($mb.Boxes).Count + ', Rollen: ' + (Format-Roles ($roles | Sort-Object -Unique)))
    if ($AgentKeyFile) {
        $mcpOk = (Test-Path -LiteralPath $AgentKeyFile) -and (Start-Mcp $AgentKeyFile)
        Add-Check 'BV.4' 'Agentenschluessel: Datei vorhanden, MCP-Sitzung angemeldet (Schluessel nie im Protokoll)' $mcpOk ([string] $script:McpDead)
    } else {
        Add-Info 'BV.4' 'ohne -AgentKeyFile: Probe als Mensch an der CLI (mail_update_draft und mail_search werden nicht geprueft)'
    }
    $w = @(Get-OwnWatch)
    $selfWatch = @($w | Where-Object { @($_.Accounts) -contains '*' -or @($_.Accounts) -icontains $SelfAccount }).Count
    Add-Check 'BV.5' 'sync --watch genau mit -Exe laeuft fuer -SelfAccount (legt die Entwuerfe ab, spiegelt Entwuerfe, Gesendet und Alle Nachrichten)' ($selfWatch -gt 0) ('sync --watch: ' + $w.Count + ', davon fuer das Konto: ' + $selfWatch)
    if ($script:NFail -gt 0) { Stop-D16 'Voraussetzungen fehlen' }
    Set-AuditMark $script:ExeFull 'BV.6' ([DateTimeOffset]::Now)
    Invoke-Step 'B.0' { Invoke-B0 }
    Invoke-Step 'B.1' { Invoke-B1 }
    Invoke-Step 'B.4a' { Invoke-B4Scheduled }
    Invoke-Step 'B.2' { Invoke-B2 }
    Invoke-Step 'B.4d' { Invoke-B4Draft }
    Invoke-Step 'B.3' { Invoke-B3 }
    $open = @()
    foreach ($x in @($script:B0Draft, $script:B1Draft)) {
        if (-not $x) { continue }
        $dd = Get-DraftDetail $script:ExeFull $x
        if ($null -ne $dd -and (Test-OpenStatus ([string] $dd.status))) { $open += ($x + ' (remote ' + [string] $dd.remote + ')') }
    }
    if ($open.Count -gt 0) {
        Add-Info 'B.9' ('noch offene Probe-Entwuerfe: ' + ($open -join ', ') + '. Aufraeumen nach abnahme_D16.md Abschnitt 6 (geplante zuerst im Webmail abbrechen, dann loeschen und draft abandon).')
    } else {
        Add-Info 'B.9' 'keine offenen Probe-Entwuerfe'
    }
    $script:CompleteB = $true
}

# ---------------------------------------------------------------- Rueckweg

function Invoke-Rueckweg {
    Write-Log '--- Rueckweg: altes Programm zurueck (keine Migration in A10)'
    Add-Check 'R.0' 'aufgehobenes altes Programm vorhanden (postkauz-rc7.exe)' (Test-Path $OldCopy) $OldCopy
    if (-not (Test-Path $OldCopy)) { Stop-D16 'postkauz-rc7.exe fehlt; nichts geaendert' }
    $newCopy = Get-NewCopyName
    $present = Test-Path $script:ExeFull
    if ($present) {
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Check 'R.0b' ('laufendes Programm meldet ' + $ExpectVersion) ($v.Out -match [regex]::Escape($ExpectVersion)) ($v.Text + "`nneues Programm wird beiseitegelegt als " + (Split-Path -Leaf $newCopy))
    } else {
        Add-Info 'R.0b' ($script:ExeFull + ' fehlt (harter Abbruch?); das alte Programm kommt an seine Stelle')
    }
    $o = Invoke-Pk $OldCopy @('version') -NoDirs
    Add-Check 'R.0c' ('postkauz-rc7.exe meldet ' + $OldVersion) ($o.Out -match [regex]::Escape($OldVersion)) $o.Text
    Add-Check 'R.0d' '-Tasks und -Accounts angegeben (Pflicht)' ($Tasks.Count -gt 0 -and $Accounts.Count -gt 0) ('Aufgaben: ' + $Tasks.Count + ', Konten: ' + $Accounts.Count)
    # 0g (f): vor R.1 pruefen, dass jede Aufgabe aus -Tasks vorhanden ist
    # (sonst schaltete R.5 nur einen Teil wieder ein).
    $missingTasks = @($Tasks | Where-Object { $null -eq (Get-ScheduledTask -TaskName $_ -ErrorAction SilentlyContinue) })
    Add-Check 'R.0e' 'jede Aufgabe aus -Tasks vorhanden' ($missingTasks.Count -eq 0) ('Aufgaben: ' + $Tasks.Count + ', nicht gefunden: ' + $missingTasks.Count)
    # A10 Teil 0 (c): R.5 startete eine Sicherungsaufgabe aus -Tasks.
    Test-NoBackupTasks 'R.0f'
    if ($script:NFail -gt 0) { Stop-D16 'Rueckweg nicht moeglich; nichts geaendert' }
    # Wie D13/D14: erst hier vermerken, dass am Ende alle Aufgaben wieder an
    # sein sollen (ein Abbruch in R.0x schaltet keine ein).
    foreach ($tn in $Tasks) { $script:WasEnabled[$tn] = $true }
    Stop-Tasks 'R.1'
    if ($script:NFail -gt 0) { Stop-D16 'es laeuft noch ein postkauz.exe; nichts getauscht' }
    $err = ''
    $renamed = $false
    try {
        if ($present) {
            Rename-Item -LiteralPath $script:ExeFull -NewName (Split-Path -Leaf $newCopy) -ErrorAction Stop
            $renamed = $true
        }
        Copy-Item -LiteralPath $OldCopy -Destination $script:ExeFull -Force -ErrorAction Stop
    } catch {
        $err = $_.Exception.Message
    }
    Add-Check 'R.1b' ('neues Programm als ' + (Split-Path -Leaf $newCopy) + ' beiseite (falls vorhanden), altes an seine Stelle kopiert') (-not $err -and (Test-Path $script:ExeFull) -and (-not $present -or (Test-Path $newCopy))) ('Fehler: ' + $err)
    if ($err) {
        $back = ''
        if ($renamed) {
            try {
                if (Test-Path $script:ExeFull) { Rename-Item -LiteralPath $script:ExeFull -NewName (Split-Path -Leaf (Get-FreeName 'postkauz-teilkopie')) -ErrorAction Stop }
                Rename-Item -LiteralPath $newCopy -NewName (Split-Path -Leaf $script:ExeFull) -ErrorAction Stop
            } catch { $back = $_.Exception.Message }
        }
        Add-Check 'R.1d' 'Zustand vor dem Rueckweg wiederhergestellt' (-not $back) ('Fehler: ' + $back)
        $script:KeepTasksOff = $true
        Stop-D16 'Rueckweg gescheitert; Aufgaben bleiben AUS (von Hand nach abnahme_D16.md Abschnitt 5 weiter)'
    }
    if ($KeepStdioMcp) {
        Add-Info 'R.1c' 'Der stdio-MCP der Claude-App laeuft bis zum naechsten Neustart der App mit dem bisherigen Programm.'
    }
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    $d = Get-Doctor $script:ExeFull
    Add-Check 'R.2' ('altes Programm laeuft mit der Datenbank: ' + $OldVersion + ', database ok, Schema 22') ($v.Out -match [regex]::Escape($OldVersion) -and $d.Database -eq 'ok' -and $d.Schema -eq 22) ($v.Text + "`ndatabase " + $d.Database + ', schema ' + $d.Schema)
    # Wie D14 (Nachbesserung A08, Punkt 2): jeder FAIL vor dem
    # Wiedereinschalten stoppt; die Aufgaben bleiben aus.
    if ($script:NFail -gt 0) {
        $script:KeepTasksOff = $true
        Stop-D16 'R.2 nicht in Ordnung; kein sync, Aufgaben bleiben AUS (von Hand nach abnahme_D16.md Abschnitt 5 weiter)'
    }
    Invoke-SyncRun 'R.3' ('Rueckweg mit ' + $OldVersion) $OldVersion
    if ($script:NFail -gt 0) {
        $script:KeepTasksOff = $true
        Stop-D16 'R.3 nicht in Ordnung; Aufgaben bleiben AUS (von Hand nach abnahme_D16.md Abschnitt 5 weiter)'
    }
    Add-Info 'R.4' 'Fall: ohne Migration. Mit rc8 geschlossene Entwuerfe (auch Grund scheduled) bleiben geschlossen. rc7 kennt kein geplantes Senden: Die Vermerke in meta (scheduled/<id>) bleiben liegen und wirken erst wieder mit rc8; ein im Mailprogramm geplanter Entwurf zeigt mit rc7 remote missing und kann mit rc7 wieder faelschlich als gesendet schliessen (Fehler B4) oder nach [draft] copy_gone_after als copy_gone; Treffer haben keine roles und kein scheduled_for, is:scheduled ist unbekannt.'
    [void] (Start-Tasks 'R.5' $OldVersion)
    $script:Restarted = $true
}

# ---------------------------------------------------------------- Start

$script:ExeFull = [System.IO.Path]::GetFullPath($Exe)
$dir = Split-Path -Parent $script:ExeFull
$script:ExeDir = $dir
$script:SelfAddressFound = ''
if (-not $LogFile) {
    if ($TeilB) { $LogFile = Join-Path $dir 'D16_teilB.txt' } else { $LogFile = Join-Path $dir 'D16_windows.txt' }
}
# Ein vorhandenes Protokoll wird nicht fortgeschrieben, sondern unter einem
# Namen mit Zeitstempel beiseitegelegt (nie geloescht).
if (Test-Path -LiteralPath $LogFile) {
    $aside = [System.IO.Path]::Combine((Split-Path -Parent $LogFile), [System.IO.Path]::GetFileNameWithoutExtension($LogFile) + '-' +
        (Get-Date -Format 'yyyyMMddHHmmss') + [System.IO.Path]::GetExtension($LogFile))
    try { Rename-Item -LiteralPath $LogFile -NewName (Split-Path -Leaf $aside) -ErrorAction Stop } catch {
        Write-Host ('Protokoll ' + $LogFile + ' laesst sich nicht beiseitelegen: ' + $_.Exception.Message)
        exit 1
    }
}
# 0g (f): Namen vor der ersten Protokollzeile bekannt machen.
Register-TaskNames
Register-AccountNames (@($Accounts) + @($SelfAccount))
$script:Log = $LogFile
$OldCopy = Join-Path $dir 'postkauz-rc7.exe'
$script:Swapped = $false
$script:Restarted = $false
$mode = 'Teil A, Umstellung'
if ($Rueckweg) { $mode = 'Rueckweg' }
if ($TeilB) { $mode = 'Teil B' }
Write-Log ('=== Abnahme D16 (' + $mode + '), ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + ' ===')
Write-Log ('Programm: ' + $script:ExeFull + '; Daten: ' + $DataDir + '; Konfiguration: ' + $Config)
$elevated = $false
try { $elevated = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator) } catch { }
Add-Info '0.0' ('PowerShell mit Administratorrechten: ' + $elevated + ' (Anleitung Abschnitt 3: so wie die Aufgaben angelegt sind)')

$guard = $false
try {
    if ($TeilB) { Invoke-TeilB } elseif ($Rueckweg) { Invoke-Rueckweg } else { Invoke-Umstellung }
} catch {
    $msg = [string] $_.Exception.Message
    if ($msg.StartsWith('D16-ABBRUCH: ')) {
        Write-Log ('Abbruch: ' + $msg.Substring(13))
    } else {
        Add-Check 'X' ('unerwarteter Fehler im Skript: ' + $msg) $false
    }
} finally {
    # 0g (f): ab hier kein Abbruch durch Strg+C mehr (4.1, E.0, E.1).
    $guard = Set-CtrlCGuard $true
    if ($guard -and -not $TeilB) { Add-Info 'E.4' 'Strg+C bis zum Ende des Abschlusses abgeschaltet' }
    try { Stop-Mcp } catch { }
    $umst = (-not $Rueckweg -and -not $TeilB)
    if ($umst -and -not $script:Complete) {
        Add-Check 'E.2' 'Teil 3 lief nicht vollstaendig: Abnahme nicht bestanden' $false
    }
    # 0g (f): Abschlussmerker fuer Teil B.
    if ($TeilB -and -not $script:CompleteB) {
        Add-Check 'E.2' 'Teil B lief nicht vollstaendig (Abbruch vor dem Ende von B.3): Teil B nicht bestanden' $false
    }
    $restoreFailed = $false
    if ($umst -and $script:Swapped -and $script:NFail -gt 0) {
        try {
            if (-not (Restore-Program $OldCopy 'E.0')) {
                $restoreFailed = $true
            } else {
                # Wie D14 (E.0c): erst die Datenbank mit dem alten Programm
                # pruefen, dann Aufgaben einschalten.
                $d = Get-Doctor $script:ExeFull
                $okDb = ($d.Database -eq 'ok' -and $d.Schema -eq 22)
                Add-Check 'E.0c' ('altes Programm laeuft mit der Datenbank: database ok, Schema 22') $okDb ('database ' + $d.Database + ', schema ' + $d.Schema)
                if (-not $okDb) { $restoreFailed = $true }
            }
        } catch {
            Add-Check 'E.0' ('altes Programm zurueck: ' + $_.Exception.Message) $false
            $restoreFailed = $true
        }
    }
    # Wie D14: Im Rueckweg schaltet ein FAIL (R.0 bis R.3) keine Aufgabe
    # wieder ein. 0g (f): ebenso jeder andere unvollstaendige Rueckweg (etwa
    # Strg+C oder ein Abbruch in R.1 bei fehlender postkauz.exe); dann ist
    # nicht sicher, welches Programm unter -Exe liegt.
    if ($Rueckweg -and ($script:NFail -gt 0 -or -not $script:Restarted)) { $script:KeepTasksOff = $true }
    if ($script:WasEnabled.Count -gt 0 -and -not $script:Restarted) {
        if ($restoreFailed -or $script:KeepTasksOff) {
            Add-Check 'E.1' 'Aufgaben NICHT wieder eingeschaltet: FAIL vor dem Wiedereinschalten oder das richtige Programm liegt nicht sicher an seinem Platz (von Hand nach abnahme_D16.md Abschnitt 5)' $false
        } elseif ($umst -and $script:Complete -and $script:NFail -eq 0) {
            Write-Log '--- Teil 4: Dauerbetrieb wieder an'
            $started = $false
            $done41 = $false
            try {
                $started = Start-Tasks '4.1' $ExpectVersion
                $done41 = $true
            } catch {
                Add-Check '4.1' ('Aufgaben wieder einschalten: ' + $_.Exception.Message) $false
            } finally {
                if (-not $done41) { Write-Log 'Teil 4 nicht vollstaendig: Aufgaben von Hand pruefen (abnahme_D16.md Abschnitt 3, harter Abbruch).' }
            }
            if (-not $started) {
                try {
                    $before = $script:NFail
                    Stop-Tasks 'E.0a'
                    if ($script:NFail -gt $before) {
                        Add-Check 'E.0b' 'Aufgaben nach 4.1 nicht sauber angehalten: altes Programm NICHT zurueckgelegt, Aufgaben bleiben aus (von Hand nach abnahme_D16.md Abschnitt 5)' $false
                    } elseif (Restore-Program $OldCopy 'E.0') {
                        [void] (Start-Tasks 'E.1' $OldVersion)
                    } else {
                        Add-Check 'E.1' 'Aufgaben angehalten, altes Programm liess sich nicht zuruecklegen (von Hand nach abnahme_D16.md Abschnitt 5)' $false
                    }
                } catch { Add-Check 'E.1' ('Rueckweg nach 4.1: ' + $_.Exception.Message) $false }
            }
        } else {
            # Teil A ohne Tausch oder nach gelungenem E.0/E.0c: das alte
            # Programm liegt an seinem Platz.
            try { [void] (Start-Tasks 'E.1' $OldVersion) } catch { Add-Check 'E.1' ('Aufgaben wieder einschalten: ' + $_.Exception.Message) $false }
        }
    }
    if ($umst) {
        # A10 Teil 0 (c): Lesetest. Ohne Teil 1 hat das Skript nichts
        # angehalten, getauscht oder gesichert.
        if (-not $script:Changed) { Write-Log 'Ergebnis: nichts geaendert (Teil 1 nicht begonnen: keine Aufgabe angehalten, kein Programm getauscht, keine Sicherung)' }
        $cur = Get-ProgramVersion $script:ExeFull
        if (-not $cur) { $cur = '(fehlt oder meldet keine Version)' }
        $keep = 'fehlt'
        if (Test-Path $OldCopy) { $keep = (Get-ProgramVersion $OldCopy) }
        Write-Log ('Stand: ' + $script:ExeFull + ' meldet ' + $cur + '; postkauz-rc7.exe: ' + $keep)
    }
    Write-Log ''
    if ($script:Fails.Count -gt 0) { Write-Log ('Fehlgeschlagen: ' + ($script:Fails -join '; ')) }
    if ($script:LogBroken) { try { Write-Host 'ACHTUNG: Das Protokoll liess sich nicht vollstaendig schreiben (Datentraeger voll oder gesperrt).' } catch { } }
    Write-Log ('D16: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL, ' + $script:NWarn + ' WARN, ' + $script:NSkip + ' SKIP')
    if ($guard) { [void] (Set-CtrlCGuard $false) }
}
if ($script:NFail -gt 0) { exit 1 }
exit 0
