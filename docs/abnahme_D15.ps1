<#
.SYNOPSIS
    Abnahme D15 (PostKauz, Auftrag A09): Umstellung des Dauerbetriebs von
    0.3.0-rc6 auf 0.3.0-rc7 mit zwei vollstaendigen Laeufen und Rueckweg
    (Teil A) und drei Proben im eigenen Gmail-Konto (Teil B).

.DESCRIPTION
    Laeuft auf dem Buero-PC (Windows 11, Windows PowerShell 5.1) am
    Live-Datenverzeichnis des Dauerbetriebs, ohne Go und ohne Docker.
    Anleitung: docs/abnahme_D15.md. Claude fuehrt das Skript aus.
    Muster: abnahme_D14.ps1 (Tausch, Rueckweg) und abnahme_D13.ps1 (zwei
    Laeufe, Teil B ueber den MCP-Server), mit den Restfunden aus
    Sammelpunkt 0g (f) der Roadmap (siehe abnahme_D15.md, Abschnitt 8).

    Teil A "Umstellung" (Standard):
      Teil 0  Voraussetzungen: beide Programme (Version, Pruefsumme),
              Konten (-Accounts genau wie accounts list), Aufgaben (-Tasks
              ist Pflicht). Die Schluessel [draft] attach_deny_dirs und
              attach_deny_files aus A08 bleiben erlaubt (rc6 kennt sie).
      Teil 1  Mit dem alten Programm (rc6): Aufgaben anhalten und
              abschalten (mit -KeepStdioMcp laeuft der stdio-MCP der
              Claude-App weiter), doctor vorher (database ok, Schema 22),
              offene Entwuerfe vorher, Marke im Audit, Sicherung nur mit
              -BackupTo.
      Teil 2  Programm tauschen: altes als postkauz-rc6.exe aufheben
              (umbenennen statt ueberschreiben mit -KeepStdioMcp), neues an
              seine Stelle; doctor nachher: Schema unveraendert, keine neue
              Warnung, Zeile attach_guard da.
      Teil 3  ZWEI vollstaendige Laeufe mit rc7 (je Konto sync; ein Fehler
              ist nur WARN, wenn ein sofortiger zweiter Versuch ohne Fehler
              endet); nach jedem Lauf alle seit der Marke geschlossenen
              Entwuerfe mit Status, Grund und (neu in A09) recipients_added
              aus dem Audit; nach einem FAIL im ersten Lauf kein zweiter;
              doctor ohne neue Warnung, Schema 22; audit verify ok.
      Teil 4  Aufgaben wieder einschalten und starten, nur wenn Teil 3
              vollstaendig und ohne FAIL lief (4.1 wie D13/D14). Sonst kommt
              nach dem Tausch zuerst das alte Programm zurueck und die
              Aufgaben laufen mit ihm an. Strg+C ist im Abschluss
              abgeschaltet (0g f).

    Teil B (-TeilB -SelfAccount <konto>, Mitmach, nach Teil A):
      B.1  Supersede mit langem Umlaut-Betreff (ueber 78 Zeichen, mehrere
           encoded-words auf einer Zeile): zwei Fassungen einer neuen Mail an
           sich selbst, je Warten auf die Kopie IM SPIEGEL (nicht nur auf
           remote pushed), Trockenlauf, supersede; die Papierkorb-Aktion
           endet done, jedes Ziel ok, die alte Kopie liegt im Papierkorb.
      B.2  Antwort-Entwurf auf -ReplyToMessage; der Mensch ergaenzt in Gmail
           genau einen Empfaenger und sendet unveraendert: sent_by_human,
           Audit drafts.sent_detected mit Grund thread und recipients_added
           1, closed_note "recipients added in the mail client (1)".
      B.3  draft show --json (mit -AgentKeyFile auch mail_get_draft) der
           geschlossenen Entwuerfe aus B.1 und B.2: kein remote_note; beim
           alten aus B.1 nennt closed_note "queued for the trash".
      Nach einem FAIL folgt kein Schritt, der davon abhaengt; unabhaengige
      Schritte (B.2 nach einem FAIL in B.1) laufen weiter. Die Aufgaben
      laufen dabei weiter. Mit -AgentKeyFile laufen Anlegen und supersede
      ueber den MCP-Server mit dem Schluessel eines Agenten.

    Modus "Rueckweg" (-Rueckweg): A09 hat keine Migration (Schema 22). Das
      alte Programm (postkauz-rc6.exe) kommt zurueck und laeuft weiter.

    Jede Pruefung ist eine Zeile "OK <Nr> ...", "FAIL <Nr> ...", "WARN
    <Nr> ...", "INFO <Nr> ..." oder "SKIP <Nr> ...". Betreff, Adressen,
    Mailtext, Konto- und Aufgabennamen stehen nie im Protokoll (Konten als
    "Konto <n>", Aufgaben als "Aufgabe <n>", auch in doctor-Warnungen und
    Ausnahmetexten). Letzte Zeile: "D15: <n> OK, <m> FAIL, <w> WARN, <s>
    SKIP". Exit 0 nur ohne FAIL. Ein vorhandenes Protokoll wird unter einem
    Namen mit Zeitstempel beiseitegelegt.

.PARAMETER Exe
    Das Programm des Dauerbetriebs (Standard C:\PostKauz\postkauz.exe).

.PARAMETER NewExe
    Das neue Programm (postkauz-windows-amd64.exe aus dem Build 0.3.0-rc7).
    Neben ihm muss SHA256SUMS liegen. Nur fuer Teil A.

.PARAMETER DataDir
    Live-Datenverzeichnis (Standard C:\PostKauz\data).

.PARAMETER Config
    Live-Konfiguration (Standard C:\PostKauz\config.toml).

.PARAMETER BackupTo
    Optional: Ordner einer Sicherung vor dem Tausch (betrieb.md 2.2/2.3) auf
    einem verschluesselten Datentraeger. A09 hat keine Migration; ohne
    -BackupTo keine Sicherung (INFO 1.4).

.PARAMETER Accounts
    Alle Konten (mit Komma); Teil 3 und der Rueckweg syncen je Konto, 4.1
    prueft je Konto ein sync --watch.

.PARAMETER Tasks
    Pflicht in Teil A und im Rueckweg: alle geplanten Aufgaben, die
    PostKauz starten, mit Komma getrennt (Namen nicht raten).

.PARAMETER ExpectSha256
    SHA-256 der neuen Programmdatei (aus dem Protokoll des Builds).

.PARAMETER ExpectVersion
    Version des neuen Programms (Standard 0.3.0-rc7).

.PARAMETER OldVersion
    Version des alten Programms (Standard 0.3.0-rc6).

.PARAMETER Rueckweg
    Statt der Umstellung den Rueckweg auf das alte Programm pruefen.

.PARAMETER KeepStdioMcp
    Der stdio-MCP der Claude-App darf weiterlaufen (Tausch per Umbenennen;
    nur ohne Migration moeglich, A09 hat keine).

.PARAMETER TeilB
    Teil B statt Teil A ausfuehren (braucht -SelfAccount).

.PARAMETER SelfAccount
    Teil B: das eigene Gmail-Konto, in dem die Probe-Entwuerfe entstehen
    (nie das geteilte Postfach ohne Anweisung des Betreibers).

.PARAMETER SelfAddress
    Teil B, optional: eigene Adresse des Kontos (Empfaenger der Probe in
    B.1). Ohne Angabe die Adresse aus accounts list. Steht nie im Protokoll.

.PARAMETER ReplyToMessage
    Teil B: lokale ID der Nachricht, auf die B.2 antwortet (vom Betreiber
    gewaehlt; Absender eine EIGENE andere Adresse). Ohne Angabe SKIP B.2.

.PARAMETER AgentKeyFile
    Teil B, optional: Datei mit dem API-Schluessel eines Agenten (pk_key_...,
    etwa der Schluessel der Claude-App). Dann laufen Anlegen, supersede und
    mail_get_draft ueber den MCP-Server (postkauz mcp, stdio) mit diesem
    Schluessel. Schluessel und Dateiname stehen nie im Protokoll.

.PARAMETER WaitMinutes
    Teil B: so lange wartet das Skript in B.2 auf das Senden (Standard 20).

.PARAMETER LogFile
    Protokolldatei. Standard: D15_windows.txt (Teil B: D15_teilB.txt)
    neben dem Programm.

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d15\abnahme_D15.ps1 -NewExe C:\PostKauz\d15\postkauz-windows-amd64.exe -Accounts a,b,c -Tasks "PostKauz sync watch a,PostKauz sync watch b,PostKauz sync watch c,PostKauz audit export,PostKauz mcp http" -KeepStdioMcp

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d15\abnahme_D15.ps1 -TeilB -SelfAccount a -ReplyToMessage 12345 -AgentKeyFile C:\PostKauz\d15\agent.key
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
    [string] $ExpectVersion = '0.3.0-rc7',
    [string] $OldVersion = '0.3.0-rc6',
    [switch] $Rueckweg,
    [switch] $KeepStdioMcp,
    [switch] $TeilB,
    [string] $SelfAccount = '',
    [string] $SelfAddress = '',
    [string] $ReplyToMessage = '',
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
# 0g (f): Abschlussmerker fuer Teil B (alle Schritte B.1 bis B.3 sind
# gelaufen; ein abgebrochener Schritt nach FAIL zaehlt als gelaufen, der
# FAIL steht schon im Protokoll).
$script:CompleteB = $false
# Aufgaben am Ende nicht wieder einschalten (Rueckweg gescheitert).
$script:KeepTasksOff = $false
# Teil B: Ergebnisse der Schritte fuer die abhaengigen Schritte.
$script:B1Old = ''
$script:B1Action = ''
$script:B1ActionOk = $false
$script:B2Draft = ''
$script:B2Closed = $false
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
        throw ('D15-ABBRUCH: Prozessliste nicht lesbar (Get-CimInstance): ' + $_.Exception.Message)
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
        throw ('D15-ABBRUCH: Prozessliste nicht lesbar (Get-CimInstance): ' + $_.Exception.Message)
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
# ExecutablePath gleich -Exe; postkauz-rc6.exe im selben Ordner zaehlt
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
    if ($KeepStdioMcp) { $hint = 'Claude-App nach D15 neu starten, damit ihr MCP das aktuelle Programm nimmt' }
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

function Get-NewCopyName { return (Get-FreeName 'postkauz-rc7') }

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
                ', missing ' + [int] $dc.missing + ', taken_over ' + [int] $dc.taken_over
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
        foreach ($d in $j.drafts) { $list += New-Object PSObject -Property @{ Id = [int64] $d.draft_id; Status = [string] $d.status; Remote = [string] $d.remote; Kind = [string] $d.kind } }
    }
    return New-Object PSObject -Property @{ Exit = $r.Exit; List = $list; Parsed = $parsed; Text = $r.Text }
}

function Format-Drafts {
    param($List)
    if ($List.Count -eq 0) { return '(keine)' }
    return (($List | Sort-Object Id | ForEach-Object { 'draft ' + $_.Id + ': ' + $_.Status + ', ' + $_.Kind + ', remote ' + $_.Remote }) -join "`n")
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
    $sent = @(); $withAdded = @(); $replaced = @(); $gone = @(); $superseded = @(); $odd = @(); $lines = @()
    foreach ($f in $foreign) { $lines += ('draft ' + $f + ': vorher nicht offen, Grund ' + $cr.Reasons[$f]) }
    foreach ($c in $closed) {
        $r = $c.Reason
        $shown = $r
        if (-not $shown) { $shown = '(kein Audit-Eintrag des Systems)' }
        $line = 'draft ' + $c.Id + ': ' + $c.Status + ', Grund ' + $shown
        if ($c.Added -gt 0) { $line += ', recipients_added ' + $c.Added }
        $lines += $line
        if ($c.Status -eq 'sent_by_human' -and @('message_id', 'fallback', 'thread', 'subject') -contains $r) {
            $sent += $c.Id
            if ($c.Added -gt 0) { $withAdded += $c.Id }
        }
        elseif ($c.Status -eq 'abandoned' -and $r -eq 'replaced') { $replaced += $c.Id }
        elseif ($c.Status -eq 'abandoned' -and $r -eq 'copy_gone') { $gone += $c.Id }
        elseif ($c.Status -eq 'abandoned' -and $r -eq 'superseded_by_draft') { $superseded += $c.Id }
        else { $odd += $c.Id }
    }
    return New-Object PSObject -Property @{ After = $after; AuditOk = $cr.Ok; Closed = $closed; Vanished = $vanished; Sent = $sent;
        WithAdded = $withAdded; Replaced = $replaced; Gone = $gone; Superseded = $superseded; Odd = $odd; Foreign = $foreign; Lines = $lines }
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
function Stop-D15 {
    param([string] $Why)
    throw ('D15-ABBRUCH: ' + $Why)
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
# erlaubt (A08); D15 nennt es nur.
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

# ---------------------------------------------------------------- Teil A

function Invoke-Umstellung {
    Write-Log '--- Teil 0: Voraussetzungen'
    $newFull = ''
    if ($NewExe) { $newFull = [System.IO.Path]::GetFullPath($NewExe) }
    Add-Check '0.1' 'neues Programm angegeben und vorhanden' ($newFull -and (Test-Path $newFull)) $newFull
    if (-not ($newFull -and (Test-Path $newFull))) { Stop-D15 'neues Programm fehlt' }
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
    # A08-Schluessel bleiben erlaubt: rc6 kennt sie (anders als rc5 in D14).
    Add-Info '0.7' ('[draft] attach_deny_dirs/attach_deny_files in der Konfiguration: ' + $(if (Test-DenyKeys) { 'ja' } else { 'nein' }) + ' (rc6 und rc7 kennen sie; kein Hindernis fuer den Rueckweg)')
    if ($script:NFail -gt 0) { Stop-D15 'Voraussetzungen fehlen; nichts geaendert' }

    Write-Log '--- Teil 1: Stand vorher (altes Programm)'
    Stop-Tasks '1.1'
    if ($script:NFail -gt 0) { Stop-D15 'es laeuft noch ein postkauz.exe (Claude-App oder Aufgabe) oder eine Aufgabe liess sich nicht abschalten; nichts getauscht' }
    $script:DocBefore = Get-Doctor $script:ExeFull
    Add-Check '1.2' 'doctor vorher: database ok, Schema 22' ($script:DocBefore.Database -eq 'ok' -and $script:DocBefore.Schema -eq 22) ('database ' + $script:DocBefore.Database + ', schema ' + $script:DocBefore.Schema + "`n" + (($script:DocBefore.Warnings) -join "`n"))
    $script:Before = Get-Drafts $script:ExeFull @('draft', 'pending_approval', 'ready')
    $beforeAt = [DateTimeOffset]::Now
    Add-Check '1.3' ('draft list vorher: ' + $script:Before.List.Count + ' offene Entwuerfe (unter der Grenze von 500)') ($script:Before.Exit -eq 0 -and $script:Before.Parsed -and $script:Before.List.Count -lt 500) ('JSON lesbar: ' + $script:Before.Parsed + "`n" + (Format-Drafts $script:Before.List))
    # Wie D13: Marke nach der Vorher-Liste; nur spaetere Abschluesse zaehlen.
    Set-AuditMark $script:ExeFull '1.3a' $beforeAt
    if ($script:NFail -gt 0) { Stop-D15 'Stand vorher nicht lesbar; nichts getauscht' }
    if ($BackupTo) {
        $b = Invoke-Pk $script:ExeFull @('backup', 'create', '--to', $BackupTo) -TimeoutSec 7200
        Add-Check '1.4' 'Sicherung geschrieben (betrieb.md 2.2): Exit 0 und "backup written to"' ($b.Exit -eq 0 -and $b.Out -match 'backup written to') $b.Text
        if ($script:NFail -gt 0) { Stop-D15 'Sicherung gescheitert; nichts getauscht' }
    } else {
        Add-Info '1.4' 'keine Sicherung (ohne -BackupTo): A09 hat keine Migration; ein Abschluss in Teil 3 bleibt auch nach dem Rueckweg bestehen'
    }

    Write-Log '--- Teil 2: Programm tauschen'
    $why = ''
    try {
        Install-Program $newFull $OldCopy
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Check '2.1' ('getauscht: ' + $script:ExeFull + ' meldet ' + $ExpectVersion + ', altes Programm als postkauz-rc6.exe aufgehoben') ($v.Out -match [regex]::Escape($ExpectVersion) -and (Test-Path $OldCopy)) $v.Text
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
    if ($why) { Stop-D15 ($why + '; kein sync mit dem neuen Programm') }
    if ($KeepStdioMcp) {
        Add-Info '2.4' 'Der stdio-MCP der Claude-App laeuft bis zum naechsten Neustart der App mit dem alten Programm (postkauz-rc6.exe).'
    }

    Write-Log '--- Teil 3: zwei vollstaendige Laeufe mit dem neuen Programm'
    $failsBefore = $script:NFail
    Invoke-SyncRun '3.1' 'erster Lauf' $ExpectVersion
    [void] (Test-Closed '3.2' 'ersten Lauf')
    # Wie D13 (Nachtrag 1, A3): Nach einem FAIL im ersten Lauf kein zweiter,
    # damit er nicht weitere Entwuerfe endgueltig schliesst.
    if ($script:NFail -gt $failsBefore) { Stop-D15 'FAIL nach dem ersten Lauf; kein zweiter Lauf mit dem neuen Programm' }
    Invoke-SyncRun '3.3' 'zweiter Lauf' $ExpectVersion
    $g = Test-Closed '3.4' 'zweiten Lauf'
    Add-Info '3.4c' 'Summe nach zwei Laeufen (A09 Teil 1: eine Antwort mit im Mailprogramm ergaenzten Empfaengern schliesst jetzt mit Grund thread und recipients_added)' ('sent_by_human: ' + (Format-Ids $g.Sent) + "`n  davon mit ergaenzten Empfaengern: " + (Format-Ids $g.WithAdded) + "`nersetzt: " + (Format-Ids $g.Replaced) + "`ncopy_gone: " + (Format-Ids $g.Gone) + "`nsupersede: " + (Format-Ids $g.Superseded))
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
    $init = Send-Mcp 'initialize' @{ protocolVersion = '2025-06-18'; capabilities = @{}; clientInfo = @{ name = 'abnahme-d15'; version = '1' } }
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
    Stop-D15 ('MCP-Server antwortet nicht; Teil B beendet (' + $script:McpDead + ')')
}

function Stop-Mcp {
    if ($null -eq $script:Mcp) { return }
    try { if ($null -ne $script:McpIn) { $script:McpIn.Close() } } catch { }
    $script:McpIn = $null
    if ($script:McpDead -or -not $script:Mcp.WaitForExit(15000)) { try { $script:Mcp.Kill() } catch { } }
    $script:Mcp = $null
}

# ---- Schritte

# 0g (f): Abbruch NUR des laufenden Schritts (B.1, B.2 oder B.3) nach einem
# FAIL; die abhaengigen Pruefungen dieses Schritts laufen nicht mehr.
function Stop-Step {
    param([string] $Why)
    throw ('D15-SCHRITT: ' + $Why)
}

function Invoke-Step {
    param([string] $Nr, [scriptblock] $Body)
    try {
        & $Body
    } catch {
        $msg = [string] $_.Exception.Message
        if ($msg.StartsWith('D15-SCHRITT: ')) {
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

# Betreff fuer B.1: ueber 78 Zeichen mit Umlauten (u-Umlaut 0xFC, a-Umlaut
# 0xE4), damit der Bau ihn als mehrere encoded-words auf einer Zeile ablegt
# (A09 Teil 1b); synthetisch, mit Zeitstempel, steht nie im Protokoll.
function Get-B1Subject {
    $ue = [string] [char] 0x00FC
    $ae = [string] [char] 0x00E4
    return ('D15 Pr' + $ue + 'fung: R' + $ue + 'ckfrage zur Nebenkostenabrechnung f' + $ue + 'r das Jahr mit Belegen und Erl' + $ae + 'uterungen ' + (Get-Date -Format 'yyyyMMdd-HHmm'))
}

# Voraussetzung von B.1: Steht der Betreff in der gebauten Nachricht (draft
# show --eml) als EINE Zeile ueber 78 Zeichen mit mindestens zwei
# encoded-words? Sonst prueft B.1 nicht den Fall aus A09 Teil 1b (WARN).
# Ins Protokoll kommen nur Laenge und Zahlen.
function Test-SubjectShape {
    param([string] $Nr, [string] $Id)
    $r = Invoke-Pk $script:ExeFull @('draft', 'show', $Id, '--eml')
    $head = ([regex]::Split($r.Out, "\r?\n\r?\n"))[0]
    $lines = @($head -split "\r?\n")
    $len = 0; $words = 0; $folds = 0; $found = $false
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -notmatch '^(?i)subject:') { continue }
        $found = $true
        $len = $lines[$i].Length
        $words = [regex]::Matches($lines[$i], '=\?[^?]+\?[BbQq]\?[^?]*\?=').Count
        for ($k = $i + 1; $k -lt $lines.Count -and $lines[$k] -match '^[ \t]'; $k++) { $folds++ }
        break
    }
    $detail = 'exit ' + $r.Exit + ', Zeile Subject: ' + $len + ' Zeichen, encoded-words auf der ersten Zeile: ' + $words + ', Faltungen: ' + $folds
    if ($r.Exit -eq 0 -and $found -and $len -gt 78 -and $words -ge 2 -and $folds -eq 0) {
        Add-Check $Nr 'Voraussetzung: Betreff als eine lange Zeile (ueber 78 Zeichen) aus mehreren encoded-words gebaut' $true $detail
    } else {
        Add-Warn $Nr 'Betreff NICHT als eine lange Zeile aus mehreren encoded-words gebaut: B.1 prueft dann nicht den Fall aus A09 Teil 1b (Claude meldet es)' $detail
    }
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
    $body = Join-Path ([System.IO.Path]::GetTempPath()) ('d15-teilb-' + [guid]::NewGuid().ToString('N') + '.md')
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

# Antwort-Entwurf auf die lokale Nachricht $Msg (kind reply, Empfaenger aus
# der Ausgangsnachricht).
function New-ReplyDraft {
    param([string] $Msg, [string] $Text, [string] $Nr)
    if ($AgentKeyFile) {
        $r = Invoke-McpTool 'mail_create_draft' @{ account = $SelfAccount; kind = 'reply'; in_reply_to = [int64] $Msg; body_markdown = $Text; dry_run = $false }
        Test-McpAlive $Nr
        $id = ''
        if ($null -ne $r -and -not $r.code -and $null -ne $r.draft_id) { $id = [string] $r.draft_id }
        $why = 'draft ' + $id
        if (-not $id) { $why = Format-McpError $r }
        Add-Check $Nr ('Antwort-Entwurf auf Nachricht ' + $Msg + ' ueber MCP mit dem Agentenschluessel angelegt (mail_create_draft, kind reply)') ([bool] $id) $why
        return $id
    }
    $body = Join-Path ([System.IO.Path]::GetTempPath()) ('d15-teilb-' + [guid]::NewGuid().ToString('N') + '.md')
    [System.IO.File]::WriteAllText($body, $Text, $script:Utf8)
    try {
        $c = Invoke-Pk $script:ExeFull @('draft', 'reply', $Msg, '-a', $SelfAccount, '--body-file', $body, '--json')
    } finally {
        Remove-Item -LiteralPath $body -Force -ErrorAction SilentlyContinue
    }
    $cj = Get-Json $c.Out
    $id = ''
    if ($null -ne $cj -and $null -ne $cj.draft_id) { $id = [string] $cj.draft_id }
    Add-Check $Nr ('Antwort-Entwurf auf Nachricht ' + $Msg + ' angelegt (draft reply)') ($c.Exit -eq 0 -and $id) ('exit ' + $c.Exit + ', draft ' + $id + ', Grund ' + (Get-DenyReason $c))
    return $id
}

# supersede (Trockenlauf oder echt) ueber MCP bzw. CLI. Liefert Ok, Json
# (Ergebnis), Why (Code, Status, Grund) und Reason (Grund einer Ablehnung).
function Invoke-Supersede {
    param([string] $Old, [string] $New, [bool] $Dry, [string] $Nr)
    if ($AgentKeyFile) {
        $j = Invoke-McpTool 'mail_supersede_draft' @{ draft_id = [int64] $Old; by_draft_id = [int64] $New; dry_run = $Dry }
        Test-McpAlive $Nr
        $ok = ($null -ne $j -and -not $j.code)
        $reason = ''
        if (-not $ok -and $null -ne $j) { $reason = [string] $j.reason }
        return New-Object PSObject -Property @{ Ok = $ok; Json = $j; Why = (Format-McpError $j); Reason = $reason }
    }
    $a = @('draft', 'supersede', $Old, '--by', $New, '--json')
    if ($Dry) { $a += '--dry-run' }
    $r = Invoke-Pk $script:ExeFull $a
    $reason = Get-DenyReason $r
    return New-Object PSObject -Property @{ Ok = ($r.Exit -eq 0); Json = (Get-Json $r.Out); Why = ('exit ' + $r.Exit + ', Grund ' + $reason); Reason = $reason }
}

# Wartet bis $Minutes, bis die Aktion abgeschlossen ist (done, partial,
# failed, rejected, expired); liefert action show --json oder $null.
function Wait-Action {
    param([string] $Id, [int] $Minutes)
    $deadline = (Get-Date).AddMinutes($Minutes)
    $aj = $null
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 10
        $a = Invoke-Pk $script:ExeFull @('action', 'show', $Id, '--json')
        $aj = Get-Json $a.Out
        if ($null -ne $aj -and @('done', 'partial', 'failed', 'rejected', 'expired') -contains [string] $aj.state) { break }
    }
    return $aj
}

# B.1: Supersede mit langem Umlaut-Betreff (A09 Teil 1b).
function Invoke-B1 {
    Write-Log '--- B.1: drafts.supersede mit langem Umlaut-Betreff (Papierkorb-Aktion endet ok)'
    $subject = Get-B1Subject
    $first = New-ProbeDraft $subject "Hallo,`n`ndas ist Abnahme D15 Teil B.1, erste Fassung. Diese Fassung wird ersetzt und nicht gesendet.`n`nViele Gruesse" 'B.1a'
    if (-not $first) { Stop-Step 'kein erster Entwurf' }
    Test-SubjectShape 'B.1b' $first
    $w1 = Wait-MirrorCopy $first 12
    Add-Check 'B.1c' ('erste Fassung (Entwurf ' + $first + '): Kopie im Spiegel des Entwuerfe-Ordners (nicht nur remote pushed)') $w1.Ok $w1.Why
    if (-not $w1.Ok) { Stop-Step 'erste Fassung nicht im Spiegel; kein zweiter Entwurf, kein supersede' }
    Start-Sleep -Seconds 2
    $second = New-ProbeDraft $subject "Hallo,`n`ndas ist Abnahme D15 Teil B.1, zweite Fassung. Sie ersetzt die erste und bleibt offen.`n`nViele Gruesse" 'B.1d'
    if (-not $second) { Stop-Step 'kein zweiter Entwurf; kein supersede' }
    $w2 = Wait-MirrorCopy $second 12
    Add-Check 'B.1e' ('zweite Fassung (Entwurf ' + $second + '): Kopie im Spiegel des Entwuerfe-Ordners') $w2.Ok $w2.Why
    if (-not $w2.Ok) { Stop-Step 'zweite Fassung nicht im Spiegel; kein supersede' }
    # Trockenlauf; meldet er noch copy_missing oder push_in_progress (Spiegel
    # oder Ablage einen Augenblick hinterher), bis zu 3 Minuten wiederholen.
    $deadline = (Get-Date).AddMinutes(3)
    $tries = 0
    do {
        $tries++
        $dry = Invoke-Supersede $first $second $true 'B.1f'
        if ($dry.Ok -or @('copy_missing', 'push_in_progress') -notcontains $dry.Reason) { break }
        Request-Sync
        Start-Sleep -Seconds 15
    } while ((Get-Date) -lt $deadline)
    $d1 = Get-DraftDetail $script:ExeFull $first
    $st = ''
    if ($null -ne $d1) { $st = [string] $d1.status }
    $copies = ''
    if ($null -ne $dry.Json) { $copies = [string] $dry.Json.copies }
    $dryOk = ($dry.Ok -and $null -ne $dry.Json -and [bool] $dry.Json.dry_run -and [int] $dry.Json.copies -ge 1 -and $st -eq 'draft')
    Add-Check 'B.1f' 'supersede im Trockenlauf: nichts geaendert, mindestens 1 Kopie wuerde in den Papierkorb gehen' $dryOk ($dry.Why + ', Kopien ' + $copies + ', Versuche ' + $tries + ', Status erste Fassung ' + $st)
    if (-not $dryOk) { Stop-Step 'Trockenlauf nicht in Ordnung; kein supersede' }
    $s = Invoke-Supersede $first $second $false 'B.1g'
    $action = ''
    $sStatus = ''
    if ($s.Ok -and $null -ne $s.Json) {
        $sStatus = [string] $s.Json.status
        if ($null -ne $s.Json.action_id) { $action = [string] $s.Json.action_id }
    }
    Add-Check 'B.1g' ('supersede ' + $first + ' durch ' + $second + ': alter Entwurf abandoned, Aktion angelegt') ($s.Ok -and $sStatus -eq 'abandoned' -and $action) ($s.Why + ', Status ' + $sStatus + ', Aktion ' + $action)
    if (-not $action) { Stop-Step 'keine Aktion' }
    $script:B1Old = $first
    $script:B1Action = $action
    $aj = Wait-Action $action 5
    $state = ''
    $results = @()
    if ($null -ne $aj) {
        $state = [string] $aj.state
        if ($null -ne $aj.targets) { $results = @(@($aj.targets) | ForEach-Object { [string] $_.result }) }
    }
    $allOk = ($results.Count -gt 0 -and @($results | Where-Object { $_ -ne 'ok' }).Count -eq 0)
    $errText = ''
    if ($null -ne $aj) { $errText = [string] $aj.error }
    $script:B1ActionOk = ($state -eq 'done' -and $allOk)
    Add-Check 'B.1h' ('Aktion ' + $action + ' (Papierkorb): Zustand done, jedes Ziel ok (vor A09: stale, weil der Server die lange Kopfzeile in BODY.PEEK[HEADER] gefaltet liefert)') $script:B1ActionOk ('Zustand ' + $state + ', Ergebnis je Ziel: ' + ($results -join ', ') + ', Fehler ' + $errText)
    if (-not $script:B1ActionOk) { Stop-Step 'Papierkorb-Aktion nicht ok; B.1i und B.1j entfallen (B.3 prueft den Entwurf trotzdem)' }
    # Alte Kopie im Papierkorb laut Spiegel (der Runner verlegt die
    # Verknuepfung nach dem MOVE in den Papierkorb).
    $roles = @()
    $inTrash = $true
    foreach ($t in @($aj.targets)) {
        $sh = Invoke-Pk $script:ExeFull @('show', ([string] $t.message_id), '--json')
        $sj = Get-Json $sh.Out
        $r = @()
        if ($sh.Exit -eq 0 -and $null -ne $sj) { $r = @(@($sj.mailboxes) | Where-Object { $null -ne $_ } | ForEach-Object { [string] $_.role }) }
        $roles += ('Nachricht ' + [string] $t.message_id + ': ' + $(if ($r.Count -gt 0) { $r -join ', ' } else { '(keine Ordnerkopie lesbar)' }))
        if (-not ($r -contains 'trash') -or ($r -contains 'drafts')) { $inTrash = $false }
    }
    Add-Check 'B.1i' 'alte Kopie im Papierkorb (Spiegel: Ordnerrolle trash, keine Kopie mehr im Entwuerfe-Ordner)' $inTrash ($roles -join "`n")
    $old = Get-DraftDetail $script:ExeFull $first
    $okOld = ($null -ne $old -and [string] $old.status -eq 'abandoned' -and [string] $old.closed_reason -eq 'superseded_by_draft' -and
        [string] $old.superseded_by_draft_id -eq $second -and [string] $old.supersede_action_id -eq $action)
    Add-Check 'B.1j' ('Entwurf ' + $first + ': abandoned, closed_reason superseded_by_draft, Nachfolger ' + $second + ', supersede_action_id ' + $action) $okOld ('Status ' + [string] $old.status + ', Grund ' + [string] $old.closed_reason + ', Nachfolger ' + [string] $old.superseded_by_draft_id + ', Aktion ' + [string] $old.supersede_action_id)
    Add-Info 'B.1k' ('Entwurf ' + $second + ' (zweite Fassung) bleibt offen. Nach Teil B: im Webmail loeschen und mit postkauz draft abandon ' + $second + ' schliessen (abnahme_D15.md, Abschnitt 6).')
}

# B.2: Antwort, in Gmail ein Empfaenger ergaenzt (A09 Teil 1).
function Invoke-B2 {
    Write-Log '--- B.2: Antwort-Entwurf, im Webmail einen Empfaenger ergaenzen und senden (Mitmach)'
    if (-not $ReplyToMessage) {
        Add-Skip 'B.2' 'ohne -ReplyToMessage (Nachricht, auf die geantwortet wird); B.2 und B.3c entfallen'
        return
    }
    $n = [int64] 0
    if (-not ([int64]::TryParse($ReplyToMessage.Trim(), [ref] $n) -and $n -gt 0)) {
        Add-Check 'B.2' '-ReplyToMessage ist eine lokale Nachrichten-ID (Zahl)' $false ''
        Stop-Step 'keine gueltige Nachrichten-ID'
    }
    $msg = [string] $n
    $id = New-ReplyDraft $msg "Hallo,`n`ndas ist Abnahme D15 Teil B.2 (Antwort). Bitte in Gmail genau einen Empfaenger ergaenzen und den Text unveraendert senden.`n`nViele Gruesse" 'B.2a'
    if (-not $id) { Stop-Step 'kein Antwort-Entwurf' }
    $script:B2Draft = $id
    $d = Get-DraftDetail $script:ExeFull $id
    $nRcpt = 0
    # Leere Listen kommen als null: nur echte Eintraege zaehlen.
    if ($null -ne $d) { $nRcpt = @(@($d.to) + @($d.cc) | Where-Object { $_ }).Count }
    $okD = ($null -ne $d -and [string] $d.kind -eq 'reply' -and [string] $d.status -eq 'draft' -and [string] $d.in_reply_to -and $nRcpt -ge 1)
    Add-Check 'B.2b' ('Entwurf ' + $id + ': kind reply, offen, In-Reply-To gesetzt, mindestens ein Empfaenger (aus der Ausgangsnachricht)') $okD ('kind ' + [string] $d.kind + ', Status ' + [string] $d.status + ', In-Reply-To gesetzt: ' + [bool] ([string] $d.in_reply_to) + ', Empfaenger (To und Cc): ' + $nRcpt + ' (0: die Ausgangsnachricht kam von einer Adresse dieses Kontos; eine andere waehlen)')
    if (-not $okD) { Stop-Step 'Antwort-Entwurf nicht wie erwartet; nichts im Webmail tun, Entwurf mit draft abandon schliessen' }
    $w = Wait-MirrorCopy $id 12
    Add-Check 'B.2c' ('Entwurf ' + $id + ': Kopie im Spiegel des Entwuerfe-Ordners (im Webmail sichtbar)') $w.Ok $w.Why
    if (-not $w.Ok) { Stop-Step 'Kopie nicht im Spiegel; nicht im Webmail senden' }
    Add-Info 'B.2d' ('JETZT: Entwurf ' + $id + ' (Antwort) im Webmail im Browser oeffnen, GENAU EINEN Empfaenger ergaenzen (eigene Testadresse des Betreibers), den Text NICHT aendern und senden. Das Skript wartet bis zu ' + $WaitMinutes + ' Minuten auf den Sync.')
    $deadline = (Get-Date).AddMinutes($WaitMinutes)
    $lastRequest = Get-Date
    $d = $null
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 15
        $d = Get-DraftDetail $script:ExeFull $id
        if ($null -ne $d -and @('sent_by_human', 'abandoned') -contains [string] $d.status) { break }
        if (((Get-Date) - $lastRequest).TotalSeconds -ge 120) { Request-Sync; $lastRequest = Get-Date }
    }
    $st = ''
    if ($null -ne $d) { $st = [string] $d.status }
    if (@('sent_by_human', 'abandoned') -contains $st) { $script:B2Closed = $true }
    Add-Check 'B.2e' ('Entwurf ' + $id + ' als gesendet erkannt (sent_by_human)') ($st -eq 'sent_by_human') ('Status ' + $st + ', remote ' + [string] $d.remote)
    if ($st -ne 'sent_by_human') { Stop-Step 'nicht als gesendet erkannt; B.2f und B.2g entfallen' }
    $cr = Get-CloseReasons $script:ExeFull
    $reason = [string] $cr.Reasons[[int64] $id]
    $added = -1
    if ($cr.Added.ContainsKey([int64] $id)) { $added = [int] $cr.Added[[int64] $id] }
    Add-Check 'B.2f' 'Audit drafts.sent_detected: Grund thread, recipients_added 1' ($cr.Ok -and $reason -eq 'thread' -and $added -eq 1) ('Audit lesbar: ' + $cr.Ok + ', Grund ' + $reason + ', recipients_added ' + $(if ($added -ge 0) { [string] $added } else { '(fehlt)' }))
    $d = Get-DraftDetail $script:ExeFull $id
    $note = ''
    $creason = ''
    if ($null -ne $d) { $note = [string] $d.closed_note; $creason = [string] $d.closed_reason }
    Add-Check 'B.2g' 'draft show --json: closed_note "recipients added in the mail client (1)", closed_reason leer' ($null -ne $d -and $note -eq 'recipients added in the mail client (1)' -and -not $creason) ('closed_note: ' + $note + "`nclosed_reason: " + $creason)
}

# B.3: geschlossene Entwuerfe ohne Hinweis zur Kopie (A09 Teil 2 (b)).
function Invoke-B3 {
    Write-Log '--- B.3: draft show geschlossener Entwuerfe ohne Hinweis zur Kopie (remote_note)'
    $ids = @()
    if ($script:B1Old) {
        $ids += $script:B1Old
        $d = Get-DraftDetail $script:ExeFull $script:B1Old
        $rn = ''; $pn = ''; $note = ''; $st = ''
        if ($null -ne $d) { $rn = [string] $d.remote_note; $pn = [string] $d.push_note; $note = [string] $d.closed_note; $st = [string] $d.status }
        $detail = 'Status ' + $st + ', remote_note leer: ' + (-not $rn) + ', push_note ' + $pn
        if ($rn -and $pn -eq 'pushed_after_close') {
            Add-Warn 'B.3a' ('Entwurf ' + $script:B1Old + ' (alt, B.1): remote_note nur wegen einer Kopie, die nach dem Schliessen ankam (pushed_after_close; erlaubt)') $detail
        } else {
            Add-Check 'B.3a' ('Entwurf ' + $script:B1Old + ' (alt, B.1): geschlossen, kein remote_note') ($null -ne $d -and $st -eq 'abandoned' -and -not $rn) $detail
        }
        $notMoved = ($note -match 'did not move it')
        if ($script:B1ActionOk) {
            Add-Check 'B.3b' ('Entwurf ' + $script:B1Old + ': closed_note nennt "queued for the trash" (Aktion ok), nicht "did not move it"') ($note -match 'queued for the trash' -and -not $notMoved) ('closed_note: ' + $note)
        } else {
            # Haengt von B.1h ab (dort FAIL): nur nennen, was closed_note sagt.
            Add-Info 'B.3b' ('Aktion aus B.1 nicht ok (B.1h): closed_note nennt "copy still in the Drafts folder; action ' + $script:B1Action + ' did not move it": ' + $notMoved) ('closed_note: ' + $note)
        }
    } else {
        Add-Skip 'B.3a' 'kein ersetzter Entwurf aus B.1 (B.1 vor supersede abgebrochen)'
    }
    if ($script:B2Draft -and $script:B2Closed) {
        $ids += $script:B2Draft
        $d = Get-DraftDetail $script:ExeFull $script:B2Draft
        $rn = ''; $pn = ''; $st = ''
        if ($null -ne $d) { $rn = [string] $d.remote_note; $pn = [string] $d.push_note; $st = [string] $d.status }
        $detail = 'Status ' + $st + ', remote_note leer: ' + (-not $rn) + ', push_note ' + $pn
        if ($rn -and $pn -eq 'pushed_after_close') {
            Add-Warn 'B.3c' ('Entwurf ' + $script:B2Draft + ' (Antwort, B.2): remote_note nur wegen pushed_after_close (erlaubt)') $detail
        } else {
            Add-Check 'B.3c' ('Entwurf ' + $script:B2Draft + ' (Antwort, B.2): geschlossen, kein remote_note') ($null -ne $d -and -not $rn) $detail
        }
    } else {
        Add-Skip 'B.3c' 'kein geschlossener Antwort-Entwurf aus B.2'
    }
    if ($AgentKeyFile -and $ids.Count -gt 0) {
        # mail_get_draft (der Weg eines Agenten) muss dasselbe sagen wie draft show.
        foreach ($i in $ids) {
            $m = Invoke-McpTool 'mail_get_draft' @{ draft_id = [int64] $i }
            Test-McpAlive 'B.3d'
            $c = Get-DraftDetail $script:ExeFull $i
            if ($null -eq $m -or $m.code) {
                Add-Warn 'B.3d' ('mail_get_draft ' + $i + ' nicht lesbar (Agentenschluessel); nur draft show geprueft') (Format-McpError $m)
                continue
            }
            $same = ($null -ne $c -and [string] $m.closed_note -eq [string] $c.closed_note -and [string] $m.closed_reason -eq [string] $c.closed_reason)
            Add-Check 'B.3d' ('mail_get_draft ' + $i + ': kein remote_note, closed_note und closed_reason wie draft show') (-not [string] $m.remote_note -and $same) ('remote_note leer: ' + (-not [string] $m.remote_note) + ', closed_note gleich: ' + $same)
        }
    }
}

function Invoke-TeilB {
    Write-Log '--- Teil B: Proben im eigenen Gmail-Konto (Mitmach)'
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    Add-Check 'B.0' ('Programm meldet ' + $ExpectVersion) ($v.Exit -eq 0 -and $v.Out -match [regex]::Escape($ExpectVersion)) $v.Text
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
    Add-Check 'B.0b' '-SelfAccount ist ein konfiguriertes Konto, eigene Adresse bekannt (-SelfAddress oder accounts list)' ($SelfAccount -and ($known -contains $SelfAccount) -and [bool] $SelfAddress) ('Konten: ' + $known.Count + ', Konto gefunden: ' + ($known -contains $SelfAccount) + ', Adresse bekannt: ' + [bool] $SelfAddress)
    if ($AgentKeyFile) {
        $mcpOk = (Test-Path -LiteralPath $AgentKeyFile) -and (Start-Mcp $AgentKeyFile)
        Add-Check 'B.0c' 'Agentenschluessel: Datei vorhanden, MCP-Sitzung angemeldet (Schluessel nie im Protokoll)' $mcpOk ([string] $script:McpDead)
    } else {
        Add-Info 'B.0c' 'ohne -AgentKeyFile: Probe als Mensch an der CLI (ohne Eigentuemerpruefung und ohne die Ausnahme im Runner)'
    }
    $w = @(Get-OwnWatch)
    $selfWatch = @($w | Where-Object { @($_.Accounts) -contains '*' -or @($_.Accounts) -icontains $SelfAccount }).Count
    Add-Check 'B.0d' 'sync --watch genau mit -Exe laeuft fuer -SelfAccount (legt die Entwuerfe ab, fuehrt die Aktion aus, spiegelt die gesendete Nachricht)' ($selfWatch -gt 0) ('sync --watch: ' + $w.Count + ', davon fuer das Konto: ' + $selfWatch)
    if ($script:NFail -gt 0) { Stop-D15 'Voraussetzungen fehlen' }
    Set-AuditMark $script:ExeFull 'B.0e' ([DateTimeOffset]::Now)
    Invoke-Step 'B.1' { Invoke-B1 }
    Invoke-Step 'B.2' { Invoke-B2 }
    Invoke-Step 'B.3' { Invoke-B3 }
    $script:CompleteB = $true
}

# ---------------------------------------------------------------- Rueckweg

function Invoke-Rueckweg {
    Write-Log '--- Rueckweg: altes Programm zurueck (keine Migration in A09)'
    Add-Check 'R.0' 'aufgehobenes altes Programm vorhanden (postkauz-rc6.exe)' (Test-Path $OldCopy) $OldCopy
    if (-not (Test-Path $OldCopy)) { Stop-D15 'postkauz-rc6.exe fehlt; nichts geaendert' }
    $newCopy = Get-NewCopyName
    $present = Test-Path $script:ExeFull
    if ($present) {
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Check 'R.0b' ('laufendes Programm meldet ' + $ExpectVersion) ($v.Out -match [regex]::Escape($ExpectVersion)) ($v.Text + "`nneues Programm wird beiseitegelegt als " + (Split-Path -Leaf $newCopy))
    } else {
        Add-Info 'R.0b' ($script:ExeFull + ' fehlt (harter Abbruch?); das alte Programm kommt an seine Stelle')
    }
    $o = Invoke-Pk $OldCopy @('version') -NoDirs
    Add-Check 'R.0c' ('postkauz-rc6.exe meldet ' + $OldVersion) ($o.Out -match [regex]::Escape($OldVersion)) $o.Text
    Add-Check 'R.0d' '-Tasks und -Accounts angegeben (Pflicht)' ($Tasks.Count -gt 0 -and $Accounts.Count -gt 0) ('Aufgaben: ' + $Tasks.Count + ', Konten: ' + $Accounts.Count)
    # 0g (f): vor R.1 pruefen, dass jede Aufgabe aus -Tasks vorhanden ist
    # (sonst schaltete R.5 nur einen Teil wieder ein).
    $missingTasks = @($Tasks | Where-Object { $null -eq (Get-ScheduledTask -TaskName $_ -ErrorAction SilentlyContinue) })
    Add-Check 'R.0e' 'jede Aufgabe aus -Tasks vorhanden' ($missingTasks.Count -eq 0) ('Aufgaben: ' + $Tasks.Count + ', nicht gefunden: ' + $missingTasks.Count)
    if ($script:NFail -gt 0) { Stop-D15 'Rueckweg nicht moeglich; nichts geaendert' }
    # Wie D13/D14: erst hier vermerken, dass am Ende alle Aufgaben wieder an
    # sein sollen (ein Abbruch in R.0x schaltet keine ein).
    foreach ($tn in $Tasks) { $script:WasEnabled[$tn] = $true }
    Stop-Tasks 'R.1'
    if ($script:NFail -gt 0) { Stop-D15 'es laeuft noch ein postkauz.exe; nichts getauscht' }
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
        Stop-D15 'Rueckweg gescheitert; Aufgaben bleiben AUS (von Hand nach abnahme_D15.md Abschnitt 5 weiter)'
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
        Stop-D15 'R.2 nicht in Ordnung; kein sync, Aufgaben bleiben AUS (von Hand nach abnahme_D15.md Abschnitt 5 weiter)'
    }
    Invoke-SyncRun 'R.3' ('Rueckweg mit ' + $OldVersion) $OldVersion
    if ($script:NFail -gt 0) {
        $script:KeepTasksOff = $true
        Stop-D15 'R.3 nicht in Ordnung; Aufgaben bleiben AUS (von Hand nach abnahme_D15.md Abschnitt 5 weiter)'
    }
    Add-Info 'R.4' 'Fall: ohne Migration. Mit rc7 geschlossene Entwuerfe (auch Grund thread mit recipients_added) bleiben geschlossen; rc6 zeigt bei ihnen kein closed_note zu ergaenzten Empfaengern, bei geschlossenen Entwuerfen wieder Hinweise zur Kopie, und eine Papierkorb-Aktion mit langer Kopfzeile endet mit rc6 wieder stale.'
    [void] (Start-Tasks 'R.5' $OldVersion)
    $script:Restarted = $true
}

# ---------------------------------------------------------------- Start

$script:ExeFull = [System.IO.Path]::GetFullPath($Exe)
$dir = Split-Path -Parent $script:ExeFull
$script:ExeDir = $dir
$script:SelfAddressFound = ''
if (-not $LogFile) {
    if ($TeilB) { $LogFile = Join-Path $dir 'D15_teilB.txt' } else { $LogFile = Join-Path $dir 'D15_windows.txt' }
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
$OldCopy = Join-Path $dir 'postkauz-rc6.exe'
$script:Swapped = $false
$script:Restarted = $false
$mode = 'Teil A, Umstellung'
if ($Rueckweg) { $mode = 'Rueckweg' }
if ($TeilB) { $mode = 'Teil B' }
Write-Log ('=== Abnahme D15 (' + $mode + '), ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + ' ===')
Write-Log ('Programm: ' + $script:ExeFull + '; Daten: ' + $DataDir + '; Konfiguration: ' + $Config)
$elevated = $false
try { $elevated = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator) } catch { }
Add-Info '0.0' ('PowerShell mit Administratorrechten: ' + $elevated + ' (Anleitung Abschnitt 3: so wie die Aufgaben angelegt sind)')

$guard = $false
try {
    if ($TeilB) { Invoke-TeilB } elseif ($Rueckweg) { Invoke-Rueckweg } else { Invoke-Umstellung }
} catch {
    $msg = [string] $_.Exception.Message
    if ($msg.StartsWith('D15-ABBRUCH: ')) {
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
            Add-Check 'E.1' 'Aufgaben NICHT wieder eingeschaltet: FAIL vor dem Wiedereinschalten oder das richtige Programm liegt nicht sicher an seinem Platz (von Hand nach abnahme_D15.md Abschnitt 5)' $false
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
                if (-not $done41) { Write-Log 'Teil 4 nicht vollstaendig: Aufgaben von Hand pruefen (abnahme_D15.md Abschnitt 3, harter Abbruch).' }
            }
            if (-not $started) {
                try {
                    $before = $script:NFail
                    Stop-Tasks 'E.0a'
                    if ($script:NFail -gt $before) {
                        Add-Check 'E.0b' 'Aufgaben nach 4.1 nicht sauber angehalten: altes Programm NICHT zurueckgelegt, Aufgaben bleiben aus (von Hand nach abnahme_D15.md Abschnitt 5)' $false
                    } elseif (Restore-Program $OldCopy 'E.0') {
                        [void] (Start-Tasks 'E.1' $OldVersion)
                    } else {
                        Add-Check 'E.1' 'Aufgaben angehalten, altes Programm liess sich nicht zuruecklegen (von Hand nach abnahme_D15.md Abschnitt 5)' $false
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
        $cur = Get-ProgramVersion $script:ExeFull
        if (-not $cur) { $cur = '(fehlt oder meldet keine Version)' }
        $keep = 'fehlt'
        if (Test-Path $OldCopy) { $keep = (Get-ProgramVersion $OldCopy) }
        Write-Log ('Stand: ' + $script:ExeFull + ' meldet ' + $cur + '; postkauz-rc6.exe: ' + $keep)
    }
    Write-Log ''
    if ($script:Fails.Count -gt 0) { Write-Log ('Fehlgeschlagen: ' + ($script:Fails -join '; ')) }
    if ($script:LogBroken) { try { Write-Host 'ACHTUNG: Das Protokoll liess sich nicht vollstaendig schreiben (Datentraeger voll oder gesperrt).' } catch { } }
    Write-Log ('D15: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL, ' + $script:NWarn + ' WARN, ' + $script:NSkip + ' SKIP')
    if ($guard) { [void] (Set-CtrlCGuard $false) }
}
if ($script:NFail -gt 0) { exit 1 }
exit 0
