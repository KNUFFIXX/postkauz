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
              Erwartung (-ExpectSent, -ExpectReplaced, -ExpectCopyGone,
              -ExpectStaysOpen; keine ID in zwei Listen), kein Schluessel
              copy_gone_after in der Konfiguration (rc4 startet damit
              nicht; Rueckweg).
      Teil 1  Mit dem alten Programm (rc4): Aufgaben anhalten und
              abschalten, kein postkauz.exe laeuft mehr (mit -KeepStdioMcp
              ausser dem stdio-MCP der Claude-App), Stand vorher (draft
              list, doctor: database ok und Schema 22), Marke im Audit
              (hoechste ID nach der Vorher-Liste; nur spaetere Abschluesse
              zaehlen), Erwartung der Auffangregel (1.2d/1.2e), Sicherung,
              dieselbe Pruefung noch einmal direkt vor dem Tausch
              (1.6d/1.6e, Nachtrag 2, N4).
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
              -ExpectReplaced und -ExpectCopyGone, und jeder Entwurf aus
              -ExpectStaysOpen ist noch offen; doctor ohne neue
              Warnung, Schema 22; audit verify ok.
      Teil 4  Aufgaben wieder einschalten und starten, nur wenn Teil 3
              vollstaendig und ohne FAIL lief; 4.1 prueft je Aufgabe den
              Zustand Running UND danach, dass je Konto ein sync --watch
              genau mit dem Programm -Exe laeuft (Konto aus -a, --account
              oder --all der Befehlszeile). Sonst (FAIL,
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
      Postfach ohne Anweisung des Betreibers. Mit -AgentKeyFile laufen
      Anlegen und supersede ueber den MCP-Server mit dem Schluessel eines
      Agenten (Nachtrag 1, A8).

    Modus "Rueckweg" (-Rueckweg): A07 hat keine Migration (Schema 22). Das
      alte Programm (postkauz-rc4.exe) kommt zurueck und laeuft weiter;
      eine Wiederherstellung der Sicherung ist nicht noetig.

    Jede Pruefung ist eine Zeile "OK <Nr> ...", "FAIL <Nr> ...", "WARN
    <Nr> ..." oder "INFO <Nr> ...", darunter eingerueckt die Ist-Ausgabe
    (gekuerzt). Betreff, Adressen, Mailtext und die Namen der Aufgaben
    stehen nie im Protokoll (Aufgaben als laufende Nummer in der
    Reihenfolge von -Tasks).
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

.PARAMETER ExpectStaysOpen
    IDs der Entwuerfe, die laut Audit laenger als 24 h fehlen, die die
    beiden Laeufe aber NICHT schliessen sollen (etwa weil die Kopie wieder
    da ist und rc5 sie als present erkennt). 1.2d nimmt sie hin, 3.4c
    prueft, dass sie offen bleiben (Nachtrag 2, N3).

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

.PARAMETER AgentKeyFile
    Teil B, optional: Datei mit dem API-Schluessel eines Agenten (pk_key_...,
    etwa der Schluessel der Claude-App). Ist sie angegeben, legt Teil B beide
    Probe-Entwuerfe und den Aufruf supersede ueber den MCP-Server
    (postkauz mcp, stdio) mit diesem Schluessel an: der Weg eines Agenten,
    mit Eigentuemerpruefung und der Ausnahme im Runner. Der Schluessel und
    der Dateiname stehen nie im Protokoll.

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
    [string[]] $ExpectStaysOpen = @(),
    [string] $ExpectSha256 = '',
    [string] $ExpectVersion = '0.3.0-rc5',
    [string] $OldVersion = '0.3.0-rc4',
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
# Nachtrag 2, N1: Die Parameter sind [string[]]; eine Zuweisung an sie
# bliebe string[] (die Typangabe der Variablen gilt weiter), und
# Hashtable-Abfragen mit [int64]-Schluesseln liefen ins Leere. Darum eigene
# Variablen vom Typ [int64[]]; die Parameter werden danach nicht mehr
# gelesen. Was keine Zahl ist, steht in $script:BadIds (FAIL in 0.8).
$script:BadIds = New-Object System.Collections.Generic.List[string]
function ConvertTo-IdList {
    param([string[]] $L, [string] $Name)
    $ids = New-Object System.Collections.Generic.List[int64]
    foreach ($s in (Split-List $L)) {
        $n = [int64] 0
        if ([int64]::TryParse($s, [ref] $n) -and $n -gt 0) { $ids.Add($n) } else { $script:BadIds.Add($Name + ': ' + $s) }
    }
    return , ($ids.ToArray())
}
$script:ExpSent = [int64[]] (ConvertTo-IdList $ExpectSent '-ExpectSent')
$script:ExpReplaced = [int64[]] (ConvertTo-IdList $ExpectReplaced '-ExpectReplaced')
$script:ExpGone = [int64[]] (ConvertTo-IdList $ExpectCopyGone '-ExpectCopyGone')
$script:ExpOpen = [int64[]] (ConvertTo-IdList $ExpectStaysOpen '-ExpectStaysOpen')
# Marke im Audit (Nachtrag 2, N2): nur Abschluesse mit hoeherer ID zaehlen;
# -1 = keine Marke, dann ab $script:AuditSince (Zeitstempel, RFC 3339).
$script:AuditMark = [int64] -1
$script:AuditSince = ''
$script:Utf8 = New-Object System.Text.UTF8Encoding $false
$script:NOk = 0
$script:NFail = 0
$script:NWarn = 0
$script:Log = $null
$script:LogBroken = $false
$script:Fails = New-Object System.Collections.Generic.List[string]
$script:WasEnabled = @{}
$script:Started = Get-Date
$script:Mcp = $null
$script:McpIn = $null
$script:McpDead = ''
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

# Nachtrag 2, N8: Aufgaben stehen im Protokoll nur als laufende Nummer in
# der Reihenfolge von -Tasks (ihre Namen koennen Kontonamen tragen).
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
# ExecutablePath gleich -Exe; postkauz-rc4.exe im selben Ordner zaehlt
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
        # Nachtrag 1, A5: je Konto aus -Accounts ein sync --watch aus dem
        # Ordner von -Exe mit diesem Konto in der Befehlszeile (oder --all).
        # Nachtrag 2, N6: unabhaengig von den Namen der Aufgaben; zaehlt nur
        # -a/--account/--all der zerlegten Befehlszeile.
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

# audit [--operation <op>] --since <since> [--after-id <n>] --limit 1000
# --json (NDJSON), seitenweise ueber --after-id (aufsteigende IDs).
# Nachtrag 1, A1: audit.list liefert hoechstens 1000 je Seite. Nachtrag 2,
# N2/N5: weiter mit --after-id statt Abbruch; ist nach $MaxPages Seiten
# noch keine Seite kuerzer als 1000, gilt die Abfrage als nicht lesbar
# (Exit 99). --since ist immer gesetzt (ohne gilt 24h).
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

# Nachtrag 2, N2: Marke im Audit setzen, die hoechste ID (alle Zeilen,
# --raw) der letzten 15 Minuten; die eigene Abfrage steht schon darin. Ohne
# Marke gilt der Zeitpunkt $At (Vorher-Liste) als Grenze.
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

# Audit-Eintraege zu einer Operation seit der Marke (Nachtrag 2, N2; vorher
# seit dem Start des Skripts minus 10 Minuten: ein Abschluss durch rc4 vor
# 1.1 war dann ein falscher FAIL).
function Get-AuditOp {
    param([string] $Prog, [string] $Op)
    if ($script:AuditMark -gt 0) { return Get-AuditPaged $Prog $Op '36500d' $script:AuditMark }
    $since = $script:AuditSince
    if (-not $since) { $since = Format-Rfc3339 ([DateTimeOffset] $script:Started.AddMinutes(-10)) }
    return Get-AuditPaged $Prog $Op $since
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
    # Nachtrag 1, A7: Abschluesse aus dem Audit, deren Entwurf nicht in der
    # Vorher-Liste stand, sind nie erwartet.
    $beforeIds = @($script:Before.List | ForEach-Object { $_.Id })
    $foreign = @($cr.Reasons.Keys | Where-Object { $beforeIds -notcontains $_ } | ForEach-Object { [int64] $_ })
    $sent = @(); $replaced = @(); $gone = @(); $odd = @(); $lines = @()
    foreach ($f in $foreign) { $lines += ('draft ' + $f + ': vorher nicht offen, Grund ' + $cr.Reasons[$f]) }
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
        Replaced = $replaced; Gone = $gone; Odd = $odd; Foreign = $foreign; Lines = $lines }
}

# Nach jedem Lauf: Liste und "kein nicht erwarteter Abschluss".
function Test-Closed {
    param([string] $Nr, [string] $Label)
    $g = Get-Closed $script:ExeFull
    Add-Check $Nr ('nach dem ' + $Label + ': alle seit dem Tausch geschlossenen Entwuerfe mit Grund aus dem Audit (' + $g.Closed.Count + ')') ($g.After.Exit -eq 0 -and $g.After.Parsed -and $g.After.List.Count -lt 500 -and $g.AuditOk -and $g.Odd.Count -eq 0 -and $g.Vanished.Count -eq 0) ((@($g.Lines) -join "`n") + "`nohne passenden Grund: " + (Format-Ids $g.Odd) + "`nvorher offen, jetzt weder offen noch geschlossen lesbar: " + (Format-Ids $g.Vanished))
    $unexpected = @(@($g.Sent | Where-Object { $script:ExpSent -notcontains $_ }) + @($g.Replaced | Where-Object { $script:ExpReplaced -notcontains $_ }) +
        @($g.Gone | Where-Object { $script:ExpGone -notcontains $_ }) + @($g.Odd) + @($g.Vanished) + @($g.Foreign))
    Add-Check ($Nr + 'b') ('nach dem ' + $Label + ': kein nicht erwarteter Abschluss') ($unexpected.Count -eq 0) ('nicht erwartet: ' + (Format-Ids $unexpected))
    return $g
}

# Abbruch mit Grund; der catch am Ende erkennt das Praefix (Nachtrag 1,
# A2: fehlte in rc5 vor dem Nachtrag).
function Stop-D13 {
    param([string] $Why)
    throw ('D13-ABBRUCH: ' + $Why)
}

# Date-Kopfzeile (pushed_date, RFC 1123 mit Zahlen-Zone, etwa "Mon, 02 Nov
# 2026 15:04:05 +0100") als DateTimeOffset; $null = nicht lesbar. Erst
# fest nach Muster, dann (etwa RFC 3339) ueber TryParse.
function ConvertFrom-MailDate {
    param([string] $S)
    if (-not $S) { return $null }
    $m = [regex]::Match($S.Trim(), '^(?:[A-Za-z]{3},\s*)?(\d{1,2})\s+([A-Za-z]{3})\s+(\d{4})\s+(\d{1,2}):(\d{2})(?::(\d{2}))?\s+([+-])(\d{2})(\d{2})')
    if ($m.Success) {
        $mon = [array]::IndexOf([string[]] @('jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'), $m.Groups[2].Value.ToLowerInvariant()) + 1
        if ($mon -ge 1) {
            $sec = 0
            if ($m.Groups[6].Success) { $sec = [int] $m.Groups[6].Value }
            $off = [TimeSpan]::new([int] $m.Groups[8].Value, [int] $m.Groups[9].Value, 0)
            if ($m.Groups[7].Value -eq '-') { $off = $off.Negate() }
            try {
                return [DateTimeOffset]::new([int] $m.Groups[3].Value, $mon, [int] $m.Groups[1].Value, [int] $m.Groups[4].Value, [int] $m.Groups[5].Value, $sec, $off)
            } catch { return $null }
        }
    }
    $p = [DateTimeOffset]::MinValue
    if ([DateTimeOffset]::TryParse($S, [System.Globalization.CultureInfo]::InvariantCulture, [System.Globalization.DateTimeStyles]::AssumeUniversal, [ref] $p)) { return $p }
    return $null
}

# Nachtrag 1, A4, und Nachtrag 2, N5: seit wann die Kopie jedes offenen
# missing-Entwurfs fehlt, aus dem Audit, wie rc5 es rechnet (juengster
# drafts.remote_check mit Grund missing bzw. present, Ergebnis ok, seit der
# letzten Ablage pushed_date; mit dem alten Programm, das
# copy_missing_since noch nicht kennt). Je Entwurf eine eigene Abfrage ab
# seiner letzten Ablage, seitenweise mit --after-id. Ergebnis: Objekt mit
# Map (ID -> DateTimeOffset) und Error; Error ist nicht leer (mit der ID),
# wenn draft show nichts liefert, pushed_date fehlt oder nicht lesbar ist
# ("Zeitpunkt unbekannt"), das Audit nicht bis zum Ende lesbar ist oder
# der Entwurf seit der Ablage keinen Eintrag missing als juengsten hat.
function Get-MissingSince {
    param([string] $Prog, $Drafts)
    $out = @{}
    foreach ($d in $Drafts) {
        $id = [int64] $d.Id
        $dd = Get-DraftDetail $Prog ([string] $id)
        $floor = $null
        if ($null -ne $dd) { $floor = ConvertFrom-MailDate ([string] $dd.pushed_date) }
        if ($null -eq $dd) {
            return New-Object PSObject -Property @{ Map = $out; Error = ('draft ' + $id + ': draft show liefert nichts (Zeitpunkt der Ablage unbekannt)') }
        }
        if ($null -eq $floor) {
            return New-Object PSObject -Property @{ Map = $out; Error = ('draft ' + $id + ': pushed_date fehlt oder ist nicht lesbar (Zeitpunkt der Ablage unbekannt)') }
        }
        $lower = $floor.AddSeconds(-1)
        $au = Get-AuditPaged $Prog 'drafts.remote_check' (Format-Rfc3339 $lower)
        if ($au.Exit -ne 0) {
            return New-Object PSObject -Property @{ Map = $out; Error = ('draft ' + $id + ': Audit drafts.remote_check seit der Ablage nicht bis zum Ende lesbar (exit ' + $au.Exit + ', 99 = mehr als 50 Seiten zu 1000)') }
        }
        $best = $null
        $bestId = [int64] -1
        $bestReason = ''
        foreach ($ev in $au.Events) {
            if (@('missing', 'present') -notcontains [string] $ev.reason) { continue }
            if ([string] $ev.result -ne 'ok') { continue }
            if (@($ev.targets | ForEach-Object { [string] $_ }) -notcontains ('draft:' + $id)) { continue }
            $ts = $null
            try { $ts = [DateTimeOffset]::Parse([string] $ev.ts, [System.Globalization.CultureInfo]::InvariantCulture) } catch { }
            if ($null -eq $ts -or $ts -lt $lower) { continue }
            if ($null -eq $best -or $ts -gt $best -or ($ts -eq $best -and [int64] $ev.id -gt $bestId)) { $best = $ts; $bestId = [int64] $ev.id; $bestReason = [string] $ev.reason }
        }
        if ($bestReason -ne 'missing') {
            return New-Object PSObject -Property @{ Map = $out; Error = ('draft ' + $id + ': remote missing, aber seit der Ablage kein drafts.remote_check missing als juengster Eintrag im Audit (Zeitpunkt unbekannt)') }
        }
        $out[$id] = $best
    }
    return New-Object PSObject -Property @{ Map = $out; Error = '' }
}

# Nachtrag 1, A4, und Nachtrag 2, N3/N4: Erwartung der Auffangregel vor dem
# Tausch pruefen, je mit der aktuellen Uhrzeit ($Nr 1.2 vor der Sicherung,
# 1.6 direkt vor dem Tausch). Grenze 24 h (copy_gone_after ist nicht
# gesetzt, 0.9). Wer bei Ende von Lauf 2 (jetzt + 3 h Sicherheitsabstand)
# laenger fehlt, muss in einer Erwartungsliste stehen (auch
# -ExpectStaysOpen); wer in -ExpectCopyGone steht, muss die Grenze schon
# jetzt (also sicher vor Lauf 1) ueberschritten haben. Ist fuer einen
# missing-Entwurf nicht bekannt, seit wann seine Kopie fehlt, ist das ein
# FAIL mit seiner ID (Abbruch vor dem Tausch).
function Test-GoneExpectation {
    param([string] $Nr)
    $limit = [TimeSpan]::FromHours(24)
    $missingDrafts = @($script:Before.List | Where-Object { $_.Remote -eq 'missing' })
    $ms = Get-MissingSince $script:ExeFull $missingDrafts
    $nowD = [DateTimeOffset]::Now
    if ($ms.Error) {
        Add-Check ($Nr + 'd') 'Auffangregel: fuer jeden missing-Entwurf ist bekannt, seit wann seine Kopie fehlt (Audit drafts.remote_check seit der Ablage)' $false ('missing vorher: ' + $missingDrafts.Count + "`n" + $ms.Error)
        return
    }
    $listed = [int64[]] @($script:ExpGone + $script:ExpSent + $script:ExpReplaced + $script:ExpOpen)
    $other = [int64[]] @($script:ExpSent + $script:ExpReplaced)
    $unexpectedGone = @(); $notYet = @(); $infoOther = @()
    foreach ($d in $missingDrafts) {
        $since = $ms.Map[[int64] $d.Id]
        $willExceed = ($null -ne $since -and ($nowD.AddHours(3) - $since) -gt $limit)
        if ($willExceed -and $listed -notcontains [int64] $d.Id) { $unexpectedGone += $d.Id }
        if ($willExceed -and $other -contains [int64] $d.Id) { $infoOther += $d.Id }
    }
    foreach ($id in $script:ExpGone) {
        $since = $ms.Map[[int64] $id]
        if ($null -eq $since -or ($nowD - $since) -le $limit) { $notYet += $id }
    }
    Add-Check ($Nr + 'd') 'Auffangregel: jeder Entwurf, dessen Kopie bis zum Ende von Lauf 2 laenger als 24 h fehlt, steht in einer Erwartungsliste (auch -ExpectStaysOpen)' ($unexpectedGone.Count -eq 0) ('Stand ' + (Format-Rfc3339 $nowD) + ' (UTC), missing vorher: ' + $missingDrafts.Count + "`nnicht erwartet: " + (Format-Ids $unexpectedGone))
    Add-Check ($Nr + 'e') 'Auffangregel: jeder Entwurf aus -ExpectCopyGone fehlt schon jetzt laenger als 24 h' ($notYet.Count -eq 0) ('noch nicht sicher ueber der Grenze: ' + (Format-Ids $notYet))
    if ($infoOther.Count -gt 0) { Add-Info ($Nr + 'f') 'erwartet gesendet bzw. ersetzt, aber auch ueber der Grenze von 24 h (ein anderer Weg muss im selben Lauf greifen)' (Format-Ids $infoOther) }
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
    Add-Check '0.7' '-Tasks angegeben (Pflicht) und jede Aufgabe vorhanden' ($Tasks.Count -gt 0 -and $missingTasks.Count -eq 0) ('Aufgaben: ' + $Tasks.Count + ', nicht gefunden: ' + $missingTasks.Count)
    # Nachtrag 2, N1/N3: nur die [int64[]]-Listen; -ExpectStaysOpen zaehlt mit.
    $all = [int64[]] @($script:ExpSent + $script:ExpReplaced + $script:ExpGone + $script:ExpOpen)
    $dupes = @($all | Group-Object | Where-Object { $_.Count -gt 1 } | ForEach-Object { $_.Name })
    Add-Check '0.8' 'Erwartung: nur Zahlen, -ExpectSent, -ExpectReplaced, -ExpectCopyGone und -ExpectStaysOpen ohne gemeinsame ID' ($dupes.Count -eq 0 -and $script:BadIds.Count -eq 0) ('erwartet sent_by_human: ' + (Format-Ids $script:ExpSent) + "`nerwartet ersetzt: " + (Format-Ids $script:ExpReplaced) + "`nerwartet copy_gone: " + (Format-Ids $script:ExpGone) + "`nerwartet offen: " + (Format-Ids $script:ExpOpen) + "`ndoppelt: " + (Format-Ids $dupes) + "`nkeine Zahl: " + (Format-Ids $script:BadIds))
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
    $beforeAt = [DateTimeOffset]::Now
    Add-Check '1.2' ('draft list vorher: ' + $script:Before.List.Count + ' offene Entwuerfe (unter der Grenze von 500)') ($script:Before.Exit -eq 0 -and $script:Before.Parsed -and $script:Before.List.Count -lt 500) ('JSON lesbar: ' + $script:Before.Parsed + "`n" + (Format-Drafts $script:Before.List))
    # Nachtrag 2, N2: Marke nach der Vorher-Liste; ein Abschluss davor (rc4
    # vor 1.1) zaehlt nicht mehr als fremder Abschluss.
    Set-AuditMark $script:ExeFull '1.2a' $beforeAt
    $script:DocBefore = Get-Doctor $script:ExeFull
    Add-Check '1.3' 'doctor vorher: database ok, Schema 22' ($script:DocBefore.Database -eq 'ok' -and $script:DocBefore.Schema -eq 22) ('database ' + $script:DocBefore.Database + ', schema ' + $script:DocBefore.Schema + "`n" + (($script:DocBefore.Warnings) -join "`n"))
    if ($script:NFail -gt 0) { Stop-D13 'Stand vorher nicht lesbar oder zu viele offene Entwuerfe; nichts getauscht' }
    $openIds = @($script:Before.List | ForEach-Object { $_.Id })
    $notOpen = @($all | Where-Object { $openIds -notcontains $_ })
    Add-Check '1.2b' 'jeder erwartete Entwurf (auch -ExpectStaysOpen) ist vorher offen' ($notOpen.Count -eq 0) ('nicht offen: ' + (Format-Ids $notOpen))
    # Ersetzt werden kann nur, was vor dem Lauf missing war oder (A07 Teil 2
    # b) taken_over ist und im ersten Lauf missing wird; copy_gone nur, was
    # schon missing ist (seit mehr als 24 h, prueft rc5 selbst).
    $remoteOf = @{}
    foreach ($d in $script:Before.List) { $remoteOf[$d.Id] = $d.Remote }
    $badReplaced = @($script:ExpReplaced | Where-Object { @('missing', 'taken_over') -notcontains $remoteOf[$_] })
    $badGone = @($script:ExpGone | Where-Object { $remoteOf[$_] -ne 'missing' })
    Add-Check '1.2c' 'jeder Entwurf aus -ExpectReplaced hat vorher remote missing oder taken_over, jeder aus -ExpectCopyGone missing' ($badReplaced.Count + $badGone.Count -eq 0) ('ersetzt, aber weder missing noch taken_over: ' + (Format-Ids $badReplaced) + "`ncopy_gone, aber nicht missing: " + (Format-Ids $badGone))
    if ($script:NFail -gt 0) { Stop-D13 'Erwartung passt nicht zum Stand; nichts getauscht' }
    Test-GoneExpectation '1.2'
    if ($script:NFail -gt 0) { Stop-D13 'Erwartung der Auffangregel passt nicht oder ist nicht pruefbar; nichts getauscht' }
    $b = Invoke-Pk $script:ExeFull @('backup', 'create', '--to', $BackupTo) -TimeoutSec 7200
    Add-Check '1.5' 'Sicherung geschrieben (betrieb.md 2.2): Exit 0 und "backup written to"' ($b.Exit -eq 0 -and $b.Out -match 'backup written to') $b.Text
    if ($script:NFail -gt 0) { Stop-D13 'ohne Sicherung kein Tausch' }
    # Nachtrag 2, N4: Eine lange Sicherung kann einen Entwurf ueber die
    # Grenze von 24 h schieben; darum direkt vor dem Tausch dieselbe Pruefung
    # mit der aktuellen Uhrzeit.
    Test-GoneExpectation '1.6'
    if ($script:NFail -gt 0) { Stop-D13 'Erwartung der Auffangregel passt nach der Sicherung nicht mehr (1.6d/1.6e); nichts getauscht' }

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
    # Nachtrag 1, A3: Nach einem FAIL im ersten Lauf kein zweiter, damit er
    # nicht weitere Entwuerfe endgueltig schliesst.
    if ($script:NFail -gt $failsBefore) { Stop-D13 'FAIL nach dem ersten Lauf; kein zweiter Lauf mit dem neuen Programm' }
    Invoke-SyncRun '3.3' 'zweiter Lauf' $ExpectVersion
    $g = Test-Closed '3.4' 'zweiten Lauf'
    $missingExp = @(@($script:ExpSent | Where-Object { $g.Sent -notcontains $_ }) + @($script:ExpReplaced | Where-Object { $g.Replaced -notcontains $_ }) +
        @($script:ExpGone | Where-Object { $g.Gone -notcontains $_ }))
    # Nachtrag 2, N3: -ExpectStaysOpen muss nach Lauf 2 noch offen sein.
    $openNow = @($g.After.List | ForEach-Object { [int64] $_.Id })
    $notOpenNow = @($script:ExpOpen | Where-Object { $openNow -notcontains $_ })
    Add-Check '3.4c' 'Summe nach zwei Laeufen: jeder erwartete Abschluss mit dem erwarteten Ergebnis, jeder Entwurf aus -ExpectStaysOpen noch offen' ($missingExp.Count + $notOpenNow.Count -eq 0 -and $g.After.Parsed) ('sent_by_human: ' + (Format-Ids $g.Sent) + "`nersetzt: " + (Format-Ids $g.Replaced) + "`ncopy_gone: " + (Format-Ids $g.Gone) + "`nfehlt: " + (Format-Ids $missingExp) + "`nerwartet offen, aber nicht mehr offen: " + (Format-Ids $notOpenNow))
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

# ---- MCP (stdio) fuer -AgentKeyFile (Nachtrag 1, A8)

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
    # Nachtrag 2, N9: eigener Schreiber auf den Rohstrom, UTF-8 ohne BOM und
    # mit LF; ueber $p.StandardInput (Kodierung der Konsole) kaeme je nach
    # Einstellung ein BOM vor die erste Zeile. $p.StandardInput selbst wird
    # nie benutzt und nie geschlossen (sein Flush schriebe das BOM nach).
    $script:McpIn = New-Object System.IO.StreamWriter($p.StandardInput.BaseStream, $script:Utf8)
    $script:McpIn.NewLine = "`n"
    $script:McpIn.AutoFlush = $true
    $init = Send-Mcp 'initialize' @{ protocolVersion = '2025-06-18'; capabilities = @{}; clientInfo = @{ name = 'abnahme-d13'; version = '1' } }
    if ($null -eq $init -or $null -ne $init.error) { return $false }
    $line = (@{ jsonrpc = '2.0'; method = 'notifications/initialized' } | ConvertTo-Json -Compress -Depth 10)
    $script:McpIn.WriteLine($line)
    return $true
}

# Eine JSON-RPC-Anfrage; liefert die Antwort mit derselben id oder $null
# (Zeitgrenze 60 s). Nachtrag 2, N9: Nach einer Zeitgrenze liest das Skript
# nicht noch einmal (das haengende ReadLineAsync wuerde jeden weiteren
# Lesevorgang abweisen); $script:McpDead nennt den Grund, und jeder weitere
# Aufruf liefert sofort $null.
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
# Objekt mit den Feldern code, status und reason bei einem Fehler
# (Nachtrag 2, N9: ToolError des Servers, etwa status needs_approval mit
# dem Grund der Policy; nie die Meldung).
function Invoke-McpTool {
    param([string] $Tool, $Arguments)
    $r = Send-Mcp 'tools/call' @{ name = $Tool; arguments = $Arguments }
    if ($null -eq $r) { return New-Object PSObject -Property @{ code = 'no_answer'; status = 'error'; reason = $script:McpDead } }
    if ($null -ne $r.error) { return New-Object PSObject -Property @{ code = ('rpc_' + [string] $r.error.code); status = 'error'; reason = '' } }
    if ([bool] $r.result.isError) {
        $txt = ''
        foreach ($c in $r.result.content) { $txt += [string] $c.text }
        $e = Get-Json $txt
        $code = 'error'; $status = ''; $reason = ''
        if ($null -ne $e) {
            if ($e.code) { $code = [string] $e.code }
            if ($e.status) { $status = [string] $e.status }
            if ($e.reason) { $reason = [string] $e.reason }
        }
        return New-Object PSObject -Property @{ code = $code; status = $status; reason = $reason }
    }
    return $r.result.structuredContent
}

# Fehler eines MCP-Aufrufs fuer das Protokoll: Code, Status, Grund.
function Format-McpError {
    param($R)
    if ($null -eq $R) { return 'keine Antwort' }
    if (-not $R.code) { return 'kein Fehler' }
    return ('Fehler ' + [string] $R.code + ', Status ' + [string] $R.status + ', Grund ' + [string] $R.reason)
}

# Nach einer Zeitgrenze oder einem Ende des Servers Teil B sauber beenden
# (Nachtrag 2, N9): FAIL mit Grund, dann Abbruch.
function Test-McpAlive {
    param([string] $Nr)
    if (-not $script:McpDead) { return }
    Add-Check $Nr ('MCP-Sitzung: ' + $script:McpDead) $false
    Stop-D13 ('MCP-Server antwortet nicht; Teil B beendet (' + $script:McpDead + ')')
}

function Stop-Mcp {
    if ($null -eq $script:Mcp) { return }
    # Den eigenen Schreiber schliessen (schliesst den Rohstrom = Ende der
    # Eingabe); $p.StandardInput nicht (N9).
    try { if ($null -ne $script:McpIn) { $script:McpIn.Close() } } catch { }
    $script:McpIn = $null
    if ($script:McpDead -or -not $script:Mcp.WaitForExit(15000)) { try { $script:Mcp.Kill() } catch { } }
    $script:Mcp = $null
}

function New-ProbeDraft {
    param([string] $Subject, [string] $Text, [string] $Nr)
    if ($AgentKeyFile) {
        $r = Invoke-McpTool 'mail_create_draft' @{ account = $SelfAccount; kind = 'new'; to = @($SelfAddress); subject = $Subject; body_markdown = $Text; dry_run = $false }
        $id = ''
        if ($null -ne $r -and $null -ne $r.draft_id) { $id = [string] $r.draft_id }
        $why = 'draft ' + $id
        if (-not $id) { $why = Format-McpError $r }
        Add-Check $Nr 'Probe-Entwurf ueber MCP mit dem Agentenschluessel angelegt (neue Mail an sich selbst)' ([bool] $id) $why
        Test-McpAlive $Nr
        return $id
    }
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
    Add-Check 'B.0b' 'Konto und eigene Adresse angegeben (-SelfAccount, -SelfAddress)' ([bool] $SelfAccount -and [bool] $SelfAddress) ''
    if ($AgentKeyFile) {
        $mcpOk = (Test-Path -LiteralPath $AgentKeyFile) -and (Start-Mcp $AgentKeyFile)
        Add-Check 'B.0c' 'Agentenschluessel: Datei vorhanden, MCP-Sitzung angemeldet (Schluessel nie im Protokoll)' $mcpOk ([string] $script:McpDead)
    } else {
        Add-Info 'B.0c' 'ohne -AgentKeyFile: Probe als Mensch an der CLI (ohne Eigentuemerpruefung und ohne die Ausnahme im Runner)'
    }
    $w = @(Get-OwnWatch)
    $selfWatch = @($w | Where-Object { @($_.Accounts) -contains '*' -or @($_.Accounts) -icontains $SelfAccount }).Count
    Add-Check 'B.1' 'sync --watch genau mit -Exe laeuft fuer -SelfAccount (legt die Entwuerfe ab, fuehrt die Aktion aus, spiegelt die gesendete Nachricht)' ($selfWatch -gt 0) ('sync --watch: ' + $w.Count + ', davon fuer das Konto: ' + $selfWatch)
    if ($script:NFail -gt 0) { Stop-D13 'Voraussetzungen fehlen' }
    # Nachtrag 2, N2: Marke im Audit vor dem ersten Probe-Entwurf (B.10 und
    # B.12 lesen die Gruende danach).
    Set-AuditMark $script:ExeFull 'B.1b' ([DateTimeOffset]::Now)
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
    if ($AgentKeyFile) {
        $dj = Invoke-McpTool 'mail_supersede_draft' @{ draft_id = [int64] $first; by_draft_id = [int64] $second; dry_run = $true }
        Test-McpAlive 'B.6'
        $dryOk = ($null -ne $dj -and -not $dj.code -and [bool] $dj.dry_run -and [int] $dj.copies -ge 1)
        $dryWhy = Format-McpError $dj
    } else {
        $dry = Invoke-Pk $script:ExeFull @('draft', 'supersede', $first, '--by', $second, '--dry-run', '--json')
        $dj = Get-Json $dry.Out
        $dryOk = ($dry.Exit -eq 0 -and $null -ne $dj -and [bool] $dj.dry_run -and [int] $dj.copies -ge 1)
        $dryWhy = 'exit ' + $dry.Exit + ', Grund ' + (Get-DenyReason $dry)
    }
    Add-Check 'B.6' 'supersede im Trockenlauf: nichts geaendert, 1 Kopie wuerde in den Papierkorb gehen' ($dryOk -and [string] (Get-DraftDetail $script:ExeFull $first).status -eq 'draft') $dryWhy
    if ($AgentKeyFile) {
        $sj = Invoke-McpTool 'mail_supersede_draft' @{ draft_id = [int64] $first; by_draft_id = [int64] $second; dry_run = $false }
        Test-McpAlive 'B.7'
        $sOk = ($null -ne $sj -and -not $sj.code)
        $sWhy = Format-McpError $sj
        Stop-Mcp
    } else {
        $s = Invoke-Pk $script:ExeFull @('draft', 'supersede', $first, '--by', $second, '--json')
        $sj = Get-Json $s.Out
        $sOk = ($s.Exit -eq 0)
        $sWhy = 'exit ' + $s.Exit + ', Grund ' + (Get-DenyReason $s)
    }
    $action = ''
    if ($null -ne $sj -and $null -ne $sj.action_id) { $action = [string] $sj.action_id }
    Add-Check 'B.7' ('supersede ' + $first + ' durch ' + $second + ': alter Entwurf abandoned, Aktion angelegt') ($sOk -and $null -ne $sj -and [string] $sj.status -eq 'abandoned' -and $action) ($sWhy + ', Aktion ' + $action + ', Kopien ' + [string] $sj.copies + ', uebersprungen ' + [string] $sj.skipped)
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
    # wieder ein, auch wenn sie beim Start aus waren (vermerkt direkt vor
    # R.1, Nachtrag 1, A6).
    Add-Check 'R.0' 'aufgehobenes altes Programm vorhanden (postkauz-rc4.exe)' (Test-Path $OldCopy) $OldCopy
    if (-not (Test-Path $OldCopy)) { Stop-D13 'postkauz-rc4.exe fehlt; nichts geaendert' }
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
    # Nachtrag 2, N7: auch -Accounts ist Pflicht (R.3 und R.5 pruefen je Konto).
    Add-Check 'R.0d' '-Tasks und -Accounts angegeben (Pflicht)' ($Tasks.Count -gt 0 -and $Accounts.Count -gt 0) ('Aufgaben: ' + $Tasks.Count + ', Konten: ' + $Accounts.Count)
    $cfgText = ''
    if (Test-Path $Config) { $cfgText = [System.IO.File]::ReadAllText($Config) }
    Add-Check 'R.0e' 'Konfiguration ohne [draft] copy_gone_after (rc4 startet sonst nicht)' (-not ($cfgText -match '(?m)^\s*copy_gone_after\s*=')) 'sonst den Schluessel vorher von Hand entfernen'
    if ($script:NFail -gt 0) { Stop-D13 'Rueckweg nicht moeglich; nichts geaendert' }
    # Nachtrag 1, A6: erst hier (nach R.0 bis R.0e) vermerken, dass am Ende
    # alle Aufgaben wieder an sein sollen; ein Abbruch in R.0x ("nichts
    # geaendert") schaltet keine Aufgabe ein. Vor R.1 vermerkt, damit auch
    # ein Abbruch in R.1 sie im Abschluss wieder einschaltet.
    foreach ($tn in $Tasks) { $script:WasEnabled[$tn] = $true }
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
    try { Stop-Mcp } catch { }
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
            if ($Rueckweg) {
                # Nachtrag 2, N7: Brach der Rueckweg ab (etwa in R.1), liegt
                # unter -Exe noch das neue Programm; E.1 prueft die Version,
                # die wirklich dort liegt, statt falsch "nicht rc4" zu melden.
                $cur = Get-ProgramVersion $script:ExeFull
                if ($cur) { $ver = $cur }
                Add-Info 'E.1' ('Rueckweg nicht vollstaendig: unter ' + $script:ExeFull + ' liegt ' + $ver + '; die Aufgaben laufen damit wieder an')
            }
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
