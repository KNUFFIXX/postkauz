<#
.SYNOPSIS
    Abnahme A3 (PostKauz, A03 Teil T1c): verdeckte Passwortabfrage unter
    Windows messen (Befund D5).

.DESCRIPTION
    Misst je Fenster (Konsole x Shell) und je Leser (console = ReadConsoleW,
    neu ab 0.3.0; term = golang.org/x/term wie bis 0.2.0-rc8), ob die
    verdeckte Abfrage Tippen, Einfuegen mit Strg+V und Einfuegen mit
    Rechtsklick annimmt, je mit ASCII und mit Umlauten (sechs Schritte,
    A3 Punkt 2). Dazu
    ruft es das versteckte Kommando "postkauz internal prompt-probe --json"
    auf; das liest eine Zeile wie "account add" und meldet nur Zaehler,
    Konsolenmodus und, ob der SHA-256 der Eingabe dem mit --expect-sha256
    vorgegebenen Hash des Testwerts entspricht (hash_match), nie den Wert
    und nie den Hash. Nie ein echtes Passwort eingeben.

    Der Betreiber tippt bzw. fuegt nur auf Aufforderung ein. Testwerte sind
    keine Geheimnisse (fest im Skript); vor jedem Einfuegen legt das Skript
    den Wert in die Zwischenablage und leert sie danach.

    Es ruft postkauz.exe immer mit --data-dir und --config auf (Rahmen 5
    aus auftraege/A03_teile.md). prompt-probe oeffnet weder Datenbank noch
    Schluesselbund; das Skript verweigert trotzdem das
    Standard-Datenverzeichnis (%LOCALAPPDATA%\postkauz) und jeden mit
    -LivePath genannten Pfad, und prueft am Ende, dass im Datenverzeichnis
    nichts entstanden ist.

    Je Schritt eine Zeile "OK <Fenster> <Leser> <Schritt>" oder "FAIL ...",
    darunter eingerueckt die Messwerte. Fuer den Leser console gilt
    "Erwartet: OK" (Abnahme); die Zeilen des Lesers term sind Messung
    (Vergleich mit dem Befund D5) und zaehlen nicht als FAIL der Abnahme.
    Letzte Zeile: "A3 <Fenster>: <n> OK, <m> FAIL (console), term: <k> OK,
    <l> FAIL". Exit 0 nur ohne FAIL beim Leser console.

    Vor jedem Schritt wartet das Skript auf Enter (Startsignal); erst dann
    erscheint die Abfrage und laufen die 60 s Zeitgrenze.

    Mit -Launch misst das Skript zuerst das eigene Fenster und oeffnet
    danach die weiteren Fenster der Matrix (Windows Terminal, Legacy-Konsole
    conhost.exe, PowerShell 7 falls vorhanden, cmd.exe) NACHEINANDER, jedes
    mit demselben Skript und eigenem -HostLabel: Das naechste Fenster
    startet erst, wenn das vorige fertig ist (Start-Process -Wait, bei
    Windows Terminal ueber eine Fertig-Datei im Temp-Ordner, weil wt.exe
    sofort zurueckkehrt). So teilen sich nie zwei Fenster Zwischenablage
    oder Protokoll; das Protokoll schreibt jede Zeile zusaetzlich mit
    Wiederholung, falls die Datei kurz gesperrt ist (IOException).

.PARAMETER Exe
    Pfad zu postkauz.exe (Build 0.3.0-alpha.c2 aus dist\).

.PARAMETER DataDir
    Frisches, leeres Datenverzeichnis nur fuer diesen Test (wird angelegt).

.PARAMETER Config
    Konfigurationsdatei nur fuer diesen Test (wird leer angelegt, wenn sie
    fehlt).

.PARAMETER Protocol
    Protokolldatei (Text, UTF-8; wird ergaenzt). Fuer das Repository:
    docs\protokolle\A3_prompt_windows.txt.

.PARAMETER HostLabel
    Name des Fensters, z. B. "WT+PS51", "conhost+PS51", "WT+PS7",
    "conhost+cmd". Steht in jeder Protokollzeile.

.PARAMETER Via
    direct (Standard): postkauz.exe direkt aus dieser PowerShell starten;
    cmd: ueber cmd.exe /c (misst cmd.exe als Elternprozess).

.PARAMETER Readers
    Leser, Standard console und term.

.PARAMETER LivePath
    Pfade, die nie benutzt werden duerfen (Live-Datenverzeichnis des
    Dauerbetriebs, dessen Konfiguration).

.PARAMETER Launch
    Erst selbst messen, dann die weiteren Fenster der Matrix nacheinander
    oeffnen und auf jedes warten.

.PARAMETER DoneFile
    Nur intern (von -Launch gesetzt): Datei, in die das Fenster am Ende
    seinen Exit-Code schreibt.

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File abnahme_A3.ps1 -Exe D:\pk\a3\postkauz.exe -DataDir D:\pk\a3\data -Config D:\pk\a3\config.toml -Protocol D:\pk\a3\A3_prompt_windows.txt -HostLabel WT+PS51 -LivePath D:\pk\live -Launch
#>
param(
    [Parameter(Mandatory = $true)] [string] $Exe,
    [Parameter(Mandatory = $true)] [string] $DataDir,
    [Parameter(Mandatory = $true)] [string] $Config,
    [Parameter(Mandatory = $true)] [string] $Protocol,
    [Parameter(Mandatory = $true)] [string] $HostLabel,
    [ValidateSet('direct', 'cmd')] [string] $Via = 'direct',
    [string[]] $Readers = @('console', 'term'),
    [string[]] $LivePath = @(),
    [switch] $Launch,
    [string] $DoneFile = ''
)

$ErrorActionPreference = 'Stop'
$script:Utf8 = New-Object System.Text.UTF8Encoding $false
$script:Ok = @{ console = 0; term = 0 }
$script:Fail = @{ console = 0; term = 0 }
# Die Ausgabe-Codepage bleibt, wie das Fenster sie hat: Sie ist ein
# Messwert (Hypothese c). Die JSON-Ausgabe der Probe ist ASCII.

# -File reicht "a,b" als einen String durch: Pfadlisten selbst trennen,
# sonst griffe der Schutz vor dem Live-Pfad nicht.
$LivePath = @($LivePath | ForEach-Object { $_ -split ',' } | ForEach-Object { $_.Trim() } | Where-Object { $_ })

# ---------------------------------------------------------------- Hilfen

# Protokollzeile anhaengen; ist die Datei kurz gesperrt (Virenscanner,
# Editor, IOException), bis zu 20-mal im Abstand von 250 ms wiederholen.
function Write-Log {
    param([string] $Line)
    Write-Host $Line
    for ($i = 1; ; $i++) {
        try {
            [System.IO.File]::AppendAllText($Protocol, $Line + "`r`n", $script:Utf8)
            return
        } catch {
            $ex = $_.Exception
            while ($ex -and -not ($ex -is [System.IO.IOException])) { $ex = $ex.InnerException }
            if (-not $ex -or $i -ge 20) { throw }
            Start-Sleep -Milliseconds 250
        }
    }
}

# Setzt einen Wert fuer die Befehlszeile in Anfuehrungszeichen; Backslashes
# am Ende werden verdoppelt (sonst maskierte "D:\x\" das Zeichen ").
function Format-Arg {
    param([string] $Value)
    return '"' + ($Value -replace '(\\+)$', '$1$1') + '"'
}

# Wartet, bis ein Fenster seine Fertig-Datei geschrieben hat; Q bricht das
# Warten ab (Rueckgabe $false).
function Wait-DoneFile {
    param([string] $File, [string] $Label)
    Write-Host ("Warte auf das Fenster {0} (Q = nicht mehr warten) ..." -f $Label)
    while (-not (Test-Path -LiteralPath $File)) {
        try {
            if ([Console]::KeyAvailable -and [Console]::ReadKey($true).Key -eq 'Q') { return $false }
        } catch { }
        Start-Sleep -Milliseconds 500
    }
    return $true
}

function Get-Sha256Hex {
    param([string] $Text)
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        $bytes = $sha.ComputeHash($script:Utf8.GetBytes($Text))
        return -join ($bytes | ForEach-Object { $_.ToString('x2') })
    } finally { $sha.Dispose() }
}

function Get-FullPath {
    param([string] $Path)
    return [System.IO.Path]::GetFullPath($Path).TrimEnd('\').ToLowerInvariant()
}

function Assert-NotLive {
    param([string] $Path, [string] $Name)
    $p = Get-FullPath $Path
    $forbidden = @()
    if ($env:LOCALAPPDATA) { $forbidden += (Join-Path $env:LOCALAPPDATA 'postkauz') }
    if ($env:APPDATA) { $forbidden += (Join-Path $env:APPDATA 'postkauz') }
    $forbidden += $LivePath
    foreach ($f in $forbidden) {
        if (-not $f) { continue }
        $q = Get-FullPath $f
        if ($p -eq $q -or $p.StartsWith($q + '\')) {
            throw "$Name points to a live or default path ($f); use a fresh folder for this test"
        }
    }
}

# Testwerte nur aus ASCII-Zeichen im Skript (Datei ist ASCII mit BOM).
$uuml = [char]0x00FC; $auml = [char]0x00E4; $ouml = [char]0x00F6; $szlig = [char]0x00DF
$ValueAscii = 'PostKauz-Test-123'
$ValueUmlaut = 'Pr' + $uuml + 'fung-' + $auml + $ouml + $uuml + '-' + $szlig + '7'

$Steps = @(
    @{ Id = 'type-ascii'; Value = $ValueAscii; Paste = $false; How = "Tippe  $ValueAscii  und druecke Enter." },
    @{ Id = 'paste-ctrlv-ascii'; Value = $ValueAscii; Paste = $true; How = 'Fuege mit Strg+V ein und druecke Enter.' },
    @{ Id = 'paste-rightclick-ascii'; Value = $ValueAscii; Paste = $true; How = 'Fuege mit RECHTSKLICK ins Fenster ein und druecke Enter.' },
    @{ Id = 'type-umlaut'; Value = $ValueUmlaut; Paste = $false; How = "Tippe  $ValueUmlaut  und druecke Enter." },
    @{ Id = 'paste-ctrlv-umlaut'; Value = $ValueUmlaut; Paste = $true; How = 'Fuege mit Strg+V ein und druecke Enter.' },
    @{ Id = 'paste-rightclick-umlaut'; Value = $ValueUmlaut; Paste = $true; How = 'Fuege mit RECHTSKLICK ins Fenster ein und druecke Enter.' }
)

# ---------------------------------------------------------------- Vorpruefung

# try umfasst Vorpruefung und Messung: Ein Fenster der Matrix (-DoneFile)
# meldet sein Ende auch nach einem Abbruch, und der Fehler bleibt lesbar,
# bis Enter das Fenster schliesst.
$ownCode = 1
try {

if (-not (Test-Path -LiteralPath $Exe -PathType Leaf)) { throw "postkauz.exe not found: $Exe" }
$Exe = (Resolve-Path -LiteralPath $Exe).Path
Assert-NotLive $DataDir '-DataDir'
Assert-NotLive $Config '-Config'
if (Test-Path -LiteralPath $DataDir) {
    if (@(Get-ChildItem -LiteralPath $DataDir -Force).Count -gt 0) { throw "-DataDir must be empty: $DataDir" }
} else {
    New-Item -ItemType Directory -Path $DataDir | Out-Null
}
if (-not (Test-Path -LiteralPath $Config)) { [System.IO.File]::WriteAllText($Config, "", $script:Utf8) }
$DataDir = (Resolve-Path -LiteralPath $DataDir).Path
if ($DataDir.Length -gt 3) { $DataDir = $DataDir.TrimEnd('\') }
$Config = (Resolve-Path -LiteralPath $Config).Path
# Absolut, damit weitere Fenster (Windows Terminal startet im Profilordner)
# in dieselbe Datei schreiben.
$Protocol = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Protocol)
$protoDir = Split-Path -Parent $Protocol
if ($protoDir -and -not (Test-Path -LiteralPath $protoDir)) { New-Item -ItemType Directory -Path $protoDir | Out-Null }

# ---------------------------------------------------------------- Messung

$psv = $PSVersionTable.PSVersion.ToString()
$wtSession = [bool]$env:WT_SESSION
Write-Log ''
Write-Log ("== A3 {0}  {1}  PowerShell {2}  via {3}  Windows Terminal: {4}  {5}" -f $HostLabel, (Get-Date -Format 's'), $psv, $Via, $wtSession, $Exe)
$ver = 'unknown'
try { $ver = & $Exe version --data-dir $DataDir --config $Config | Select-Object -First 1 } catch { }
Write-Log ("   version: {0}" -f $ver)

foreach ($reader in $Readers) {
    foreach ($s in $Steps) {
        $expected = Get-Sha256Hex $s.Value
        Write-Host ''
        Write-Host ("--- {0} / Leser {1} / Schritt {2}" -f $HostLabel, $reader, $s.Id) -ForegroundColor Cyan
        if ($s.Paste) {
            Set-Clipboard -Value $s.Value
            Write-Host 'Der Testwert liegt in der Zwischenablage.'
        }
        Write-Host ("Erst Enter druecken. Erscheint dann 'Type or paste the test value': {0} (Es erscheint nichts beim Tippen. Geht nichts: 60 s warten.)" -f $s.How) -ForegroundColor Yellow
        [void](Read-Host 'Bereit? Enter startet die Abfrage')
        $probeArgs = @('internal', 'prompt-probe', '--json', '--reader', $reader, '--timeout', '60s', '--expect-sha256', $expected, '--data-dir', $DataDir, '--config', $Config)
        if ($Via -eq 'cmd') {
            # Ueber cmd.exe: im Ordner des Programms, damit der erste Teil
            # der Befehlszeile keine Anfuehrungszeichen braucht.
            Push-Location (Split-Path -Parent $Exe)
            try { $out = & cmd.exe /d /c (Split-Path -Leaf $Exe) @probeArgs } finally { Pop-Location }
        } else {
            $out = & $Exe @probeArgs
        }
        $code = $LASTEXITCODE
        if ($s.Paste) { Set-Clipboard -Value ' ' }
        $p = $null
        try { $p = ($out | Out-String) | ConvertFrom-Json } catch { }
        $why = @()
        if ($null -eq $p) {
            $why += 'no JSON output'
        } else {
            $r = $p.report
            if ($code -ne 0) { $why += "exit $code" }
            if ($p.error) { $why += "error: $($p.error)" }
            if ($p.hash_match -ne $true) { $why += 'hash differs from the test value' }
            if ($r.mode_before -and $r.mode_after -and $r.mode_before -ne $r.mode_after) { $why += 'console mode not restored' }
        }
        $verdict = if ($why.Count -eq 0) { 'OK' } else { 'FAIL' }
        if ($verdict -eq 'OK') { $script:Ok[$reader]++ } else { $script:Fail[$reader]++ }
        $tag = if ($reader -eq 'console') { 'Erwartet: OK' } else { 'Messung' }
        Write-Log ("{0} {1} {2} {3}  ({4})" -f $verdict, $HostLabel, $reader, $s.Id, $tag)
        if ($null -ne $p) {
            $e = $p.env; $r = $p.report
            Write-Log ("     stdin {0}, terminal {1}, code pages in/out/ansi {2}/{3}/{4}, windows terminal {5}" -f $e.stdin_type, $r.is_terminal, $e.input_code_page, $e.output_code_page, $e.ansi_code_page, $e.windows_terminal)
            Write-Log ("     mode before {0}; during {1}; after {2}" -f $r.mode_before, $r.mode_during, $r.mode_after)
            Write-Log ("     reads {0}, utf16 {1}, bytes {2}, chars {3}, non-ascii {4}, invalid {5}, escape {6}, control {7}, line end {8}, {9}" -f $r.reads, $r.utf16_units, $r.bytes, $r.chars, $r.non_ascii, $r.invalid, $r.escapes, $r.controls, $r.terminator, $r.duration)
            Write-Log ("     expected {0} bytes, hash match {1}" -f $script:Utf8.GetByteCount($s.Value), ($p.hash_match -eq $true))
        }
        if ($why.Count -gt 0) { Write-Log ("     why: {0}" -f ($why -join '; ')) }
    }
}

# Nichts im Datenverzeichnis entstanden (prompt-probe oeffnet es nicht).
$left = @(Get-ChildItem -LiteralPath $DataDir -Force)
if ($left.Count -gt 0) {
    $script:Fail['console']++
    Write-Log ("FAIL {0} data-dir untouched  ({1} entries)" -f $HostLabel, $left.Count)
} else {
    Write-Log ("OK {0} data-dir untouched" -f $HostLabel)
    $script:Ok['console']++
}
Write-Log ("A3 {0}: {1} OK, {2} FAIL (console), term: {3} OK, {4} FAIL" -f $HostLabel, $script:Ok['console'], $script:Fail['console'], $script:Ok['term'], $script:Fail['term'])
$ownCode = if ($script:Fail['console'] -gt 0) { 1 } else { 0 }
} catch {
    Write-Host ("Abbruch: {0}" -f $_) -ForegroundColor Red
    try { Write-Log ("FAIL {0} aborted: {1}" -f $HostLabel, $_) } catch { }
    throw
} finally {
    if ($DoneFile) {
        # Erst unter anderem Namen, dann umbenennen: Das Startfenster sieht
        # die Datei nie halb geschrieben.
        try {
            [System.IO.File]::WriteAllText("$DoneFile.tmp", "$ownCode", $script:Utf8)
            [System.IO.File]::Move("$DoneFile.tmp", $DoneFile)
        } catch { }
        [void](Read-Host 'Fertig. Enter schliesst dieses Fenster')
    }
}
if ($DoneFile) { exit $ownCode }

# ---------------------------------------------------------------- Matrix

# Die weiteren Fenster nacheinander: das naechste erst, wenn das vorige
# fertig ist (gemeinsame Zwischenablage, gemeinsames Protokoll).
$matrixFail = 0
if ($Launch) {
    $self = $PSCommandPath
    $common = "-NoProfile -ExecutionPolicy Bypass -File $(Format-Arg $self) -Exe $(Format-Arg $Exe) -DataDir $(Format-Arg $DataDir) -Config $(Format-Arg $Config) -Protocol $(Format-Arg $Protocol)"
    if ($LivePath.Count -gt 0) { $common += " -LivePath $(Format-Arg ($LivePath -join ','))" }
    $ps51 = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $pwsh = (Get-Command pwsh.exe -ErrorAction SilentlyContinue)
    $wt = (Get-Command wt.exe -ErrorAction SilentlyContinue)
    $conhost = Join-Path $env:SystemRoot 'System32\conhost.exe'
    $windows = @()
    if ($wt) {
        $windows += @{ Label = 'WT+cmd'; Term = $wt.Source; Shell = $ps51; Via = 'cmd' }
        if ($pwsh) { $windows += @{ Label = 'WT+PS7'; Term = $wt.Source; Shell = $pwsh.Source; Via = 'direct' } }
    }
    if (Test-Path $conhost) {
        $windows += @{ Label = 'conhost+PS51'; Term = $conhost; Shell = $ps51; Via = 'direct' }
        $windows += @{ Label = 'conhost+cmd'; Term = $conhost; Shell = $ps51; Via = 'cmd' }
        if ($pwsh) { $windows += @{ Label = 'conhost+PS7'; Term = $conhost; Shell = $pwsh.Source; Via = 'direct' } }
    }
    foreach ($w in $windows) {
        Write-Host ''
        Write-Host ("=== Naechstes Fenster: {0}. Es oeffnet sich nach Enter; dort genauso abarbeiten, danach geht es hier weiter." -f $w.Label) -ForegroundColor Cyan
        [void](Read-Host 'Enter oeffnet das Fenster')
        $done = Join-Path ([System.IO.Path]::GetTempPath()) ("postkauz-a3-{0}.done" -f [guid]::NewGuid())
        $args1 = "$common -HostLabel $($w.Label) -Via $($w.Via) -DoneFile $(Format-Arg $done)"
        # Sonst erbten conhost-Fenster WT_SESSION aus diesem Fenster und
        # meldeten falsch "Windows Terminal: True" (Windows Terminal setzt
        # fuer seine Fenster einen eigenen Wert).
        Remove-Item Env:WT_SESSION -ErrorAction SilentlyContinue
        if ($w.Term -eq $conhost) {
            # conhost.exe endet mit der Shell im Fenster: -Wait reicht.
            Start-Process -FilePath $conhost -ArgumentList "`"$($w.Shell)`" $args1" -Wait
        } else {
            # wt.exe kehrt sofort zurueck; das Ende meldet die Fertig-Datei.
            Start-Process -FilePath $w.Term -ArgumentList "-w new `"$($w.Shell)`" $args1"
        }
        if ($w.Term -eq $conhost) {
            # -Wait ist zurueck; fehlt die Datei (Fenster mit X geschlossen),
            # kommt sie nicht mehr.
            Start-Sleep -Seconds 1
            $finished = Test-Path -LiteralPath $done
        } else {
            $finished = Wait-DoneFile -File $done -Label $w.Label
        }
        $code = $null
        if ($finished) {
            try {
                $text = [System.IO.File]::ReadAllText($done).Trim()
                if ($text -match '^\d+$') { $code = [int]$text }
            } catch { }
            Remove-Item -LiteralPath $done -ErrorAction SilentlyContinue
        }
        if ($code -ne 0) {
            $matrixFail++
            $how = if ($null -eq $code) { 'no result (window closed or waiting stopped)' } else { "exit $code" }
            Write-Log ("FAIL {0} window  ({1})" -f $w.Label, $how)
        }
    }
    Write-Log ("A3 matrix: {0} windows, {1} with FAIL (console), started from {2}" -f ($windows.Count + 1), ($matrixFail + $ownCode), $HostLabel)
}
if ($ownCode -ne 0 -or $matrixFail -gt 0) { exit 1 }
exit 0
