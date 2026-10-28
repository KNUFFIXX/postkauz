<#
.SYNOPSIS
    Abnahme D7a (PostKauz, Schnitt 3, Teil T2): Tags, Markieren,
    Verschieben, Papierkorb mit Freigabe; offline und (nur mit Zustimmung)
    am echten Konto.

.DESCRIPTION
    Gehoert zu docs/abnahme_D7a.md und auftraege/A03_teile.md, Teil T2,
    Haltepunkt (D7 Schritte 1 bis 3 und 6 aus auftraege/A03_s3.md, Teil D).
    Laeuft auf Windows mit Windows PowerShell 5.1, ohne Go, ohne Docker,
    nur mit dem Programm aus dist/ (-ExePath).

    Teil A (immer, offline, ohne Mailserver): frisches Datenverzeichnis
    -DataDir mit Konfiguration -Config (beide legt das Skript an), Konto
    demo ohne Server, zwei synthetische Mails per import mbox (Adressen nur
    example.org), Schluessel test-agent (reader), test-assistant
    (assistant), test-triage (triage), test-operator (operator). Geprueft:
    tag add/list, search tag:, mark und trash ohne Serverkopie (Ablehnung,
    nichts angelegt), action approve mit Schluessel (human_only), policy
    show je Rolle, Grenzen mit Schluessel (missing_scope,
    confirm_count_required), sync request, MCP-Probe (abnahme_D7a_mcp.py,
    falls Python vorhanden), Audit (eine Zeile je Aufruf, --raw mit Start
    und Abschluss, export, verify).

    Teil B (nur mit -LiveAccount und -AllowRealMailbox): am echten Konto,
    aber nie auf der Live-Datenbank, sondern auf -AccountDataDir (eine
    KOPIE der Live-Daten wie in Abnahme T1 oder ein eigenes
    Datenverzeichnis mit diesem Konto) mit -AccountConfig. Jeder Schritt
    fragt vorher mit Read-Host nach "ja"; sonst wird er uebersprungen.
    Nur an einer Mail, die der Betreiber sich selbst geschickt hat (das
    Skript prueft: Absender = Adresse des Kontos oder ein Alias, Mail im
    Posteingang; zeigt den Kopf und laesst bestaetigen). Schritte: taggen,
    als gelesen markieren, in den Label-Ordner -Label verschieben, Label
    wieder entfernen (Gmail: move --from <Label> --role all), Papierkorb
    mit Freigabe. Nichts wird endgueltig geloescht.

    Sicherheitsregeln (A03_teile.md, Rahmen 5): -DataDir und -Config sind
    Pflicht. Das Skript bricht ab, ohne etwas zu aendern, wenn -DataDir,
    -Config, -WorkDir oder -AccountDataDir im Standard-Datenverzeichnis
    (%LOCALAPPDATA%\postkauz), im Standard-Konfigurationsordner
    (%APPDATA%\postkauz) oder in einem mit -LivePath genannten Pfad liegen
    (oder umgekehrt). Zeichenketten allein erkennen Junctions und
    subst-Laufwerke nicht: Das Skript legt deshalb in -DataDir, -WorkDir
    und -AccountDataDir kurz eine Markerdatei an und bricht ab, wenn sie
    im Standard- oder einem Live-Pfad sichtbar ist. Jedes Programm laeuft
    mit --data-dir und --config. Zugangsdaten stehen nie im Skript; Teil B
    nutzt, was im Schluesselbund fuer das Konto der Kopie hinterlegt ist.

    Jede Pruefung ist eine Zeile "OK <Nr> <Beschreibung>",
    "FAIL <Nr> <Beschreibung>" oder "SKIP <Nr> <Grund>", darunter
    eingerueckt die Ist-Ausgabe (gekuerzt; Schluessel, Mailadressen,
    Pfade, der Kontoname aus Teil B und der Label-Name maskiert).
    Letzte Zeile: "D7a: <n> OK, <m> FAIL, <k> SKIP". Exit 0 nur ohne FAIL.

.PARAMETER ExePath
    postkauz.exe des zu pruefenden Stands (aus dist/, umbenannt).

.PARAMETER DataDir
    Teil A: neues, leeres (oder noch nicht vorhandenes) Datenverzeichnis.

.PARAMETER Config
    Teil A: Pfad der Konfigurationsdatei, die das Skript neu anlegt (darf
    noch nicht existieren).

.PARAMETER WorkDir
    Ordner fuer das Protokoll D7a_windows.txt (wird exklusiv neu angelegt;
    ein Wiederholungslauf braucht ein neues -WorkDir oder das Umbenennen
    des alten Protokolls) und den Laufordner lauf-<Datum-Uhrzeit> mit
    Testdaten, Schluesseldateien und dem Audit-Export von Teil A. Kein
    Reparse Point.

.PARAMETER LivePath
    Pfade des Dauerbetriebs (Live-Datenverzeichnis, Live-Konfiguration),
    durch Komma getrennt. Fuer Teil B Pflicht.

.PARAMETER ExpectVersion
    Erwartete Version von -ExePath (Standard 0.3.0-alpha.5).

.PARAMETER Python
    python.exe fuer die MCP-Probe. Ohne Angabe sucht das Skript python.exe
    bzw. py.exe im PATH (nicht den Platzhalter aus dem Microsoft Store);
    ohne Python: SKIP der MCP-Probe mit Grund.

.PARAMETER Probe
    abnahme_D7a_mcp.py. Standard: neben diesem Skript.

.PARAMETER LiveAccount
    Teil B: Name des Kontos in -AccountDataDir (wie in accounts list).

.PARAMETER AllowRealMailbox
    Teil B: ausdrueckliche Erlaubnis, am echten Postfach zu schreiben.

.PARAMETER AccountDataDir
    Teil B: Datenverzeichnis mit dem Konto (Kopie oder eigenes), nie das
    Live-Datenverzeichnis.

.PARAMETER AccountConfig
    Teil B: Konfiguration zu -AccountDataDir (nie die Live-Konfiguration).

.PARAMETER Label
    Teil B: Pfad des Label-Ordners, wie mailbox list ihn zeigt (in Gmail
    vorher von Hand anlegen, z. B. D7a-Test).

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File abnahme_D7a.ps1 `
      -ExePath C:\PK\d7a\postkauz.exe -DataDir C:\PK\d7a\daten -Config C:\PK\d7a\config.toml `
      -WorkDir C:\PK\d7a -LivePath C:\PostKauz\data,C:\PostKauz\config.toml
#>
param(
    [Parameter(Mandatory = $true)] [string] $ExePath,
    [Parameter(Mandatory = $true)] [string] $DataDir,
    [Parameter(Mandatory = $true)] [string] $Config,
    [Parameter(Mandatory = $true)] [string] $WorkDir,
    [string[]] $LivePath = @(),
    [string] $ExpectVersion = '0.3.0-alpha.5',
    [string] $Python = '',
    [string] $Probe = '',
    [string] $LiveAccount = '',
    [switch] $AllowRealMailbox,
    [string] $AccountDataDir = '',
    [string] $AccountConfig = '',
    [string] $Label = ''
)

$ErrorActionPreference = 'Stop'
$script:Utf8 = New-Object System.Text.UTF8Encoding $false
$script:NOk = 0
$script:NFail = 0
$script:NSkip = 0
$script:Log = $null
$script:Fails = New-Object System.Collections.Generic.List[string]
try { [Console]::OutputEncoding = $script:Utf8 } catch { }

# ---------------------------------------------------------------- Hilfen

# Platzhalter fuer Pfade: laengste zuerst; je Pfad auch in der Schreibweise
# von JSON (doppelte Backslashes) und mit Schraegstrichen.
$script:PathMask = New-Object System.Collections.Generic.List[object]
function Add-PathMask {
    param([string] $Path, [string] $Name)
    if (-not $Path) { return }
    $full = $Path
    try { $full = [System.IO.Path]::GetFullPath($Path).TrimEnd('\') } catch { }
    foreach ($v in @($full, $full.Replace('\', '\\'), $full.Replace('\', '/'))) {
        $script:PathMask.Add((New-Object PSObject -Property @{ Text = $v; Name = $Name }))
    }
    $sorted = @($script:PathMask | Sort-Object -Property @{ Expression = { $_.Text.Length }; Descending = $true })
    $script:PathMask = New-Object System.Collections.Generic.List[object]
    foreach ($x in $sorted) { $script:PathMask.Add($x) }
}

# Schluessel, Mailadressen, Pfade, Label und Kontoname nie ins Protokoll.
function Hide-Private {
    param([string] $Text)
    if ($null -eq $Text) { return '' }
    $t = [regex]::Replace($Text, 'pk_key_[A-Za-z0-9_-]+', 'pk_key_********')
    $t = [regex]::Replace($t, '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}', '<adresse>')
    foreach ($m in $script:PathMask) {
        $t = [regex]::Replace($t, [regex]::Escape($m.Text), $m.Name, 'IgnoreCase')
    }
    if ($Label) { $t = [regex]::Replace($t, [regex]::Escape($Label), '<Label>', 'IgnoreCase') }
    if ($LiveAccount) { $t = [regex]::Replace($t, '(?<![A-Za-z0-9-])' + [regex]::Escape($LiveAccount) + '(?![A-Za-z0-9-])', 'konto1') }
    return $t
}

function Write-Log {
    param([string] $Text)
    $t = Hide-Private $Text
    if ($script:Log) { [System.IO.File]::AppendAllText($script:Log, $t + "`r`n", $script:Utf8) }
    try { Write-Host $t } catch { }
}

function Limit-Text {
    param([string] $Text, [int] $MaxLines = 20, [int] $MaxChars = 1800)
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
        foreach ($l in ((Limit-Text (Hide-Private $Detail)) -split "`r?`n")) { Write-Log ('      ' + $l) }
    }
}

function Add-Skip {
    param([string] $Nr, [string] $Why)
    $script:NSkip++
    Write-Log ('SKIP ' + $Nr + ' ' + $Why)
}

function Stop-Run {
    param([string] $Why)
    if ($script:Log) {
        Write-Log ('ABBRUCH: ' + $Why)
        Write-Log ('D7a: ' + $script:NOk + ' OK, ' + ($script:NFail + 1) + ' FAIL, ' + $script:NSkip + ' SKIP')
    } else {
        # Noch kein Protokoll (Pruefung der Pfade): nur auf den Bildschirm.
        try { Write-Host ('ABBRUCH: ' + $Why) } catch { }
    }
    exit 2
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

# Startet ein Programm ohne Fenster, liest stdout/stderr als UTF-8.
function Invoke-Native {
    param([string] $File, [string[]] $ArgList, [hashtable] $EnvExtra = @{}, [int] $TimeoutSec = 300)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $File
    $psi.Arguments = ConvertTo-ArgString $ArgList
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.StandardOutputEncoding = $script:Utf8
    $psi.StandardErrorEncoding = $script:Utf8
    $psi.WorkingDirectory = $script:Work
    # Nichts aus der Umgebung darf auf das Live-Verzeichnis oder einen
    # Schluessel zeigen.
    foreach ($k in @('POSTKAUZ_KEY', 'POSTKAUZ_KEY_FILE', 'POSTKAUZ_DATA_DIR', 'POSTKAUZ_CONFIG', 'POSTKAUZ_POLICY', 'POSTKAUZ_PROFILE', 'POSTKAUZ_PASSWORD_DEMO')) {
        if ($psi.EnvironmentVariables.ContainsKey($k)) { $psi.EnvironmentVariables.Remove($k) }
    }
    foreach ($k in $EnvExtra.Keys) { $psi.EnvironmentVariables[$k] = [string] $EnvExtra[$k] }
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $p = [System.Diagnostics.Process]::Start($psi)
    $p.StandardInput.Close()
    $to = $p.StandardOutput.ReadToEndAsync()
    $te = $p.StandardError.ReadToEndAsync()
    $timedOut = $false
    if (-not $p.WaitForExit($TimeoutSec * 1000)) {
        $timedOut = $true
        try { $p.Kill() } catch { }
    }
    $p.WaitForExit()
    $sw.Stop()
    [void] $to.Wait(10000)
    [void] $te.Wait(10000)
    $code = -1
    if (-not $timedOut) { $code = $p.ExitCode }
    return New-Object PSObject -Property @{ Exit = $code; Out = [string] $to.Result; Err = [string] $te.Result; TimedOut = $timedOut; Ms = $sw.ElapsedMilliseconds }
}

# postkauz.exe mit Datenverzeichnis und Konfiguration (Teil A oder B).
function Invoke-PkIn {
    param([string] $Data, [string] $Cfg, [string[]] $A, [string] $Shown = '', [int] $TimeoutSec = 300)
    $all = @('--data-dir', $Data, '--config', $Cfg) + $A
    $r = Invoke-Native $script:ExeFull $all @{} $TimeoutSec
    if (-not $Shown) { $Shown = ($A -join ' ') }
    $text = '> postkauz ' + $Shown + "`n" + $r.Out
    if ($r.Err) { $text = $text.TrimEnd() + "`n[stderr] " + $r.Err }
    if ($r.TimedOut) { $text = $text.TrimEnd() + "`n[Zeitgrenze erreicht, Prozess beendet]" }
    $text = $text.TrimEnd() + "`n[exit " + $r.Exit + ', ' + $r.Ms + ' ms]'
    $r | Add-Member -NotePropertyName Text -NotePropertyValue $text
    $r | Add-Member -NotePropertyName All -NotePropertyValue ($r.Out + "`n" + $r.Err)
    return $r
}

function Invoke-Pk {
    param([string[]] $A, [string] $Shown = '')
    return Invoke-PkIn $script:Data $script:Cfg $A $Shown
}

function Invoke-PkB {
    param([string[]] $A, [string] $Shown = '', [int] $TimeoutSec = 300)
    return Invoke-PkIn $script:BData $script:BCfg $A $Shown $TimeoutSec
}

function Test-Has {
    param([string] $Text, [string] $Pattern)
    if ($null -eq $Text) { return $false }
    return [regex]::IsMatch($Text, $Pattern)
}

function Get-Json {
    param([string] $Text)
    if ($null -eq $Text) { return $null }
    try { return ($Text.Trim() | ConvertFrom-Json) } catch { return $null }
}

# NDJSON (audit, search): eine Liste von Objekten.
function Get-JsonLines {
    param([string] $Text)
    $list = New-Object System.Collections.Generic.List[object]
    if ($null -eq $Text) { return $list }
    foreach ($line in ($Text -split "`r?`n")) {
        $t = $line.Trim()
        if (-not $t.StartsWith('{')) { continue }
        $o = Get-Json $t
        if ($null -ne $o) { $list.Add($o) }
    }
    return $list
}

function Get-FullPath {
    param([string] $P)
    return [System.IO.Path]::GetFullPath($P).TrimEnd('\')
}

# a liegt in b oder b in a (oder beide gleich), ohne Gross-/Kleinschreibung.
function Test-Overlap {
    param([string] $A, [string] $B)
    if (-not $A -or -not $B) { return $false }
    $x = (Get-FullPath $A).ToLowerInvariant() + '\'
    $y = (Get-FullPath $B).ToLowerInvariant() + '\'
    return $x.StartsWith($y) -or $y.StartsWith($x)
}

# Markerdatei: in Dir anlegen und pruefen, ob sie in einem der verbotenen
# Verzeichnisse auftaucht (Junction, subst, Netzlaufwerk auf denselben
# Ordner). Liefert '' oder den Ort, an dem sie sichtbar war.
function Test-Marker {
    param([string] $Dir, [string[]] $Forbidden)
    if (-not (Test-Path -LiteralPath $Dir)) { [void] [System.IO.Directory]::CreateDirectory($Dir) }
    $name = '.pk-abnahme-marker-' + [guid]::NewGuid().ToString('N')
    $marker = Join-Path $Dir $name
    [System.IO.File]::WriteAllText($marker, 'Abnahme D7a: Marker, darf geloescht werden', $script:Utf8)
    $seen = ''
    try {
        foreach ($f in $Forbidden) {
            if (-not $f -or -not (Test-Path -LiteralPath $f -PathType Container)) { continue }
            $hit = @(Get-ChildItem -LiteralPath $f -Recurse -Force -Filter $name -ErrorAction SilentlyContinue | Select-Object -First 1)
            if ($hit.Count -gt 0) { $seen = $f; break }
        }
    } finally {
        try {
            Remove-Item -LiteralPath $marker -Force -ErrorAction Stop
        } catch {
            Stop-Run ('Markerdatei liess sich nicht loeschen, bitte von Hand entfernen: ' + $marker)
        }
    }
    return $seen
}

# Sucht python.exe bzw. py.exe (nicht den Store-Platzhalter in WindowsApps).
function Find-Python {
    foreach ($n in @('python.exe', 'py.exe')) {
        $c = @(Get-Command $n -CommandType Application -ErrorAction SilentlyContinue | Where-Object { $_.Path -notmatch '\\WindowsApps\\' } | Select-Object -First 1)
        if ($c.Count -gt 0) { return $c[0].Path }
    }
    return ''
}

# Zustimmung des Betreibers (Teil B): nur "ja" zaehlt.
function Confirm-Step {
    param([string] $Nr, [string] $Text)
    Write-Log ''
    Write-Log ('[Du] ' + $Nr + ' ' + $Text)
    $a = ''
    try { $a = [string] (Read-Host '      Zustimmung: "ja" eingeben (alles andere ueberspringt den Schritt)') } catch { $a = '' }
    $yes = ($a.Trim().ToLowerInvariant() -eq 'ja')
    if ($yes) { Write-Log '      Antwort: ja' } else { Write-Log '      Antwort: nicht ja, Schritt uebersprungen' }
    return $yes
}

# Beobachtung des Betreibers im Gmail-Fenster (Teil B): ja, nein oder leer.
function Read-Observation {
    param([string] $Nr, [string] $Question)
    Write-Log ('[Du] ' + $Nr + ' ' + $Question)
    $a = ''
    try { $a = [string] (Read-Host '      Antwort "ja" oder "nein" (leer: nicht geprueft)') } catch { $a = '' }
    $a = $a.Trim().ToLowerInvariant()
    if ($a -eq 'ja') { Add-Check $Nr ('Betreiber sieht im Postfach: ' + $Question + ' -> ja') $true }
    elseif ($a -eq 'nein') { Add-Check $Nr ('Betreiber sieht im Postfach: ' + $Question + ' -> nein') $false }
    else { Add-Skip $Nr ('Betreiber hat nicht im Postfach nachgesehen: ' + $Question) }
}

# ---------------------------------------------------------------- Pfade pruefen

$script:ExeFull = Get-FullPath $ExePath
$script:Data = Get-FullPath $DataDir
$script:Cfg = Get-FullPath $Config
$script:Work = Get-FullPath $WorkDir

$forbidden = New-Object System.Collections.Generic.List[string]
if ($env:LOCALAPPDATA) { $forbidden.Add((Join-Path $env:LOCALAPPDATA 'postkauz')); $forbidden.Add((Join-Path $env:LOCALAPPDATA 'Packages')) }
if ($env:APPDATA) { $forbidden.Add((Join-Path $env:APPDATA 'postkauz')) }
# Mit -File kommt "-LivePath a,b" als ein Text an: selbst trennen.
$livePaths = New-Object System.Collections.Generic.List[string]
foreach ($l in ($LivePath | ForEach-Object { $_ -split ',' })) {
    if ($l.Trim()) { $livePaths.Add((Get-FullPath $l.Trim())); $forbidden.Add((Get-FullPath $l.Trim())) }
}
# Marker nur in Ordnern suchen (Live-Datenverzeichnis, Standardordner);
# Packages ist riesig und nur fuer den Textvergleich (MSIX-Umleitung) da.
$markerPlaces = New-Object System.Collections.Generic.List[string]
foreach ($f in $forbidden) {
    if ((Test-Path -LiteralPath $f -PathType Container) -and -not $f.EndsWith('\Packages')) { $markerPlaces.Add($f) }
}

foreach ($f in $forbidden) {
    if (Test-Overlap $script:Data $f) { Stop-Run ('-DataDir liegt im Standard- oder Live-Pfad ' + $f + ' (oder umgekehrt); nichts veraendert.') }
    if (Test-Overlap $script:Cfg $f) { Stop-Run ('-Config liegt im Standard- oder Live-Pfad ' + $f + '; nichts veraendert.') }
    if (Test-Overlap $script:Work $f) { Stop-Run ('-WorkDir liegt im Standard- oder Live-Pfad ' + $f + ' (oder umgekehrt); nichts veraendert.') }
}
if ((Test-Path -LiteralPath $script:Data) -and @(Get-ChildItem -LiteralPath $script:Data -Force).Count -gt 0) {
    Stop-Run ('-DataDir ist nicht leer; Teil A braucht ein frisches Datenverzeichnis.')
}
if (Test-Path -LiteralPath $script:Cfg) { Stop-Run '-Config gibt es schon; Teil A legt die Konfiguration selbst an (neuen Pfad waehlen).' }
if ($script:Cfg.Contains("'") -or $script:Work.Contains("'")) { Stop-Run '-Config und -WorkDir duerfen kein einfaches Anfuehrungszeichen enthalten (TOML).' }
if (-not (Test-Path -LiteralPath $script:ExeFull -PathType Leaf)) { Stop-Run ('Programm nicht gefunden: ' + $script:ExeFull) }

# Teil B: alle Angaben vollstaendig, sonst SKIP (nicht Abbruch).
$script:PartB = $false
$script:PartBWhy = 'nur mit -LiveAccount <konto> und -AllowRealMailbox (dazu -AccountDataDir, -AccountConfig, -Label, -LivePath)'
if ($LiveAccount -or $AllowRealMailbox) {
    if (-not ($LiveAccount -and $AllowRealMailbox)) {
        $script:PartBWhy = '-LiveAccount und -AllowRealMailbox gehoeren zusammen'
    } elseif (-not $AccountDataDir -or -not $AccountConfig -or -not $Label) {
        $script:PartBWhy = 'Teil B braucht -AccountDataDir, -AccountConfig und -Label'
    } elseif ($livePaths.Count -eq 0) {
        $script:PartBWhy = 'Teil B braucht -LivePath (Live-Datenverzeichnis und Live-Konfiguration), damit der Schutz greift'
    } else {
        $script:BData = Get-FullPath $AccountDataDir
        $script:BCfg = Get-FullPath $AccountConfig
        foreach ($f in $forbidden) {
            if (Test-Overlap $script:BData $f) { Stop-Run ('-AccountDataDir liegt im Standard- oder Live-Pfad ' + $f + ' (oder umgekehrt); nichts veraendert.') }
            if (Test-Overlap $script:BCfg $f) { Stop-Run ('-AccountConfig ist die Standard- oder Live-Konfiguration bzw. liegt in ' + $f + '; nichts veraendert.') }
        }
        if (Test-Overlap $script:BData $script:Data) { Stop-Run '-AccountDataDir und -DataDir ueberschneiden sich.' }
        if (-not (Test-Path -LiteralPath (Join-Path $script:BData 'postkauz.db') -PathType Leaf)) { Stop-Run '-AccountDataDir enthaelt keine postkauz.db.' }
        if (-not (Test-Path -LiteralPath $script:BCfg -PathType Leaf)) { Stop-Run '-AccountConfig gibt es nicht.' }
        $script:PartB = $true
    }
}

# Zeichenketten erkennen Junction und subst nicht: Markerdatei.
$markerDirs = @($script:Data, $script:Work)
if ($script:PartB) { $markerDirs += $script:BData }
foreach ($d in $markerDirs) {
    $where = Test-Marker $d $markerPlaces
    if ($where) { Stop-Run ('Eine Markerdatei aus ' + $d + ' ist in ' + $where + ' sichtbar (Junction, subst oder derselbe Ordner).') }
}

# Protokoll erst nach den Pruefungen der Pfade anlegen. -WorkDir darf kein
# Reparse Point sein, und die Protokolldatei wird exklusiv neu angelegt (nie
# geleert oder ueberschrieben: ein Datei-Symlink auf eine Live-Datei wuerde
# sonst diese leeren).
[void] [System.IO.Directory]::CreateDirectory($script:Work)
if ((Get-Item -LiteralPath $script:Work -Force).Attributes -band [System.IO.FileAttributes]::ReparsePoint) {
    Stop-Run ('-WorkDir ist ein Reparse Point (Junction oder Link): ' + $script:Work + '. Bitte ein normales Verzeichnis nehmen.')
}
$script:Log = Join-Path $script:Work 'D7a_windows.txt'
try {
    $fs = New-Object System.IO.FileStream($script:Log, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::Read)
    $fs.Close()
} catch {
    $bad = $script:Log
    $script:Log = $null
    Stop-Run ('Protokolldatei existiert schon (oder ist ein Link): ' + $bad + '. Bitte umbenennen oder ein neues -WorkDir nehmen.')
}
# Testdaten, Schluesseldateien und Audit-Export in einem neuen Laufordner.
$script:Run = Join-Path $script:Work ('lauf-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
if (Test-Path -LiteralPath $script:Run) { Stop-Run ('Laufordner existiert schon: ' + $script:Run) }
foreach ($sub in @('', 'input', 'keys')) { [void] [System.IO.Directory]::CreateDirectory((Join-Path $script:Run $sub)) }
Add-PathMask $script:Data '<DataDir>'
Add-PathMask $script:Cfg '<Config>'
Add-PathMask $script:Work '<WorkDir>'
if ($script:PartB) { Add-PathMask $script:BData '<AccountDataDir>'; Add-PathMask $script:BCfg '<AccountConfig>' }
$i = 0
foreach ($l in $livePaths) { $i++; Add-PathMask $l ('<Live' + $i + '>') }
Add-PathMask (Split-Path -Parent $script:ExeFull) '<ExeDir>'
Add-PathMask $env:USERPROFILE '<USERPROFILE>'

Write-Log ('Abnahme D7a ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + ', PowerShell ' + $PSVersionTable.PSVersion.ToString())
Write-Log ('Programm: ' + $script:ExeFull)
Write-Log ('Teil A: DataDir ' + $script:Data + ', Config ' + $script:Cfg + ', Laufordner ' + $script:Run)
if ($script:PartB) { Write-Log ('Teil B: Konto ' + $LiveAccount + ' in ' + $script:BData + ', Label ' + $Label) } else { Write-Log ('Teil B: nein (' + $script:PartBWhy + ')') }

# ================================================================ Teil A

Write-Log ''
Write-Log '===== Teil A, Schritt 0: Vorbereitung (offline, synthetische Testdaten)'

$det = 'SHA-256 postkauz.exe: ' + (Get-FileHash -LiteralPath $script:ExeFull -Algorithm SHA256).Hash
$r = Invoke-Native $script:ExeFull @('version')
Add-Check 'A0.1' ('version nennt ' + $ExpectVersion) ($r.Exit -eq 0 -and (Test-Has $r.Out ([regex]::Escape($ExpectVersion) + '(?![0-9.])'))) ($r.Out.Trim() + "`n" + $det)

[void] [System.IO.Directory]::CreateDirectory($script:Data)
[void] [System.IO.Directory]::CreateDirectory((Split-Path -Parent $script:Cfg))
# Exklusiv neu anlegen (nie eine vorhandene Datei oder einen Link ueberschreiben).
$cfgBytes = $script:Utf8.GetBytes("# Abnahme D7a, Teil A: nur dieses Datenverzeichnis`r`n[operator]`r`nname = 'D7a-Abnahme'`r`n")
try {
    $fs = New-Object System.IO.FileStream($script:Cfg, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::Read)
    $fs.Write($cfgBytes, 0, $cfgBytes.Length)
    $fs.Close()
} catch {
    Stop-Run ('-Config existiert schon (oder ist ein Link): ' + $script:Cfg + '. Bitte einen neuen Pfad nehmen.')
}

# Zwei synthetische Mails (Adressen nur example.org).
$mbox = @'
From MAILER-DAEMON Mon Oct 19 09:00:00 2026
Message-ID: <d7a.rechnung@corpus.example.org>
Date: Mon, 19 Oct 2026 09:00:00 +0200
From: Muster Handwerk <rechnung@example.org>
To: Demo Buero <demo@example.org>
Subject: Rechnung 2026-117 Heizungswartung
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8

Guten Tag,

anbei die Rechnung fuer die Wartung.

From MAILER-DAEMON Tue Oct 20 10:15:00 2026
Message-ID: <d7a.verein@corpus.example.org>
Date: Tue, 20 Oct 2026 10:15:00 +0200
From: Beispiel Verein <info@example.org>
To: Demo Buero <demo@example.org>
Subject: Vereinsnachrichten Oktober
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8

Hallo,

die Neuigkeiten aus dem Verein.

'@
$mboxFile = Join-Path $script:Run 'input\d7a-korpus.mbox'
[System.IO.File]::WriteAllText($mboxFile, ($mbox -replace "`r`n", "`n"), $script:Utf8)

$r = Invoke-Native $script:ExeFull @('--data-dir', $script:Data, '--config', $script:Cfg, 'account', 'add', 'demo', '--imap', '127.0.0.1:1', '--user', 'demo@example.org', '--insecure-test-server', '--password-from-env') @{ 'POSTKAUZ_PASSWORD_DEMO' = 'unbenutzt-kein-server' }
Add-Check 'A0.2' 'Konto demo ohne Server angelegt (Passwort nur aus der Umgebung, nicht gespeichert)' ($r.Exit -eq 0 -and (Test-Has $r.Out 'account demo added')) ($r.Out + $r.Err)

$r = Invoke-Pk @('import', 'mbox', $mboxFile, '-a', 'demo', '--mailbox', 'INBOX', '--quiet')
Add-Check 'A0.3' 'import mbox: 2 neue Nachrichten' ($r.Exit -eq 0 -and (Test-Has $r.Out 'new: 2, existing: 0, skipped: 0, errors: 0')) $r.Text

$ids = @{}
foreach ($mid in @('d7a.rechnung@corpus.example.org', 'd7a.verein@corpus.example.org')) {
    $r = Invoke-Pk @('show', $mid, '--json')
    $j = Get-Json $r.Out
    if ($null -ne $j -and $null -ne $j.id) { $ids[$mid] = [int] $j.id } else { $ids[$mid] = 0 }
}
$id1 = $ids['d7a.rechnung@corpus.example.org']
$id2 = $ids['d7a.verein@corpus.example.org']
Add-Check 'A0.4' 'lokale IDs der beiden Mails' ($id1 -gt 0 -and $id2 -gt 0 -and $id1 -ne $id2) ('Rechnung: ' + $id1 + ', Verein: ' + $id2)
if ($id1 -le 0) { Stop-Run 'Testdaten fehlen.' }
$s1 = [string] $id1
$s2 = [string] $id2

$keyFiles = @{}
$ok = $true
$det = ''
foreach ($kv in @(@('test-agent', 'reader'), @('test-assistant', 'assistant'), @('test-triage', 'triage'), @('test-operator', 'operator'))) {
    $r = Invoke-Pk @('key', 'create', '--name', $kv[0], '--role', $kv[1], '--expires', '2d')
    $m = [regex]::Match($r.Out, 'pk_key_[A-Za-z0-9_-]+')
    $kf = Join-Path $script:Run ('keys\' + $kv[0] + '.key')
    if ($r.Exit -eq 0 -and $m.Success) {
        [System.IO.File]::WriteAllText($kf, $m.Value, [System.Text.Encoding]::ASCII)
        $keyFiles[$kv[0]] = $kf
        $det += $kv[0] + ' (' + $kv[1] + '): angelegt, Datei ' + $kf + "`n"
    } else {
        $ok = $false
        $det += $r.Text + "`n"
    }
}
$r = Invoke-Pk @('key', 'list')
$ok = $ok -and $r.Exit -eq 0 -and -not (Test-Has $r.Out 'pk_key_') -and (Test-Has $r.Out '(?m)^test-triage\s+triage\s+mail:read,mail:search,mail:draft,mail:tag\s')
Add-Check 'A0.5' 'Schluessel test-agent, test-assistant, test-triage, test-operator angelegt; triage = lesen + Entwurf + Tag; key list zeigt keinen Schluessel' $ok ($det + $r.Text)
if (-not $ok) { Stop-Run 'Schluessel fehlen.' }

$r = Invoke-Pk @('doctor')
$ok = $r.Exit -eq 0 -and (Test-Has $r.Out ('(?m)^data dir:\s+' + [regex]::Escape($script:Data))) -and -not (Test-Has $r.Out '(?m)^\s*really:')
Add-Check 'A0.6' 'doctor: Datenverzeichnis von Teil A (nicht umgeleitet)' $ok (Limit-Text $r.Text 12)

# ------------------------------------------------ Schritt 1: Tag und Markieren
Write-Log ''
Write-Log '===== Teil A, Schritt 1 (D7.1): tag add, search tag:, tag list, mark'
$r = Invoke-Pk @('tag', 'add', 'rechnung', $s1)
Add-Check 'A1.1' ('tag add rechnung ' + $s1 + ' -> "1 tags added", Nachricht mit Tag rechnung') ($r.Exit -eq 0 -and (Test-Has $r.Out '1 tags added, 0 removed in account demo') -and (Test-Has $r.Out ('(?m)^\s+' + $s1 + '\s+rechnung\s*$'))) $r.Text

$r = Invoke-Pk @('search', 'tag:rechnung', '--json')
$found = @(Get-JsonLines $r.Out | ForEach-Object { [string] $_.id })
Add-Check 'A1.2' ('search tag:rechnung -> genau die ID ' + $s1) ($r.Exit -eq 0 -and $found.Count -eq 1 -and $found[0] -eq $s1) ('Treffer: ' + ($found -join ', '))

$r = Invoke-Pk @('tag', 'list')
Add-Check 'A1.3' 'tag list -> rechnung mit 1 Nachricht' ($r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^rechnung\s+1\s*$')) $r.Text

$r = Invoke-Pk @('show', $s1)
Add-Check 'A1.4' ('show ' + $s1+ ' nennt "tags: rechnung"') ($r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^tags:\s+rechnung\s*$')) (Limit-Text $r.Text 14)

$r = Invoke-Pk @('mark', $s1, '--seen')
Add-Check 'A1.5' ('mark ' + $s1 + ' --seen ohne Serverkopie -> denied (no_server_copy), Exit 3 (offline erwartet; "pending" prueft Teil B)') ($r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(no_server_copy\)')) $r.Text
$r = Invoke-Pk @('show', $s1, '--json')
$j = Get-Json $r.Out
$ok = $null -ne $j -and -not (@($j.flags) -contains 'seen') -and ($j.pending_flags -ne $true)
Add-Check 'A1.6' 'danach unveraendert: nicht gelesen, nichts pending' $ok ('flags: ' + (@($j.flags) -join ',') + ', pending_flags: ' + $j.pending_flags)

# ------------------------------------------------ Schritt 2: Papierkorb und Freigabe
Write-Log ''
Write-Log '===== Teil A, Schritt 2 (D7.2): trash und action approve ohne Server'
$r = Invoke-Pk @('trash', $s1)
Add-Check 'A2.1' ('trash ' + $s1 + ' ohne Server -> denied (no_trash_mailbox), Exit 3; kein Antrag (offline erwartet; Antrag und Freigabe prueft Teil B)') ($r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(no_trash_mailbox\)')) $r.Text
$r = Invoke-Pk @('action', 'approve', '999', '--key', 'test-agent')
Add-Check 'A2.2' 'action approve 999 --key test-agent -> denied (human_only), Exit 3 (vor jeder anderen Pruefung)' ($r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(human_only\)')) $r.Text
$r = Invoke-Pk @('action', 'reject', '999', '--key', 'test-operator')
Add-Check 'A2.3' 'action reject 999 --key test-operator -> denied (human_only), Exit 3 (auch die Rolle operator nicht)' ($r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(human_only\)')) $r.Text
$r = Invoke-Pk @('action', 'approve', '999')
# Befund D7a-1 (behoben in T2): Code not_found, ohne Text des Stores.
Add-Check 'A2.4' 'action approve 999 ohne Schluessel -> "error: action not found", Exit 1 (Code not_found)' ($r.Exit -eq 1 -and (Test-Has $r.Err 'error: action not found') -and -not (Test-Has $r.All 'store:')) $r.Text
$r = Invoke-Pk @('action', 'list')
Add-Check 'A2.5' 'action list -> "no actions" (offline entsteht keine Aktion)' ($r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^no actions\s*$')) $r.Text

# ------------------------------------------------ Schritt 3: Policy je Rolle und MCP
Write-Log ''
Write-Log '===== Teil A, Schritt 3 (D7.3): policy show je Rolle, Grenzen mit Schluessel, MCP'
$r = Invoke-Pk @('policy', 'show', '--key', 'test-assistant')
$ok = $r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^\s*actions\.approve: human_only\s*$') -and (Test-Has $r.Out '(?m)^\s*messages\.tag\s+mail:tag\s') -and (Test-Has $r.Out '(?m)^\s*sync\.request\s+mail:sync\s') -and -not (Test-Has $r.Out '(?m)^\s*messages\.(mark|move|trash)\s')
Add-Check 'A3.1' 'policy show --key test-assistant: tag, sync.request ja; messages.mark/move/trash nein (mark braucht mail:mark, V20); actions.approve: human_only' $ok $r.Text
$r = Invoke-Pk @('policy', 'show', '--key', 'test-operator')
$ok = $r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^\s*messages\.trash\s+mail:trash\s+R3\s+needs_approval\s*$') -and (Test-Has $r.Out '(?m)^\s*messages\.move\s+mail:move\s') -and (Test-Has $r.Out '(?m)^\s*messages\.mark\s+mail:mark\s') -and (Test-Has $r.Out '(?m)^\s*actions\.approve: human_only\s*$')
Add-Check 'A3.2' 'policy show --key test-operator: messages.trash R3 needs_approval, messages.move, messages.mark (mail:mark), actions.approve: human_only' $ok $r.Text
$r = Invoke-Pk @('policy', 'show', '--key', 'test-triage')
$ok = $r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^scopes:\s+mail:draft, mail:read, mail:search, mail:tag\s*$') -and (Test-Has $r.Out '(?m)^\s*messages\.tag\s') -and (Test-Has $r.Out '(?m)^\s*drafts\.create\s') -and -not (Test-Has $r.Out '(?m)^\s*(messages\.move|messages\.trash|sync\.request|sync\.run)\s')
Add-Check 'A3.3' 'policy show --key test-triage (Nacht-Triage): lesen + Entwurf + Tag, kein move/trash/sync' $ok $r.Text

$r = Invoke-Pk @('move', $s1, '--role', 'archive', '--key', 'test-triage')
$r2 = Invoke-Pk @('trash', $s1, '--key', 'test-triage')
Add-Check 'A3.4' 'move und trash mit --key test-triage -> denied (missing_scope), Exit 3' ($r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(missing_scope\)') -and $r2.Exit -eq 3 -and (Test-Has $r2.Err 'denied \(missing_scope\)')) ($r.Text + "`n" + $r2.Text)
$r = Invoke-Pk @('tag', 'add', 'rechnung', $s2, '--key', 'test-triage')
Add-Check 'A3.5' 'tag add mit --key test-triage -> erlaubt, als Trockenlauf ("dry run:"), nichts gespeichert' ($r.Exit -eq 0 -and (Test-Has $r.Out '^dry run: 1 tags added')) $r.Text
$many = (1..51 | ForEach-Object { [string] $_ }) -join ','
$r = Invoke-Pk @('trash', $many, '--key', 'test-operator') 'trash 1,2,...,51 --key test-operator'
Add-Check 'A3.6' 'trash mit 51 IDs, --key test-operator, ohne --confirm-count -> denied (confirm_count_required), Exit 3' ($r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(confirm_count_required\)')) $r.Text
$r = Invoke-Pk @('sync', 'request', '-a', 'demo', '--key', 'test-triage')
$r2 = Invoke-Pk @('sync', 'request', '-a', 'demo', '--key', 'test-assistant')
Add-Check 'A3.7' 'sync request: triage -> denied (missing_scope); assistant -> Trockenlauf "would queue a sync"' ($r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(missing_scope\)') -and $r2.Exit -eq 0 -and (Test-Has $r2.Out 'dry run: would queue a sync of account demo')) ($r.Text + "`n" + $r2.Text)
$r = Invoke-Pk @('sync', 'request', '-a', 'demo')
Add-Check 'A3.8' 'sync request -a demo (Mensch) -> "queued as job <n>" (bleibt ohne Server liegen)' ($r.Exit -eq 0 -and (Test-Has $r.Out 'sync of account demo \(all mailboxes\) queued as job \d+')) $r.Text

# MCP-Probe (Python).
if (-not $Probe) { $Probe = Join-Path $PSScriptRoot 'abnahme_D7a_mcp.py' }
if (-not $Python) { $Python = Find-Python }
if (-not (Test-Path -LiteralPath $Probe -PathType Leaf)) {
    Add-Skip 'A3.9' ('MCP-Probe: abnahme_D7a_mcp.py fehlt (' + $Probe + ')')
} elseif (-not $Python -or -not (Test-Path -LiteralPath $Python -PathType Leaf)) {
    Add-Skip 'A3.9' 'MCP-Probe: kein Python gefunden (Parameter -Python angeben, um sie auszufuehren)'
} else {
    $pyArgs = @()
    if ((Split-Path -Leaf $Python) -ieq 'py.exe') { $pyArgs += '-3' }
    $pyArgs += @('-I', $Probe, '--exe', $script:ExeFull, '--data-dir', $script:Data, '--config', $script:Cfg,
        '--assistant-key-file', $keyFiles['test-assistant'], '--operator-key-file', $keyFiles['test-operator'])
    Write-Log ('      Python: ' + $Python + ', Probe SHA-256 ' + (Get-FileHash -LiteralPath $Probe -Algorithm SHA256).Hash)
    $pr = Invoke-Native $Python $pyArgs @{} 300
    $seen = 0
    foreach ($line in ($pr.Out -split "`r?`n")) {
        if ($line -match '^(OK|FAIL) (\S+) (.*)$') {
            $seen++
            if ($Matches[1] -eq 'OK') { $script:NOk++ } else { $script:NFail++; $script:Fails.Add($Matches[2] + ' ' + $Matches[3]) }
            Write-Log $line
        } elseif ($line -match '^\s+\S') {
            Write-Log $line
        }
    }
    if ($pr.Err.Trim()) { Write-Log ('      [stderr der Probe] ' + (Limit-Text $pr.Err 8 800)) }
    Add-Check 'A3.9' 'MCP-Probe lief vollstaendig (3 Pruefungen A3.10 bis A3.12, Exit 0)' ($pr.Exit -eq 0 -and $seen -eq 3) ('exit ' + $pr.Exit + ', Pruefungen ' + $seen)
}

# ------------------------------------------------ Schritt 6: Audit
Write-Log ''
Write-Log '===== Teil A, Schritt 6 (D7.6): audit, audit --raw, audit export, audit verify'
$r = Invoke-Pk @('audit', '--since', '10m', '--limit', '1000')
Add-Check 'A6.1' 'audit --since 10m laeuft (Exit 0) und zeigt die Aufrufe dieses Laufs' ($r.Exit -eq 0 -and (Test-Has $r.Out 'messages\.tag') -and (Test-Has $r.Out 'denied \(human_only\)')) (Limit-Text $r.Text 30 3000)

$r = Invoke-Pk @('audit', '--since', '10m', '--limit', '1000', '--json')
$ev = @(Get-JsonLines $r.Out)
$open = @($ev | Where-Object { $_.result -eq 'started' -and $_.operation -ne 'audit.list' })
$ended = @($ev | Where-Object { $null -ne $_.end_id -and [int] $_.end_id -gt 0 })
$tagOk = @($ev | Where-Object { $_.operation -eq 'messages.tag' -and $_.result -eq 'ok' })
$deniedHo = @($ev | Where-Object { $_.operation -eq 'actions.approve' -and $_.result -eq 'denied' -and $_.reason -eq 'human_only' })
$ok = $r.Exit -eq 0 -and $ev.Count -ge 20 -and $open.Count -eq 0 -and $ended.Count -ge 10 -and $tagOk.Count -ge 1 -and $deniedHo.Count -ge 1
Add-Check 'A6.2' 'audit --since 10m --json: je Aufruf eine Zeile mit Endergebnis (kein "started" ausser dem laufenden audit.list)' $ok ('Zeilen: ' + $ev.Count + ', mit Abschluss (end_id): ' + $ended.Count + ', offen: ' + $open.Count + ', messages.tag ok: ' + $tagOk.Count + ', actions.approve denied (human_only): ' + $deniedHo.Count)

$r = Invoke-Pk @('audit', '--since', '10m', '--limit', '1000', '--raw', '--json')
$raw = @(Get-JsonLines $r.Out)
$starts = @($raw | Where-Object { $_.result -eq 'started' })
$ends = @($raw | Where-Object { $null -ne $_.ref_id -and [int] $_.ref_id -gt 0 })
$startIds = @{}
foreach ($e in $starts) { $startIds[[string] $e.id] = $true }
$orphans = @($ends | Where-Object { -not $startIds.ContainsKey([string] $_.ref_id) })
$tagStart = @($starts | Where-Object { $_.operation -eq 'messages.tag' })
$tagEnd = @($ends | Where-Object { $_.operation -eq 'messages.tag' -and $_.result -eq 'ok' })
# Policy-Ablehnung: ein Eintrag ohne Start (human_only kommt vor dem Handler).
$hoStart = @($starts | Where-Object { $_.operation -eq 'actions.approve' -and $_.key -eq 'key:test-agent' })
$ok = $r.Exit -eq 0 -and $starts.Count -ge 10 -and $ends.Count -ge 10 -and $orphans.Count -eq 0 -and $tagStart.Count -ge 1 -and $tagEnd.Count -ge 1 -and $hoStart.Count -eq 0
Add-Check 'A6.3' 'audit --raw: Start "started" und Abschluss mit ref_id je erlaubtem Aufruf; Policy-Ablehnung nur ein Eintrag' $ok ('Starts: ' + $starts.Count + ', Abschluesse: ' + $ends.Count + ', Abschluesse ohne Start: ' + $orphans.Count + ', messages.tag Start/ok: ' + $tagStart.Count + '/' + $tagEnd.Count + ', Start fuer actions.approve mit key:test-agent: ' + $hoStart.Count + ' (0 erwartet)')

$words = @('Heizungswartung', 'Vereinsnachrichten', 'rechnung@example.org', 'info@example.org', 'Neuigkeiten')
$hit = @($words | Where-Object { $r.Out.IndexOf($_, [System.StringComparison]::OrdinalIgnoreCase) -ge 0 })
Add-Check 'A6.4' 'Audit (--raw --json) ohne Mailinhalt (Betreff, Absender, Text)' ($raw.Count -gt 0 -and $hit.Count -eq 0) ('gesucht: ' + ($words -join ', ') + '; Treffer: [' + ($hit -join ', ') + '] (leer = gut)')

$exportDir = Join-Path $script:Run 'audit-export'
$r = Invoke-Pk @('audit', 'export', '--to', $exportDir)
$files = @()
if (Test-Path -LiteralPath $exportDir) { $files = @(Get-ChildItem -LiteralPath $exportDir -File | ForEach-Object { $_.Name }) }
$ok = $r.Exit -eq 0 -and (Test-Has $r.Out 'written') -and ($files -contains 'postkauz-audit-manifest.json') -and @($files | Where-Object { $_ -like 'postkauz-audit-*.jsonl' }).Count -ge 1
Add-Check 'A6.5' 'audit export --to <neuer Ordner> -> Tagesdatei(en) und Manifest geschrieben' $ok ($r.Text + "`nDateien: " + ($files -join ', '))

$r = Invoke-Pk @('audit', 'verify', '--json')
$j = Get-Json $r.Out
Add-Check 'A6.6' 'audit verify -> status ok' ($r.Exit -eq 0 -and $null -ne $j -and $j.status -eq 'ok') $r.Text

# ================================================================ Teil B

Write-Log ''
Write-Log '===== Teil B: am echten Konto (nur mit Zustimmung je Schritt)'
if (-not $script:PartB) {
    Add-Skip 'B' ('Teil B nicht ausgefuehrt: ' + $script:PartBWhy)
} else {
    $acc = $LiveAccount
    Write-Log ('Datenverzeichnis ' + $script:BData + ' (nie das Live-Verzeichnis), Konfiguration ' + $script:BCfg)
    $bOk = $true

    $r = Invoke-PkB @('accounts', 'list', '--json')
    $aj = Get-Json $r.Out
    $own = @()
    $accFound = $false
    if ($null -ne $aj) {
        foreach ($x in @($aj.accounts)) {
            if ([string] $x.name -eq $acc) { $accFound = $true; $own += ([string] $x.email).ToLowerInvariant(); foreach ($al in @($x.aliases)) { if ($al) { $own += ([string] $al).ToLowerInvariant() } } }
        }
    }
    Add-Check 'B0.1' 'Konto in -AccountDataDir vorhanden (accounts list); Datenbank laeuft mit dem neuen Programm (Migration nur an dieser Kopie)' ($r.Exit -eq 0 -and $accFound -and $own.Count -gt 0) ('Exit ' + $r.Exit + ', Konto gefunden: ' + $accFound + ', eigene Adressen: ' + $own.Count + "`n" + $r.Err)
    if (-not $accFound) { $bOk = $false }

    if ($bOk) {
        $r = Invoke-PkB @('doctor', '--json')
        $dj = Get-Json $r.Out
        $oauth = $false
        if ($null -ne $dj) { foreach ($x in @($dj.accounts)) { if ([string] $x.name -eq $acc -and [string] $x.auth -eq 'oauth2') { $oauth = $true } } }
        if ($null -eq $dj) {
            Add-Check 'B0.2' 'doctor --json lesbar' $false $r.Text
            $bOk = $false
        } elseif ($oauth) {
            # Eine Kopie teilt den Verweis im Schluesselbund mit dem Dauerbetrieb;
            # ein Token-Refresh wuerde dort speichern (wie Abnahme T1, Schritt 3).
            Add-Skip 'B' 'Konto mit OAuth2 (Microsoft): Teil B nur fuer Konten mit Passwort (Gmail)'
            $bOk = $false
        } else {
            Add-Check 'B0.2' ('doctor: Datenbank ' + $dj.database + ', Schema ' + $dj.schema_version + ', Konto mit Passwort') ($dj.database -eq 'ok') ''
        }
    }

    if ($bOk) {
        $r = Invoke-PkB @('draft', 'list', '--account', $acc, '--status', 'draft', '--status', 'pending_approval', '--status', 'ready', '--limit', '500', '--json')
        $dl = Get-Json $r.Out
        $waiting = 0
        if ($null -ne $dl) { foreach ($x in @($dl.drafts)) { if ($x.remote -eq 'pending') { $waiting++ } } }
        if ($r.Exit -ne 0 -or $null -eq $dl) {
            Add-Check 'B0.3' 'draft list lesbar' $false $r.Text
            $bOk = $false
        } elseif (@($dl.drafts).Count -ge 500) {
            Add-Skip 'B' 'draft list liefert 500 Entwuerfe (Obergrenze); ob einer auf seine Kopie wartet, ist nicht pruefbar'
            $bOk = $false
        } elseif ($waiting -gt 0) {
            Add-Skip 'B' ([string] $waiting + ' Entwurf/Entwuerfe warten auf ihre Kopie; jeder sync wuerde sie im echten Entwuerfe-Ordner ablegen')
            $bOk = $false
        } else {
            Add-Check 'B0.3' 'kein Entwurf wartet auf seine Kopie (sync legt also nichts im Entwuerfe-Ordner ab)' $true ''
        }
    }

    # V2 (Nachbesserung T2): Bei einer Wiederholung koennen aus dem letzten
    # Lauf Aktionen offen sein (queued/running). Der "lesende" Sync B0.4 fuehrte
    # sie am echten Postfach aus; dann Teil B mit Grund ueberspringen.
    if ($bOk) {
        $r = Invoke-PkB @('action', 'list', '--account', $acc, '--state', 'queued', '--state', 'running', '--limit', '500', '--json')
        $al = Get-Json $r.Out
        if ($r.Exit -ne 0 -or $null -eq $al) {
            Add-Check 'B0.3a' 'action list lesbar' $false $r.Text
            $bOk = $false
        } elseif (@($al.actions).Count -gt 0) {
            $openIds = (@($al.actions) | ForEach-Object { [string] $_.id + ' (' + $_.kind + ', ' + $_.state + ')' }) -join ', '
            Add-Skip 'B' ('offene Aktionen aus einem frueheren Lauf: ' + $openIds + '; jeder sync wuerde sie am echten Postfach ausfuehren. Neue -AccountDataDir-Kopie nehmen oder die Aktionen erst ansehen (action show <n>)')
            $bOk = $false
        } else {
            Add-Check 'B0.3a' 'keine offene Aktion (queued/running): sync fuehrt also nichts am Postfach aus' $true ''
        }
    }

    $boxRole = @{}
    $inboxPath = ''
    $haveAll = $false
    $haveTrash = $false
    $haveLabel = $false
    if ($bOk) {
        if (Confirm-Step 'B0.4' ('Erster Sync der Kopie mit dem echten Konto (liest nur; holt neue Mails, auch deine Testmail): sync -a ' + $acc)) {
            $r = Invoke-PkB @('sync', '-a', $acc) ('sync -a ' + $acc) 1800
            $sum = (@($r.Out -split "`r?`n" | Where-Object { $_ -match 'messages mirrored|^\d+ errors' }) -join '; ')
            Add-Check 'B0.4' ('sync -a <konto> (' + $r.Ms + ' ms): ' + $sum) ($r.Exit -eq 0) ('exit ' + $r.Exit + "`n" + $r.Err)
            if ($r.Exit -ne 0) { $bOk = $false }
        } else {
            Add-Skip 'B0.4' 'kein Sync; der Spiegel der Kopie kennt die Testmail womoeglich nicht'
        }
    }
    if ($bOk) {
        $r = Invoke-PkB @('mailbox', 'list', '--account', $acc, '--json')
        $mj = Get-Json $r.Out
        if ($null -ne $mj) {
            foreach ($b in @($mj.mailboxes)) {
                $boxRole[[string] $b.path] = [string] $b.role
                if ($b.role -eq 'inbox') { $inboxPath = [string] $b.path }
                if ($b.role -eq 'all') { $haveAll = $true }
                if ($b.role -eq 'trash') { $haveTrash = $true }
                if ([string] $b.path -eq $Label) { $haveLabel = $true }
            }
        }
        Add-Check 'B0.5' ('Ordner im Spiegel: Posteingang ' + [bool] $inboxPath + ', Rolle all ' + $haveAll + ', Papierkorb ' + $haveTrash + ', Label ' + $haveLabel) ($r.Exit -eq 0 -and [bool] $inboxPath) ''
        if (-not $inboxPath) { $bOk = $false }
        if (-not $haveLabel) { Write-Log '      Hinweis: Label fehlt im Spiegel. In Gmail das Label anlegen, dann das Skript neu starten (Schritt B0.4 holt es).' }
    }

    # Zustand einer Nachricht der Kopie, ohne Betreff und Adressen.
    function Get-MsgState {
        param([string] $Id)
        $rr = Invoke-PkB @('show', $Id, '--json')
        $jj = Get-Json $rr.Out
        $st = New-Object PSObject -Property @{ Ok = $false; Boxes = @(); Flags = @(); Pending = $false; Tags = @(); From = ''; Account = ''; Summary = ''; Raw = $jj }
        if ($rr.Exit -ne 0 -or $null -eq $jj) { $st.Summary = 'show ' + $Id + ': exit ' + $rr.Exit + ' ' + $rr.Err.Trim(); return $st }
        $st.Ok = $true
        $st.Account = [string] $jj.account
        $st.Flags = @($jj.flags | ForEach-Object { [string] $_ })
        $st.Pending = ($jj.pending_flags -eq $true)
        $st.Tags = @($jj.tags | ForEach-Object { [string] $_ })
        $m = [regex]::Match([string] $jj.from, '<([^>]+)>')
        if ($m.Success) { $st.From = $m.Groups[1].Value.Trim().ToLowerInvariant() } else { $st.From = ([string] $jj.from).Trim().ToLowerInvariant() }
        $names = @()
        foreach ($b in @($jj.mailboxes)) {
            $p = [string] $b.mailbox
            $st.Boxes += $p
            if ($p -eq $Label) { $names += '<Label>' } elseif ($b.role -and $b.role -ne 'custom') { $names += ('role:' + $b.role) } else { $names += '<ordner>' }
        }
        $st.Summary = 'Ordner: ' + ($names -join ', ') + '; flags: ' + ($st.Flags -join ',') + '; pending: ' + $st.Pending + '; tags: ' + ($st.Tags -join ',')
        return $st
    }

    function Get-ActionState {
        param([string] $N)
        $rr = Invoke-PkB @('action', 'show', $N, '--json')
        $jj = Get-Json $rr.Out
        if ($rr.Exit -ne 0 -or $null -eq $jj) { return New-Object PSObject -Property @{ State = '(nicht lesbar)'; Results = @(); Text = $rr.Text } }
        $res = @($jj.targets | ForEach-Object { [string] $_.result })
        return New-Object PSObject -Property @{ State = [string] $jj.state; Results = $res; Text = ('action ' + $N + ': ' + $jj.kind + ', state ' + $jj.state + ', Ergebnisse: ' + ($res -join ',')) }
    }

    function Invoke-SyncB {
        param([string] $Nr)
        $rr = Invoke-PkB @('sync', '-a', $acc) ('sync -a ' + $acc) 1800
        $sum = (@($rr.Out -split "`r?`n" | Where-Object { $_ -match 'messages mirrored|^\d+ errors' }) -join '; ')
        Add-Check $Nr ('sync -a <konto> fuehrt die Aktion aus (im Dauerbetrieb taete das ein laufendes sync --watch binnen 10 s) (' + $rr.Ms + ' ms): ' + $sum) ($rr.Exit -eq 0) ('exit ' + $rr.Exit + "`n" + $rr.Err)
        return ($rr.Exit -eq 0)
    }

    # ------------------------------------------------ B1: Mail auswaehlen
    $mid = ''
    if ($bOk) {
        Write-Log ''
        Write-Log '----- B1 Testmail waehlen: eine Mail, die du dir SELBST geschickt hast, im Posteingang'
        Write-Log ('      Finden z. B. mit: postkauz --data-dir <AccountDataDir> --config <AccountConfig> search "D7a Test" -a <konto> (Spalte ID)')
        for ($try = 1; $try -le 3 -and -not $mid; $try++) {
            $in = ''
            try { $in = [string] (Read-Host '      Lokale ID der Testmail (leer = Teil B beenden)') } catch { $in = '' }
            $in = $in.Trim()
            if (-not $in) { break }
            if ($in -notmatch '^\d+$') { Write-Log '      Die ID ist eine Zahl (Spalte ID von search).'; continue }
            $st = Get-MsgState $in
            if (-not $st.Ok) { Write-Log ('      ' + $st.Summary); continue }
            $raw = $st.Raw
            # Kopf nur auf den Bildschirm (nicht ins Protokoll).
            try {
                Write-Host ''
                Write-Host ('      Datum:   ' + $raw.date)
                Write-Host ('      Von:     ' + $raw.from)
                Write-Host ('      An:      ' + ((@($raw.to) | ForEach-Object { $_.address }) -join ', '))
                Write-Host ('      Betreff: ' + $raw.subject)
                Write-Host ('      Ordner:  ' + ($st.Boxes -join ', '))
                Write-Host ''
            } catch { }
            $selfSent = ($st.Account -eq $acc) -and ($own -contains $st.From)
            $inInbox = $st.Boxes -contains $inboxPath
            if (-not $selfSent) { Write-Log ('      ID ' + $in + ': Absender ist nicht die Adresse des Kontos (oder ein Alias) bzw. anderes Konto; bitte eine Mail an dich selbst waehlen.'); continue }
            if (-not $inInbox) { Write-Log ('      ID ' + $in + ': liegt nicht im Posteingang; bitte eine Mail aus dem Posteingang waehlen.'); continue }
            if (Confirm-Step 'B1' ('Ist das die Testmail, die du dir selbst geschickt hast (Kopf oben)? Nur an ihr arbeitet Teil B.')) { $mid = $in }
        }
        if ($mid) {
            $st = Get-MsgState $mid
            Add-Check 'B1' ('Testmail gewaehlt: ID ' + $mid + ', Absender = eigene Adresse, im Posteingang') $true $st.Summary
        } else {
            Add-Skip 'B1' 'keine Testmail gewaehlt; Teil B endet hier'
            $bOk = $false
        }
    }

    # ------------------------------------------------ B2: Tag
    if ($bOk) {
        Write-Log ''
        Write-Log '----- B2 Taggen (nur lokal im Archiv der Kopie)'
        if (Confirm-Step 'B2' ('tag add d7a-test ' + $mid + ' -a <konto>')) {
            $r = Invoke-PkB @('tag', 'add', 'd7a-test', $mid, '-a', $acc)
            Add-Check 'B2.1' 'tag add -> Exit 0, Tag an der Nachricht' ($r.Exit -eq 0 -and (Test-Has $r.Out ('(?m)^\s+' + $mid + '\s+.*d7a-test'))) $r.Text
            if (Test-Has $r.Out 'keywords: queued') { Write-Log '      Hinweis: [tags] sync_keywords ist an; der naechste sync setzt den Tag als IMAP-Keyword am Server.' }
            $r = Invoke-PkB @('search', 'tag:d7a-test', '-a', $acc, '--json')
            $found = @(Get-JsonLines $r.Out | ForEach-Object { [string] $_.id })
            Add-Check 'B2.2' 'search tag:d7a-test findet die Testmail' ($r.Exit -eq 0 -and ($found -contains $mid)) ('Treffer: ' + ($found -join ', '))
        } else { Add-Skip 'B2' 'ohne Zustimmung' }
    }

    # ------------------------------------------------ B3: als gelesen markieren
    if ($bOk) {
        Write-Log ''
        Write-Log '----- B3 Als gelesen markieren (Server: STORE \Seen)'
        $st = Get-MsgState $mid
        if ($st.Flags -contains 'seen') { Write-Log '      Hinweis: Die Mail ist schon gelesen; der Ablauf wird trotzdem geprueft, in Gmail aendert sich nichts Sichtbares.' }
        if (Confirm-Step 'B3' ('mark ' + $mid + ' --seen -a <konto>, danach sync')) {
            $r = Invoke-PkB @('mark', $mid, '--seen', '-a', $acc)
            $m = [regex]::Match($r.Out, 'queued \(action (\d+),')
            $n = ''
            if ($m.Success) { $n = $m.Groups[1].Value }
            Add-Check 'B3.1' 'mark --seen -> queued (action n), Exit 0' ($r.Exit -eq 0 -and $n) $r.Text
            $st = Get-MsgState $mid
            Add-Check 'B3.2' 'show: seen, pending (noch nicht am Server)' (($st.Flags -contains 'seen') -and $st.Pending) $st.Summary
            if ($n -and (Invoke-SyncB 'B3.3')) {
                $st = Get-MsgState $mid
                Add-Check 'B3.4' 'show nach dem Sync: seen, pending verschwunden' (($st.Flags -contains 'seen') -and -not $st.Pending) $st.Summary
                $as = Get-ActionState $n
                Add-Check 'B3.5' ('action show ' + $n + ' -> done, jede Kopie ok') ($as.State -eq 'done' -and $as.Results.Count -ge 1 -and @($as.Results | Where-Object { $_ -ne 'ok' }).Count -eq 0) $as.Text
                Read-Observation 'B3.6' 'Ist die Testmail in Gmail (Browser oder App, neu laden) jetzt als gelesen markiert?'
            }
        } else { Add-Skip 'B3' 'ohne Zustimmung' }
    }

    # ------------------------------------------------ B4: in den Label-Ordner verschieben
    $labelDone = $false
    if ($bOk) {
        Write-Log ''
        Write-Log '----- B4 In den Label-Ordner verschieben (Gmail: Label setzen, aus dem Posteingang nehmen)'
        if (-not $haveLabel) {
            Add-Skip 'B4' 'Label-Ordner -Label fehlt im Spiegel (in Gmail anlegen, Skript neu starten)'
        } elseif (Confirm-Step 'B4' ('move ' + $mid + ' --from ' + $inboxPath + ' --to <Label> -a <konto>, danach sync')) {
            $r = Invoke-PkB @('move', $mid, '--from', $inboxPath, '--to', $Label, '-a', $acc)
            $m = [regex]::Match($r.Out, 'queued \(action (\d+),')
            $n = ''
            if ($m.Success) { $n = $m.Groups[1].Value }
            Add-Check 'B4.1' 'move --to <Label> -> queued (action n), Exit 0' ($r.Exit -eq 0 -and $n) $r.Text
            if ($n -and (Invoke-SyncB 'B4.2')) {
                $as = Get-ActionState $n
                Add-Check 'B4.3' ('action show ' + $n + ' -> done, ok') ($as.State -eq 'done' -and @($as.Results | Where-Object { $_ -ne 'ok' }).Count -eq 0) $as.Text
                $st = Get-MsgState $mid
                $labelDone = ($st.Boxes -contains $Label)
                Add-Check 'B4.4' 'show: Mail im Label-Ordner, nicht mehr im Posteingang' ($labelDone -and -not ($st.Boxes -contains $inboxPath)) $st.Summary
                Read-Observation 'B4.5' 'Hat die Testmail in Gmail jetzt das Label und ist aus dem Posteingang verschwunden (archiviert)?'
            }
        } else { Add-Skip 'B4' 'ohne Zustimmung' }
    }

    # ------------------------------------------------ B5: Label wieder entfernen
    if ($bOk) {
        Write-Log ''
        Write-Log '----- B5 Label wieder entfernen (Gmail: move --from <Label> --role all)'
        if (-not $labelDone) {
            Add-Skip 'B5' 'B4 nicht ausgefuehrt oder die Mail liegt nicht im Label-Ordner'
        } elseif (-not $haveAll) {
            Add-Skip 'B5' 'kein Ordner mit Rolle all (Alle Nachrichten) im Spiegel'
        } elseif (Confirm-Step 'B5' ('move ' + $mid + ' --from <Label> --role all -a <konto>, danach sync')) {
            $r = Invoke-PkB @('move', $mid, '--from', $Label, '--role', 'all', '-a', $acc)
            $m = [regex]::Match($r.Out, 'queued \(action (\d+),')
            $n = ''
            if ($m.Success) { $n = $m.Groups[1].Value }
            Add-Check 'B5.1' 'move --from <Label> --role all -> queued (action n), Exit 0' ($r.Exit -eq 0 -and $n) $r.Text
            if ($n -and (Invoke-SyncB 'B5.2')) {
                $as = Get-ActionState $n
                Add-Check 'B5.3' ('action show ' + $n + ' -> done, ok') ($as.State -eq 'done' -and @($as.Results | Where-Object { $_ -ne 'ok' }).Count -eq 0) $as.Text
                $st = Get-MsgState $mid
                Add-Check 'B5.4' 'show: Mail nicht mehr im Label-Ordner (Gmail: Label entfernt)' ($st.Ok -and -not ($st.Boxes -contains $Label)) $st.Summary
                Read-Observation 'B5.5' 'Ist das Label an der Testmail in Gmail weg (die Mail liegt unter Alle Nachrichten)?'
            }
        } else { Add-Skip 'B5' 'ohne Zustimmung' }
    }

    # ------------------------------------------------ B6: Papierkorb mit Freigabe
    if ($bOk) {
        Write-Log ''
        Write-Log '----- B6 Papierkorb mit Freigabe (nicht endgueltig)'
        if (-not $haveTrash) {
            Add-Skip 'B6' 'kein Papierkorb (Rolle trash) im Spiegel'
        } elseif (Confirm-Step 'B6' ('trash ' + $mid + ' -a <konto> (legt nur einen Antrag an)')) {
            $r = Invoke-PkB @('trash', $mid, '-a', $acc)
            $m = [regex]::Match($r.Out, 'requested \(action (\d+), approval (\d+)\)')
            $n = ''
            if ($m.Success) { $n = $m.Groups[1].Value }
            Add-Check 'B6.1' 'trash -> Exit 4, needs_approval, Ausgabe nennt "action approve <n>"' ($r.Exit -eq 4 -and $n -and (Test-Has $r.Out ('action approve ' + $n + '\b')) -and (Test-Has $r.Err 'needs_approval')) $r.Text
            if ($n) {
                $as = Get-ActionState $n
                Add-Check 'B6.2' ('action show ' + $n + ' -> requested (wartet auf Freigabe)') ($as.State -eq 'requested') $as.Text
                # Ein Schluessel darf nie freigeben: Probe-Schluessel nur in dieser Kopie.
                $probeKey = 'd7a-agent-' + (Get-Date -Format 'MMddHHmmss')
                $kr = Invoke-PkB @('key', 'create', '--name', $probeKey, '--role', 'reader', '--expires', '1d')
                $r = Invoke-PkB @('action', 'approve', $n, '--key', $probeKey)
                $as = Get-ActionState $n
                Add-Check 'B6.3' ('action approve ' + $n + ' --key <Probe-Schluessel, Rolle reader> -> denied (human_only), Exit 3; bleibt requested') ($kr.Exit -eq 0 -and $r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(human_only\)') -and $as.State -eq 'requested') ($r.Text + "`n" + $as.Text)
                [void] (Invoke-PkB @('key', 'revoke', $probeKey))
                if (Confirm-Step 'B6.4' ('Freigeben: action approve ' + $n + ' (verschiebt die Testmail in den Papierkorb; dort bleibt sie wiederherstellbar)')) {
                    $r = Invoke-PkB @('action', 'approve', $n)
                    Add-Check 'B6.4' ('action approve ' + $n + ' -> "action ' + $n + ': queued"') ($r.Exit -eq 0 -and (Test-Has $r.Out ('action ' + $n + ': queued'))) $r.Text
                    if (Invoke-SyncB 'B6.5') {
                        $as = Get-ActionState $n
                        Add-Check 'B6.6' ('action show ' + $n + ' -> done, ok') ($as.State -eq 'done' -and @($as.Results | Where-Object { $_ -ne 'ok' }).Count -eq 0) $as.Text
                        $st = Get-MsgState $mid
                        $inTrash = $false
                        foreach ($p in $st.Boxes) { if ($boxRole[$p] -eq 'trash') { $inTrash = $true } }
                        Add-Check 'B6.7' 'show: Mail im Papierkorb' $inTrash $st.Summary
                        Read-Observation 'B6.8' 'Liegt die Testmail in Gmail im Papierkorb (und laesst sich von dort wiederherstellen)?'
                    }
                } else {
                    # Keinen offenen Antrag zuruecklassen.
                    $r = Invoke-PkB @('action', 'reject', $n, '--reason', 'Abnahme D7a: nicht freigegeben')
                    Add-Check 'B6.4' ('ohne Freigabe: action reject ' + $n + ' -> rejected, nichts am Server') ($r.Exit -eq 0 -and (Test-Has $r.Out 'rejected')) $r.Text
                }
            }
        } else { Add-Skip 'B6' 'ohne Zustimmung' }
    }

    # ------------------------------------------------ B7: Audit der Kopie
    if ($bOk) {
        Write-Log ''
        Write-Log '----- B7 Audit der Kopie'
        $r = Invoke-PkB @('audit', '--since', '2h', '--limit', '1000', '--json')
        $ev = @(Get-JsonLines $r.Out)
        $ops = @($ev | Where-Object { @('messages.tag', 'messages.mark', 'messages.move', 'messages.trash', 'actions.approve') -contains $_.operation } | ForEach-Object { $_.operation + ' ' + $_.result + ' (' + $_.reason + ')' })
        Add-Check 'B7.1' 'audit --since 2h: die Aufrufe von Teil B mit Endergebnis' ($r.Exit -eq 0 -and $ops.Count -ge 1) ($ops -join "`n")
        $r = Invoke-PkB @('audit', 'verify', '--json')
        $j = Get-Json $r.Out
        Add-Check 'B7.2' 'audit verify der Kopie -> status ok' ($r.Exit -eq 0 -and $null -ne $j -and $j.status -eq 'ok') $r.Text
    }
}

Write-Log ''
foreach ($f in $script:Fails) { Write-Log ('FAIL-Liste: ' + $f) }
Write-Log ('D7a: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL, ' + $script:NSkip + ' SKIP')
if ($script:NFail -gt 0) { exit 1 }
exit 0
