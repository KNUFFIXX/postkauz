<#
.SYNOPSIS
    Abnahme D13 (PostKauz, Auftrag A07): Umstellung des Dauerbetriebs von
    0.3.0-rc4 auf 0.3.0-rc5 mit Sicherung, zwei vollstaendigen Laeufen,
    Pruefung und Rueckweg (Teil A) und Probe von drafts.supersede an zwei
    eigenen Entwuerfen an sich selbst (Teil B).

.DESCRIPTION
    Laeuft auf dem Buero-PC (Windows 11, Windows PowerShell 5.1) am
    Live-Datenverzeichnis des Dauerbetriebs, ohne Go und ohne Docker.
    Anleitung: docs/abnahme_D13.md. Claude fuehrt das Skript aus.
    Muster: abnahme_D12.ps1 mit den Verbesserungen aus Sammelpunkt 0e
    (A07 Teil 6).

    Teil A "Umstellung" (Standard):
      Teil 0  Voraussetzungen: beide Programme (Version, Pruefsumme),
              Aufgaben (-Tasks ist Pflicht), Zielordner der Sicherung,
              Erwartung (-ExpectSent, -ExpectReplaced, -ExpectCopyGone),
              kein Schluessel copy_gone_after in der Konfiguration (rc4
              startet damit nicht; Rueckweg).
      Teil 1  Mit dem alten Programm (rc4): Aufgaben anhalten und
              abschalten, kein postkauz.exe laeuft mehr (mit -KeepStdioMcp
              ausser dem stdio-MCP der Claude-App), Stand vorher (draft
              list, doctor: database ok und Schema 22), Sicherung.
      Teil 2  Programm tauschen: altes als postkauz-rc4.exe aufheben
              (umbenennen statt ueberschreiben mit -KeepStdioMcp), neues an
              seine Stelle, version, doctor ohne Migration.
      Teil 3  ZWEI vollstaendige Laeufe mit rc5 (A07 Teil 2 b schliesst
              eine uebernommene Fassung erst im zweiten Lauf): je Lauf sync
              je Konto; ein Fehler eines Laufs ist nur WARN, wenn ein
              sofortiger zweiter Versuch fuer das Konto ohne Fehler endet
              (Sammelpunkt 0e); nach jedem Lauf ALLE seit dem Tausch
              geschlossenen Entwuerfe mit ID, Status und Grund aus dem
              Audit; ein nicht erwarteter Abschluss ist FAIL; nach dem
              zweiten Lauf Vergleich der Summe mit -ExpectSent,
              -ExpectReplaced und -ExpectCopyGone; doctor ohne neue
              Warnung, Schema 22; audit verify ok.
      Teil 4  Aufgaben wieder einschalten und starten, nur wenn Teil 3
              vollstaendig und ohne FAIL lief; 4.1 prueft je Aufgabe den
              Zustand Running UND danach, dass sync --watch aus dem Ordner
              von -Exe mit dem neuen Programm laeuft. Sonst (FAIL,
              Ausnahme, Strg+C oder Teil 3 nicht vollstaendig) kommt nach
              dem Tausch zuerst das alte Programm zurueck und die Aufgaben
              laufen mit ihm an.

    Teil B (-TeilB -SelfAccount <konto> -SelfAddress <adresse>, Mitmach):
      zwei neue Entwuerfe an sich selbst mit gleichem Betreff; wartet, bis
      sync --watch beide abgelegt hat; draft supersede <erster> --by
      <zweiter>; prueft: erster abandoned mit closed_reason
      superseded_by_draft und Audit drafts.supersede, die Aktion ist done
      (Kopie im Papierkorb); bittet um das unveraenderte Senden des
      zweiten in Gmail im Browser und prueft dessen Abschluss
      sent_by_human. Die Aufgaben laufen dabei weiter. Nie im geteilten
      Postfach ohne Anweisung des Betreibers.

    Modus "Rueckweg" (-Rueckweg): A07 hat keine Migration (Schema 22). Das
      alte Programm (postkauz-rc4.exe) kommt zurueck und laeuft weiter;
      eine Wiederherstellung der Sicherung ist nicht noetig.

    Jede Pruefung ist eine Zeile "OK <Nr> ...", "FAIL <Nr> ...", "WARN
    <Nr> ..." oder "INFO <Nr> ...", darunter eingerueckt die Ist-Ausgabe
    (gekuerzt). Betreff, Adressen und Mailtext stehen nie im Protokoll.
    Letzte Zeile: "D13: <n> OK, <m> FAIL, <w> WARN". Exit 0 nur ohne FAIL.
    Ein vorhandenes Protokoll wird nicht fortgeschrieben, sondern unter
    einem Namen mit Zeitstempel beiseitegelegt (Sammelpunkt 0e).

.PARAMETER Exe
    Das Programm des Dauerbetriebs (Standard C:\PostKauz\postkauz.exe).

.PARAMETER NewExe
    Das neue Programm (postkauz-windows-amd64.exe aus dem Build 0.3.0-rc5).
    Neben ihm muss SHA256SUMS liegen. Nur fuer Teil A.

.PARAMETER DataDir
    Live-Datenverzeichnis (Standard C:\PostKauz\data).

.PARAMETER Config
    Live-Konfiguration (Standard C:\PostKauz\config.toml).

.PARAMETER BackupTo
    Ordner der Sicherung (betrieb.md 2.2/2.3) auf einem verschluesselten
    Datentraeger.

.PARAMETER Accounts
    Konten fuer die Laeufe (je ein sync -a <Konto>; mehrere mit Komma).

.PARAMETER Tasks
    Pflicht in Teil A und im Rueckweg: alle geplanten Aufgaben, die
    PostKauz starten, mit Komma getrennt (Namen nicht raten).

.PARAMETER ExpectSent
    IDs der Entwuerfe, die die beiden Laeufe als sent_by_human schliessen
    sollen (mit Komma; leer = keiner).

.PARAMETER ExpectReplaced
    IDs der Entwuerfe, die die beiden Laeufe als ersetzt (abandoned, Grund
    replaced) schliessen sollen.

.PARAMETER ExpectCopyGone
    IDs der Entwuerfe, die die beiden Laeufe nach der Auffangregel
    (abandoned, Grund copy_gone) schliessen sollen.

.PARAMETER ExpectSha256
    SHA-256 der neuen Programmdatei (aus dem Protokoll des Builds).

.PARAMETER ExpectVersion
    Version des neuen Programms (Standard 0.3.0-rc5).

.PARAMETER OldVersion
    Version des alten Programms (Standard 0.3.0-rc4).

.PARAMETER Rueckweg
    Statt der Umstellung den Rueckweg auf das alte Programm pruefen.

.PARAMETER KeepStdioMcp
    Der stdio-MCP der Claude-App darf weiterlaufen (Tausch per Umbenennen;
    nur ohne Migration moeglich, A07 hat keine).

.PARAMETER TeilB
    Teil B statt Teil A ausfuehren (braucht -SelfAccount und -SelfAddress).

.PARAMETER SelfAccount
    Teil B: Konto, in dem die Probe-Entwuerfe entstehen (das eigene, nicht
    das geteilte Postfach ohne Anweisung des Betreibers).

.PARAMETER SelfAddress
    Teil B: eigene Adresse dieses Kontos (Empfaenger der Probe; steht nie
    im Protokoll).

.PARAMETER WaitMinutes
    Teil B: so lange wartet das Skript auf das Senden (Standard 20).

.PARAMETER LogFile
    Protokolldatei. Standard: D13_windows.txt (Teil B: D13_teilB.txt)
    neben dem Programm.

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d13\abnahme_D13.ps1 -NewExe C:\PostKauz\d13\postkauz-windows-amd64.exe -BackupTo E:\PostKauz-Sicherung -Accounts a,b,c -Tasks "PostKauz sync watch a,PostKauz sync watch b,PostKauz sync watch c,PostKauz audit export,PostKauz mcp http" -ExpectSent 41,44 -ExpectReplaced 39 -ExpectCopyGone 30 -KeepStdioMcp
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
    [string[]] $ExpectCopyGone = @(),
    [string] $ExpectSha256 = '',
    [string] $ExpectVersion = '0.3.0-rc5',
    [string] $OldVersion = '0.3.0-rc4',
    [switch] $Rueckweg,
    [switch] $KeepStdioMcp,
    [switch] $TeilB,
    [string] $SelfAccount = '',
    [string] $SelfAddress = '',
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
$ExpectCopyGone = @(Split-List $ExpectCopyGone | ForEach-Object { [int64] $_ })
$script:Utf8 = New-Object System.Text.UTF8Encoding $false
$script:NOk = 0
$script:NFail = 0
$script:NWarn = 0
$script:Log = $null
$script:LogBroken = $false
$script:Fails = New-Object System.Collections.Generic.List[string]
$script:WasEnabled = @{}
$script:Started = Get-Date
# Teil 3 vollstaendig durchgelaufen: nur dann ist ein Ergebnis ohne FAIL
# eine bestandene Abnahme.
$script:Complete = $false
# Aufgaben am Ende nicht wieder einschalten (Rueckweg gescheitert).
$script:KeepTasksOff = $false
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

# Schreiben ins Protokoll mit try/catch (Sammelpunkt 0e): Ein voller
# Datentraeger oder eine gesperrte Datei bricht die Abnahme nicht mitten im
# Tausch ab; die Zeile steht dann nur auf dem Bildschirm, und die
# Schlusszeile nennt den Fehler.
function Write-Log {
    param([string] $Text)
    $t = Hide-Secrets $Text
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
        throw ('D13-ABBRUCH: Prozessliste nicht lesbar (Get-CimInstance): ' + $_.Exception.Message)
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
        if ($k -and $c.Contains(([string] $k).ToLowerInvariant())) { return $true }
    }
    foreach ($a in $TaskArgs) {
        $m = [regex]::Match($a, '(?i)[^\s"]*run-?sync-?watch\.ps1')
        if ($m.Success -and $c.Contains($m.Value.ToLowerInvariant())) { return $true }
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
        if ($null -eq $t) { Add-Info $Nr ('Aufgabe "' + $tn + '" nicht vorhanden'); continue }
        if (-not $script:WasEnabled.ContainsKey($tn)) { $script:WasEnabled[$tn] = ($t.State -ne 'Disabled') }
        try { Stop-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue } catch { }
        try {
            Disable-ScheduledTask -TaskName $tn -ErrorAction Stop | Out-Null
        } catch {
            # Meist fehlende Rechte (Aufgabe "mit hoechsten Privilegien" oder
            # von einem anderen Benutzer angelegt): Abschnitt 3 der Anleitung.
            Add-Check $Nr ('Aufgabe "' + $tn + '" abschalten') $false $_.Exception.Message
        }
    }
    try {
        $allHelpers = @(Get-CimInstance Win32_Process -Filter "Name = 'powershell.exe' OR Name = 'pwsh.exe'" -OperationTimeoutSec 15 -ErrorAction Stop |
            Where-Object { $_.ProcessId -ne $PID -and ([string] $_.CommandLine) -match '(?i)run-?sync-?watch\.ps1' })
    } catch {
        throw ('D13-ABBRUCH: Prozessliste nicht lesbar (Get-CimInstance): ' + $_.Exception.Message)
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

# Laufende sync --watch aus dem Ordner von -Exe (fuer 4.1 und Teil B).
function Get-OwnWatch {
    return @(Get-PkProcesses | Where-Object { (Test-OwnPk $_) -and ([string] $_.CommandLine) -match '(?i)\ssync(\s|$)' -and ([string] $_.CommandLine) -match '(?i)--watch' })
}

# Aufgaben aus -Tasks wieder einschalten und (ausser audit export)
# starten; jede Aufgabe einzeln mit try und eigener Pruefung. Seit D13
# (Sammelpunkt 0e) reicht "Running" nicht: Nach dem Start muss die Aufgabe
# nach 20 s noch laufen, und es muss mindestens ein sync --watch aus dem
# Ordner von -Exe laufen, dessen Programm die erwartete Version meldet
# ($Version). Liefert $true, wenn alles in Ordnung ist.
function Start-Tasks {
    param([string] $Nr, [string] $Version)
    $hint = 'Claude-App danach wieder starten'
    if ($KeepStdioMcp) { $hint = 'Claude-App nach D13 neu starten, damit ihr MCP das aktuelle Programm nimmt' }
    $all = $true
    $todo = @()
    foreach ($tn in $Tasks) {
        if ($null -eq (Get-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue)) { continue }
        if ($script:WasEnabled.ContainsKey($tn) -and -not $script:WasEnabled[$tn]) { continue }
        $err = ''
        try {
            Enable-ScheduledTask -TaskName $tn -ErrorAction Stop | Out-Null
            if (-not (Test-AuditTask $tn)) { Start-ScheduledTask -TaskName $tn -ErrorAction Stop }
        } catch { $err = $_.Exception.Message }
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
        Add-Check $Nr ('Aufgabe "' + $x.Name + '"' + $what + ' (' + $hint + ')') $ok $detail
    }
    $syncTasks = @($todo | Where-Object { -not (Test-AuditTask $_.Name) -and $_.Name -match '(?i)sync' }).Count
    if ($syncTasks -gt 0) {
        $watch = @(Get-OwnWatch)
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        $ok = ($watch.Count -gt 0 -and $v.Exit -eq 0 -and $v.Out -match [regex]::Escape($Version))
        if (-not $ok) { $all = $false }
        Add-Check $Nr ('sync --watch laeuft aus dem Ordner von -Exe, ' + $script:ExeFull + ' meldet ' + $Version) $ok ('sync --watch: ' + $watch.Count + ' (Aufgaben mit sync: ' + $syncTasks + ")`n" + $v.Out.Trim())
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

function Get-NewCopyName { return (Get-FreeName 'postkauz-rc5') }

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

# Audit-Eintraege seit dem Start des Skripts (plus 10 Minuten Reserve) zu
# einer Operation (NDJSON). Seit D13 aus der Startzeit berechnet statt fest
# 3h (Sammelpunkt 0e): Eine lange Sicherung schob D12 sonst ueber die Grenze.
function Get-AuditOp {
    param([string] $Prog, [string] $Op)
    $min = [int] [math]::Ceiling(((Get-Date) - $script:Started).TotalMinutes) + 10
    $r = Invoke-Pk $Prog @('audit', '--operation', $Op, '--since', ([string] $min + 'm'), '--limit', '5000', '--json')
    $events = New-Object System.Collections.Generic.List[object]
    foreach ($line in ($r.Out -split "`r?`n")) {
        $t = $line.Trim()
        if (-not $t.StartsWith('{')) { continue }
        $o = Get-Json $t
        if ($null -ne $o -and $null -ne $o.operation) { $events.Add($o) }
    }
    return New-Object PSObject -Property @{ Exit = $r.Exit; Events = $events }
}

# Grund je Entwurf aus den Systemereignissen der Versanderkennung
# (sent_detected, replaced_detected, seit A07 copy_gone_detected) und aus
# drafts.supersede (A07 Teil 5, nur der Abschluss mit Grund
# superseded_by_draft; als Agent-Abschluss nicht erwartet).
function Get-CloseReasons {
    param([string] $Prog)
    $reasons = @{}
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
                }
            }
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

# Alle seit dem Tausch geschlossenen Entwuerfe (vorher offen, jetzt nicht
# mehr) mit Status und Grund; jeder einzeln mit draft show.
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
            $closed += New-Object PSObject -Property @{ Id = $o.Id; Status = [string] $dd.status; Reason = $r }
        } else {
            $vanished += $o.Id
        }
    }
    $sent = @(); $replaced = @(); $gone = @(); $odd = @(); $lines = @()
    foreach ($c in $closed) {
        $r = $c.Reason
        $shown = $r
        if (-not $shown) { $shown = '(kein Audit-Eintrag des Systems)' }
        $lines += ('draft ' + $c.Id + ': ' + $c.Status + ', Grund ' + $shown)
        if ($c.Status -eq 'sent_by_human' -and @('message_id', 'fallback', 'thread', 'subject') -contains $r) { $sent += $c.Id }
        elseif ($c.Status -eq 'abandoned' -and $r -eq 'replaced') { $replaced += $c.Id }
        elseif ($c.Status -eq 'abandoned' -and $r -eq 'copy_gone') { $gone += $c.Id }
        else { $odd += $c.Id }
    }
    return New-Object PSObject -Property @{ After = $after; AuditOk = $cr.Ok; Closed = $closed; Vanished = $vanished; Sent = $sent;
        Replaced = $replaced; Gone = $gone; Odd = $odd; Lines = $lines }
}

# Nach jedem Lauf: Liste und "kein nicht erwarteter Abschluss".
function Test-Closed {
    param([string] $Nr, [string] $Label)
    $g = Get-Closed $script:ExeFull
    Add-Check $Nr ('nach dem ' + $Label + ': alle seit dem Tausch geschlossenen Entwuerfe mit Grund aus dem Audit (' + $g.Closed.Count + ')') ($g.After.Exit -eq 0 -and $g.After.Parsed -and $g.After.List.Count -lt 500 -and $g.AuditOk -and $g.Odd.Count -eq 0 -and $g.Vanished.Count -eq 0) ((@($g.Lines) -join "`n") + "`nohne passenden Grund: " + (Format-Ids $g.Odd) + "`nvorher offen, jetzt weder offen noch geschlossen lesbar: " + (Format-Ids $g.Vanished))
    $unexpected = @(@($g.Sent | Where-Object { $ExpectSent -notcontains $_ }) + @($g.Replaced | Where-Object { $ExpectReplaced -notcontains $_ }) +
        @($g.Gone | Where-Object { $ExpectCopyGone -notcontains $_ }) + $g.Odd + $g.Vanished)
    Add-Check ($Nr + 'b') ('nach dem ' + $Label + ': kein nicht erwarteter Abschluss') ($unexpected.Count -eq 0) ('nicht erwartet: ' + (Format-Ids $unexpected))
    return $g
}

# ---------------------------------------------------------------- Teil A

function Invoke-Umstellung {
    Write-Log '--- Teil 0: Voraussetzungen'
    $newFull = ''
    if ($NewExe) { $newFull = [System.IO.Path]::GetFullPath($NewExe) }
    Add-Check '0.1' 'neues Programm angegeben und vorhanden' ($newFull -and (Test-Path $newFull)) $newFull
    if (-not ($newFull -and (Test-Path $newFull))) { Stop-D13 'neues Programm fehlt' }
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
    Add-Check '0.5' 'Ordner der Sicherung angegeben' ([bool] $BackupTo) $BackupTo
    $al = Invoke-Pk $script:ExeFull @('accounts', 'list', '--json')
    $aj = Get-Json $al.Out
    $known = @()
    if ($null -ne $aj -and $null -ne $aj.accounts) { $known = @($aj.accounts | ForEach-Object { [string] $_.name }) }
    $unknown = @($Accounts | Where-Object { $known -notcontains $_ })
    $notGiven = @($known | Where-Object { $Accounts -notcontains $_ })
    Add-Check '0.6' '-Accounts entspricht genau den konfigurierten Konten (accounts list)' ($Accounts.Count -gt 0 -and $al.Exit -eq 0 -and $unknown.Count -eq 0 -and $notGiven.Count -eq 0) ('angegeben: ' + $Accounts.Count + ', konfiguriert: ' + $known.Count + ', nicht konfiguriert: ' + $unknown.Count + ', konfiguriert, aber nicht angegeben: ' + $notGiven.Count)
    $missingTasks = @($Tasks | Where-Object { $null -eq (Get-ScheduledTask -TaskName $_ -ErrorAction SilentlyContinue) })
    Add-Check '0.7' '-Tasks angegeben (Pflicht) und jede Aufgabe vorhanden' ($Tasks.Count -gt 0 -and $missingTasks.Count -eq 0) ('Aufgaben: ' + $Tasks.Count + ', nicht gefunden: ' + $missingTasks.Count + "`n" + ($Tasks -join "`n"))
    $all = @(@($ExpectSent) + @($ExpectReplaced) + @($ExpectCopyGone))
    $dupes = @($all | Group-Object | Where-Object { $_.Count -gt 1 } | ForEach-Object { $_.Name })
    Add-Check '0.8' 'Erwartung: -ExpectSent, -ExpectReplaced und -ExpectCopyGone ohne gemeinsame ID' ($dupes.Count -eq 0) ('erwartet sent_by_human: ' + (Format-Ids $ExpectSent) + "`nerwartet ersetzt: " + (Format-Ids $ExpectReplaced) + "`nerwartet copy_gone: " + (Format-Ids $ExpectCopyGone) + "`ndoppelt: " + (Format-Ids $dupes))
    # A07 Teil 4: rc4 kennt [draft] copy_gone_after nicht und startet mit dem
    # Schluessel nicht (unbekannte Schluessel sind ein Fehler); fuer den
    # Rueckweg darf er nicht gesetzt sein.
    $cfgText = ''
    if (Test-Path $Config) { $cfgText = [System.IO.File]::ReadAllText($Config) }
    Add-Check '0.9' 'Konfiguration ohne [draft] copy_gone_after (rc4 startet sonst nicht; Rueckweg)' (-not ($cfgText -match '(?m)^\s*copy_gone_after\s*=')) ''
    if ($script:NFail -gt 0) { Stop-D13 'Voraussetzungen fehlen; nichts geaendert' }

    Write-Log '--- Teil 1: Stand vorher (altes Programm), Sicherung'
    Stop-Tasks '1.1'
    if ($script:NFail -gt 0) { Stop-D13 'es laeuft noch ein postkauz.exe (Claude-App oder Aufgabe) oder eine Aufgabe liess sich nicht abschalten; nichts getauscht' }
    $script:Before = Get-Drafts $script:ExeFull @('draft', 'pending_approval', 'ready')
    Add-Check '1.2' ('draft list vorher: ' + $script:Before.List.Count + ' offene Entwuerfe (unter der Grenze von 500)') ($script:Before.Exit -eq 0 -and $script:Before.Parsed -and $script:Before.List.Count -lt 500) ('JSON lesbar: ' + $script:Before.Parsed + "`n" + (Format-Drafts $script:Before.List))
    $script:DocBefore = Get-Doctor $script:ExeFull
    Add-Check '1.3' 'doctor vorher: database ok, Schema 22' ($script:DocBefore.Database -eq 'ok' -and $script:DocBefore.Schema -eq 22) ('database ' + $script:DocBefore.Database + ', schema ' + $script:DocBefore.Schema + "`n" + (($script:DocBefore.Warnings) -join "`n"))
    if ($script:NFail -gt 0) { Stop-D13 'Stand vorher nicht lesbar oder zu viele offene Entwuerfe; nichts getauscht' }
    $openIds = @($script:Before.List | ForEach-Object { $_.Id })
    $notOpen = @($all | Where-Object { $openIds -notcontains $_ })
    Add-Check '1.2b' 'jeder erwartete Entwurf ist vorher offen' ($notOpen.Count -eq 0) ('nicht offen: ' + (Format-Ids $notOpen))
    # Ersetzt werden kann nur, was vor dem Lauf missing war oder (A07 Teil 2
    # b) taken_over ist und im ersten Lauf missing wird; copy_gone nur, was
    # schon missing ist (seit mehr als 24 h, prueft rc5 selbst).
    $remoteOf = @{}
    foreach ($d in $script:Before.List) { $remoteOf[$d.Id] = $d.Remote }
    $badReplaced = @($ExpectReplaced | Where-Object { @('missing', 'taken_over') -notcontains $remoteOf[$_] })
    $badGone = @($ExpectCopyGone | Where-Object { $remoteOf[$_] -ne 'missing' })
    Add-Check '1.2c' 'jeder Entwurf aus -ExpectReplaced hat vorher remote missing oder taken_over, jeder aus -ExpectCopyGone missing' ($badReplaced.Count + $badGone.Count -eq 0) ('ersetzt, aber weder missing noch taken_over: ' + (Format-Ids $badReplaced) + "`ncopy_gone, aber nicht missing: " + (Format-Ids $badGone))
    if ($script:NFail -gt 0) { Stop-D13 'Erwartung passt nicht zum Stand; nichts getauscht' }
    $b = Invoke-Pk $script:ExeFull @('backup', 'create', '--to', $BackupTo) -TimeoutSec 7200
    Add-Check '1.5' 'Sicherung geschrieben (betrieb.md 2.2): Exit 0 und "backup written to"' ($b.Exit -eq 0 -and $b.Out -match 'backup written to') $b.Text
    if ($script:NFail -gt 0) { Stop-D13 'ohne Sicherung kein Tausch' }

    Write-Log '--- Teil 2: Programm tauschen'
    $why = ''
    try {
        Install-Program $newFull $OldCopy
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Check '2.1' ('getauscht: ' + $script:ExeFull + ' meldet ' + $ExpectVersion + ', altes Programm als postkauz-rc4.exe aufgehoben') ($v.Out -match [regex]::Escape($ExpectVersion) -and (Test-Path $OldCopy)) $v.Text
        $d = Get-Doctor $script:ExeFull
        Add-Check '2.2' 'doctor: database ok, Schema 22 (keine Migration)' ($d.Database -eq 'ok' -and $d.Schema -eq 22) ('database ' + $d.Database + ', schema ' + $d.Schema)
        if ($script:NFail -gt 0) { $why = 'Tausch nicht in Ordnung' }
    } catch {
        Add-Check '2.x' ('Fehler beim Tausch: ' + $_.Exception.Message) $false
        $why = 'Fehler beim Tausch'
    }
    if ($why) { Stop-D13 ($why + '; kein sync mit dem neuen Programm') }
    if ($KeepStdioMcp) {
        Add-Info '2.4' 'Der stdio-MCP der Claude-App laeuft bis zum naechsten Neustart der App mit dem alten Programm (postkauz-rc4.exe).'
    }

    Write-Log '--- Teil 3: zwei vollstaendige Laeufe mit dem neuen Programm'
    $failsBefore = $script:NFail
    Invoke-SyncRun '3.1' 'erster Lauf' $ExpectVersion
    [void] (Test-Closed '3.2' 'ersten Lauf')
    Invoke-SyncRun '3.3' 'zweiter Lauf' $ExpectVersion
    $g = Test-Closed '3.4' 'zweiten Lauf'
    $missingExp = @(@($ExpectSent | Where-Object { $g.Sent -notcontains $_ }) + @($ExpectReplaced | Where-Object { $g.Replaced -notcontains $_ }) +
        @($ExpectCopyGone | Where-Object { $g.Gone -notcontains $_ }))
    Add-Check '3.4c' 'Summe nach zwei Laeufen: jeder erwartete Abschluss mit dem erwarteten Ergebnis' ($missingExp.Count -eq 0) ('sent_by_human: ' + (Format-Ids $g.Sent) + "`nersetzt: " + (Format-Ids $g.Replaced) + "`ncopy_gone: " + (Format-Ids $g.Gone) + "`nfehlt: " + (Format-Ids $missingExp))
    Add-Info '3.5' 'Betreiber: Stimmen die Abschluesse (draft show <id> zeigt Betreff und Grund nur am Bildschirm)? Noch offen:' (Format-Drafts $g.After.List)
    $docAfter = Get-Doctor $script:ExeFull
    $old = @{}
    foreach ($w in $script:DocBefore.Warnings) { $old[(Get-WarningKey $w)] = $true }
    $new = @($docAfter.Warnings | Where-Object { -not $old.ContainsKey((Get-WarningKey $_)) })
    Add-Check '3.6' 'doctor ohne neue Warnung, Schema 22 (keine Migration)' ($docAfter.Database -eq 'ok' -and $docAfter.Schema -eq 22 -and $new.Count -eq 0) ('database ' + $docAfter.Database + ', schema ' + $docAfter.Schema + "`nneu: " + (($new) -join "`n") + "`nalle: " + (($docAfter.Warnings) -join "`n"))
    $av = Invoke-Pk $script:ExeFull @('audit', 'verify', '--json') -TimeoutSec 1800
    $avj = Get-Json $av.Out
    Add-Check '3.7' 'audit verify: ok' ($av.Exit -eq 0 -and $null -ne $avj -and $avj.status -eq 'ok') ('exit ' + $av.Exit + ', status ' + [string] $avj.status)
    if ($script:NFail -gt $failsBefore) {
        Add-Info '3.8' 'FAIL in Teil 3: Die geschlossenen Entwuerfe bleiben geschlossen (Liste in 3.2/3.4); das alte Programm kommt vor dem Neustart der Aufgaben zurueck (E.0).'
    }
    $script:Complete = $true
}

# ---------------------------------------------------------------- Teil B

# Wartet bis $Minutes, bis der Entwurf $Id remote pushed ist.
function Wait-Pushed {
    param([string] $Id, [int] $Minutes)
    $deadline = (Get-Date).AddMinutes($Minutes)
    $d = $null
    while ((Get-Date) -lt $deadline) {
        $d = Get-DraftDetail $script:ExeFull $Id
        if ($null -ne $d -and [string] $d.remote -eq 'pushed') { break }
        Start-Sleep -Seconds 10
    }
    return $d
}

function New-ProbeDraft {
    param([string] $Subject, [string] $Text, [string] $Nr)
    $body = Join-Path ([System.IO.Path]::GetTempPath()) ('d13-teilb-' + [guid]::NewGuid().ToString('N') + '.md')
    [System.IO.File]::WriteAllText($body, $Text, $script:Utf8)
    try {
        $c = Invoke-Pk $script:ExeFull @('draft', 'new', '-a', $SelfAccount, '--to', $SelfAddress, '--subject', $Subject, '--body-file', $body, '--json')
    } finally {
        Remove-Item -LiteralPath $body -Force -ErrorAction SilentlyContinue
    }
    $cj = Get-Json $c.Out
    $id = ''
    if ($null -ne $cj -and $null -ne $cj.draft_id) { $id = [string] $cj.draft_id }
    Add-Check $Nr 'Probe-Entwurf angelegt (neue Mail an sich selbst)' ($c.Exit -eq 0 -and $id) ('exit ' + $c.Exit + ', draft ' + $id)
    return $id
}

function Invoke-TeilB {
    Write-Log '--- Teil B: drafts.supersede an zwei eigenen Probe-Entwuerfen (Mitmach)'
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    Add-Check 'B.0' ('Programm meldet ' + $ExpectVersion) ($v.Exit -eq 0 -and $v.Out -match [regex]::Escape($ExpectVersion)) $v.Text
    Add-Check 'B.0b' 'Konto und eigene Adresse angegeben (-SelfAccount, -SelfAddress)' ([bool] $SelfAccount -and [bool] $SelfAddress) ('Konto ' + $SelfAccount)
    Add-Check 'B.1' 'sync --watch laeuft (legt die Entwuerfe ab, fuehrt die Aktion aus, spiegelt die gesendete Nachricht)' ((Get-OwnWatch).Count -gt 0) ('sync --watch: ' + (Get-OwnWatch).Count)
    if ($script:NFail -gt 0) { Stop-D13 'Voraussetzungen fehlen' }
    $subject = 'PostKauz Abnahme D13 Teil B ' + (Get-Date -Format 'yyyy-MM-dd HH:mm')
    $first = New-ProbeDraft $subject "Hallo,`n`ndas ist Abnahme D13 Teil B, erste Fassung. Diese Fassung wird ersetzt und nicht gesendet.`n`nViele Gruesse" 'B.2'
    if (-not $first) { Stop-D13 'kein erster Entwurf' }
    $d1 = Wait-Pushed $first 5
    Add-Check 'B.3' ('erste Fassung (Entwurf ' + $first + ') im Entwuerfe-Ordner abgelegt') ($null -ne $d1 -and [string] $d1.remote -eq 'pushed') ('remote ' + [string] $d1.remote)
    Start-Sleep -Seconds 2
    $second = New-ProbeDraft $subject "Hallo,`n`ndas ist Abnahme D13 Teil B, zweite Fassung. Diese Fassung bitte in Gmail im Browser unveraendert senden.`n`nViele Gruesse" 'B.4'
    if (-not $second) { Stop-D13 'kein zweiter Entwurf' }
    $d2 = Wait-Pushed $second 5
    Add-Check 'B.5' ('zweite Fassung (Entwurf ' + $second + ') im Entwuerfe-Ordner abgelegt') ($null -ne $d2 -and [string] $d2.remote -eq 'pushed') ('remote ' + [string] $d2.remote)
    if ($script:NFail -gt 0) { Stop-D13 'Entwuerfe nicht abgelegt' }
    $dry = Invoke-Pk $script:ExeFull @('draft', 'supersede', $first, '--by', $second, '--dry-run', '--json')
    $dj = Get-Json $dry.Out
    Add-Check 'B.6' 'draft supersede --dry-run: nichts geaendert, 1 Kopie wuerde in den Papierkorb gehen' ($dry.Exit -eq 0 -and $null -ne $dj -and [bool] $dj.dry_run -and [int] $dj.copies -ge 1 -and [string] (Get-DraftDetail $script:ExeFull $first).status -eq 'draft') ('exit ' + $dry.Exit + ', Grund ' + (Get-DenyReason $dry))
    $s = Invoke-Pk $script:ExeFull @('draft', 'supersede', $first, '--by', $second, '--json')
    $sj = Get-Json $s.Out
    $action = ''
    if ($null -ne $sj -and $null -ne $sj.action_id) { $action = [string] $sj.action_id }
    Add-Check 'B.7' ('draft supersede ' + $first + ' --by ' + $second + ': Exit 0, alter Entwurf abandoned, Aktion angelegt') ($s.Exit -eq 0 -and $null -ne $sj -and [string] $sj.status -eq 'abandoned' -and $action) ('exit ' + $s.Exit + ', Aktion ' + $action + ', Kopien ' + [string] $sj.copies + ', Grund ' + (Get-DenyReason $s))
    if (-not $action) { Stop-D13 'keine Aktion' }
    $deadline = (Get-Date).AddMinutes(3)
    $aj = $null
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 10
        $a = Invoke-Pk $script:ExeFull @('action', 'show', $action, '--json')
        $aj = Get-Json $a.Out
        if ($null -ne $aj -and @('done', 'partial', 'failed', 'rejected', 'expired') -contains [string] $aj.state) { break }
    }
    $results = ''
    if ($null -ne $aj -and $null -ne $aj.targets) { $results = ((@($aj.targets) | ForEach-Object { [string] $_.result }) -join ', ') }
    Add-Check 'B.8' ('Aktion ' + $action + ' ausgefuehrt: done (Kopie der ersten Fassung im Papierkorb, ohne Antrag)') ($null -ne $aj -and [string] $aj.state -eq 'done') ('Zustand ' + [string] $aj.state + ', Ergebnis je Ziel: ' + $results + ', Fehler ' + [string] $aj.error)
    $old = Get-DraftDetail $script:ExeFull $first
    Add-Check 'B.9' ('Entwurf ' + $first + ': abandoned, closed_reason superseded_by_draft, Nachfolger ' + $second) ($null -ne $old -and [string] $old.status -eq 'abandoned' -and [string] $old.closed_reason -eq 'superseded_by_draft' -and [string] $old.superseded_by_draft_id -eq $second) ('Status ' + [string] $old.status + ', Grund ' + [string] $old.closed_reason + ', Nachfolger ' + [string] $old.superseded_by_draft_id)
    $cr = Get-CloseReasons $script:ExeFull
    Add-Check 'B.10' 'Audit: drafts.supersede mit Grund superseded_by_draft fuer die erste Fassung' ($cr.Reasons[[int64] $first] -eq 'superseded_by_draft') ('Grund ' + [string] $cr.Reasons[[int64] $first])
    Add-Info 'B.11' ('JETZT: Entwurf ' + $second + ' (zweite Fassung) in Gmail im Browser oeffnen und UNVERAENDERT senden. Das Skript wartet bis zu ' + $WaitMinutes + ' Minuten auf den Sync.')
    $deadline = (Get-Date).AddMinutes($WaitMinutes)
    $d = $null
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 15
        $d = Get-DraftDetail $script:ExeFull $second
        if ($null -ne $d -and @('sent_by_human', 'abandoned') -contains [string] $d.status) { break }
    }
    $cr = Get-CloseReasons $script:ExeFull
    $reason = $cr.Reasons[[int64] $second]
    if (-not $reason) { $reason = '(keiner)' }
    Add-Check 'B.12' ('Entwurf ' + $second + ' als gesendet erkannt (Grund aus dem Audit)') ($null -ne $d -and [string] $d.status -eq 'sent_by_human') ('Status ' + [string] $d.status + ', Grund ' + $reason)
    $old = Get-DraftDetail $script:ExeFull $first
    Add-Check 'B.13' ('Entwurf ' + $first + ' bleibt abandoned (kein spaeterer Lauf schliesst ihn als gesendet)') ($null -ne $old -and [string] $old.status -eq 'abandoned') ('Status ' + [string] $old.status)
}

# ---------------------------------------------------------------- Rueckweg

function Invoke-Rueckweg {
    Write-Log '--- Rueckweg: altes Programm zurueck (keine Migration in A07)'
    # Sammelpunkt 0e: Der Rueckweg schaltet am Ende alle Aufgaben aus -Tasks
    # wieder ein, auch wenn sie beim Start aus waren (etwa nach einem harten
    # Abbruch); vermerkt VOR R.1, damit auch ein Abbruch in R.1 sie im
    # finally wieder einschaltet.
    foreach ($tn in $Tasks) { $script:WasEnabled[$tn] = $true }
    Add-Check 'R.0' 'aufgehobenes altes Programm vorhanden (postkauz-rc4.exe)' (Test-Path $OldCopy) $OldCopy
    if (-not (Test-Path $OldCopy)) { $script:WasEnabled = @{}; Stop-D13 'postkauz-rc4.exe fehlt; nichts geaendert' }
    $newCopy = Get-NewCopyName
    # Sammelpunkt 0e: postkauz.exe kann fehlen (harter Abbruch zwischen
    # Umbenennen und Kopieren); dann nur das alte an seine Stelle.
    $present = Test-Path $script:ExeFull
    if ($present) {
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Check 'R.0b' ('laufendes Programm meldet ' + $ExpectVersion) ($v.Out -match [regex]::Escape($ExpectVersion)) ($v.Text + "`nneues Programm wird beiseitegelegt als " + (Split-Path -Leaf $newCopy))
    } else {
        Add-Info 'R.0b' ($script:ExeFull + ' fehlt (harter Abbruch?); das alte Programm kommt an seine Stelle')
    }
    $o = Invoke-Pk $OldCopy @('version') -NoDirs
    Add-Check 'R.0c' ('postkauz-rc4.exe meldet ' + $OldVersion) ($o.Out -match [regex]::Escape($OldVersion)) $o.Text
    Add-Check 'R.0d' '-Tasks angegeben (Pflicht)' ($Tasks.Count -gt 0) ('Aufgaben: ' + $Tasks.Count)
    $cfgText = ''
    if (Test-Path $Config) { $cfgText = [System.IO.File]::ReadAllText($Config) }
    Add-Check 'R.0e' 'Konfiguration ohne [draft] copy_gone_after (rc4 startet sonst nicht)' (-not ($cfgText -match '(?m)^\s*copy_gone_after\s*=')) 'sonst den Schluessel vorher von Hand entfernen'
    if ($script:NFail -gt 0) { Stop-D13 'Rueckweg nicht moeglich; nichts geaendert' }
    Stop-Tasks 'R.1'
    if ($script:NFail -gt 0) { Stop-D13 'es laeuft noch ein postkauz.exe; nichts getauscht' }
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
        Stop-D13 'Rueckweg gescheitert; Aufgaben bleiben AUS (von Hand nach abnahme_D13.md Abschnitt 5 weiter)'
    }
    if ($KeepStdioMcp) {
        Add-Info 'R.1c' 'Der stdio-MCP der Claude-App laeuft bis zum naechsten Neustart der App mit dem bisherigen Programm.'
    }
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    $d = Get-Doctor $script:ExeFull
    Add-Check 'R.2' ('altes Programm laeuft mit der Datenbank: ' + $OldVersion + ', database ok, Schema 22') ($v.Out -match [regex]::Escape($OldVersion) -and $d.Database -eq 'ok' -and $d.Schema -eq 22) ($v.Text + "`ndatabase " + $d.Database + ', schema ' + $d.Schema)
    # Sammelpunkt 0e: R.3 prueft auch errors (mit einer Wiederholung).
    Invoke-SyncRun 'R.3' ('Rueckweg mit ' + $OldVersion) $OldVersion
    Add-Info 'R.4' 'Fall: ohne Migration. Die Sicherung wird nicht zurueckgespielt. Mit rc5 geschlossene Entwuerfe (Gruende subject, copy_gone, superseded_by_draft) bleiben geschlossen; rc4 kennt diese Regeln nicht und schliesst keine weiteren.'
    [void] (Start-Tasks 'R.5' $OldVersion)
    $script:Restarted = $true
}

# ---------------------------------------------------------------- Start

$script:ExeFull = [System.IO.Path]::GetFullPath($Exe)
$dir = Split-Path -Parent $script:ExeFull
$script:ExeDir = $dir
if (-not $LogFile) {
    if ($TeilB) { $LogFile = Join-Path $dir 'D13_teilB.txt' } else { $LogFile = Join-Path $dir 'D13_windows.txt' }
}
# Sammelpunkt 0e: Ein vorhandenes Protokoll wird nicht fortgeschrieben,
# sondern unter einem Namen mit Zeitstempel beiseitegelegt (nie geloescht).
if (Test-Path -LiteralPath $LogFile) {
    $aside = [System.IO.Path]::Combine((Split-Path -Parent $LogFile), [System.IO.Path]::GetFileNameWithoutExtension($LogFile) + '-' +
        (Get-Date -Format 'yyyyMMddHHmmss') + [System.IO.Path]::GetExtension($LogFile))
    try { Rename-Item -LiteralPath $LogFile -NewName (Split-Path -Leaf $aside) -ErrorAction Stop } catch {
        Write-Host ('Protokoll ' + $LogFile + ' laesst sich nicht beiseitelegen: ' + $_.Exception.Message)
        exit 1
    }
}
$script:Log = $LogFile
$OldCopy = Join-Path $dir 'postkauz-rc4.exe'
$script:Swapped = $false
$script:Restarted = $false
$mode = 'Teil A, Umstellung'
if ($Rueckweg) { $mode = 'Rueckweg' }
if ($TeilB) { $mode = 'Teil B' }
Write-Log ('=== Abnahme D13 (' + $mode + '), ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + ' ===')
Write-Log ('Programm: ' + $script:ExeFull + '; Daten: ' + $DataDir + '; Konfiguration: ' + $Config)
$elevated = $false
try { $elevated = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator) } catch { }
Add-Info '0.0' ('PowerShell mit Administratorrechten: ' + $elevated + ' (Anleitung Abschnitt 3: so wie die Aufgaben angelegt sind)')

try {
    if ($TeilB) { Invoke-TeilB } elseif ($Rueckweg) { Invoke-Rueckweg } else { Invoke-Umstellung }
} catch {
    $msg = [string] $_.Exception.Message
    if ($msg.StartsWith('D13-ABBRUCH: ')) {
        Write-Log ('Abbruch: ' + $msg.Substring(13))
    } else {
        Add-Check 'X' ('unerwarteter Fehler im Skript: ' + $msg) $false
    }
} finally {
    $umst = (-not $Rueckweg -and -not $TeilB)
    if ($umst -and -not $script:Complete) {
        Add-Check 'E.2' 'Teil 3 lief nicht vollstaendig: Abnahme nicht bestanden' $false
    }
    $restoreFailed = $false
    if ($umst -and $script:Swapped -and $script:NFail -gt 0) {
        try {
            if (-not (Restore-Program $OldCopy 'E.0')) { $restoreFailed = $true }
        } catch {
            Add-Check 'E.0' ('altes Programm zurueck: ' + $_.Exception.Message) $false
            $restoreFailed = $true
        }
    }
    if ($script:WasEnabled.Count -gt 0 -and -not $script:Restarted) {
        if ($restoreFailed -or $script:KeepTasksOff) {
            Add-Check 'E.1' 'Aufgaben NICHT wieder eingeschaltet: das richtige Programm liegt nicht sicher an seinem Platz (von Hand nach abnahme_D13.md Abschnitt 5)' $false
        } elseif ($umst -and $script:Complete -and $script:NFail -eq 0) {
            Write-Log '--- Teil 4: Dauerbetrieb wieder an'
            # Sammelpunkt 0e: 4.1 mit eigenem try/finally; bricht der Start
            # ab (Strg+C, Ausnahme), steht das im Protokoll.
            $started = $false
            $done41 = $false
            try {
                $started = Start-Tasks '4.1' $ExpectVersion
                $done41 = $true
            } catch {
                Add-Check '4.1' ('Aufgaben wieder einschalten: ' + $_.Exception.Message) $false
            } finally {
                if (-not $done41) { Write-Log 'Teil 4 nicht vollstaendig: Aufgaben von Hand pruefen (abnahme_D13.md Abschnitt 3, harter Abbruch).' }
            }
            if (-not $started) {
                # Mit dem neuen Programm laeuft der Dauerbetrieb nicht an:
                # anhalten, und nur wenn das gelang, altes Programm zurueck
                # und mit ihm starten (Sammelpunkt 0e: Fehler von Stop-Tasks
                # werden ausgewertet).
                try {
                    $before = $script:NFail
                    Stop-Tasks 'E.0a'
                    if ($script:NFail -gt $before) {
                        Add-Check 'E.0b' 'Aufgaben nach 4.1 nicht sauber angehalten: altes Programm NICHT zurueckgelegt, Aufgaben bleiben aus (von Hand nach abnahme_D13.md Abschnitt 5)' $false
                    } elseif (Restore-Program $OldCopy 'E.0') {
                        [void] (Start-Tasks 'E.1' $OldVersion)
                    } else {
                        Add-Check 'E.1' 'Aufgaben angehalten, altes Programm liess sich nicht zuruecklegen (von Hand nach abnahme_D13.md Abschnitt 5)' $false
                    }
                } catch { Add-Check 'E.1' ('Rueckweg nach 4.1: ' + $_.Exception.Message) $false }
            }
        } else {
            $ver = $OldVersion
            if ($TeilB) { $ver = $ExpectVersion }
            try { [void] (Start-Tasks 'E.1' $ver) } catch { Add-Check 'E.1' ('Aufgaben wieder einschalten: ' + $_.Exception.Message) $false }
        }
    }
    if ($umst) {
        # Sammelpunkt 0e: Stand-Zeile aus dem, was wirklich daliegt (auch
        # nach einem gescheiterten E.0).
        $cur = Get-ProgramVersion $script:ExeFull
        if (-not $cur) { $cur = '(fehlt oder meldet keine Version)' }
        $keep = 'fehlt'
        if (Test-Path $OldCopy) { $keep = (Get-ProgramVersion $OldCopy) }
        Write-Log ('Stand: ' + $script:ExeFull + ' meldet ' + $cur + '; postkauz-rc4.exe: ' + $keep)
    }
    Write-Log ''
    if ($script:Fails.Count -gt 0) { Write-Log ('Fehlgeschlagen: ' + ($script:Fails -join '; ')) }
    if ($script:LogBroken) { try { Write-Host 'ACHTUNG: Das Protokoll liess sich nicht vollstaendig schreiben (Datentraeger voll oder gesperrt).' } catch { } }
    Write-Log ('D13: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL, ' + $script:NWarn + ' WARN')
}
if ($script:NFail -gt 0) { exit 1 }
exit 0
