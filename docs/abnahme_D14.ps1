<#
.SYNOPSIS
    Abnahme D14 (PostKauz, Auftrag A08): Umstellung des Dauerbetriebs von
    0.3.0-rc5 auf 0.3.0-rc6 (Anhang-Sperre) mit fuenf Proben ohne Ablage
    und Rueckweg.

.DESCRIPTION
    Laeuft auf dem Buero-PC (Windows 11, Windows PowerShell 5.1) am
    Live-Datenverzeichnis des Dauerbetriebs, ohne Go und ohne Docker.
    Anleitung: docs/abnahme_D14.md. Claude fuehrt das Skript aus.
    Muster: abnahme_D13.ps1, nur das Noetige (A08 Teil 3): keine
    Erwartungslisten, keine zwei Laeufe, kein Teil B.

    Teil A "Umstellung" (Standard):
      Teil 0  Voraussetzungen: beide Programme (Version, Pruefsumme),
              Konten (-Accounts genau wie accounts list), Aufgaben (-Tasks
              ist Pflicht), Probe-Konto, keine Schluessel attach_deny_dirs
              oder attach_deny_files in der Konfiguration (rc5 startet
              damit nicht; Rueckweg).
      Teil 1  Mit dem alten Programm (rc5): Aufgaben anhalten und
              abschalten (mit -KeepStdioMcp laeuft der stdio-MCP der
              Claude-App weiter), doctor vorher (database ok, Schema 22),
              offene Entwuerfe zaehlen, Sicherung nur mit -BackupTo.
      Teil 2  Programm tauschen: altes als postkauz-rc5.exe aufheben
              (umbenennen statt ueberschreiben mit -KeepStdioMcp), neues an
              seine Stelle; doctor nachher: Schema unveraendert, Zeile
              attach_guard da, keine neue Warnung.
      Teil 3  Fuenf Proben mit draft new --dry-run (keine Mail, kein
              Entwurf, kein APPEND) aus dem Pruefordner
              <attach_dir>\postkauz-d14-probe, den das Skript selbst anlegt
              und am Ende wieder entfernt (nur seine eigenen Dateien):
              harmlose Datei erlaubt; probe.kdbx, _secrets\probe.txt,
              probe-token.json und probe.txt:stream (nur wenn das
              Dateisystem Datenstroeme kann, sonst SKIP) je attach_denied
              mit der passenden Regelart. Danach: gleich viele offene
              Entwuerfe wie vorher, die Ablehnungen stehen im Audit.
      Teil 4  Aufgaben wieder einschalten und starten, nur wenn Teil 3
              vollstaendig und ohne FAIL lief (4.1 wie D13: Running nach
              20 s, je Konto ein sync --watch genau mit -Exe). Sonst kommt
              nach dem Tausch zuerst das alte Programm zurueck und die
              Aufgaben laufen mit ihm an.

    Modus "Rueckweg" (-Rueckweg): A08 hat keine Migration (Schema 22). Das
      alte Programm (postkauz-rc5.exe) kommt zurueck und laeuft weiter.

    Jede Pruefung ist eine Zeile "OK <Nr> ...", "FAIL <Nr> ...", "WARN
    <Nr> ...", "INFO <Nr> ..." oder "SKIP <Nr> ...". Pfade unter attach_dir,
    Betreff, Adressen und die Namen der Aufgaben stehen nie im Protokoll.
    Letzte Zeile: "D14: <n> OK, <m> FAIL, <w> WARN, <s> SKIP". Exit 0 nur
    ohne FAIL. Ein vorhandenes Protokoll wird unter einem Namen mit
    Zeitstempel beiseitegelegt.

.PARAMETER Exe
    Das Programm des Dauerbetriebs (Standard C:\PostKauz\postkauz.exe).

.PARAMETER NewExe
    Das neue Programm (postkauz-windows-amd64.exe aus dem Build 0.3.0-rc6).
    Neben ihm muss SHA256SUMS liegen. Nur fuer Teil A.

.PARAMETER DataDir
    Live-Datenverzeichnis (Standard C:\PostKauz\data).

.PARAMETER Config
    Live-Konfiguration (Standard C:\PostKauz\config.toml).

.PARAMETER BackupTo
    Optional: Ordner einer Sicherung vor dem Tausch (betrieb.md 2.2/2.3) auf
    einem verschluesselten Datentraeger. A08 hat keine Migration und die
    Proben schreiben nichts; ohne -BackupTo keine Sicherung (INFO 1.4).

.PARAMETER Accounts
    Alle Konten (mit Komma); 4.1 prueft je Konto ein sync --watch, der
    Rueckweg je Konto einen sync.

.PARAMETER Tasks
    Pflicht in Teil A und im Rueckweg: alle geplanten Aufgaben, die
    PostKauz starten, mit Komma getrennt (Namen nicht raten).

.PARAMETER ProbeAccount
    Konto fuer die Proben (draft new -a <Konto> --dry-run; es entsteht
    nichts). Standard: das erste aus -Accounts.

.PARAMETER ExpectSha256
    SHA-256 der neuen Programmdatei (aus dem Protokoll des Builds).

.PARAMETER ExpectVersion
    Version des neuen Programms (Standard 0.3.0-rc6).

.PARAMETER OldVersion
    Version des alten Programms (Standard 0.3.0-rc5).

.PARAMETER Rueckweg
    Statt der Umstellung den Rueckweg auf das alte Programm pruefen.

.PARAMETER KeepStdioMcp
    Der stdio-MCP der Claude-App darf weiterlaufen (Tausch per Umbenennen;
    nur ohne Migration moeglich, A08 hat keine).

.PARAMETER LogFile
    Protokolldatei. Standard: D14_windows.txt neben dem Programm.

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d14\abnahme_D14.ps1 -NewExe C:\PostKauz\d14\postkauz-windows-amd64.exe -Accounts a,b,c -Tasks "PostKauz sync watch a,PostKauz sync watch b,PostKauz sync watch c,PostKauz audit export,PostKauz mcp http" -ProbeAccount a -KeepStdioMcp
#>
param(
    [string] $Exe = 'C:\PostKauz\postkauz.exe',
    [string] $NewExe = '',
    [string] $DataDir = 'C:\PostKauz\data',
    [string] $Config = 'C:\PostKauz\config.toml',
    [string] $BackupTo = '',
    [string[]] $Accounts = @(),
    [string[]] $Tasks = @(),
    [string] $ProbeAccount = '',
    [string] $ExpectSha256 = '',
    [string] $ExpectVersion = '0.3.0-rc6',
    [string] $OldVersion = '0.3.0-rc5',
    [switch] $Rueckweg,
    [switch] $KeepStdioMcp,
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
if (-not $ProbeAccount -and $Accounts.Count -gt 0) { $ProbeAccount = $Accounts[0] }
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
# Teil 3 vollstaendig durchgelaufen: nur dann ist ein Ergebnis ohne FAIL
# eine bestandene Abnahme.
$script:Complete = $false
# Aufgaben am Ende nicht wieder einschalten (Rueckweg gescheitert).
$script:KeepTasksOff = $false
# Vom Skript angelegte Pruefdateien und -ordner (nur diese entfernt es).
$script:ProbeFiles = New-Object System.Collections.Generic.List[string]
$script:ProbeDirs = New-Object System.Collections.Generic.List[string]
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
        throw ('D14-ABBRUCH: Prozessliste nicht lesbar (Get-CimInstance): ' + $_.Exception.Message)
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
        throw ('D14-ABBRUCH: Prozessliste nicht lesbar (Get-CimInstance): ' + $_.Exception.Message)
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
# ExecutablePath gleich -Exe; postkauz-rc5.exe im selben Ordner zaehlt
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
    if ($KeepStdioMcp) { $hint = 'Claude-App nach D14 neu starten, damit ihr MCP das aktuelle Programm nimmt' }
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

function Get-NewCopyName { return (Get-FreeName 'postkauz-rc6') }

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


function Stop-D14 {
    param([string] $Why)
    throw ('D14-ABBRUCH: ' + $Why)
}

function Add-Skip {
    param([string] $Nr, [string] $Desc)
    $script:NSkip++
    Write-Log ('SKIP ' + $Nr + ' ' + $Desc)
}

# Zeile "attach_guard: <n> dir rules, <m> file rules" aus doctor (Text);
# "" = fehlt (rc5 kennt sie nicht).
function Get-GuardLine {
    param([string] $Prog)
    $r = Invoke-Pk $Prog @('doctor')
    $m = [regex]::Match($r.Out, '(?m)^attach_guard: [0-9]+ dir rules, [0-9]+ file rules\s*$')
    if ($m.Success) { return $m.Value.Trim() }
    return ''
}

# Zahl der offenen Entwuerfe (draft list --json); -1 = nicht lesbar.
function Get-OpenCount {
    param([string] $Prog)
    $r = Invoke-Pk $Prog @('draft', 'list', '--json', '--limit', '500', '--status', 'draft', '--status', 'pending_approval', '--status', 'ready')
    $j = Get-Json $r.Out
    if ($r.Exit -ne 0 -or $null -eq $j -or $null -eq $j.drafts) { return -1 }
    return @($j.drafts).Count
}

# Konfiguration mit [draft] attach_deny_dirs/attach_deny_files? rc5 und
# aelter starten damit nicht (unbekannte Schluessel sind ein Fehler).
function Test-DenyKeys {
    $cfgText = ''
    if (Test-Path $Config) { $cfgText = [System.IO.File]::ReadAllText($Config) }
    return ($cfgText -match '(?m)^\s*attach_deny_(dirs|files)\s*=')
}

# Pruefdatei anlegen (nur wenn sie noch nicht da ist) und vermerken.
function New-ProbeFile {
    param([string] $Path)
    $d = Split-Path -Parent $Path
    if (-not (Test-Path -LiteralPath $d)) {
        [void] (New-Item -ItemType Directory -Path $d -ErrorAction Stop)
        $script:ProbeDirs.Add($d)
    }
    if (Test-Path -LiteralPath $Path) { throw ('D14-ABBRUCH: Pruefdatei existiert schon (' + (Split-Path -Leaf $Path) + '); Pruefordner von Hand ansehen') }
    [System.IO.File]::WriteAllText($Path, "PostKauz D14 Probe, harmloser Inhalt`r`n", $script:Utf8)
    $script:ProbeFiles.Add($Path)
}

# Entfernt nur die vom Skript angelegten Dateien und dann die von ihm
# angelegten Ordner, wenn sie leer sind (tiefste zuerst).
function Remove-Probes {
    param([string] $Nr)
    $left = @()
    foreach ($f in $script:ProbeFiles) {
        try { if (Test-Path -LiteralPath $f) { Remove-Item -LiteralPath $f -Force -ErrorAction Stop } } catch { $left += (Split-Path -Leaf $f) }
    }
    foreach ($d in @($script:ProbeDirs | Sort-Object { $_.Length } -Descending)) {
        try {
            if ((Test-Path -LiteralPath $d) -and @(Get-ChildItem -LiteralPath $d -Force).Count -eq 0) { Remove-Item -LiteralPath $d -Force -ErrorAction Stop }
            elseif (Test-Path -LiteralPath $d) { $left += ('Ordner ' + (Split-Path -Leaf $d) + ' nicht leer') }
        } catch { $left += ('Ordner ' + (Split-Path -Leaf $d)) }
    }
    if ($script:ProbeFiles.Count + $script:ProbeDirs.Count -gt 0) {
        Add-Check $Nr 'Pruefdateien und Pruefordner entfernt (nur die vom Skript angelegten)' ($left.Count -eq 0) ('uebrig: ' + ($left -join ', '))
    }
    $script:ProbeFiles.Clear()
    $script:ProbeDirs.Clear()
}

# Eine Probe: draft new -a <Probe-Konto> --dry-run mit der Datei als Anhang.
# Erwartet $Rule = '' (erlaubt) oder die Regelart der Ablehnung. Ins
# Protokoll kommen nur Exit, Grund und Regelart, nie der Pfad.
function Test-Probe {
    param([string] $Nr, [string] $Label, [string] $Path, [string] $Rule)
    $r = Invoke-Pk $script:ExeFull @('draft', 'new', '-a', $ProbeAccount, '--to', 'probe@example.invalid', '--subject', 'PostKauz D14 Probe',
        '--attach', $Path, '--dry-run') -TimeoutSec 300
    $reason = Get-DenyReason $r
    $m = [regex]::Match(($r.Out + "`n" + $r.Err), 'rule: ([a-z_]+)')
    $got = ''
    if ($m.Success) { $got = $m.Groups[1].Value }
    $leak = ($r.Out + $r.Err).Contains($script:AttachDir)
    $detail = 'exit ' + $r.Exit + ', Grund ' + $reason + ', Regelart ' + $got + ', Pfad in der Ausgabe: ' + $leak
    if ($Rule -eq '') {
        Add-Check $Nr ($Label + ': erlaubt (draft new --dry-run, Exit 0)') ($r.Exit -eq 0) $detail
        return
    }
    Add-Check $Nr ($Label + ': denied, Grund attach_denied, Regelart ' + $Rule + ', ohne Pfad') ($r.Exit -eq 3 -and $reason -eq 'attach_denied' -and $got -eq $Rule -and -not $leak) $detail
}

# ---------------------------------------------------------------- Teil A

function Invoke-Umstellung {
    Write-Log '--- Teil 0: Voraussetzungen'
    $newFull = ''
    if ($NewExe) { $newFull = [System.IO.Path]::GetFullPath($NewExe) }
    Add-Check '0.1' 'neues Programm angegeben und vorhanden' ($newFull -and (Test-Path $newFull)) $newFull
    if (-not ($newFull -and (Test-Path $newFull))) { Stop-D14 'neues Programm fehlt' }
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
    $unknown = @($Accounts | Where-Object { $known -notcontains $_ })
    $notGiven = @($known | Where-Object { $Accounts -notcontains $_ })
    Add-Check '0.5' '-Accounts entspricht genau den konfigurierten Konten (accounts list)' ($Accounts.Count -gt 0 -and $al.Exit -eq 0 -and $unknown.Count -eq 0 -and $notGiven.Count -eq 0) ('angegeben: ' + $Accounts.Count + ', konfiguriert: ' + $known.Count + ', nicht konfiguriert: ' + $unknown.Count + ', konfiguriert, aber nicht angegeben: ' + $notGiven.Count)
    $missingTasks = @($Tasks | Where-Object { $null -eq (Get-ScheduledTask -TaskName $_ -ErrorAction SilentlyContinue) })
    Add-Check '0.6' '-Tasks angegeben (Pflicht) und jede Aufgabe vorhanden' ($Tasks.Count -gt 0 -and $missingTasks.Count -eq 0) ('Aufgaben: ' + $Tasks.Count + ', nicht gefunden: ' + $missingTasks.Count)
    Add-Check '0.7' 'Probe-Konto ist eines aus -Accounts' ($ProbeAccount -and ($Accounts -contains $ProbeAccount)) ''
    Add-Check '0.8' 'Konfiguration ohne [draft] attach_deny_dirs/attach_deny_files (rc5 startet sonst nicht; Rueckweg; erst nach D14 setzen)' (-not (Test-DenyKeys)) ''
    if ($script:NFail -gt 0) { Stop-D14 'Voraussetzungen fehlen; nichts geaendert' }

    Write-Log '--- Teil 1: Stand vorher (altes Programm)'
    Stop-Tasks '1.1'
    if ($script:NFail -gt 0) { Stop-D14 'es laeuft noch ein postkauz.exe (Claude-App oder Aufgabe) oder eine Aufgabe liess sich nicht abschalten; nichts getauscht' }
    $script:DocBefore = Get-Doctor $script:ExeFull
    Add-Check '1.2' 'doctor vorher: database ok, Schema 22' ($script:DocBefore.Database -eq 'ok' -and $script:DocBefore.Schema -eq 22) ('database ' + $script:DocBefore.Database + ', schema ' + $script:DocBefore.Schema + "`n" + (($script:DocBefore.Warnings) -join "`n"))
    $gl = Get-GuardLine $script:ExeFull
    Add-Info '1.2b' ('doctor vorher, Zeile attach_guard (rc5 kennt sie nicht): ' + $(if ($gl) { $gl } else { 'fehlt' }))
    $script:OpenBefore = Get-OpenCount $script:ExeFull
    Add-Check '1.3' ('offene Entwuerfe vorher lesbar: ' + $script:OpenBefore) ($script:OpenBefore -ge 0) ''
    if ($script:NFail -gt 0) { Stop-D14 'Stand vorher nicht lesbar; nichts getauscht' }
    if ($BackupTo) {
        $b = Invoke-Pk $script:ExeFull @('backup', 'create', '--to', $BackupTo) -TimeoutSec 7200
        Add-Check '1.4' 'Sicherung geschrieben (betrieb.md 2.2): Exit 0 und "backup written to"' ($b.Exit -eq 0 -and $b.Out -match 'backup written to') $b.Text
        if ($script:NFail -gt 0) { Stop-D14 'Sicherung gescheitert; nichts getauscht' }
    } else {
        Add-Info '1.4' 'keine Sicherung (ohne -BackupTo): A08 hat keine Migration, die Proben schreiben nichts'
    }

    Write-Log '--- Teil 2: Programm tauschen'
    $why = ''
    try {
        Install-Program $newFull $OldCopy
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Check '2.1' ('getauscht: ' + $script:ExeFull + ' meldet ' + $ExpectVersion + ', altes Programm als postkauz-rc5.exe aufgehoben') ($v.Out -match [regex]::Escape($ExpectVersion) -and (Test-Path $OldCopy)) $v.Text
        $d = Get-Doctor $script:ExeFull
        $old = @{}
        foreach ($w in $script:DocBefore.Warnings) { $old[(Get-WarningKey $w)] = $true }
        $new = @($d.Warnings | Where-Object { -not $old.ContainsKey((Get-WarningKey $_)) })
        Add-Check '2.2' ('doctor nachher: database ok, Schema unveraendert ' + $script:DocBefore.Schema + ', keine neue Warnung') ($d.Database -eq 'ok' -and $d.Schema -eq $script:DocBefore.Schema -and $new.Count -eq 0) ('database ' + $d.Database + ', schema ' + $d.Schema + "`nneu: " + (($new) -join "`n"))
        $gl = Get-GuardLine $script:ExeFull
        Add-Check '2.3' 'doctor nachher zeigt die Zeile attach_guard' ([bool] $gl) $gl
        $j = Get-Json (Invoke-Pk $script:ExeFull @('doctor', '--json')).Out
        $script:AttachDir = ''
        if ($null -ne $j) { $script:AttachDir = [string] $j.attach_dir }
        Add-Check '2.4' 'attach_dir aus doctor --json gelesen und vorhanden' ($script:AttachDir -and (Test-Path -LiteralPath $script:AttachDir)) ''
        if ($script:NFail -gt 0) { $why = 'Tausch nicht in Ordnung' }
    } catch {
        Add-Check '2.x' ('Fehler beim Tausch: ' + $_.Exception.Message) $false
        $why = 'Fehler beim Tausch'
    }
    if ($why) { Stop-D14 $why }
    if ($KeepStdioMcp) {
        Add-Info '2.5' 'Der stdio-MCP der Claude-App laeuft bis zum naechsten Neustart der App mit dem alten Programm (postkauz-rc5.exe).'
    }

    Write-Log '--- Teil 3: fuenf Proben ohne Ablage (draft new --dry-run)'
    $probe = Join-Path $script:AttachDir 'postkauz-d14-probe'
    Add-Check '3.0' 'Pruefordner postkauz-d14-probe unter attach_dir existiert noch nicht (sonst von Hand ansehen und entfernen)' (-not (Test-Path -LiteralPath $probe)) ''
    if ($script:NFail -gt 0) { Stop-D14 'Pruefordner existiert schon' }
    $harmless = Join-Path $probe 'probe-harmlos.txt'
    $kdbx = Join-Path $probe 'probe.kdbx'
    $secret = Join-Path (Join-Path $probe '_secrets') 'probe.txt'
    $token = Join-Path $probe 'probe-token.json'
    $streamBase = Join-Path $probe 'probe.txt'
    New-ProbeFile $harmless
    New-ProbeFile $kdbx
    New-ProbeFile $secret
    New-ProbeFile $token
    $failsBefore = $script:NFail
    Test-Probe '3.1' 'harmlose Datei aus dem Pruefordner' $harmless ''
    Test-Probe '3.2' 'probe.kdbx' $kdbx 'file'
    Test-Probe '3.3' '_secrets\probe.txt' $secret 'dir'
    Test-Probe '3.4' 'probe-token.json' $token 'file'
    # Datenstrom nur, wenn das Dateisystem ihn zulaesst (NTFS); sonst SKIP.
    $streamOk = $false
    try {
        New-ProbeFile $streamBase
        Set-Content -LiteralPath $streamBase -Stream 'stream' -Value 'PostKauz D14 Probe' -ErrorAction Stop
        $streamOk = $true
    } catch { }
    if ($streamOk) {
        Test-Probe '3.5' 'probe.txt:stream (alternativer Datenstrom)' ($streamBase + ':stream') 'stream'
    } else {
        Add-Skip '3.5' 'probe.txt:stream: das Dateisystem laesst keinen Datenstrom zu'
    }
    $after = Get-OpenCount $script:ExeFull
    Add-Check '3.6' ('offene Entwuerfe nachher gleich vorher (' + $script:OpenBefore + '): die Proben haben nichts angelegt') ($after -eq $script:OpenBefore) ('nachher: ' + $after)
    $nDenied = 3
    if ($streamOk) { $nDenied = 4 }
    $au = Get-AuditPaged $script:ExeFull 'drafts.create' (Format-Rfc3339 ([DateTimeOffset] $script:Started.AddMinutes(-1)))
    $denied = @($au.Events | Where-Object { [string] $_.result -eq 'denied' -and [string] $_.reason -eq 'attach_denied' }).Count
    Add-Check '3.7' ('Audit: ' + $nDenied + ' Ablehnungen drafts.create mit Grund attach_denied seit dem Start') ($au.Exit -eq 0 -and $denied -eq $nDenied) ('exit ' + $au.Exit + ', gefunden: ' + $denied)
    Remove-Probes '3.8'
    if ($script:NFail -eq $failsBefore) { $script:Complete = $true }
}

# ---------------------------------------------------------------- Rueckweg

function Invoke-Rueckweg {
    Write-Log '--- Rueckweg: altes Programm zurueck (keine Migration in A08)'
    Add-Check 'R.0' 'aufgehobenes altes Programm vorhanden (postkauz-rc5.exe)' (Test-Path $OldCopy) $OldCopy
    if (-not (Test-Path $OldCopy)) { Stop-D14 'postkauz-rc5.exe fehlt; nichts geaendert' }
    $newCopy = Get-NewCopyName
    $present = Test-Path $script:ExeFull
    if ($present) {
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Check 'R.0b' ('laufendes Programm meldet ' + $ExpectVersion) ($v.Out -match [regex]::Escape($ExpectVersion)) ($v.Text + "`nneues Programm wird beiseitegelegt als " + (Split-Path -Leaf $newCopy))
    } else {
        Add-Info 'R.0b' ($script:ExeFull + ' fehlt (harter Abbruch?); das alte Programm kommt an seine Stelle')
    }
    $o = Invoke-Pk $OldCopy @('version') -NoDirs
    Add-Check 'R.0c' ('postkauz-rc5.exe meldet ' + $OldVersion) ($o.Out -match [regex]::Escape($OldVersion)) $o.Text
    Add-Check 'R.0d' '-Tasks und -Accounts angegeben (Pflicht)' ($Tasks.Count -gt 0 -and $Accounts.Count -gt 0) ('Aufgaben: ' + $Tasks.Count + ', Konten: ' + $Accounts.Count)
    Add-Check 'R.0e' 'Konfiguration ohne [draft] attach_deny_dirs/attach_deny_files (rc5 startet sonst nicht)' (-not (Test-DenyKeys)) 'sonst die Schluessel vorher von Hand entfernen'
    if ($script:NFail -gt 0) { Stop-D14 'Rueckweg nicht moeglich; nichts geaendert' }
    # Wie D13: erst hier vermerken, dass am Ende alle Aufgaben wieder an sein
    # sollen (ein Abbruch in R.0x schaltet keine ein, einer in R.1 schon).
    foreach ($tn in $Tasks) { $script:WasEnabled[$tn] = $true }
    Stop-Tasks 'R.1'
    if ($script:NFail -gt 0) { Stop-D14 'es laeuft noch ein postkauz.exe; nichts getauscht' }
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
        Stop-D14 'Rueckweg gescheitert; Aufgaben bleiben AUS (von Hand nach abnahme_D14.md Abschnitt 5 weiter)'
    }
    if ($KeepStdioMcp) {
        Add-Info 'R.1c' 'Der stdio-MCP der Claude-App laeuft bis zum naechsten Neustart der App mit dem bisherigen Programm.'
    }
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    $d = Get-Doctor $script:ExeFull
    Add-Check 'R.2' ('altes Programm laeuft mit der Datenbank: ' + $OldVersion + ', database ok, Schema 22') ($v.Out -match [regex]::Escape($OldVersion) -and $d.Database -eq 'ok' -and $d.Schema -eq 22) ($v.Text + "`ndatabase " + $d.Database + ', schema ' + $d.Schema)
    Invoke-SyncRun 'R.3' ('Rueckweg mit ' + $OldVersion) $OldVersion
    Add-Info 'R.4' 'Fall: ohne Migration. rc5 kennt die Anhang-Sperre nicht: Dateien unter attach_dir sind wieder nur durch die Betriebsregel geschuetzt.'
    [void] (Start-Tasks 'R.5' $OldVersion)
    $script:Restarted = $true
}

# ---------------------------------------------------------------- Start

$script:ExeFull = [System.IO.Path]::GetFullPath($Exe)
$dir = Split-Path -Parent $script:ExeFull
$script:ExeDir = $dir
$script:AttachDir = ''
if (-not $LogFile) { $LogFile = Join-Path $dir 'D14_windows.txt' }
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
$script:Log = $LogFile
$OldCopy = Join-Path $dir 'postkauz-rc5.exe'
$script:Swapped = $false
$script:Restarted = $false
$mode = 'Teil A, Umstellung'
if ($Rueckweg) { $mode = 'Rueckweg' }
Write-Log ('=== Abnahme D14 (' + $mode + '), ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + ' ===')
Write-Log ('Programm: ' + $script:ExeFull + '; Daten: ' + $DataDir + '; Konfiguration: ' + $Config)
$elevated = $false
try { $elevated = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator) } catch { }
Add-Info '0.0' ('PowerShell mit Administratorrechten: ' + $elevated + ' (Anleitung Abschnitt 3: so wie die Aufgaben angelegt sind)')

try {
    if ($Rueckweg) { Invoke-Rueckweg } else { Invoke-Umstellung }
} catch {
    $msg = [string] $_.Exception.Message
    if ($msg.StartsWith('D14-ABBRUCH: ')) {
        Write-Log ('Abbruch: ' + $msg.Substring(13))
    } else {
        Add-Check 'X' ('unerwarteter Fehler im Skript: ' + $msg) $false
    }
} finally {
    # Pruefdateien auch nach einem Abbruch entfernen (nur die eigenen).
    try { Remove-Probes 'E.3' } catch { Add-Check 'E.3' ('Pruefdateien entfernen: ' + $_.Exception.Message) $false }
    $umst = (-not $Rueckweg)
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
            Add-Check 'E.1' 'Aufgaben NICHT wieder eingeschaltet: das richtige Programm liegt nicht sicher an seinem Platz (von Hand nach abnahme_D14.md Abschnitt 5)' $false
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
                if (-not $done41) { Write-Log 'Teil 4 nicht vollstaendig: Aufgaben von Hand pruefen (abnahme_D14.md Abschnitt 3, harter Abbruch).' }
            }
            if (-not $started) {
                try {
                    $before = $script:NFail
                    Stop-Tasks 'E.0a'
                    if ($script:NFail -gt $before) {
                        Add-Check 'E.0b' 'Aufgaben nach 4.1 nicht sauber angehalten: altes Programm NICHT zurueckgelegt, Aufgaben bleiben aus (von Hand nach abnahme_D14.md Abschnitt 5)' $false
                    } elseif (Restore-Program $OldCopy 'E.0') {
                        [void] (Start-Tasks 'E.1' $OldVersion)
                    } else {
                        Add-Check 'E.1' 'Aufgaben angehalten, altes Programm liess sich nicht zuruecklegen (von Hand nach abnahme_D14.md Abschnitt 5)' $false
                    }
                } catch { Add-Check 'E.1' ('Rueckweg nach 4.1: ' + $_.Exception.Message) $false }
            }
        } else {
            $ver = $OldVersion
            if ($Rueckweg) {
                $cur = Get-ProgramVersion $script:ExeFull
                if ($cur) { $ver = $cur }
                Add-Info 'E.1' ('Rueckweg nicht vollstaendig: unter ' + $script:ExeFull + ' liegt ' + $ver + '; die Aufgaben laufen damit wieder an')
            }
            try { [void] (Start-Tasks 'E.1' $ver) } catch { Add-Check 'E.1' ('Aufgaben wieder einschalten: ' + $_.Exception.Message) $false }
        }
    }
    if ($umst) {
        $cur = Get-ProgramVersion $script:ExeFull
        if (-not $cur) { $cur = '(fehlt oder meldet keine Version)' }
        $keep = 'fehlt'
        if (Test-Path $OldCopy) { $keep = (Get-ProgramVersion $OldCopy) }
        Write-Log ('Stand: ' + $script:ExeFull + ' meldet ' + $cur + '; postkauz-rc5.exe: ' + $keep)
    }
    Write-Log ''
    if ($script:Fails.Count -gt 0) { Write-Log ('Fehlgeschlagen: ' + ($script:Fails -join '; ')) }
    if ($script:LogBroken) { try { Write-Host 'ACHTUNG: Das Protokoll liess sich nicht vollstaendig schreiben (Datentraeger voll oder gesperrt).' } catch { } }
    Write-Log ('D14: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL, ' + $script:NWarn + ' WARN, ' + $script:NSkip + ' SKIP')
}
if ($script:NFail -gt 0) { exit 1 }
exit 0
