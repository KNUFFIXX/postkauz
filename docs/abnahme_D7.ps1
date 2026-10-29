<#
.SYNOPSIS
    Abnahme D7 (PostKauz, Schnitt 3, Teil T3): Anhaenge sichern, Export,
    Sicherung und Wiederherstellung, Loeschkonzept; offline, ohne Server.

.DESCRIPTION
    Gehoert zu docs/abnahme_D7.md und auftraege/A03_teile.md, Teil T3,
    Haltepunkt "Abnahme D7" (D7 Schritte 4, 5 und 7 aus
    auftraege/A03_s3.md, Teil D). Laeuft auf Windows mit Windows
    PowerShell 5.1, ohne Go, ohne Docker, nur mit dem Programm aus dist/
    (-ExePath). Kein Mailserver, kein echtes Postfach, keine Zugangsdaten.

    Teil A (Normalfall): frisches Datenverzeichnis -DataDir mit
    Konfiguration -Config (beide legt das Skript an), Konto demo ohne
    Server, drei synthetische Mails per import mbox (Adressen nur
    example.org/.net; Anhaenge: eine PDF, eine Textdatei CON.txt, ein
    Programm werkzeug.pdf.exe), Schluessel test-agent (reader) und
    test-operator (operator). Geprueft:
      Schritt 4 (D7.4): attach save (Namensdoppel, Geraetename, .blocked,
        .., Scope), export eml und mbox (index.csv ohne Betreff, Bytes
        gleich, frisches Ziel, nur Mensch), Rundlauf export mbox ->
        import mbox in ein zweites Konto; Thunderbird prueft der
        Betreiber (mit -Interactive fragt das Skript danach).
      Schritt 5 (D7.5): audit export, backup create (Manifest,
        ENCRYPT-ME.txt, Warnung), zweites backup create kopiert 0 Blobs,
        restore in ein frisches Verzeichnis auf diesem Rechner (neue
        Identitaet), Vergleich von Suche, Nachrichtenzahl und doctor
        --blobs=all, audit verify in Original und Kopie ohne Befund (A10),
        manipulierte Sicherung -> Abbruch ohne Zielordner; Paket der
        Sicherung (tar) und Referenzdatei fuer den Restore im
        Linux-Container (docs/abnahme_D7_restore.sh) und auf dem Heim-PC.
      Schritt 7 (D7.7): purge --dry-run --older-than 0s zaehlt die
        importierten Mails nur als "kept (imported archive)" (A13.2),
        blob gc --dry-run, audit rotate --dry-run, alle drei mit --key
        human_only.
      Schritt 6: audit verify am Ende.

    Teil R (mit -RestoreFrom, z. B. auf dem Heim-PC): nur Wiederherstellung
    einer Sicherung aus Teil A in -DataDir und Vergleich mit der
    Referenzdatei -Reference (Suche, Nachrichtenzahl, doctor --blobs=all,
    audit verify).

    Sicherheitsregeln (A03_teile.md, Rahmen 5): -DataDir, -Config und
    (seit der Nachbesserung T3) -LivePath sind Pflicht. Das Skript bricht
    ab, ohne etwas zu aendern, wenn -DataDir, -Config oder -WorkDir im
    Standard-Datenverzeichnis (%LOCALAPPDATA%\postkauz), im
    Standard-Konfigurationsordner (%APPDATA%\postkauz) oder in einem mit
    -LivePath genannten Pfad liegen (oder umgekehrt); ein Datei-Eintrag in
    -LivePath (etwa die Live-Konfiguration) schuetzt auch seinen Ordner.
    Dazu eine Markerdatei gegen Junction und subst (auch fuer den Ordner
    von -Config) und ein Abbruch, wenn neben -Config schon eine
    policy.toml liegt (Ordner einer anderen Konfiguration).

    Jede Pruefung ist eine Zeile "OK <Nr> <Beschreibung>",
    "FAIL <Nr> <Beschreibung>" oder "SKIP <Nr> <Grund>", darunter
    eingerueckt die Ist-Ausgabe (gekuerzt; Schluessel, Mailadressen und
    Pfade maskiert). Letzte Zeile: "D7: <n> OK, <m> FAIL, <k> SKIP".
    Exit 0 nur ohne FAIL, Exit 2 bei Abbruch vor dem ersten Schritt.

.PARAMETER ExePath
    postkauz.exe des zu pruefenden Stands (aus dist/, umbenannt).

.PARAMETER DataDir
    Neues, leeres (oder noch nicht vorhandenes) Datenverzeichnis.

.PARAMETER Config
    Pfad der Konfigurationsdatei, die das Skript neu anlegt (darf noch
    nicht existieren).

.PARAMETER WorkDir
    Ordner fuer das Protokoll D7_windows.txt (Teil R: D7_restore.txt;
    exklusiv neu angelegt) und den Laufordner lauf-<Datum-Uhrzeit>. Kein
    Reparse Point.

.PARAMETER LivePath
    Pflicht: Pfade des Dauerbetriebs (Live-Datenverzeichnis,
    Live-Konfiguration), durch Komma getrennt; Schutz gegen Verwechslung.
    Ein Datei-Eintrag schuetzt auch den Ordner, in dem die Datei liegt.

.PARAMETER ExpectVersion
    Erwartete Version von -ExePath (Standard 0.3.0-alpha.7b).

.PARAMETER Interactive
    Fragt nach der Thunderbird-Pruefung (D7.4); ohne: SKIP mit Hinweis.

.PARAMETER RestoreFrom
    Teil R: Ordner einer Sicherung aus Teil A (entpackt).

.PARAMETER Reference
    Teil R: D7_reference.json aus Teil A.

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File abnahme_D7.ps1 `
      -ExePath C:\PK\d7\postkauz.exe -DataDir C:\PK\d7\daten -Config C:\PK\d7\config.toml `
      -WorkDir C:\PK\d7 -LivePath C:\PostKauz\data,C:\PostKauz\config.toml
#>
param(
    [Parameter(Mandatory = $true)] [string] $ExePath,
    [Parameter(Mandatory = $true)] [string] $DataDir,
    [Parameter(Mandatory = $true)] [string] $Config,
    [Parameter(Mandatory = $true)] [string] $WorkDir,
    [Parameter(Mandatory = $true)] [string[]] $LivePath,
    [string] $ExpectVersion = '0.3.0-alpha.7b',
    [switch] $Interactive,
    [string] $RestoreFrom = '',
    [string] $Reference = ''
)

$ErrorActionPreference = 'Stop'
$script:Utf8 = New-Object System.Text.UTF8Encoding $false
$script:NOk = 0
$script:NFail = 0
$script:NSkip = 0
$script:Log = $null
$script:Fails = New-Object System.Collections.Generic.List[string]
# Die Hilfen aus D7a maskieren auch Label und Kontoname aus deren Teil B;
# hier gibt es beides nicht.
$Label = ''
$LiveAccount = ''
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
        Write-Log ('D7: ' + $script:NOk + ' OK, ' + ($script:NFail + 1) + ' FAIL, ' + $script:NSkip + ' SKIP')
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
    # Alle POSTKAUZ_*-Variablen entfernen (wie abnahme_D7_restore.sh), nicht
    # nur eine Liste: Auch kuenftige Variablen duerfen nicht durchreichen.
    foreach ($k in @($psi.EnvironmentVariables.Keys | Where-Object { $_ -like 'POSTKAUZ_*' })) {
        $psi.EnvironmentVariables.Remove($k)
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
# Ordner). Forbidden wird rekursiv durchsucht, Shallow nur der Ordner selbst
# (Elternordner von Live-Dateien; sonst liefe die Suche mehrfach ueber das
# Live-Blob-Verzeichnis). Liefert '' oder den Ort, an dem sie sichtbar war.
function Test-Marker {
    param([string] $Dir, [string[]] $Forbidden, [string[]] $Shallow = @())
    if (-not (Test-Path -LiteralPath $Dir)) { [void] [System.IO.Directory]::CreateDirectory($Dir) }
    $name = '.pk-abnahme-marker-' + [guid]::NewGuid().ToString('N')
    $marker = Join-Path $Dir $name
    [System.IO.File]::WriteAllText($marker, 'Abnahme D7: Marker, darf geloescht werden', $script:Utf8)
    $seen = ''
    try {
        foreach ($f in $Forbidden) {
            if (-not $f -or -not (Test-Path -LiteralPath $f -PathType Container)) { continue }
            $hit = @(Get-ChildItem -LiteralPath $f -Recurse -Force -Filter $name -ErrorAction SilentlyContinue | Select-Object -First 1)
            if ($hit.Count -gt 0) { $seen = $f; break }
        }
        if (-not $seen) {
            foreach ($f in $Shallow) {
                if (-not $f -or -not (Test-Path -LiteralPath $f -PathType Container)) { continue }
                if (Test-Path -LiteralPath (Join-Path $f $name)) { $seen = $f; break }
            }
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


# Treffer-IDs einer Suche (NDJSON; Zeilen ohne id, etwa die Huelle, zaehlen nicht).
function Get-SearchIds {
    param([string] $Data, [string] $Q)
    $r = Invoke-PkIn $Data $script:Cfg @('search', $Q, '--json', '--limit', '100')
    $ids = @(Get-JsonLines $r.Out | Where-Object { $null -ne $_.id } | ForEach-Object { [int] $_.id } | Sort-Object)
    return New-Object PSObject -Property @{ Exit = $r.Exit; Ids = $ids; Text = $r.Text }
}

# Lokale ID einer Nachricht ueber ihre Message-ID im Konto demo (0 = nicht
# gefunden; nach dem Rundlauf in Schritt 4 hat Konto zwei dieselben).
function Get-MsgId {
    param([string] $Data, [string] $Mid)
    $r = Invoke-PkIn $Data $script:Cfg @('show', $Mid, '-a', 'demo', '--json')
    $j = Get-Json $r.Out
    if ($r.Exit -eq 0 -and $null -ne $j -and $null -ne $j.id) { return [int] $j.id }
    return 0
}

# Kennzeilen von doctor --blobs=all: verify und result.
function Get-BlobReport {
    param([string] $Data)
    $r = Invoke-PkIn $Data $script:Cfg @('doctor', '--blobs=all')
    $verify = ''
    $result = ''
    $m = [regex]::Match($r.Out, '(?m)^\s+verify:\s+(.*?)\s*$')
    if ($m.Success) { $verify = $m.Groups[1].Value }
    $m = [regex]::Match($r.Out, '(?m)^\s+result:\s+(.*?)\s*$')
    if ($m.Success) { $result = $m.Groups[1].Value }
    return New-Object PSObject -Property @{ Exit = $r.Exit; Verify = $verify; Result = $result; Text = $r.Text }
}

function Get-AuditVerify {
    param([string] $Data)
    $r = Invoke-PkIn $Data $script:Cfg @('audit', 'verify', '--json')
    $j = Get-Json $r.Out
    $status = ''
    $problems = -1
    if ($null -ne $j) { $status = [string] $j.status; $problems = @($j.problems).Count }
    return New-Object PSObject -Property @{ Exit = $r.Exit; Status = $status; Problems = $problems; Json = $j; Text = $r.Text }
}

$script:Queries = @('Rechnung', 'Verein', 'Werkzeug', 'Heizungswartung', 'tag:rechnung')
$script:MsgIds = @('d7.rechnung@corpus.example.org', 'd7.verein@corpus.example.org', 'd7.programm@corpus.example.org')

# ---------------------------------------------------------------- Pfade pruefen

$script:ExeFull = Get-FullPath $ExePath
$script:Data = Get-FullPath $DataDir
$script:Cfg = Get-FullPath $Config
$script:Work = Get-FullPath $WorkDir
$script:ModeR = [bool] $RestoreFrom

$forbidden = New-Object System.Collections.Generic.List[string]
if ($env:LOCALAPPDATA) { $forbidden.Add((Join-Path $env:LOCALAPPDATA 'postkauz')); $forbidden.Add((Join-Path $env:LOCALAPPDATA 'Packages')) }
if ($env:APPDATA) { $forbidden.Add((Join-Path $env:APPDATA 'postkauz')) }
$livePaths = New-Object System.Collections.Generic.List[string]
# Elternordner von Live-Dateien: verboten fuer -DataDir, -Config, -WorkDir,
# bei der Markersuche aber nur flach geprueft.
$parentPlaces = New-Object System.Collections.Generic.List[string]
foreach ($l in ($LivePath | ForEach-Object { $_ -split ',' })) {
    if (-not $l.Trim()) { continue }
    $lp = Get-FullPath $l.Trim()
    $livePaths.Add($lp)
    $forbidden.Add($lp)
    # Ein Datei-Eintrag (vorhandene Datei oder Name mit Endung, etwa
    # config.toml) schuetzt auch seinen Ordner: Dort liegen policy.toml
    # und oft das Datenverzeichnis des Dauerbetriebs.
    if ((Test-Path -LiteralPath $lp -PathType Leaf) -or ([System.IO.Path]::GetExtension($lp) -and -not (Test-Path -LiteralPath $lp -PathType Container))) {
        $parent = Split-Path -Parent $lp
        if ($parent) { $forbidden.Add($parent); $parentPlaces.Add($parent) }
    }
}
if ($livePaths.Count -eq 0) { Stop-Run '-LivePath ist leer: Live-Datenverzeichnis und Live-Konfiguration angeben (durch Komma getrennt).' }
$markerPlaces = New-Object System.Collections.Generic.List[string]
$markerShallow = New-Object System.Collections.Generic.List[string]
foreach ($f in $forbidden) {
    if (-not (Test-Path -LiteralPath $f -PathType Container) -or $f.EndsWith('\Packages')) { continue }
    if ($parentPlaces.Contains($f) -and -not $livePaths.Contains($f)) { $markerShallow.Add($f) } else { $markerPlaces.Add($f) }
}
foreach ($f in $forbidden) {
    if (Test-Overlap $script:Data $f) { Stop-Run ('-DataDir liegt im Standard- oder Live-Pfad ' + $f + ' (oder umgekehrt); nichts veraendert.') }
    if (Test-Overlap $script:Cfg $f) { Stop-Run ('-Config liegt im Standard- oder Live-Pfad ' + $f + '; nichts veraendert.') }
    if (Test-Overlap $script:Work $f) { Stop-Run ('-WorkDir liegt im Standard- oder Live-Pfad ' + $f + ' (oder umgekehrt); nichts veraendert.') }
}
if ((Test-Path -LiteralPath $script:Data) -and @(Get-ChildItem -LiteralPath $script:Data -Force).Count -gt 0) {
    Stop-Run '-DataDir ist nicht leer; die Abnahme braucht ein frisches Datenverzeichnis.'
}
if (Test-Path -LiteralPath $script:Cfg) { Stop-Run '-Config gibt es schon; das Skript legt die Konfiguration selbst an (neuen Pfad waehlen).' }
if (Test-Path -LiteralPath (Join-Path (Split-Path -Parent $script:Cfg) 'policy.toml')) {
    Stop-Run 'Neben -Config liegt schon eine policy.toml (Ordner einer anderen Konfiguration, vielleicht der Live-Konfiguration); bitte einen neuen, leeren Ordner fuer -Config nehmen.'
}
if ($script:Cfg.Contains("'")) { Stop-Run '-Config darf kein einfaches Anfuehrungszeichen enthalten (TOML).' }
if (-not (Test-Path -LiteralPath $script:ExeFull -PathType Leaf)) { Stop-Run ('Programm nicht gefunden: ' + $script:ExeFull) }
if ($script:ModeR) {
    $script:From = Get-FullPath $RestoreFrom
    if (-not (Test-Path -LiteralPath (Join-Path $script:From 'manifest.json') -PathType Leaf)) { Stop-Run '-RestoreFrom enthaelt keine manifest.json (Sicherung entpacken).' }
    if (-not $Reference -or -not (Test-Path -LiteralPath $Reference -PathType Leaf)) { Stop-Run 'Teil R braucht -Reference (D7_reference.json aus Teil A).' }
    foreach ($f in $forbidden) { if (Test-Overlap $script:From $f) { Stop-Run ('-RestoreFrom liegt im Standard- oder Live-Pfad ' + $f + '; nichts veraendert.') } }
    if (Test-Overlap $script:From $script:Data) { Stop-Run '-RestoreFrom und -DataDir ueberschneiden sich.' }
}
foreach ($d in @($script:Data, $script:Work, (Split-Path -Parent $script:Cfg))) {
    $where = Test-Marker $d $markerPlaces $markerShallow
    if ($where) { Stop-Run ('Eine Markerdatei aus ' + $d + ' ist in ' + $where + ' sichtbar (Junction, subst oder derselbe Ordner).') }
}
# Test-Marker legt -DataDir an; restore will ein leeres oder fehlendes Ziel.

[void] [System.IO.Directory]::CreateDirectory($script:Work)
if ((Get-Item -LiteralPath $script:Work -Force).Attributes -band [System.IO.FileAttributes]::ReparsePoint) {
    Stop-Run ('-WorkDir ist ein Reparse Point (Junction oder Link): ' + $script:Work + '. Bitte ein normales Verzeichnis nehmen.')
}
$logName = 'D7_windows.txt'
if ($script:ModeR) { $logName = 'D7_restore.txt' }
$script:Log = Join-Path $script:Work $logName
try {
    $fs = New-Object System.IO.FileStream($script:Log, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::Read)
    $fs.Close()
} catch {
    $bad = $script:Log
    $script:Log = $null
    Stop-Run ('Protokolldatei existiert schon (oder ist ein Link): ' + $bad + '. Bitte umbenennen oder ein neues -WorkDir nehmen.')
}
$script:Run = Join-Path $script:Work ('lauf-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
if (Test-Path -LiteralPath $script:Run) { Stop-Run ('Laufordner existiert schon: ' + $script:Run) }
foreach ($sub in @('', 'input', 'keys')) { [void] [System.IO.Directory]::CreateDirectory((Join-Path $script:Run $sub)) }
Add-PathMask $script:Data '<DataDir>'
Add-PathMask $script:Cfg '<Config>'
Add-PathMask $script:Run '<Lauf>'
Add-PathMask $script:Work '<WorkDir>'
if ($script:ModeR) { Add-PathMask $script:From '<Sicherung>' }
$i = 0
foreach ($l in $livePaths) { $i++; Add-PathMask $l ('<Live' + $i + '>') }
Add-PathMask (Split-Path -Parent $script:ExeFull) '<ExeDir>'
Add-PathMask $env:USERPROFILE '<USERPROFILE>'

Write-Log ('Abnahme D7 ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + ', PowerShell ' + $PSVersionTable.PSVersion.ToString())
Write-Log ('Programm: ' + $script:ExeFull)
Write-Log ('DataDir ' + $script:Data + ', Config ' + $script:Cfg + ', Laufordner ' + $script:Run)

$det = 'SHA-256 postkauz.exe: ' + (Get-FileHash -LiteralPath $script:ExeFull -Algorithm SHA256).Hash
$r = Invoke-Native $script:ExeFull @('version')
Add-Check '0.1' ('version nennt ' + $ExpectVersion) ($r.Exit -eq 0 -and (Test-Has $r.Out ([regex]::Escape($ExpectVersion) + '(?![0-9.])'))) ($r.Out.Trim() + "`n" + $det)

[void] [System.IO.Directory]::CreateDirectory((Split-Path -Parent $script:Cfg))
$cfgBytes = $script:Utf8.GetBytes("# Abnahme D7: nur dieses Datenverzeichnis`r`n[operator]`r`nname = 'D7-Abnahme'`r`n")
try {
    $fs = New-Object System.IO.FileStream($script:Cfg, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::Read)
    $fs.Write($cfgBytes, 0, $cfgBytes.Length)
    $fs.Close()
} catch {
    Stop-Run ('-Config existiert schon (oder ist ein Link): ' + $script:Cfg + '. Bitte einen neuen Pfad nehmen.')
}

# ================================================================ Teil R
if ($script:ModeR) {
    Write-Log ''
    Write-Log '===== Teil R: Wiederherstellung einer Sicherung aus Teil A (D7.5, zweite saubere Maschine)'
    $ref = $null
    try { $ref = Get-Content -LiteralPath $Reference -Raw -Encoding UTF8 | ConvertFrom-Json } catch { $ref = $null }
    if ($null -eq $ref) { Stop-Run 'Referenzdatei nicht lesbar.' }
    # restore verlangt ein leeres oder fehlendes Ziel; Test-Marker hat
    # -DataDir als leeren Ordner angelegt.
    $r = Invoke-Native $script:ExeFull @('--config', $script:Cfg, 'restore', '--from', $script:From, '--data-dir', $script:Data, '--dry-run')
    Add-Check 'R.1' 'restore --dry-run: Sicherung vollstaendig und unveraendert' ($r.Exit -eq 0 -and (Test-Has $r.Out 'is complete and unchanged')) ($r.Out + $r.Err)
    $r = Invoke-Native $script:ExeFull @('--config', $script:Cfg, 'restore', '--from', $script:From, '--data-dir', $script:Data)
    Add-Check 'R.2' 'restore: "restored:", neue Identitaet, doctor --blobs=all "result: ok"' ($r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^restored:') -and (Test-Has $r.Out 'identity:\s+new data directory id') -and (Test-Has $r.Out '(?m)^\s+result:\s+ok\s*$')) (Limit-Text ($r.Out + $r.Err) 40 4000)
    $det = ''
    $ok = $true
    foreach ($q in $script:Queries) {
        $want = @($ref.searches.$q | ForEach-Object { [int] $_ } | Sort-Object)
        $got = Get-SearchIds $script:Data $q
        $same = ($got.Exit -eq 0) -and (($want -join ',') -eq ($got.Ids -join ','))
        $ok = $ok -and $same
        $det += $q + ': erwartet [' + ($want -join ',') + '], ist [' + ($got.Ids -join ',') + "]`n"
    }
    Add-Check 'R.3' 'Suche wie im Original (Referenzdatei)' $ok $det
    $det = ''
    $ok = $true
    foreach ($mid in $script:MsgIds) {
        $want = [int] $ref.messages.$mid
        $got = Get-MsgId $script:Data $mid
        $ok = $ok -and ($want -gt 0) -and ($want -eq $got)
        $det += $mid + ': erwartet ' + $want + ', ist ' + $got + "`n"
    }
    Add-Check 'R.4' 'Nachrichten mit denselben IDs wie im Original' $ok $det
    $b = Get-BlobReport $script:Data
    Add-Check 'R.5' ('doctor --blobs=all: "' + $ref.blobs_verify + '", result ok') ($b.Result -eq 'ok' -and $b.Verify -eq [string] $ref.blobs_verify) (Limit-Text $b.Text 30)
    $v = Get-AuditVerify $script:Data
    Add-Check 'R.6' 'audit verify der Kopie -> ok, ohne Befund (Tage des Originals nur gegen die Datenbank)' ($v.Exit -eq 0 -and $v.Status -eq 'ok' -and $v.Problems -eq 0) $v.Text
    Write-Log ''
    foreach ($f in $script:Fails) { Write-Log ('FAIL-Liste: ' + $f) }
    Write-Log ('D7: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL, ' + $script:NSkip + ' SKIP')
    if ($script:NFail -gt 0) { exit 1 }
    exit 0
}

# ================================================================ Teil A
Write-Log ''
Write-Log '===== Schritt 0: Vorbereitung (offline, synthetische Testdaten)'

$pdf = [System.Text.Encoding]::ASCII.GetBytes("%PDF-1.4`n1 0 obj<<>>endobj`ntrailer<<>>`n%%EOF`n")
$exe = New-Object byte[] 64
$exe[0] = 0x4D; $exe[1] = 0x5A; $exe[2] = 0x90
$exe = [byte[]] ($exe + [System.Text.Encoding]::ASCII.GetBytes("This program cannot be run in DOS mode.`r`n"))
$pdf64 = [Convert]::ToBase64String($pdf)
$exe64 = [Convert]::ToBase64String($exe)
$mbox = @"
From MAILER-DAEMON Mon Oct 19 09:00:00 2026
Message-ID: <d7.rechnung@corpus.example.org>
Date: Mon, 19 Oct 2026 09:00:00 +0200
From: Muster Handwerk <rechnung@example.org>
To: Demo Buero <demo@example.org>
Subject: Rechnung 2026-117 Heizungswartung
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="b1"

--b1
Content-Type: text/plain; charset=utf-8

Guten Tag, anbei die Rechnung fuer die Wartung.
--b1
Content-Type: application/pdf; name="Rechnung-2026-117.pdf"
Content-Disposition: attachment; filename="Rechnung-2026-117.pdf"
Content-Transfer-Encoding: base64

$pdf64
--b1--

From MAILER-DAEMON Tue Oct 20 10:15:00 2026
Message-ID: <d7.verein@corpus.example.org>
Date: Tue, 20 Oct 2026 10:15:00 +0200
From: Beispiel Verein <info@example.org>
To: Demo Buero <demo@example.org>
Subject: Vereinsnachrichten Oktober
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="b2"

--b2
Content-Type: text/plain; charset=utf-8

Hallo, die Neuigkeiten aus dem Verein.
--b2
Content-Type: text/plain; name="CON.txt"
Content-Disposition: attachment; filename="CON.txt"

Termine im November.
--b2--

From MAILER-DAEMON Wed Oct 21 11:30:00 2026
Message-ID: <d7.programm@corpus.example.org>
Date: Wed, 21 Oct 2026 11:30:00 +0200
From: Unbekannt <werkzeug@example.net>
To: Demo Buero <demo@example.org>
Subject: Neues Werkzeug
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="b3"

--b3
Content-Type: text/plain; charset=utf-8

Bitte installieren.
--b3
Content-Type: application/octet-stream; name="werkzeug.pdf.exe"
Content-Disposition: attachment; filename="werkzeug.pdf.exe"
Content-Transfer-Encoding: base64

$exe64
--b3--

"@
$mboxFile = Join-Path $script:Run 'input\d7-korpus.mbox'
[System.IO.File]::WriteAllText($mboxFile, ($mbox -replace "`r`n", "`n"), $script:Utf8)

$r = Invoke-Native $script:ExeFull @('--data-dir', $script:Data, '--config', $script:Cfg, 'account', 'add', 'demo', '--imap', '127.0.0.1:1', '--user', 'demo@example.org', '--insecure-test-server', '--password-from-env') @{ 'POSTKAUZ_PASSWORD_DEMO' = 'unbenutzt-kein-server' }
Add-Check '0.2' 'Konto demo ohne Server angelegt (Passwort nur aus der Umgebung, nicht gespeichert)' ($r.Exit -eq 0 -and (Test-Has $r.Out 'account demo added')) ($r.Out + $r.Err)
$r = Invoke-Pk @('import', 'mbox', $mboxFile, '-a', 'demo', '--mailbox', 'INBOX', '--quiet')
Add-Check '0.3' 'import mbox: 3 neue Nachrichten' ($r.Exit -eq 0 -and (Test-Has $r.Out 'new: 3, existing: 0, skipped: 0, errors: 0')) $r.Text

$ids = @{}
foreach ($mid in $script:MsgIds) { $ids[$mid] = Get-MsgId $script:Data $mid }
$idR = $ids['d7.rechnung@corpus.example.org']
$idV = $ids['d7.verein@corpus.example.org']
$idP = $ids['d7.programm@corpus.example.org']
Add-Check '0.4' 'lokale IDs der drei Mails' ($idR -gt 0 -and $idV -gt 0 -and $idP -gt 0) ('Rechnung ' + $idR + ', Verein ' + $idV + ', Programm ' + $idP)
if ($idR -le 0 -or $idV -le 0 -or $idP -le 0) { Stop-Run 'Testdaten fehlen.' }

$ok = $true
$det = ''
foreach ($kv in @(@('test-agent', 'reader'), @('test-operator', 'operator'))) {
    $r = Invoke-Pk @('key', 'create', '--name', $kv[0], '--role', $kv[1], '--expires', '2d')
    $ok = $ok -and $r.Exit -eq 0 -and (Test-Has $r.Out 'pk_key_')
    $det += $kv[0] + ' (' + $kv[1] + '): exit ' + $r.Exit + "`n"
}
Add-Check '0.5' 'Schluessel test-agent (reader) und test-operator (operator) angelegt' $ok $det
$r = Invoke-Pk @('tag', 'add', 'rechnung', [string] $idR)
Add-Check '0.6' 'tag add rechnung (fuer den Vergleich nach dem Restore)' ($r.Exit -eq 0) $r.Text
$r = Invoke-Pk @('doctor')
$m = [regex]::Match($r.Out, '(?m)^export:\s+(.+?)\s+\[paths\.export_dir\]\s*$')
$exportDir = ''
if ($m.Success) { $exportDir = $m.Groups[1].Value }
$wantExport = Join-Path $script:Data 'export'
Add-Check '0.7' 'doctor: Datenverzeichnis von D7; export: <DataDir>\export (Standard von [paths] export_dir)' ($r.Exit -eq 0 -and (Test-Has $r.Out ('(?m)^data dir:\s+' + [regex]::Escape($script:Data))) -and ($exportDir -eq $wantExport)) (Limit-Text $r.Text 14)

# ------------------------------------------------ Schritt 4: Anhaenge und Export
Write-Log ''
Write-Log '===== Schritt 4 (D7.4): attach save, export eml und mbox, Rundlauf'

function Get-AttId {
    param([int] $Msg)
    $r = Invoke-Pk @('attach', 'list', [string] $Msg, '--json')
    $j = Get-Json $r.Out
    if ($null -ne $j -and @($j.attachments).Count -gt 0) { return [int] @($j.attachments)[0].id }
    return 0
}
$aPdf = Get-AttId $idR
$aCon = Get-AttId $idV
$aExe = Get-AttId $idP
Add-Check '4.1' 'attach list: je Mail ein Anhang' ($aPdf -gt 0 -and $aCon -gt 0 -and $aExe -gt 0) ('PDF ' + $aPdf + ', CON.txt ' + $aCon + ', Programm ' + $aExe)

$r = Invoke-Pk @('attach', 'save', [string] $aPdf, '--subdir', 'rechnungen')
$f1 = Join-Path $wantExport 'rechnungen\Rechnung-2026-117.pdf'
Add-Check '4.2' 'attach save <PDF> --subdir rechnungen -> rechnungen/Rechnung-2026-117.pdf unter export_dir, Bytes wie im Original' ($r.Exit -eq 0 -and (Test-Has $r.Out 'as rechnungen/Rechnung-2026-117\.pdf below export_dir') -and (Test-Path -LiteralPath $f1 -PathType Leaf) -and ((Get-FileHash -LiteralPath $f1 -Algorithm SHA256).Hash -eq (Get-FileHash -InputStream (New-Object System.IO.MemoryStream (,$pdf)) -Algorithm SHA256).Hash)) $r.Text
$r = Invoke-Pk @('attach', 'save', [string] $aPdf, '--subdir', 'rechnungen')
Add-Check '4.3' 'zweimal dasselbe -> Rechnung-2026-117-2.pdf (nie ueberschreiben)' ($r.Exit -eq 0 -and (Test-Has $r.Out 'Rechnung-2026-117-2\.pdf') -and (Test-Path -LiteralPath (Join-Path $wantExport 'rechnungen\Rechnung-2026-117-2.pdf'))) $r.Text
$r = Invoke-Pk @('attach', 'save', [string] $aCon)
Add-Check '4.4' 'attach save <CON.txt> -> _CON.txt (Geraetename von Windows entschaerft)' ($r.Exit -eq 0 -and (Test-Has $r.Out 'as _CON\.txt below export_dir') -and (Test-Path -LiteralPath (Join-Path $wantExport '_CON.txt'))) $r.Text
$r = Invoke-Pk @('attach', 'save', [string] $aExe)
Add-Check '4.5' 'attach save <werkzeug.pdf.exe> -> werkzeug.pdf.exe.blocked mit Warnung' ($r.Exit -eq 0 -and (Test-Has $r.Out 'werkzeug\.pdf\.exe\.blocked') -and (Test-Has $r.All 'warning: an executable type') -and (Test-Path -LiteralPath (Join-Path $wantExport 'werkzeug.pdf.exe.blocked')) -and -not (Test-Path -LiteralPath (Join-Path $wantExport 'werkzeug.pdf.exe'))) $r.Text
$r = Invoke-Pk @('attach', 'save', [string] $aPdf, '--subdir', '..\ausserhalb')
Add-Check '4.6' 'attach save --subdir ..\ausserhalb -> denied (outside_export_dir), Exit 3, nichts angelegt' ($r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(outside_export_dir\)') -and -not (Test-Path -LiteralPath (Join-Path $script:Data 'ausserhalb'))) $r.Text
$r = Invoke-Pk @('attach', 'save', [string] $aPdf, '--key', 'test-agent')
$r2 = Invoke-Pk @('attach', 'save', [string] $aPdf, '--key', 'test-operator')
Add-Check '4.7' 'attach save mit --key: reader -> denied (missing_scope); operator (mail:export) -> erlaubt, als Trockenlauf' ($r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(missing_scope\)') -and $r2.Exit -eq 0 -and (Test-Has $r2.Out '^dry run: would save attachment')) ($r.Text + "`n" + $r2.Text)

$emlDir = Join-Path $script:Run 'export-eml'
$mboxDir = Join-Path $script:Run 'export-mbox'
$r = Invoke-Pk @('export', 'eml', '--to', $emlDir, '-a', 'demo')
$emls = @()
if (Test-Path -LiteralPath $emlDir) { $emls = @(Get-ChildItem -LiteralPath $emlDir -Recurse -File -Filter '*.eml') }
$csvFile = Join-Path $emlDir 'index.csv'
$csv = ''
if (Test-Path -LiteralPath $csvFile) { $csv = [System.IO.File]::ReadAllText($csvFile, $script:Utf8) }
$sameBytes = $emls.Count -eq 3
foreach ($e in $emls) {
    $m = [regex]::Match($e.Name, '^\d+-([0-9a-f]{12})\.eml$')
    $h = (Get-FileHash -LiteralPath $e.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    if (-not $m.Success -or -not $h.StartsWith($m.Groups[1].Value)) { $sameBytes = $false }
}
$ok = $r.Exit -eq 0 -and (Test-Has $r.Out 'messages: 3, skipped: 0') -and $sameBytes -and (Test-Has $csv '(?m)^id,message_id,date,folder,size,sha256,file') -and -not (Test-Has $csv 'Rechnung 2026-117|Vereinsnachrichten|Neues Werkzeug|example\.(org|net)>')
Add-Check '4.8' 'export eml: 3 Dateien (Name = Anfang des SHA-256 des Inhalts), index.csv ohne Betreff' $ok ($r.Text + "`nDateien: " + (($emls | ForEach-Object { $_.Name }) -join ', ') + "`n" + (Limit-Text $csv 6))
$r = Invoke-Pk @('export', 'mbox', '--to', $mboxDir, '-a', 'demo')
$mboxFiles = @()
if (Test-Path -LiteralPath $mboxDir) { $mboxFiles = @(Get-ChildItem -LiteralPath $mboxDir -Recurse -File) }
$fromLines = 0
foreach ($mf in $mboxFiles) { $fromLines += @([regex]::Matches([System.IO.File]::ReadAllText($mf.FullName, $script:Utf8), '(?m)^From ')).Count }
Add-Check '4.9' 'export mbox: eine Datei je Ordner (_imported.sbd\INBOX), 3 Trennzeilen "From "' ($r.Exit -eq 0 -and (Test-Has $r.Out 'messages: 3, skipped: 0') -and $mboxFiles.Count -eq 1 -and $fromLines -eq 3) ($r.Text + "`nDateien: " + (($mboxFiles | ForEach-Object { $_.FullName }) -join ', '))
$r = Invoke-Pk @('export', 'eml', '--to', $emlDir, '-a', 'demo')
Add-Check '4.10' 'export eml in denselben (nicht leeren) Ordner -> denied (target_not_fresh), Exit 3' ($r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(target_not_fresh\)')) $r.Text
$r = Invoke-Pk @('export', 'mbox', '--to', (Join-Path $script:Run 'export-key'), '-a', 'demo', '--key', 'test-operator')
Add-Check '4.11' 'export mit --key test-operator -> denied (human_only), Exit 3 (nur der Mensch kopiert das Archiv)' ($r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(human_only\)') -and -not (Test-Path -LiteralPath (Join-Path $script:Run 'export-key'))) $r.Text

$r = Invoke-Native $script:ExeFull @('--data-dir', $script:Data, '--config', $script:Cfg, 'account', 'add', 'zwei', '--imap', '127.0.0.1:1', '--user', 'zwei@example.org', '--insecure-test-server', '--password-from-env') @{ 'POSTKAUZ_PASSWORD_ZWEI' = 'unbenutzt-kein-server' }
$r1 = Invoke-Pk @('import', 'mbox', $mboxDir, '-a', 'zwei', '--quiet')
$r2 = Invoke-Pk @('import', 'mbox', $mboxDir, '-a', 'zwei', '--quiet')
Add-Check '4.12' 'Rundlauf: export mbox -> import mbox in Konto zwei: new: 3; zweiter Import new: 0' ($r.Exit -eq 0 -and $r1.Exit -eq 0 -and (Test-Has $r1.Out 'new: 3, existing: 0, skipped: 0, errors: 0') -and $r2.Exit -eq 0 -and (Test-Has $r2.Out 'new: 0,')) ($r1.Text + "`n" + $r2.Text)

if ($Interactive) {
    Write-Log ''
    Write-Log ('[Du] 4.13 Thunderbird: die mbox-Datei ' + (($mboxFiles | Select-Object -First 1 | ForEach-Object { $_.FullName })) + ' mit ImportExportTools NG importieren (docs/betrieb.md, Abschnitt 6.3).')
    $a = ''
    try { $a = [string] (Read-Host '      Sind die drei Mails mit Betreff, Text und Anhang lesbar? "ja" oder "nein" (leer: nicht geprueft)') } catch { $a = '' }
    $a = $a.Trim().ToLowerInvariant()
    if ($a -eq 'ja') { Add-Check '4.13' 'Thunderbird zeigt die exportierte mbox lesbar an (Akzeptanzkriterium 4)' $true }
    elseif ($a -eq 'nein') { Add-Check '4.13' 'Thunderbird zeigt die exportierte mbox lesbar an (Akzeptanzkriterium 4)' $false }
    else { Add-Skip '4.13' 'Thunderbird nicht geprueft' }
} else {
    Add-Skip '4.13' ('Thunderbird prueft der Betreiber (Datei: ' + (($mboxFiles | Select-Object -First 1 | ForEach-Object { $_.FullName })) + '; mit -Interactive fragt das Skript danach)')
}

# ------------------------------------------------ Schritt 5: Sicherung und Wiederherstellung
Write-Log ''
Write-Log '===== Schritt 5 (D7.5): audit export, backup create, restore, Vergleich'

$auditDir = Join-Path $script:Run 'audit-export'
$r = Invoke-Pk @('audit', 'export', '--to', $auditDir)
Add-Check '5.1' 'audit export vor der Sicherung (damit die Kopie Tage des Originals kennt, A10)' ($r.Exit -eq 0) $r.Text

$bak = Join-Path $script:Run 'sicherung'
$r = Invoke-Pk @('backup', 'create', '--to', $bak)
$m = [regex]::Match($r.Out, 'blobs:\s+(\d+) \(copied (\d+), already there (\d+)')
$nBlobs = -1
if ($m.Success) { $nBlobs = [int] $m.Groups[1].Value }
$ok = $r.Exit -eq 0 -and $m.Success -and $nBlobs -gt 0 -and ([int] $m.Groups[2].Value -eq $nBlobs) -and (Test-Path -LiteralPath (Join-Path $bak 'manifest.json')) -and (Test-Path -LiteralPath (Join-Path $bak 'ENCRYPT-ME.txt')) -and (Test-Has $r.Err 'warning: the backup is not encrypted')
Add-Check '5.2' 'backup create --to <ordner>: manifest.json, ENCRYPT-ME.txt, alle Blobs kopiert, Warnung "not encrypted" (Akzeptanzkriterium 6)' $ok $r.Text
$r = Invoke-Pk @('backup', 'create', '--to', $bak)
$m = [regex]::Match($r.Out, 'blobs:\s+(\d+) \(copied (\d+), already there (\d+)')
Add-Check '5.3' 'zweites backup create in denselben Ordner -> copied 0 (inkrementell), alter Schnappschuss ersetzt' ($r.Exit -eq 0 -and $m.Success -and [int] $m.Groups[2].Value -eq 0 -and [int] $m.Groups[3].Value -eq $nBlobs -and (Test-Has $r.Out 'replaced:\s+1 database snapshot')) $r.Text
$r = Invoke-Pk @('backup', 'create', '--to', (Join-Path $script:Run 'sicherung-key'), '--key', 'test-operator')
Add-Check '5.4' 'backup create mit --key -> denied (human_only), Exit 3' ($r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(human_only\)')) $r.Text
$r = Invoke-Pk @('backup', 'create', '--to', (Join-Path $script:Data 'sicherung'))
Add-Check '5.5' 'backup create in das Datenverzeichnis -> abgelehnt (backup_target), Exit 1' ($r.Exit -eq 1 -and (Test-Has $r.Err 'backup_target|data directory')) $r.Text

# Referenz fuer die Restores (Container, Heim-PC).
$ref = [ordered]@{ program = $ExpectVersion; created = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ'); searches = [ordered]@{}; messages = [ordered]@{}; blobs_verify = '' }
foreach ($q in $script:Queries) { $ref.searches[$q] = @((Get-SearchIds $script:Data $q).Ids) }
foreach ($mid in $script:MsgIds) { $ref.messages[$mid] = Get-MsgId $script:Data $mid }
$bo = Get-BlobReport $script:Data
$ref.blobs_verify = $bo.Verify
$refFile = Join-Path $script:Run 'D7_reference.json'
[System.IO.File]::WriteAllText($refFile, ($ref | ConvertTo-Json -Depth 5), $script:Utf8)
Add-Check '5.6' 'Referenz geschrieben (Suche, IDs, doctor --blobs=all des Originals)' ($bo.Result -eq 'ok' -and $bo.Verify) ('blobs: ' + $bo.Verify + "`nDatei: " + $refFile)

$rest = Join-Path $script:Run 'wiederhergestellt'
$r = Invoke-Native $script:ExeFull @('--config', $script:Cfg, 'restore', '--from', $bak, '--data-dir', $rest)
Add-Check '5.7' 'restore --from <sicherung> --data-dir <neu>: "restored:", neue Identitaet, doctor --blobs=all "result: ok"' ($r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^restored:') -and (Test-Has $r.Out 'identity:\s+new data directory id') -and (Test-Has $r.Out '(?m)^\s+result:\s+ok\s*$')) (Limit-Text ($r.Out + $r.Err) 40 4000)
Add-PathMask $rest '<Kopie>'
$det = ''
$ok = $true
foreach ($q in $script:Queries) {
    $got = Get-SearchIds $rest $q
    $want = @($ref.searches[$q])
    $same = ($got.Exit -eq 0) -and (($want -join ',') -eq ($got.Ids -join ','))
    $ok = $ok -and $same
    $det += $q + ': Original [' + ($want -join ',') + '], Kopie [' + ($got.Ids -join ',') + "]`n"
}
foreach ($mid in $script:MsgIds) {
    $got = Get-MsgId $rest $mid
    $ok = $ok -and ($got -eq [int] $ref.messages[$mid])
    $det += $mid + ': Original ' + $ref.messages[$mid] + ', Kopie ' + $got + "`n"
}
Add-Check '5.8' 'Kopie: Suche und Nachrichten-IDs identisch (Akzeptanzkriterium 3)' $ok $det
$b = Get-BlobReport $rest
Add-Check '5.9' 'Kopie: doctor --blobs=all wie im Original' ($b.Result -eq 'ok' -and $b.Verify -eq $bo.Verify) ('Original: ' + $bo.Verify + "`nKopie: " + $b.Verify)
$v1 = Get-AuditVerify $script:Data
$v2 = Get-AuditVerify $rest
Add-Check '5.10' 'audit verify in Original und Kopie -> ok, ohne Befund (A10: kein "changed")' ($v1.Status -eq 'ok' -and $v1.Problems -eq 0 -and $v2.Status -eq 'ok' -and $v2.Problems -eq 0) ($v1.Text + "`n" + $v2.Text)
$r = Invoke-PkIn $rest $script:Cfg @('doctor')
Add-Check '5.11' 'Kopie: doctor meldet fehlende Passwoerter (Schluesselbund des Originals wird nicht benutzt)' ((Test-Has $r.Out 'account demo has no password')) (Limit-Text $r.Text 30)

# Manipulierte Sicherung: Kopie des Ordners, ein Blob veraendert.
$bad = Join-Path $script:Run 'sicherung-manipuliert'
Copy-Item -LiteralPath $bak -Destination $bad -Recurse
$blob = @(Get-ChildItem -LiteralPath (Join-Path $bad 'blobs') -Recurse -File | Select-Object -First 1)
if ($blob.Count -gt 0) {
    $bytes = [System.IO.File]::ReadAllBytes($blob[0].FullName)
    $bytes[$bytes.Length - 1] = $bytes[$bytes.Length - 1] -bxor 0x01
    [System.IO.File]::WriteAllBytes($blob[0].FullName, $bytes)
}
$badTarget = Join-Path $script:Run 'wiederhergestellt-manipuliert'
$r = Invoke-Native $script:ExeFull @('--config', $script:Cfg, 'restore', '--from', $bad, '--data-dir', $badTarget)
Add-Check '5.12' 'restore einer manipulierten Sicherung (ein Byte eines Blobs) -> Exit 1, "nothing was restored", kein Zielordner' ($blob.Count -gt 0 -and $r.Exit -eq 1 -and (Test-Has $r.Err 'nothing was restored') -and -not (Test-Path -LiteralPath $badTarget)) ($r.Out + $r.Err)

# Paket fuer den Container und den Heim-PC: tar.exe (Windows 10 1803 und
# spaeter) schreibt Pfade mit "/"; Compress-Archive unter PowerShell 5.1
# nicht verlaesslich.
$tarExe = Join-Path $env:SystemRoot 'System32\tar.exe'
$pkg = Join-Path $script:Run 'D7_sicherung.tar'
if (Test-Path -LiteralPath $tarExe -PathType Leaf) {
    $r = Invoke-Native $tarExe @('-cf', $pkg, '-C', $script:Run, 'sicherung', 'D7_reference.json')
    Add-Check '5.13' 'Paket D7_sicherung.tar (Sicherung und Referenz) fuer den Restore im Linux-Container und auf dem Heim-PC' ($r.Exit -eq 0 -and (Test-Path -LiteralPath $pkg)) ('SHA-256 ' + (Get-FileHash -LiteralPath $pkg -Algorithm SHA256).Hash + "`n" + $r.Err)
} else {
    Add-Skip '5.13' 'tar.exe fehlt; den Ordner sicherung und D7_reference.json von Hand uebertragen'
}

# ------------------------------------------------ Schritt 7: Loeschkonzept
Write-Log ''
Write-Log '===== Schritt 7 (D7.7): purge, blob gc, audit rotate (nur Trockenlauf)'
$r = Invoke-Pk @('purge', '--older-than', '0s', '--dry-run', '--json')
$j = Get-Json $r.Out
$ok = $r.Exit -eq 0 -and $null -ne $j -and [int] $j.kept_imported -eq [int] $j.gone_from_server -and [int] $j.gone_from_server -ge 3 -and [int] $j.eligible -eq 0 -and [int] $j.purged -eq 0 -and $j.dry_run -eq $true
Add-Check '7.1' 'purge --dry-run --older-than 0s: alle Kandidaten sind importiert und bleiben (kept_imported), 0 wuerden geloescht (A13.2; abweichend von A03 D7.7)' $ok $r.Text
$r = Invoke-Pk @('purge', '--older-than', '0s')
$j2 = Get-Json (Invoke-Pk @('purge', '--older-than', '0s', '--dry-run', '--json')).Out
Add-Check '7.2' 'purge --older-than 0s (echt): loescht nichts aus dem importierten Archiv' ($r.Exit -eq 0 -and (Test-Has $r.Out 'deleted: 0 messages') -and $null -ne $j2 -and [int] $j2.kept_imported -eq [int] $j.kept_imported) $r.Text
$r = Invoke-Pk @('blob', 'gc', '--dry-run')
Add-Check '7.3' 'blob gc --dry-run: 0 ohne Verweis, nichts geaendert' ($r.Exit -eq 0 -and (Test-Has $r.Out 'without reference: 0') -and (Test-Has $r.Out 'dry run: nothing changed')) $r.Text
$r = Invoke-Pk @('audit', 'rotate', '--dry-run')
Add-Check '7.4' 'audit rotate --dry-run: nichts aelter als [audit] retain (365d)' ($r.Exit -eq 0 -and (Test-Has $r.Out 'nothing to rotate')) $r.Text
$ok = $true
$det = ''
foreach ($a in @(@('purge', '--older-than', '0s'), @('blob', 'gc'), @('audit', 'rotate'))) {
    $r = Invoke-Pk ($a + @('--key', 'test-operator'))
    $ok = $ok -and $r.Exit -eq 3 -and (Test-Has $r.Err 'denied \(human_only\)')
    $det += $r.Text + "`n"
}
Add-Check '7.5' 'purge, blob gc, audit rotate mit --key test-operator -> denied (human_only), Exit 3' $ok $det

# ------------------------------------------------ Schritt 6: Audit
Write-Log ''
Write-Log '===== Schritt 6 (D7.6): Audit des Laufs'
$r = Invoke-Pk @('audit', '--since', '30m', '--limit', '1000', '--json')
$ev = @(Get-JsonLines $r.Out)
$names = @('attachments.save', 'messages.export', 'backup.create', 'retention.purge', 'blobs.gc', 'audit.rotate')
$det = ''
$ok = $r.Exit -eq 0
foreach ($n in $names) {
    $c = @($ev | Where-Object { $_.operation -eq $n }).Count
    $ok = $ok -and $c -ge 1
    $det += $n + ': ' + $c + "`n"
}
$leak = Test-Has $r.Out 'Rechnung 2026-117|Vereinsnachrichten|Neues Werkzeug|Rechnung-2026-117\.pdf'
Add-Check '6.1' 'audit --since 30m: neue Operationen mit Endergebnis, ohne Betreff und Dateinamen' ($ok -and -not $leak) $det
$v = Get-AuditVerify $script:Data
Add-Check '6.2' 'audit verify am Ende -> ok' ($v.Status -eq 'ok' -and $v.Problems -eq 0) $v.Text

Write-Log ''
foreach ($f in $script:Fails) { Write-Log ('FAIL-Liste: ' + $f) }
Write-Log ('D7: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL, ' + $script:NSkip + ' SKIP')
if ($script:NFail -gt 0) { exit 1 }
exit 0
