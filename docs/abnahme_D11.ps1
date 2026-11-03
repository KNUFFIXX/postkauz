<#
.SYNOPSIS
    Abnahme D11 (PostKauz, Auftrag A05): Umstellung des Dauerbetriebs von
    0.3.0-rc2 auf 0.3.0-rc3 mit Sicherung, Pruefung und Rueckweg.

.DESCRIPTION
    Laeuft auf dem Buero-PC (Windows 11, Windows PowerShell 5.1) am
    Live-Datenverzeichnis des Dauerbetriebs, ohne Go und ohne Docker.
    Anleitung: docs/abnahme_D11.md. Claude fuehrt das Skript aus.

    Modus "Umstellung" (Standard):
      Teil 0  Voraussetzungen: beide Programme (Version, Pruefsumme),
              Aufgaben vorhanden, Zielordner der Sicherung.
      Teil 1  Mit dem alten Programm (rc2): Aufgaben anhalten und
              abschalten (betrieb.md 1.7), kein postkauz.exe laeuft mehr,
              Stand vorher festhalten (draft list, show der Nachricht mit
              HTML im Klartextteil, doctor), Sicherung (betrieb.md 2.2).
      Teil 2  Programm tauschen: altes als postkauz-rc2.exe aufheben, neues
              an seine Stelle, version, doctor ohne Migration.
      Teil 3  Erster Lauf mit rc3: sync je Konto, draft list nachher
              (offene Entwuerfe von vorher, die jetzt sent_by_human sind,
              mit Grund aus dem Audit), show der Nachricht ohne HTML-Tags
              und mit alternative_mismatch false, doctor ohne neue Warnung,
              audit verify ok.
      Teil 4  Aufgaben wieder einschalten und starten.

    Modus "Rueckweg" (-Rueckweg): A05 hat keine Migration (Schema 22).
      Das alte Programm (postkauz-rc2.exe) kommt zurueck und laeuft weiter:
      doctor ok (kein too_new), show der Nachricht weiterhin ohne HTML (rc2
      liest die mit Stand 4 aufbereiteten Texte), ein sync je Konto ohne
      Fehler; danach Aufgaben wieder an. Eine Wiederherstellung der
      Sicherung ist dann nicht noetig (sie bleibt fuer den Notfall).

    Abbrueche: Fehlt eine Voraussetzung (Teil 0), laeuft noch ein
    postkauz.exe (1.1), ist der Stand vorher nicht lesbar (1.2, 1.3) oder
    fehlt die Sicherung (1.5: Exit 0 und "backup written to"), tauscht das
    Skript nichts. Scheitert 2.1 oder 2.2, kopiert es postkauz-rc2.exe
    zurueck, bevor das neue Programm einen sync macht. Der Rueckweg
    kopiert nur, wenn postkauz.exe die neue Version meldet und
    postkauz-rc3.exe noch nicht existiert. In jedem Fall (auch bei einem
    unerwarteten Fehler) schaltet das Skript die Aufgaben wieder ein, die
    es abgeschaltet hat, und schreibt die Schlusszeile. stderr von sync
    steht nie im Protokoll (dort koennen Ordnernamen stehen), nur
    Exit-Code und Zahl der Zeilen.

    Jede Pruefung ist eine Zeile "OK <Nr> <Beschreibung>",
    "FAIL <Nr> <Beschreibung>" oder "INFO <Nr> <Beschreibung>", darunter
    eingerueckt die Ist-Ausgabe (gekuerzt). Betreff, Adressen und Mailtext
    stehen nie im Protokoll: Entwuerfe nur mit ID, Status und Grund, die
    Nachricht nur mit Kennzahlen (Laenge, Tags gefunden ja/nein).
    Letzte Zeile: "D11: <n> OK, <m> FAIL". Exit 0 nur ohne FAIL.

.PARAMETER Exe
    Das Programm des Dauerbetriebs (Standard C:\PostKauz\postkauz.exe).

.PARAMETER NewExe
    Das neue Programm (postkauz-windows-amd64.exe aus dem Build 0.3.0-rc3).
    Neben ihm muss SHA256SUMS liegen. Im Modus -Rueckweg nicht noetig.

.PARAMETER DataDir
    Live-Datenverzeichnis (Standard C:\PostKauz\data).

.PARAMETER Config
    Live-Konfiguration (Standard C:\PostKauz\config.toml).

.PARAMETER BackupTo
    Ordner der Sicherung (betrieb.md 2.2/2.3): neu, leer oder eine fruehere
    Sicherung, auf einem verschluesselten Datentraeger.

.PARAMETER Accounts
    Konten fuer den ersten Lauf (je ein sync -a <Konto>).

.PARAMETER HtmlMessage
    Lokale ID einer Nachricht, deren Klartextteil HTML-Quelltext enthaelt
    (vorher: show zeigt Tags, alternative_mismatch true). Leer: Pruefungen
    1.4, 3.4 und R.3 entfallen (INFO im Protokoll).

.PARAMETER ExpectVersion
    Version des neuen Programms (Standard 0.3.0-rc3).

.PARAMETER OldVersion
    Version des alten Programms (Standard 0.3.0-rc2).

.PARAMETER Rueckweg
    Statt der Umstellung den Rueckweg auf das alte Programm pruefen.

.PARAMETER LogFile
    Protokolldatei. Standard: D11_windows.txt neben dem Programm.

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d11\abnahme_D11.ps1 -NewExe C:\PostKauz\d11\postkauz-windows-amd64.exe -BackupTo E:\PostKauz-Sicherung -Accounts gmail,shared -HtmlMessage 41234
#>
param(
    [string] $Exe = 'C:\PostKauz\postkauz.exe',
    [string] $NewExe = '',
    [string] $DataDir = 'C:\PostKauz\data',
    [string] $Config = 'C:\PostKauz\config.toml',
    [string] $BackupTo = '',
    [string[]] $Accounts = @(),
    [string] $HtmlMessage = '',
    [string] $ExpectVersion = '0.3.0-rc3',
    [string] $OldVersion = '0.3.0-rc2',
    [switch] $Rueckweg,
    [string] $LogFile = ''
)

$ErrorActionPreference = 'Stop'
# Mit powershell.exe -File kommt "-Accounts a,b" in PS 5.1 als ein Text an
# (wie -Live in abnahme_D9.ps1): selbst trennen.
$Accounts = @($Accounts | ForEach-Object { $_ -split ',' } | ForEach-Object { $_.Trim() } | Where-Object { $_ })
$script:Utf8 = New-Object System.Text.UTF8Encoding $false
$script:NOk = 0
$script:NFail = 0
$script:Log = $null
$script:Fails = New-Object System.Collections.Generic.List[string]
$script:TaskSync = 'PostKauz sync watch'
$script:TaskAudit = 'PostKauz audit export'
$script:TaskHttp = 'PostKauz MCP HTTP'
# Aufgaben, die vor dem Anhalten eingeschaltet waren (nur die kommen wieder an).
$script:WasEnabled = @{}
try { [Console]::OutputEncoding = $script:Utf8 } catch { }

# ---------------------------------------------------------------- Hilfen

# Maskiert API-Schluessel und Adressen (das Protokoll enthaelt ohnehin nur
# Zaehler, IDs und Zustaende; das ist die zweite Sicherung).
function Hide-Secrets {
    param([string] $Text)
    if ($null -eq $Text) { return '' }
    $t = [regex]::Replace($Text, 'pk_key_[A-Za-z0-9_-]+', 'pk_key_********')
    $t = [regex]::Replace($t, '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}', '<adresse>')
    return $t
}

function Write-Log {
    param([string] $Text)
    $t = Hide-Secrets $Text
    if ($script:Log) { [System.IO.File]::AppendAllText($script:Log, $t + "`r`n", $script:Utf8) }
    try { Write-Host $t } catch { }
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
# stderr als UTF-8, mit Zeitgrenze. Umgebungsvariablen von PostKauz werden
# entfernt (kein Schluessel, kein anderes Datenverzeichnis).
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

# Laufende postkauz.exe (alle Pfade), ohne Kommandozeile im Protokoll.
function Get-PkProcesses {
    return @(Get-CimInstance Win32_Process -Filter "Name = 'postkauz.exe'" -ErrorAction SilentlyContinue)
}

function Stop-Tasks {
    param([string] $Nr)
    foreach ($tn in @($script:TaskSync, $script:TaskAudit, $script:TaskHttp)) {
        $t = Get-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue
        if ($null -eq $t) { Add-Info $Nr ('Aufgabe "' + $tn + '" nicht vorhanden'); continue }
        if (-not $script:WasEnabled.ContainsKey($tn)) { $script:WasEnabled[$tn] = ($t.State -ne 'Disabled') }
        try { Stop-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue } catch { }
        Disable-ScheduledTask -TaskName $tn | Out-Null
    }
    # sync --watch und mcp --http beenden sich nach dem Stoppen der Aufgabe;
    # bis zu 60 s warten.
    $deadline = (Get-Date).AddSeconds(60)
    while ((Get-PkProcesses).Count -gt 0 -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 2 }
    $n = (Get-PkProcesses).Count
    Add-Check $Nr 'Aufgaben angehalten und abgeschaltet, kein postkauz.exe laeuft (Claude-App vorher ganz beenden)' ($n -eq 0) ('laufende postkauz.exe: ' + $n)
}

function Start-Tasks {
    param([string] $Nr)
    $started = @()
    foreach ($tn in @($script:TaskSync, $script:TaskAudit, $script:TaskHttp)) {
        if ($null -eq (Get-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue)) { continue }
        if ($script:WasEnabled.ContainsKey($tn) -and -not $script:WasEnabled[$tn]) { continue }
        Enable-ScheduledTask -TaskName $tn | Out-Null
        if ($tn -ne $script:TaskAudit) { Start-ScheduledTask -TaskName $tn; $started += $tn }
    }
    if ($started.Count -eq 0) {
        Add-Info $Nr 'keine Aufgabe fuer den Dauerbetrieb gestartet; von Hand starten (betrieb.md 1.3)'
        return
    }
    $deadline = (Get-Date).AddSeconds(30)
    while ((Get-PkProcesses).Count -lt $started.Count -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 2 }
    $states = @($started | ForEach-Object { $_ + ': ' + (Get-ScheduledTask -TaskName $_).State })
    $running = @($started | Where-Object { (Get-ScheduledTask -TaskName $_).State -eq 'Running' })
    Add-Check $Nr 'Aufgaben wieder eingeschaltet und gestartet (Claude-App danach wieder starten)' ($running.Count -eq $started.Count) (($states -join "`n") + "`npostkauz.exe: " + (Get-PkProcesses).Count)
}

# Entwuerfe in diesen Zustaenden als Liste (ID, Status, Art, remote), ohne
# Betreff (draft list zeigt hoechstens 500, die zuletzt geaenderten zuerst).
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

# Kennzahlen der Nachricht mit HTML im Klartextteil, ohne Inhalt.
function Get-MessageFacts {
    param([string] $Prog, [string] $Id)
    $r = Invoke-Pk $Prog @('show', $Id, '--json')
    $j = Get-Json $r.Out
    if ($null -eq $j) { return New-Object PSObject -Property @{ Ok = $false; Detail = ('[exit ' + $r.Exit + '] keine JSON-Ausgabe') } }
    $text = [string] $j.text
    $tags = [regex]::IsMatch($text, '<(html|head|body|meta|!doctype|p|div|span|table|td|br)\b', 'IgnoreCase')
    $facts = New-Object PSObject -Property @{
        Ok = ($r.Exit -eq 0); Mismatch = [bool] $j.alternative_mismatch; Source = [string] $j.text_source
        Tags = $tags; Length = $text.Length; Hidden = [bool] $j.hidden_text_removed
    }
    $facts | Add-Member -NotePropertyName Detail -NotePropertyValue ('text_source ' + $facts.Source + ', alternative_mismatch ' + $facts.Mismatch +
        ', hidden_text_removed ' + $facts.Hidden + ', HTML-Tags im Text: ' + $tags + ', Laenge ' + $facts.Length + ', exit ' + $r.Exit)
    return $facts
}

# Zahl der Suchtreffer fuer "xmlns" (steht im Suchindex nur, solange
# HTML-Quelltext aus dem Klartextteil dort liegt); -1 = Fehler. search
# --json ist NDJSON; mit --explain steht total in der ersten Zeile.
function Get-XmlnsHits {
    param([string] $Prog)
    $r = Invoke-Pk $Prog @('search', 'xmlns', '--limit', '1', '--explain', '--json')
    if ($r.Exit -ne 0) { return -1 }
    foreach ($line in ($r.Out -split "`r?`n")) {
        $t = $line.Trim()
        if (-not $t.StartsWith('{')) { continue }
        $o = Get-Json $t
        if ($null -ne $o -and $null -ne $o.explain) { return [int] $o.total }
    }
    return -1
}

# Audit-Eintraege drafts.sent_detected der letzten Stunden: audit --json
# ist NDJSON (eine Zeile je Eintrag), wie Get-AuditEvents in abnahme_D4.ps1.
function Get-SentDetected {
    param([string] $Prog)
    $r = Invoke-Pk $Prog @('audit', '--operation', 'drafts.sent_detected', '--since', '3h', '--limit', '1000', '--json')
    $events = New-Object System.Collections.Generic.List[object]
    foreach ($line in ($r.Out -split "`r?`n")) {
        $t = $line.Trim()
        if (-not $t.StartsWith('{')) { continue }
        $o = Get-Json $t
        if ($null -ne $o -and $null -ne $o.operation) { $events.Add($o) }
    }
    return New-Object PSObject -Property @{ Exit = $r.Exit; Events = $events }
}

# Zahl der Zeilen auf stderr (Inhalt nie ins Protokoll: ohne --json stehen
# dort Ordnernamen, und die Protokolle landen in docs/protokolle).
function Get-LineCount {
    param([string] $Text)
    if (-not $Text -or -not $Text.Trim()) { return 0 }
    return @($Text.Trim() -split "`r?`n").Count
}

function Get-DoctorWarnings {
    param([string] $Prog)
    $r = Invoke-Pk $Prog @('doctor', '--json')
    $j = Get-Json $r.Out
    $w = @()
    $db = ''
    if ($null -ne $j) {
        if ($null -ne $j.warnings) { $w = @($j.warnings | ForEach-Object { [string] $_ }) }
        $db = [string] $j.database
    }
    return New-Object PSObject -Property @{ Exit = $r.Exit; Warnings = $w; Database = $db; Text = $r.Text }
}

# Warnungen ohne wechselnde Zahlen vergleichen (Zaehler, Zeiten).
function Get-WarningKey {
    param([string] $W)
    return [regex]::Replace($W, '[0-9]+', '#')
}

# ---------------------------------------------------------------- Ablauf

# Geplanter Abbruch: Die Pruefung, die zum Abbruch fuehrt, ist schon als
# FAIL gezaehlt; finally schaltet die Aufgaben wieder ein und schreibt die
# Schlusszeile.
function Stop-D11 {
    param([string] $Why)
    throw ('D11-ABBRUCH: ' + $Why)
}

# Teil 0 bis 4: Umstellung rc2 -> rc3.
function Invoke-Umstellung {
    # ------------------------------------------------------------ Teil 0
    Write-Log '--- Teil 0: Voraussetzungen'
    $newFull = ''
    if ($NewExe) { $newFull = [System.IO.Path]::GetFullPath($NewExe) }
    Add-Check '0.1' 'neues Programm angegeben und vorhanden' ($newFull -and (Test-Path $newFull)) $newFull
    if (-not ($newFull -and (Test-Path $newFull))) { Stop-D11 'neues Programm fehlt' }
    $sums = Join-Path (Split-Path -Parent $newFull) 'SHA256SUMS'
    $hash = Get-Sha256 $newFull
    $want = ''
    if (Test-Path $sums) {
        foreach ($l in [System.IO.File]::ReadAllLines($sums)) { if ($l -match '^([0-9a-f]{64})\s+\*?postkauz-windows-amd64\.exe$') { $want = $Matches[1] } }
    }
    Add-Check '0.2' 'Pruefsumme des neuen Programms passt zu SHA256SUMS' ($want -and $hash -eq $want) ('sha256 ' + $hash + "`nSHA256SUMS " + $want)
    $v = Invoke-Pk $newFull @('version') -NoDirs
    Add-Check '0.3' ('neues Programm meldet ' + $ExpectVersion) ($v.Exit -eq 0 -and $v.Out -match [regex]::Escape($ExpectVersion)) $v.Text
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    Add-Check '0.4' ('altes Programm meldet ' + $OldVersion) ($v.Exit -eq 0 -and $v.Out -match [regex]::Escape($OldVersion)) $v.Text
    Add-Check '0.5' 'Ordner der Sicherung angegeben' ([bool] $BackupTo) $BackupTo
    Add-Check '0.6' 'mindestens ein Konto fuer den ersten Lauf angegeben' ($Accounts.Count -gt 0) ('Konten: ' + $Accounts.Count)
    if ($script:NFail -gt 0) { Stop-D11 'Voraussetzungen fehlen; nichts geaendert' }

    # ------------------------------------------------------------ Teil 1
    Write-Log '--- Teil 1: Stand vorher (altes Programm), Sicherung'
    Stop-Tasks '1.1'
    if ($script:NFail -gt 0) { Stop-D11 'es laeuft noch ein postkauz.exe (Claude-App oder Aufgabe); nichts getauscht' }
    $script:Before = Get-Drafts $script:ExeFull @('draft', 'pending_approval', 'ready')
    Add-Check '1.2' ('draft list vorher: ' + $script:Before.List.Count + ' offene Entwuerfe') ($script:Before.Exit -eq 0 -and $script:Before.Parsed) ('JSON lesbar: ' + $script:Before.Parsed + "`n" + (Format-Drafts $script:Before.List))
    $script:DocBefore = Get-DoctorWarnings $script:ExeFull
    Add-Check '1.3' 'doctor vorher' ($script:DocBefore.Database -eq 'ok') ('database ' + $script:DocBefore.Database + "`n" + (($script:DocBefore.Warnings) -join "`n"))
    # Ohne Liste vorher waere 3.2 wertlos (leere Liste = OK).
    if ($script:NFail -gt 0) { Stop-D11 'Stand vorher nicht lesbar; nichts getauscht' }
    if ($HtmlMessage) {
        $f = Get-MessageFacts $script:ExeFull $HtmlMessage
        Add-Info '1.4' ('Nachricht ' + $HtmlMessage + ' vorher (erwartet: HTML-Tags im Text, alternative_mismatch meist true)') $f.Detail
    } else {
        Add-Info '1.4' 'keine Nachricht mit HTML im Klartextteil angegeben (-HtmlMessage)'
    }
    $script:XmlnsBefore = Get-XmlnsHits $script:ExeFull
    Add-Info '1.4b' ('Suchtreffer fuer "xmlns" vorher: ' + $script:XmlnsBefore)
    $b = Invoke-Pk $script:ExeFull @('backup', 'create', '--to', $BackupTo) -TimeoutSec 7200
    Add-Check '1.5' 'Sicherung geschrieben (betrieb.md 2.2): Exit 0 und "backup written to"' ($b.Exit -eq 0 -and $b.Out -match 'backup written to') $b.Text
    if ($script:NFail -gt 0) { Stop-D11 'ohne Sicherung kein Tausch' }

    # ------------------------------------------------------------ Teil 2
    Write-Log '--- Teil 2: Programm tauschen'
    # Eigenes try/catch: Auch eine Ausnahme (etwa: das neue Programm startet
    # nicht) bringt das alte Programm zurueck, bevor finally die Aufgaben
    # wieder einschaltet; sonst liefe der Dauerbetrieb mit dem ungeprueften
    # Programm weiter.
    $why = ''
    try {
        Copy-Item -LiteralPath $script:ExeFull -Destination $OldCopy -Force
        # Ab hier gilt der Tausch als begonnen (auch ein halb geschriebenes
        # Programm wird zurueckkopiert).
        $script:Swapped = $true
        Copy-Item -LiteralPath $newFull -Destination $script:ExeFull -Force
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Check '2.1' ('getauscht: ' + $script:ExeFull + ' meldet ' + $ExpectVersion + ', altes Programm als postkauz-rc2.exe aufgehoben') ($v.Out -match [regex]::Escape($ExpectVersion) -and (Test-Path $OldCopy)) $v.Text
        $d = Get-DoctorWarnings $script:ExeFull
        Add-Check '2.2' 'doctor: database ok (keine Migration, Schema 22)' ($d.Database -eq 'ok') ('database ' + $d.Database)
        if ($script:NFail -gt 0) { $why = 'Tausch nicht in Ordnung' }
    } catch {
        Add-Check '2.x' ('Fehler beim Tausch: ' + $_.Exception.Message) $false
        $why = 'Fehler beim Tausch'
    }
    if ($why) {
        # Vor dem ersten sync zurueck auf das alte Programm.
        if ($script:Swapped -and (Test-Path $OldCopy)) { Copy-Item -LiteralPath $OldCopy -Destination $script:ExeFull -Force; $script:Swapped = $false }
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Info '2.3' ('altes Programm zurueckkopiert, meldet: ' + $v.Out.Trim())
        Stop-D11 ($why + '; altes Programm wieder in Betrieb, kein sync mit dem neuen')
    }

    # ------------------------------------------------------------ Teil 3
    Write-Log '--- Teil 3: erster Lauf mit dem neuen Programm'
    foreach ($acc in $Accounts) {
        $s = Invoke-Pk $script:ExeFull @('sync', '-a', $acc, '--json') -TimeoutSec 3600
        $j = Get-Json $s.Out
        $line = 'exit ' + $s.Exit + ', stderr ' + (Get-LineCount $s.Err) + ' Zeilen'
        if ($null -ne $j) {
            $line += ', neu ' + $j.new + ', Fehler ' + $j.errors
            $dc = $j.draft_check
            if ($null -ne $dc) {
                $line += ', sent_by_message_id ' + [int] $dc.sent_by_message_id + ', sent_by_fallback ' + [int] $dc.sent_by_fallback +
                    ', sent_by_thread ' + [int] $dc.sent_by_thread + ', missing ' + [int] $dc.missing + ', taken_over ' + [int] $dc.taken_over
            }
        }
        Add-Check '3.1' 'sync -a <Konto> (erster Lauf, bereitet betroffene Texte neu auf)' ($s.Exit -eq 0) $line
    }
    $after = Get-Drafts $script:ExeFull @('draft', 'pending_approval', 'ready', 'sent_by_human')
    $closed = @()
    foreach ($o in $script:Before.List) {
        $n = $after.List | Where-Object { $_.Id -eq $o.Id } | Select-Object -First 1
        if ($null -ne $n -and $n.Status -eq 'sent_by_human') { $closed += $n }
    }
    $au = Get-SentDetected $script:ExeFull
    $reasons = @{}
    foreach ($ev in $au.Events) {
        foreach ($tg in $ev.targets) { if ([string] $tg -match '^draft:([0-9]+)$') { $reasons[[int64] $Matches[1]] = [string] $ev.reason } }
    }
    $lines = @()
    $missingReason = 0
    foreach ($c in $closed) {
        $r = $reasons[[int64] $c.Id]
        if (-not $r) { $missingReason++; $r = '(kein Audit-Eintrag)' }
        $lines += ('draft ' + $c.Id + ': sent_by_human, Grund ' + $r)
    }
    $open = @($after.List | Where-Object { @('draft', 'pending_approval', 'ready') -contains $_.Status })
    Add-Check '3.2' ('draft list nachher: ' + $closed.Count + ' offene Entwuerfe von vorher jetzt sent_by_human, jeder mit Grund im Audit (message_id, fallback oder thread)') ($after.Exit -eq 0 -and $after.Parsed -and $au.Exit -eq 0 -and $missingReason -eq 0) ((@($lines) -join "`n") + "`n--- noch offen:`n" + (Format-Drafts $open))
    Add-Info '3.3' 'Betreiber: Sind die noch offenen Entwuerfe wirklich nicht gesendet? Und die geschlossenen wirklich gesendet? (draft show <id> zeigt den Betreff nur am Bildschirm)'
    if ($HtmlMessage) {
        $f = Get-MessageFacts $script:ExeFull $HtmlMessage
        Add-Check '3.4' ('show ' + $HtmlMessage + ': keine HTML-Tags, text_source html, alternative_mismatch false') ($f.Ok -and -not $f.Tags -and -not $f.Mismatch -and $f.Source -eq 'html' -and $f.Length -gt 0) $f.Detail
    } else {
        Add-Info '3.4' 'uebersprungen (keine -HtmlMessage)'
    }
    $xa = Get-XmlnsHits $script:ExeFull
    Add-Info '3.4b' ('Suchtreffer fuer "xmlns" nachher: ' + $xa + ' (vorher ' + $script:XmlnsBefore + '; erwartet deutlich weniger, 0 bis auf Mails, die das Wort im sichtbaren Text haben)')
    $docAfter = Get-DoctorWarnings $script:ExeFull
    $old = @{}
    foreach ($w in $script:DocBefore.Warnings) { $old[(Get-WarningKey $w)] = $true }
    $new = @($docAfter.Warnings | Where-Object { -not $old.ContainsKey((Get-WarningKey $_)) })
    Add-Check '3.5' 'doctor ohne neue Warnung' ($docAfter.Database -eq 'ok' -and $new.Count -eq 0) ('database ' + $docAfter.Database + "`nneu: " + (($new) -join "`n") + "`nalle: " + (($docAfter.Warnings) -join "`n"))
    $av = Invoke-Pk $script:ExeFull @('audit', 'verify', '--json') -TimeoutSec 1800
    $avj = Get-Json $av.Out
    Add-Check '3.6' 'audit verify: ok' ($av.Exit -eq 0 -and $null -ne $avj -and $avj.status -eq 'ok') ('exit ' + $av.Exit + ', status ' + [string] $avj.status)

    # ------------------------------------------------------------ Teil 4
    Write-Log '--- Teil 4: Dauerbetrieb wieder an'
    Start-Tasks '4.1'
    $script:Restarted = $true
}

# Rueckweg rc3 -> rc2 (A05 hat keine Migration).
function Invoke-Rueckweg {
    Write-Log '--- Rueckweg: altes Programm zurueck (keine Migration in A05)'
    $rc3Copy = Join-Path (Split-Path -Parent $script:ExeFull) 'postkauz-rc3.exe'
    Add-Check 'R.0' 'aufgehobenes altes Programm vorhanden (postkauz-rc2.exe)' (Test-Path $OldCopy) $OldCopy
    if (-not (Test-Path $OldCopy)) { Stop-D11 'postkauz-rc2.exe fehlt; nichts geaendert' }
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    Add-Check 'R.0b' ('laufendes Programm meldet ' + $ExpectVersion + ' und postkauz-rc3.exe gibt es noch nicht (sonst nichts ueberschreiben)') ($v.Out -match [regex]::Escape($ExpectVersion) -and -not (Test-Path $rc3Copy)) ($v.Text + "`npostkauz-rc3.exe vorhanden: " + (Test-Path $rc3Copy))
    $o = Invoke-Pk $OldCopy @('version') -NoDirs
    Add-Check 'R.0c' ('postkauz-rc2.exe meldet ' + $OldVersion) ($o.Out -match [regex]::Escape($OldVersion)) $o.Text
    if ($script:NFail -gt 0) { Stop-D11 'Rueckweg nicht moeglich; nichts geaendert' }
    Stop-Tasks 'R.1'
    if ($script:NFail -gt 0) { Stop-D11 'es laeuft noch ein postkauz.exe; nichts getauscht' }
    Copy-Item -LiteralPath $script:ExeFull -Destination $rc3Copy
    Copy-Item -LiteralPath $OldCopy -Destination $script:ExeFull -Force
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    $d = Get-DoctorWarnings $script:ExeFull
    Add-Check 'R.2' ('altes Programm laeuft mit der Datenbank: ' + $OldVersion + ', database ok (kein too_new)') ($v.Out -match [regex]::Escape($OldVersion) -and $d.Database -eq 'ok') ($v.Text + "`ndatabase " + $d.Database)
    if ($HtmlMessage) {
        $f = Get-MessageFacts $script:ExeFull $HtmlMessage
        Add-Check 'R.3' ('show ' + $HtmlMessage + ' mit ' + $OldVersion + ': weiter ohne HTML-Tags (Texte mit Stand 4 bleiben)') ($f.Ok -and -not $f.Tags -and -not $f.Mismatch) $f.Detail
    }
    foreach ($acc in $Accounts) {
        $s = Invoke-Pk $script:ExeFull @('sync', '-a', $acc, '--json') -TimeoutSec 3600
        $j = Get-Json $s.Out
        $line = 'exit ' + $s.Exit + ', stderr ' + (Get-LineCount $s.Err) + ' Zeilen'
        if ($null -ne $j) { $line += ', neu ' + $j.new + ', Fehler ' + $j.errors }
        Add-Check 'R.4' ('sync -a <Konto> mit ' + $OldVersion + ' ohne Fehler') ($s.Exit -eq 0) $line
    }
    Add-Info 'R.5' 'Fall: ohne Migration. Die Sicherung wird nicht zurueckgespielt. Grenze: Nach einem spaeteren Wechsel auf rc3 erfasst dessen erster Lauf nur Teil 3 nach (HTML im Klartext); Entwuerfe aus der rc2-Zeit, die nur der Rueckfall von Teil 1 erkannt haette, schliesst mark-sent oder der Weg thread (abnahme_D11.md, Abschnitt 5).'
    Start-Tasks 'R.6'
    $script:Restarted = $true
}

$script:ExeFull = [System.IO.Path]::GetFullPath($Exe)
$dir = Split-Path -Parent $script:ExeFull
if (-not $LogFile) { $LogFile = Join-Path $dir 'D11_windows.txt' }
$script:Log = $LogFile
$OldCopy = Join-Path $dir 'postkauz-rc2.exe'
$script:Swapped = $false
$script:Restarted = $false
$mode = 'Umstellung'
if ($Rueckweg) { $mode = 'Rueckweg' }
Write-Log ('=== Abnahme D11 (' + $mode + '), ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + ' ===')
Write-Log ('Programm: ' + $script:ExeFull + '; Daten: ' + $DataDir + '; Konfiguration: ' + $Config)

try {
    if ($Rueckweg) { Invoke-Rueckweg } else { Invoke-Umstellung }
} catch {
    $msg = [string] $_.Exception.Message
    if ($msg.StartsWith('D11-ABBRUCH: ')) {
        Write-Log ('Abbruch: ' + $msg.Substring(13))
    } else {
        Add-Check 'X' ('unerwarteter Fehler im Skript: ' + $msg) $false
    }
} finally {
    # Aufgaben wieder einschalten, wenn das Skript sie abgeschaltet hat und
    # nicht bis zum Ende kam.
    if ($script:WasEnabled.Count -gt 0 -and -not $script:Restarted) {
        try { Start-Tasks 'E.1' } catch { Add-Check 'E.1' ('Aufgaben wieder einschalten: ' + $_.Exception.Message) $false }
    }
    if ($script:Swapped -and -not $Rueckweg) {
        Write-Log ('Stand: ' + $script:ExeFull + ' ist das neue Programm; das alte liegt als postkauz-rc2.exe daneben.')
    }
    Write-Log ''
    if ($script:Fails.Count -gt 0) { Write-Log ('Fehlgeschlagen: ' + ($script:Fails -join '; ')) }
    Write-Log ('D11: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL')
}
if ($script:NFail -gt 0) { exit 1 }
exit 0
