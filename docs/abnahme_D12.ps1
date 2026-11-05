<#
.SYNOPSIS
    Abnahme D12 (PostKauz, Auftrag A06): Umstellung des Dauerbetriebs von
    0.3.0-rc3 auf 0.3.0-rc4 mit Sicherung, Pruefung und Rueckweg (Teil A)
    und Messung des Gmail-Zitats an einer Mail an sich selbst (Teil B).

.DESCRIPTION
    Laeuft auf dem Buero-PC (Windows 11, Windows PowerShell 5.1) am
    Live-Datenverzeichnis des Dauerbetriebs, ohne Go und ohne Docker.
    Anleitung: docs/abnahme_D12.md. Claude fuehrt das Skript aus.

    Teil A "Umstellung" (Standard):
      Teil 0  Voraussetzungen: beide Programme (Version, Pruefsumme),
              Aufgaben (-Tasks ist Pflicht), Zielordner der Sicherung,
              Erwartung (-ExpectSent, -ExpectReplaced).
      Teil 1  Mit dem alten Programm (rc3): Aufgaben anhalten und
              abschalten, kein postkauz.exe laeuft mehr (mit -KeepStdioMcp
              ausser dem stdio-MCP der Claude-App), Stand vorher (draft
              list, doctor: database ok und Schema 22, Entwurf mit Komma
              oder @ im Empfaengernamen), Sicherung (betrieb.md 2.2).
      Teil 2  Programm tauschen: altes als postkauz-rc3.exe aufheben
              (umbenennen statt ueberschreiben mit -KeepStdioMcp), neues an
              seine Stelle, version, doctor ohne Migration.
      Teil 3  Erster vollstaendiger Lauf mit rc4: sync je Konto; ALLE im
              Lauf geschlossenen Entwuerfe mit ID, Status und Grund aus dem
              Audit, Vergleich mit -ExpectSent und -ExpectReplaced (ein
              nicht erwarteter oder ein fehlender Abschluss ist FAIL);
              Adressen: draft edit <CommaDraft> --dry-run ohne Aenderung
              und draft request <CommaDraft> --dry-run ohne invalid_address
              (kein neuer Entwurf); doctor ohne neue Warnung, Schema 22;
              audit verify ok. Bei einem FAIL in Teil 3 kommt das alte
              Programm zurueck (der Abschluss der Entwuerfe bleibt, aber der
              Dauerbetrieb schliesst nicht mit der neuen Regel weiter).
      Teil 4  Aufgaben wieder einschalten und starten.

    Teil B (-TeilB -SelfMessage <id>, Mitmach wie D5): Antwort-Entwurf auf
      eine eigene Mail an sich selbst, die selbst ein ">"-Zitat enthaelt.
      Das Skript legt ihn an, wartet, bis sync --watch ihn abgelegt hat,
      bittet um das unveraenderte Senden in Gmail im Browser, wartet mit
      Zeitgrenze auf den Sync, findet die gesendete Nachricht ueber
      sent_message_id des Entwurfs (sonst ueber Betreff und Datum) und
      zeigt ohne Inhalte: Zahl der Zeilen ab der Einleitungszeile mit und
      ohne ">" (leere ohne Zaehlung) und den Grund, mit dem der Entwurf
      geschlossen wurde. Die Aufgaben laufen dabei weiter.

    Modus "Rueckweg" (-Rueckweg): A06 hat keine Migration (Schema 22). Das
      alte Programm (postkauz-rc3.exe) kommt zurueck und laeuft weiter;
      eine Wiederherstellung der Sicherung ist nicht noetig.

    Jede Pruefung ist eine Zeile "OK <Nr> ...", "FAIL <Nr> ..." oder
    "INFO <Nr> ...", darunter eingerueckt die Ist-Ausgabe (gekuerzt).
    Betreff, Adressen und Mailtext stehen nie im Protokoll: Entwuerfe nur
    mit ID, Status und Grund, Nachrichten nur mit Kennzahlen. Letzte Zeile:
    "D12: <n> OK, <m> FAIL". Exit 0 nur ohne FAIL.

.PARAMETER Exe
    Das Programm des Dauerbetriebs (Standard C:\PostKauz\postkauz.exe).

.PARAMETER NewExe
    Das neue Programm (postkauz-windows-amd64.exe aus dem Build 0.3.0-rc4).
    Neben ihm muss SHA256SUMS liegen. Nur fuer Teil A.

.PARAMETER DataDir
    Live-Datenverzeichnis (Standard C:\PostKauz\data).

.PARAMETER Config
    Live-Konfiguration (Standard C:\PostKauz\config.toml).

.PARAMETER BackupTo
    Ordner der Sicherung (betrieb.md 2.2/2.3): neu, leer oder eine fruehere
    Sicherung, auf einem verschluesselten Datentraeger.

.PARAMETER Accounts
    Konten fuer den ersten Lauf (je ein sync -a <Konto>; mehrere mit Komma).

.PARAMETER Tasks
    Pflicht in Teil A und im Rueckweg: alle geplanten Aufgaben, die
    PostKauz starten, mit Komma getrennt (Namen nicht raten; der Buero-PC
    hat drei "sync watch" je Konto, "audit export" und "mcp http").
    Aufgaben mit "audit export" im Namen werden am Ende nur eingeschaltet.

.PARAMETER ExpectSent
    IDs der Entwuerfe, die der erste Lauf als sent_by_human schliessen soll
    (mit Komma, z. B. 3,15; leer = keiner). Keine IDs im Skript.

.PARAMETER ExpectReplaced
    IDs der Entwuerfe, die der erste Lauf als ersetzt (abandoned, Grund
    replaced) schliessen soll (z. B. 4; leer = keiner).

.PARAMETER CommaDraft
    ID eines bestehenden offenen Antwort-Entwurfs, dessen Empfaengername
    ein Komma oder ein @ enthaelt. Leer: 1.4 und 3.4 entfallen (INFO).

.PARAMETER ExpectVersion
    Version des neuen Programms (Standard 0.3.0-rc4).

.PARAMETER OldVersion
    Version des alten Programms (Standard 0.3.0-rc3).

.PARAMETER Rueckweg
    Statt der Umstellung den Rueckweg auf das alte Programm pruefen.

.PARAMETER KeepStdioMcp
    Der stdio-MCP der Claude-App (postkauz.exe mcp ohne --http) darf
    weiterlaufen. 1.1 zaehlt dann nur die uebrigen postkauz.exe; das
    Programm wird per Umbenennen getauscht. Nur ohne Migration moeglich
    (A06 hat keine).

.PARAMETER TeilB
    Teil B statt Teil A ausfuehren (braucht -SelfMessage).

.PARAMETER SelfMessage
    Lokale ID einer eigenen Mail an sich selbst, deren Text ein ">"-Zitat
    enthaelt (Teil B).

.PARAMETER WaitMinutes
    Teil B: so lange wartet das Skript nach der Bitte um das Senden auf
    den Sync (Standard 20).

.PARAMETER LogFile
    Protokolldatei. Standard: D12_windows.txt (Teil B: D12_teilB.txt)
    neben dem Programm.

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d12\abnahme_D12.ps1 -NewExe C:\PostKauz\d12\postkauz-windows-amd64.exe -BackupTo E:\PostKauz-Sicherung -Accounts a,b,c -Tasks "PostKauz sync watch a,PostKauz sync watch b,PostKauz sync watch c,PostKauz audit export,PostKauz mcp http" -ExpectSent 3,15 -ExpectReplaced 4 -CommaDraft 21 -KeepStdioMcp
#>
param(
    [string] $Exe = 'C:\PostKauz\postkauz.exe',
    [string] $NewExe = '',
    [string] $DataDir = 'C:\PostKauz\data',
    [string] $Config = 'C:\PostKauz\config.toml',
    [string] $BackupTo = '',
    [string[]] $Accounts = @(),
    [string[]] $Tasks = @(),
    [string[]] $ExpectSent = @(),
    [string[]] $ExpectReplaced = @(),
    [string] $CommaDraft = '',
    [string] $ExpectVersion = '0.3.0-rc4',
    [string] $OldVersion = '0.3.0-rc3',
    [switch] $Rueckweg,
    [switch] $KeepStdioMcp,
    [switch] $TeilB,
    [string] $SelfMessage = '',
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
$ExpectSent = @(Split-List $ExpectSent | ForEach-Object { [int64] $_ })
$ExpectReplaced = @(Split-List $ExpectReplaced | ForEach-Object { [int64] $_ })
$script:Utf8 = New-Object System.Text.UTF8Encoding $false
$script:NOk = 0
$script:NFail = 0
$script:Log = $null
$script:Fails = New-Object System.Collections.Generic.List[string]
$script:WasEnabled = @{}
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

function Get-PkProcesses {
    return @(Get-CimInstance Win32_Process -Filter "Name = 'postkauz.exe'" -OperationTimeoutSec 5 -ErrorAction SilentlyContinue)
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

function Test-AuditTask {
    param([string] $Name)
    return ($Name -match '(?i)audit export')
}

function Stop-Tasks {
    param([string] $Nr)
    foreach ($tn in $Tasks) {
        $t = Get-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue
        if ($null -eq $t) { Add-Info $Nr ('Aufgabe "' + $tn + '" nicht vorhanden'); continue }
        if (-not $script:WasEnabled.ContainsKey($tn)) { $script:WasEnabled[$tn] = ($t.State -ne 'Disabled') }
        try { Stop-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue } catch { }
        Disable-ScheduledTask -TaskName $tn | Out-Null
    }
    $helpers = @(Get-CimInstance Win32_Process -Filter "Name = 'powershell.exe' OR Name = 'pwsh.exe'" -OperationTimeoutSec 5 -ErrorAction SilentlyContinue |
        Where-Object { $_.ProcessId -ne $PID -and ([string] $_.CommandLine) -match '(?i)run-?sync-?watch\.ps1' })
    foreach ($h in $helpers) { try { Stop-Process -Id $h.ProcessId -Force -ErrorAction Stop } catch { } }
    $isService = { param($P) ([string] $P.CommandLine) -match '(?i)\s(sync|audit)(\s|$)|--http' }
    $deadline = (Get-Date).AddSeconds(20)
    while (@(Get-PkProcesses | Where-Object { & $isService $_ }).Count -gt 0 -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 2 }
    $killed = 0
    foreach ($p in @(Get-PkProcesses | Where-Object { & $isService $_ })) {
        try { Stop-Process -Id $p.ProcessId -Force -ErrorAction Stop; $killed++ } catch { }
    }
    Add-Info $Nr ('Hilfsskripte beendet: ' + $helpers.Count + '; postkauz.exe (sync, --http, audit) nach 20 s beendet: ' + $killed)
    $deadline = (Get-Date).AddSeconds(30)
    while ((Get-BlockingPk).Count -gt 0 -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 2 }
    $n = (Get-BlockingPk).Count
    if ($KeepStdioMcp) {
        $stdio = @(Get-PkProcesses | Where-Object { Test-StdioMcp $_ }).Count
        Add-Check $Nr 'Aufgaben angehalten und abgeschaltet, ausser dem stdio-MCP der Claude-App laeuft kein postkauz.exe' ($n -eq 0) ('uebrige postkauz.exe: ' + $n + ', stdio-MCP (darf laufen): ' + $stdio)
    } else {
        Add-Check $Nr 'Aufgaben angehalten und abgeschaltet, kein postkauz.exe laeuft (Claude-App vorher ganz beenden)' ($n -eq 0) ('laufende postkauz.exe: ' + $n)
    }
}

function Start-Tasks {
    param([string] $Nr)
    $started = @()
    foreach ($tn in $Tasks) {
        if ($null -eq (Get-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue)) { continue }
        if ($script:WasEnabled.ContainsKey($tn) -and -not $script:WasEnabled[$tn]) { continue }
        Enable-ScheduledTask -TaskName $tn | Out-Null
        if (-not (Test-AuditTask $tn)) { Start-ScheduledTask -TaskName $tn; $started += $tn }
    }
    if ($started.Count -eq 0) {
        Add-Info $Nr 'keine Aufgabe fuer den Dauerbetrieb gestartet; von Hand starten (betrieb.md 1.3)'
        return
    }
    $deadline = (Get-Date).AddSeconds(30)
    while ((Get-BlockingPk).Count -lt $started.Count -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 2 }
    $states = @($started | ForEach-Object { $_ + ': ' + (Get-ScheduledTask -TaskName $_).State })
    $running = @($started | Where-Object { (Get-ScheduledTask -TaskName $_).State -eq 'Running' })
    $hint = 'Claude-App danach wieder starten'
    if ($KeepStdioMcp) { $hint = 'Claude-App nach D12 neu starten, damit ihr MCP das neue Programm nimmt' }
    Add-Check $Nr ('Aufgaben wieder eingeschaltet und gestartet (' + $hint + ')') ($running.Count -eq $started.Count) (($states -join "`n") + "`npostkauz.exe ohne stdio-MCP: " + (Get-BlockingPk).Count)
}

# Programm an $script:ExeFull durch $From ersetzen; das bisherige bleibt als
# $Keep (mit -KeepStdioMcp per Umbenennen, sonst per Kopie; D11 Abschnitt 6).
function Install-Program {
    param([string] $From, [string] $Keep)
    if ($KeepStdioMcp) {
        if (Test-Path $Keep) { Remove-Item -LiteralPath $Keep -Force }
        Rename-Item -LiteralPath $script:ExeFull -NewName (Split-Path -Leaf $Keep)
        $script:Swapped = $true
        Copy-Item -LiteralPath $From -Destination $script:ExeFull -Force
    } else {
        Copy-Item -LiteralPath $script:ExeFull -Destination $Keep -Force
        $script:Swapped = $true
        Copy-Item -LiteralPath $From -Destination $script:ExeFull -Force
    }
}

function Restore-Program {
    param([string] $Keep)
    if ($KeepStdioMcp) {
        if (Test-Path $script:ExeFull) { Remove-Item -LiteralPath $script:ExeFull -Force }
        Rename-Item -LiteralPath $Keep -NewName (Split-Path -Leaf $script:ExeFull)
    } else {
        Copy-Item -LiteralPath $Keep -Destination $script:ExeFull -Force
    }
    $script:Swapped = $false
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

# Kennzahlen der Empfaenger eines Entwurfs ohne Inhalt: Hat ein Name ein
# Komma oder ein @ (der Teil vor "<")? Steht er in Anfuehrungszeichen?
function Get-NameFacts {
    param($D)
    $special = 0
    $quoted = 0
    foreach ($a in @($D.to) + @($D.cc)) {
        $s = [string] $a
        $i = $s.LastIndexOf('<')
        if ($i -le 0) { continue }
        $name = $s.Substring(0, $i).Trim()
        if ($name -match '[,@]') {
            $special++
            if ($name.StartsWith('"') -and $name.EndsWith('"')) { $quoted++ }
        }
    }
    return New-Object PSObject -Property @{ Special = $special; Quoted = $quoted }
}

# Audit-Eintraege der letzten Stunden zu einer Operation (NDJSON).
function Get-AuditOp {
    param([string] $Prog, [string] $Op)
    $r = Invoke-Pk $Prog @('audit', '--operation', $Op, '--since', '3h', '--limit', '1000', '--json')
    $events = New-Object System.Collections.Generic.List[object]
    foreach ($line in ($r.Out -split "`r?`n")) {
        $t = $line.Trim()
        if (-not $t.StartsWith('{')) { continue }
        $o = Get-Json $t
        if ($null -ne $o -and $null -ne $o.operation) { $events.Add($o) }
    }
    return New-Object PSObject -Property @{ Exit = $r.Exit; Events = $events }
}

# Grund je Entwurf aus drafts.sent_detected und drafts.replaced_detected.
function Get-CloseReasons {
    param([string] $Prog)
    $reasons = @{}
    $ok = $true
    foreach ($op in @('drafts.sent_detected', 'drafts.replaced_detected')) {
        $au = Get-AuditOp $Prog $op
        if ($au.Exit -ne 0) { $ok = $false }
        foreach ($ev in $au.Events) {
            foreach ($tg in $ev.targets) { if ([string] $tg -match '^draft:([0-9]+)$') { $reasons[[int64] $Matches[1]] = [string] $ev.reason } }
        }
    }
    return New-Object PSObject -Property @{ Ok = $ok; Reasons = $reasons }
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

function Stop-D12 {
    param([string] $Why)
    throw ('D12-ABBRUCH: ' + $Why)
}

# ---------------------------------------------------------------- Teil A

function Invoke-Umstellung {
    Write-Log '--- Teil 0: Voraussetzungen'
    $newFull = ''
    if ($NewExe) { $newFull = [System.IO.Path]::GetFullPath($NewExe) }
    Add-Check '0.1' 'neues Programm angegeben und vorhanden' ($newFull -and (Test-Path $newFull)) $newFull
    if (-not ($newFull -and (Test-Path $newFull))) { Stop-D12 'neues Programm fehlt' }
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
    $missingTasks = @($Tasks | Where-Object { $null -eq (Get-ScheduledTask -TaskName $_ -ErrorAction SilentlyContinue) })
    Add-Check '0.7' '-Tasks angegeben (Pflicht) und jede Aufgabe vorhanden' ($Tasks.Count -gt 0 -and $missingTasks.Count -eq 0) ('Aufgaben: ' + $Tasks.Count + ', nicht gefunden: ' + $missingTasks.Count + "`n" + ($Tasks -join "`n"))
    $both = @($ExpectSent | Where-Object { $ExpectReplaced -contains $_ })
    Add-Check '0.8' 'Erwartung: -ExpectSent und -ExpectReplaced ohne gemeinsame ID' ($both.Count -eq 0) ('erwartet sent_by_human: ' + (Format-Ids $ExpectSent) + "`nerwartet ersetzt: " + (Format-Ids $ExpectReplaced))
    if ($script:NFail -gt 0) { Stop-D12 'Voraussetzungen fehlen; nichts geaendert' }

    Write-Log '--- Teil 1: Stand vorher (altes Programm), Sicherung'
    Stop-Tasks '1.1'
    if ($script:NFail -gt 0) { Stop-D12 'es laeuft noch ein postkauz.exe (Claude-App oder Aufgabe); nichts getauscht' }
    $script:Before = Get-Drafts $script:ExeFull @('draft', 'pending_approval', 'ready')
    Add-Check '1.2' ('draft list vorher: ' + $script:Before.List.Count + ' offene Entwuerfe') ($script:Before.Exit -eq 0 -and $script:Before.Parsed) ('JSON lesbar: ' + $script:Before.Parsed + "`n" + (Format-Drafts $script:Before.List))
    $script:DocBefore = Get-Doctor $script:ExeFull
    Add-Check '1.3' 'doctor vorher: database ok, Schema 22' ($script:DocBefore.Database -eq 'ok' -and $script:DocBefore.Schema -eq 22) ('database ' + $script:DocBefore.Database + ', schema ' + $script:DocBefore.Schema + "`n" + (($script:DocBefore.Warnings) -join "`n"))
    if ($script:NFail -gt 0) { Stop-D12 'Stand vorher nicht lesbar; nichts getauscht' }
    $openIds = @($script:Before.List | ForEach-Object { $_.Id })
    $notOpen = @(@($ExpectSent) + @($ExpectReplaced) | Where-Object { $openIds -notcontains $_ })
    Add-Check '1.2b' 'jeder erwartete Entwurf ist vorher offen' ($notOpen.Count -eq 0) ('nicht offen: ' + (Format-Ids $notOpen))
    if ($CommaDraft) {
        $d = Get-DraftDetail $script:ExeFull $CommaDraft
        $f = $null
        if ($null -ne $d) { $f = Get-NameFacts $d }
        $ok = ($null -ne $d -and $openIds -contains [int64] $CommaDraft -and @('reply', 'reply_all') -contains [string] $d.kind -and $f.Special -gt 0)
        $detail = 'gefunden: ' + ($null -ne $d)
        if ($null -ne $d) { $detail += ', Art ' + [string] $d.kind + ', Status ' + [string] $d.status + ', Namen mit Komma oder @: ' + $f.Special + ', davon in Anfuehrungszeichen: ' + $f.Quoted }
        Add-Check '1.4' ('Entwurf ' + $CommaDraft + ': offene Antwort, ein Empfaengername mit Komma oder @') $ok $detail
        $quote = 'true'
        if ($null -ne $d -and -not [bool] $d.quote) { $quote = 'false' }
        $e = Invoke-Pk $script:ExeFull @('draft', 'edit', $CommaDraft, ('--quote=' + $quote), '--dry-run')
        Add-Info '1.4b' ('mit ' + $OldVersion + ': draft edit ' + $CommaDraft + ' --dry-run ohne Aenderung (erwartet: denied invalid_address)') ('exit ' + $e.Exit + ', Grund ' + (Get-DenyReason $e))
    } else {
        Add-Info '1.4' 'kein Entwurf mit Komma oder @ im Empfaengernamen angegeben (-CommaDraft); 3.4 entfaellt'
    }
    $b = Invoke-Pk $script:ExeFull @('backup', 'create', '--to', $BackupTo) -TimeoutSec 7200
    Add-Check '1.5' 'Sicherung geschrieben (betrieb.md 2.2): Exit 0 und "backup written to"' ($b.Exit -eq 0 -and $b.Out -match 'backup written to') $b.Text
    if ($script:NFail -gt 0) { Stop-D12 'ohne Sicherung kein Tausch' }

    Write-Log '--- Teil 2: Programm tauschen'
    $why = ''
    try {
        Install-Program $newFull $OldCopy
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Check '2.1' ('getauscht: ' + $script:ExeFull + ' meldet ' + $ExpectVersion + ', altes Programm als postkauz-rc3.exe aufgehoben') ($v.Out -match [regex]::Escape($ExpectVersion) -and (Test-Path $OldCopy)) $v.Text
        $d = Get-Doctor $script:ExeFull
        Add-Check '2.2' 'doctor: database ok, Schema 22 (keine Migration)' ($d.Database -eq 'ok' -and $d.Schema -eq 22) ('database ' + $d.Database + ', schema ' + $d.Schema)
        if ($script:NFail -gt 0) { $why = 'Tausch nicht in Ordnung' }
    } catch {
        Add-Check '2.x' ('Fehler beim Tausch: ' + $_.Exception.Message) $false
        $why = 'Fehler beim Tausch'
    }
    if ($why) {
        if ($script:Swapped -and (Test-Path $OldCopy)) { Restore-Program $OldCopy }
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Info '2.3' ('altes Programm zurueck, meldet: ' + $v.Out.Trim())
        Stop-D12 ($why + '; altes Programm wieder in Betrieb, kein sync mit dem neuen')
    }
    if ($KeepStdioMcp) {
        Add-Info '2.4' 'Der stdio-MCP der Claude-App laeuft bis zum naechsten Neustart der App mit dem alten Programm (postkauz-rc3.exe).'
    }

    Write-Log '--- Teil 3: erster vollstaendiger Lauf mit dem neuen Programm'
    $failsBefore = $script:NFail
    foreach ($acc in $Accounts) {
        $s = Invoke-Pk $script:ExeFull @('sync', '-a', $acc, '--json') -TimeoutSec 3600
        $j = Get-Json $s.Out
        $line = 'exit ' + $s.Exit + ', stderr ' + (Get-LineCount $s.Err) + ' Zeilen'
        if ($null -ne $j) {
            $line += ', neu ' + $j.new + ', Fehler ' + $j.errors
            $dc = $j.draft_check
            if ($null -ne $dc) {
                $line += ', sent_by_message_id ' + [int] $dc.sent_by_message_id + ', sent_by_fallback ' + [int] $dc.sent_by_fallback +
                    ', sent_by_thread ' + [int] $dc.sent_by_thread + ', replaced_in_thread ' + [int] $dc.replaced_in_thread +
                    ', missing ' + [int] $dc.missing + ', taken_over ' + [int] $dc.taken_over
            }
        }
        Add-Check '3.1' 'sync -a <Konto> (erster vollstaendiger Lauf)' ($s.Exit -eq 0) $line
    }
    $after = Get-Drafts $script:ExeFull @('draft', 'pending_approval', 'ready', 'sent_by_human', 'abandoned')
    $cr = Get-CloseReasons $script:ExeFull
    $closed = @()
    foreach ($o in $script:Before.List) {
        $n = $after.List | Where-Object { $_.Id -eq $o.Id } | Select-Object -First 1
        if ($null -ne $n -and @('sent_by_human', 'abandoned') -contains $n.Status) { $closed += $n }
    }
    $lines = @()
    $gotSent = @()
    $gotReplaced = @()
    $odd = @()
    foreach ($c in $closed) {
        $r = $cr.Reasons[[int64] $c.Id]
        if (-not $r) { $r = '(kein Audit-Eintrag des Systems)' }
        $lines += ('draft ' + $c.Id + ': ' + $c.Status + ', Grund ' + $r)
        if ($c.Status -eq 'sent_by_human' -and @('message_id', 'fallback', 'thread') -contains $r) { $gotSent += $c.Id } elseif ($c.Status -eq 'abandoned' -and $r -eq 'replaced') { $gotReplaced += $c.Id } else { $odd += $c.Id }
    }
    Add-Check '3.2' ('alle im ersten Lauf geschlossenen Entwuerfe mit Grund aus dem Audit (' + $closed.Count + ')') ($after.Exit -eq 0 -and $after.Parsed -and $cr.Ok -and $odd.Count -eq 0) ((@($lines) -join "`n") + "`nohne passenden Grund: " + (Format-Ids $odd))
    $unexpected = @(@($gotSent | Where-Object { $ExpectSent -notcontains $_ }) + @($gotReplaced | Where-Object { $ExpectReplaced -notcontains $_ }) + $odd)
    $missingExp = @(@($ExpectSent | Where-Object { $gotSent -notcontains $_ }) + @($ExpectReplaced | Where-Object { $gotReplaced -notcontains $_ }))
    Add-Check '3.2b' 'kein nicht erwarteter Abschluss' ($unexpected.Count -eq 0) ('nicht erwartet: ' + (Format-Ids $unexpected))
    Add-Check '3.2c' 'jeder erwartete Abschluss mit dem erwarteten Ergebnis (-ExpectSent: sent_by_human, -ExpectReplaced: abandoned/replaced)' ($missingExp.Count -eq 0) ('sent_by_human: ' + (Format-Ids $gotSent) + "`nersetzt: " + (Format-Ids $gotReplaced) + "`nfehlt: " + (Format-Ids $missingExp))
    $open = @($after.List | Where-Object { @('draft', 'pending_approval', 'ready') -contains $_.Status })
    Add-Info '3.3' 'Betreiber: Stimmen die Abschluesse (draft show <id> zeigt den Betreff nur am Bildschirm)? Noch offen:' (Format-Drafts $open)
    if ($CommaDraft) {
        $d = Get-DraftDetail $script:ExeFull $CommaDraft
        $quote = 'true'
        if ($null -ne $d -and -not [bool] $d.quote) { $quote = 'false' }
        $e = Invoke-Pk $script:ExeFull @('draft', 'edit', $CommaDraft, ('--quote=' + $quote), '--dry-run')
        $er = Get-DenyReason $e
        Add-Check '3.4' ('draft edit ' + $CommaDraft + ' --dry-run ohne Aenderung: kein invalid_address') ($e.Exit -eq 0 -and $er -ne 'invalid_address') ('exit ' + $e.Exit + ', Grund ' + $er)
        $q = Invoke-Pk $script:ExeFull @('draft', 'request', $CommaDraft, '--dry-run')
        $qr = Get-DenyReason $q
        Add-Check '3.4b' ('draft request ' + $CommaDraft + ' --dry-run: kein invalid_address') ($qr -ne 'invalid_address' -and @(0, 3) -contains $q.Exit) ('exit ' + $q.Exit + ', Grund ' + $qr)
        $f = $null
        if ($null -ne $d) { $f = Get-NameFacts $d }
        Add-Check '3.4c' 'draft show: Namen mit Komma oder @ stehen in Anfuehrungszeichen' ($null -ne $f -and $f.Special -gt 0 -and $f.Quoted -eq $f.Special) ('Namen mit Komma oder @: ' + $f.Special + ', in Anfuehrungszeichen: ' + $f.Quoted)
        $still = Get-Drafts $script:ExeFull @('draft', 'pending_approval', 'ready')
        Add-Check '3.4d' 'kein neuer Entwurf durch die Pruefung (Zahl der offenen gleich)' ($still.List.Count -eq $open.Count) ('vorher ' + $open.Count + ', nachher ' + $still.List.Count)
    }
    $docAfter = Get-Doctor $script:ExeFull
    $old = @{}
    foreach ($w in $script:DocBefore.Warnings) { $old[(Get-WarningKey $w)] = $true }
    $new = @($docAfter.Warnings | Where-Object { -not $old.ContainsKey((Get-WarningKey $_)) })
    Add-Check '3.5' 'doctor ohne neue Warnung, Schema 22 (keine Migration)' ($docAfter.Database -eq 'ok' -and $docAfter.Schema -eq 22 -and $new.Count -eq 0) ('database ' + $docAfter.Database + ', schema ' + $docAfter.Schema + "`nneu: " + (($new) -join "`n") + "`nalle: " + (($docAfter.Warnings) -join "`n"))
    $av = Invoke-Pk $script:ExeFull @('audit', 'verify', '--json') -TimeoutSec 1800
    $avj = Get-Json $av.Out
    Add-Check '3.6' 'audit verify: ok' ($av.Exit -eq 0 -and $null -ne $avj -and $avj.status -eq 'ok') ('exit ' + $av.Exit + ', status ' + [string] $avj.status)
    if ($script:NFail -gt $failsBefore) {
        # Rueckweg bei FAIL: Der Dauerbetrieb laeuft nicht mit dem neuen
        # Programm weiter (geschlossene Entwuerfe bleiben geschlossen).
        if (Test-Path $OldCopy) { Restore-Program $OldCopy }
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Info '3.7' ('FAIL in Teil 3: altes Programm zurueck, meldet: ' + $v.Out.Trim() + '. Die im ersten Lauf geschlossenen Entwuerfe bleiben geschlossen (Liste in 3.2).')
    }

    Write-Log '--- Teil 4: Dauerbetrieb wieder an'
    Start-Tasks '4.1'
    $script:Restarted = $true
}

# ---------------------------------------------------------------- Teil B

# Einleitungszeile und Zaehlung ab dort (ohne Inhalte): erste Zeile, die
# wie eine Einleitung aussieht ("On ... wrote:" bzw. "Am ... schrieb ...:"),
# auch ueber zwei Zeilen umbrochen.
function Get-QuoteCounts {
    param([string] $Text)
    $lines = @($Text -split "`r?`n")
    $re = '(?i)^\s*(on\s.+\swrote:|am\s.+\sschrieb\s?.*:)\s*$'
    $at = -1
    $skip = 0
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match $re) { $at = $i; $skip = 1; break }
        if ($i + 1 -lt $lines.Count -and ($lines[$i].Trim() + ' ' + $lines[$i + 1].Trim()) -match $re) { $at = $i; $skip = 2; break }
    }
    $with = 0
    $without = 0
    if ($at -ge 0 -and $at + $skip -le $lines.Count - 1) {
        foreach ($l in $lines[($at + $skip)..($lines.Count - 1)]) {
            if (-not $l.Trim()) { continue }
            if ($l.TrimStart().StartsWith('>')) { $with++ } else { $without++ }
        }
    }
    return New-Object PSObject -Property @{ Found = ($at -ge 0); With = $with; Without = $without }
}

function Invoke-TeilB {
    Write-Log '--- Teil B: Gmail-Zitat an einer Mail an sich selbst (Mitmach)'
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    Add-Check 'B.0' ('Programm meldet ' + $ExpectVersion) ($v.Exit -eq 0 -and $v.Out -match [regex]::Escape($ExpectVersion)) $v.Text
    Add-Check 'B.0b' 'eigene Mail angegeben (-SelfMessage)' ([bool] $SelfMessage) $SelfMessage
    if ($script:NFail -gt 0) { Stop-D12 'Voraussetzungen fehlen' }
    $m = Invoke-Pk $script:ExeFull @('show', $SelfMessage, '--json', '--max-chars', '100000')
    $mj = Get-Json $m.Out
    $inner = 0
    if ($null -ne $mj) { $inner = @(([string] $mj.text) -split "`r?`n" | Where-Object { $_.TrimStart().StartsWith('>') }).Count }
    Add-Check 'B.1' ('Mail ' + $SelfMessage + ' enthaelt ein ">"-Zitat (damit eine innere Ebene vorkommt)') ($m.Exit -eq 0 -and $inner -gt 0) ('exit ' + $m.Exit + ', Zeilen mit ">": ' + $inner)
    $watch = @(Get-PkProcesses | Where-Object { ([string] $_.CommandLine) -match '(?i)\ssync(\s|$)' -and ([string] $_.CommandLine) -match '(?i)--watch' }).Count
    Add-Check 'B.2' 'sync --watch laeuft (legt den Entwurf ab und spiegelt die gesendete Nachricht)' ($watch -gt 0) ('sync --watch: ' + $watch)
    if ($script:NFail -gt 0) { Stop-D12 'Teil B nicht moeglich' }
    $body = Join-Path ([System.IO.Path]::GetTempPath()) ('d12-teilb-' + [guid]::NewGuid().ToString('N') + '.md')
    [System.IO.File]::WriteAllText($body, "Hallo,`n`ndas ist Abnahme D12 Teil B. Diesen Entwurf bitte in Gmail im Browser unveraendert senden.`n`nViele Gruesse", $script:Utf8)
    try {
        $c = Invoke-Pk $script:ExeFull @('draft', 'reply', $SelfMessage, '--body-file', $body, '--json')
    } finally {
        Remove-Item -LiteralPath $body -Force -ErrorAction SilentlyContinue
    }
    $cj = Get-Json $c.Out
    $id = ''
    if ($null -ne $cj -and $null -ne $cj.draft_id) { $id = [string] $cj.draft_id }
    Add-Check 'B.3' 'Antwort-Entwurf angelegt' ($c.Exit -eq 0 -and $id) ('exit ' + $c.Exit + ', draft ' + $id)
    if (-not $id) { Stop-D12 'kein Entwurf' }
    $deadline = (Get-Date).AddMinutes(5)
    $d = $null
    while ((Get-Date) -lt $deadline) {
        $d = Get-DraftDetail $script:ExeFull $id
        if ($null -ne $d -and [string] $d.remote -eq 'pushed') { break }
        Start-Sleep -Seconds 10
    }
    Add-Check 'B.4' ('Entwurf ' + $id + ' im Entwuerfe-Ordner abgelegt (remote pushed)') ($null -ne $d -and [string] $d.remote -eq 'pushed') ('remote ' + [string] $d.remote)
    if ($script:NFail -gt 0) { Stop-D12 'Entwurf nicht abgelegt' }
    $created = [datetime] $d.created_at
    Add-Info 'B.5' ('JETZT: Entwurf ' + $id + ' in Gmail im Browser oeffnen und UNVERAENDERT senden. Das Skript wartet bis zu ' + $WaitMinutes + ' Minuten auf den Sync.')
    $deadline = (Get-Date).AddMinutes($WaitMinutes)
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 15
        $d = Get-DraftDetail $script:ExeFull $id
        if ($null -ne $d -and @('sent_by_human', 'abandoned') -contains [string] $d.status) { break }
    }
    $status = [string] $d.status
    $cr = Get-CloseReasons $script:ExeFull
    $reason = $cr.Reasons[[int64] $id]
    if (-not $reason) { $reason = '(keiner)' }
    Add-Check 'B.6' ('Entwurf ' + $id + ' als gesendet erkannt (Grund aus dem Audit)') ($status -eq 'sent_by_human') ('Status ' + $status + ', Grund ' + $reason)
    $sent = ''
    $how = ''
    if ($null -ne $d.sent_message_id -and [int64] $d.sent_message_id -gt 0) { $sent = [string] $d.sent_message_id; $how = 'sent_message_id' }
    if (-not $sent) {
        # Sonst ueber Betreff und Datum: neueste Nachricht mit dem Betreff
        # des Entwurfs, nach dem Anlegen datiert, kein Entwurf.
        $subj = ([string] $d.subject) -replace '"', ' '
        $r = Invoke-Pk $script:ExeFull @('search', ('subject:"' + $subj + '"'), '--limit', '20', '--json')
        $best = $null
        foreach ($line in ($r.Out -split "`r?`n")) {
            $t = $line.Trim()
            if (-not $t.StartsWith('{')) { continue }
            $o = Get-Json $t
            if ($null -eq $o -or $null -eq $o.id -or -not $o.date) { continue }
            if (@($o.flags) -contains 'draft') { continue }
            if ([string] $o.subject -ne [string] $d.subject) { continue }
            if ([datetime] $o.date -lt $created.AddMinutes(-1)) { continue }
            if ($null -eq $best -or [int64] $o.id -gt [int64] $best.id) { $best = $o }
        }
        if ($null -ne $best) { $sent = [string] $best.id; $how = 'Betreff und Datum' }
    }
    Add-Check 'B.7' 'gesendete Nachricht gefunden' ([bool] $sent) ('ueber ' + $how + ', Nachricht ' + $sent)
    if ($sent) {
        $s = Invoke-Pk $script:ExeFull @('show', $sent, '--json', '--max-chars', '100000')
        $sj = Get-Json $s.Out
        $qc = $null
        if ($null -ne $sj) { $qc = Get-QuoteCounts ([string] $sj.text) }
        if ($null -ne $qc -and $qc.Found) {
            Add-Info 'B.8' ('Zeilen ab der Einleitungszeile (leere nicht gezaehlt): mit ">" ' + $qc.With + ', ohne ">" ' + $qc.Without + ' (erwartet mit gmail_quote: alle mit ">"; text_source ' + [string] $sj.text_source + ')')
        } else {
            Add-Info 'B.8' 'keine Einleitungszeile im Text der gesendeten Nachricht gefunden (Zaehlung entfaellt)'
        }
    }
}

# ---------------------------------------------------------------- Rueckweg

function Invoke-Rueckweg {
    Write-Log '--- Rueckweg: altes Programm zurueck (keine Migration in A06)'
    $newCopy = Join-Path (Split-Path -Parent $script:ExeFull) 'postkauz-rc4.exe'
    Add-Check 'R.0' 'aufgehobenes altes Programm vorhanden (postkauz-rc3.exe)' (Test-Path $OldCopy) $OldCopy
    if (-not (Test-Path $OldCopy)) { Stop-D12 'postkauz-rc3.exe fehlt; nichts geaendert' }
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    Add-Check 'R.0b' ('laufendes Programm meldet ' + $ExpectVersion + ' und postkauz-rc4.exe gibt es noch nicht') ($v.Out -match [regex]::Escape($ExpectVersion) -and -not (Test-Path $newCopy)) ($v.Text + "`npostkauz-rc4.exe vorhanden: " + (Test-Path $newCopy))
    $o = Invoke-Pk $OldCopy @('version') -NoDirs
    Add-Check 'R.0c' ('postkauz-rc3.exe meldet ' + $OldVersion) ($o.Out -match [regex]::Escape($OldVersion)) $o.Text
    Add-Check 'R.0d' '-Tasks angegeben (Pflicht)' ($Tasks.Count -gt 0) ('Aufgaben: ' + $Tasks.Count)
    if ($script:NFail -gt 0) { Stop-D12 'Rueckweg nicht moeglich; nichts geaendert' }
    Stop-Tasks 'R.1'
    if ($script:NFail -gt 0) { Stop-D12 'es laeuft noch ein postkauz.exe; nichts getauscht' }
    if ($KeepStdioMcp) {
        Rename-Item -LiteralPath $script:ExeFull -NewName (Split-Path -Leaf $newCopy)
        Copy-Item -LiteralPath $OldCopy -Destination $script:ExeFull -Force
        Add-Info 'R.1b' 'Der stdio-MCP der Claude-App laeuft bis zum naechsten Neustart der App mit dem bisherigen Programm (jetzt postkauz-rc4.exe).'
    } else {
        Copy-Item -LiteralPath $script:ExeFull -Destination $newCopy
        Copy-Item -LiteralPath $OldCopy -Destination $script:ExeFull -Force
    }
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    $d = Get-Doctor $script:ExeFull
    Add-Check 'R.2' ('altes Programm laeuft mit der Datenbank: ' + $OldVersion + ', database ok, Schema 22') ($v.Out -match [regex]::Escape($OldVersion) -and $d.Database -eq 'ok' -and $d.Schema -eq 22) ($v.Text + "`ndatabase " + $d.Database + ', schema ' + $d.Schema)
    foreach ($acc in $Accounts) {
        $s = Invoke-Pk $script:ExeFull @('sync', '-a', $acc, '--json') -TimeoutSec 3600
        $j = Get-Json $s.Out
        $line = 'exit ' + $s.Exit + ', stderr ' + (Get-LineCount $s.Err) + ' Zeilen'
        if ($null -ne $j) { $line += ', neu ' + $j.new + ', Fehler ' + $j.errors }
        Add-Check 'R.3' ('sync -a <Konto> mit ' + $OldVersion + ' ohne Fehler') ($s.Exit -eq 0) $line
    }
    Add-Info 'R.4' 'Fall: ohne Migration. Die Sicherung wird nicht zurueckgespielt. Als ersetzt geschlossene Entwuerfe (abandoned, Grund replaced) bleiben geschlossen; rc3 kennt die Regel nicht und schliesst keine weiteren.'
    Start-Tasks 'R.5'
    $script:Restarted = $true
}

# ---------------------------------------------------------------- Start

$script:ExeFull = [System.IO.Path]::GetFullPath($Exe)
$dir = Split-Path -Parent $script:ExeFull
if (-not $LogFile) {
    if ($TeilB) { $LogFile = Join-Path $dir 'D12_teilB.txt' } else { $LogFile = Join-Path $dir 'D12_windows.txt' }
}
$script:Log = $LogFile
$OldCopy = Join-Path $dir 'postkauz-rc3.exe'
$script:Swapped = $false
$script:Restarted = $false
$mode = 'Teil A, Umstellung'
if ($Rueckweg) { $mode = 'Rueckweg' }
if ($TeilB) { $mode = 'Teil B' }
Write-Log ('=== Abnahme D12 (' + $mode + '), ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + ' ===')
Write-Log ('Programm: ' + $script:ExeFull + '; Daten: ' + $DataDir + '; Konfiguration: ' + $Config)

try {
    if ($TeilB) { Invoke-TeilB } elseif ($Rueckweg) { Invoke-Rueckweg } else { Invoke-Umstellung }
} catch {
    $msg = [string] $_.Exception.Message
    if ($msg.StartsWith('D12-ABBRUCH: ')) {
        Write-Log ('Abbruch: ' + $msg.Substring(13))
    } else {
        Add-Check 'X' ('unerwarteter Fehler im Skript: ' + $msg) $false
    }
} finally {
    if ($script:WasEnabled.Count -gt 0 -and -not $script:Restarted) {
        try { Start-Tasks 'E.1' } catch { Add-Check 'E.1' ('Aufgaben wieder einschalten: ' + $_.Exception.Message) $false }
    }
    if ($script:Swapped -and -not $Rueckweg -and -not $TeilB) {
        Write-Log ('Stand: ' + $script:ExeFull + ' ist das neue Programm; das alte liegt als postkauz-rc3.exe daneben.')
    }
    Write-Log ''
    if ($script:Fails.Count -gt 0) { Write-Log ('Fehlgeschlagen: ' + ($script:Fails -join '; ')) }
    Write-Log ('D12: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL')
}
if ($script:NFail -gt 0) { exit 1 }
exit 0
