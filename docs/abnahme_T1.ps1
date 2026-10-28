<#
.SYNOPSIS
    Abnahme T1 (PostKauz, Schnitt 3, Teile T1a und T1b): Update einer KOPIE
    der echten Datenbank von 0.2.0-rc8 auf 0.3.0-alpha.2.

.DESCRIPTION
    Gehoert zu docs/abnahme_T1.md (Schritte 0 bis 4) und
    auftraege/A03_teile.md, Teil T1b, Haltepunkt. Laeuft auf Windows mit
    Windows PowerShell 5.1, ohne Go, ohne Docker, nur mit den beiden
    Programmen (alt: 0.2.0-rc8, neu: 0.3.0-alpha.2 aus dist/).

    Das Skript
      - Schritt 0: prueft, dass kein postkauz-Prozess laeuft (die Aufgaben
        und die Claude-App stoppt der Mensch vorher von Hand, md Schritt 0);
      - Schritt 1: prueft den freien Platz (mindestens dreimal die Datenbank)
        und kopiert postkauz.db, -wal und -shm ZUSAMMEN aus -LiveDataDir
        nach -DataDir (nur lesend an der Quelle);
      - Schritt 2: fuehrt auf der Kopie mit dem ALTEN Programm audit verify,
        feste Suchen, accounts list und die Zaehler je Ordner aus, dann mit
        dem NEUEN Programm dieselben Kommandos plus doctor (das erste
        Kommando migriert, Dauer gemessen) und vergleicht die Ergebnisse;
      - Schritt 3 (nur mit -Sync und -SyncAccount): lesender Sync nur der
        INBOX eines Kontos mit dem neuen Programm, nur wenn kein Entwurf auf
        seine Kopie im Entwuerfe-Ordner wartet (sonst SKIP);
      - Schritt 4 (Umstellung des Dauerbetriebs) fuehrt das Skript NICHT
        aus; es nennt nur den Rueckweg (Snapshot).

    Sicherheitsregeln (A03_teile.md, Rahmen 5): -DataDir und -Config sind
    Pflicht. Das Skript bricht ab, wenn -DataDir das Standard-
    Datenverzeichnis (%LOCALAPPDATA%\postkauz) oder -LiveDataDir ist oder
    darin liegt, und wenn -Config die Standard-Konfiguration
    (%APPDATA%\postkauz\config.toml) oder -LiveConfig ist. Jedes Programm
    laeuft mit --data-dir und --config (ausser "version", das keine
    Datenbank oeffnet). -WorkDir darf weder im Live- noch im Standard-
    Datenverzeichnis liegen. Die Live-Datenbank wird nie geoeffnet, nur
    kopiert.

    Jede Pruefung ist eine Zeile "OK <Nr> <Beschreibung>",
    "FAIL <Nr> <Beschreibung>" oder "SKIP <Nr> <Beschreibung>", darunter
    eingerueckt die Ist-Ausgabe (gekuerzt, Mailadressen maskiert,
    Suchbegriffe nicht im Protokoll). Letzte Zeile:
    "T1: <n> OK, <m> FAIL, <k> SKIP". Exit 0 nur ohne FAIL.

.PARAMETER OldExe
    postkauz.exe 0.2.0-rc8 (der Stand im Dauerbetrieb).

.PARAMETER NewExe
    postkauz.exe 0.3.0-alpha.2 (aus dist/, der zu pruefende Stand).

.PARAMETER LiveDataDir
    Datenverzeichnis des Dauerbetriebs (Quelle der Kopie; wird nur
    gelesen).

.PARAMETER DataDir
    Neues, leeres Verzeichnis fuer die Kopie.

.PARAMETER Config
    Konfigurationsdatei fuer die Kopie (z. B. eine Kopie der echten, mit
    [audit] export_dir auf einen Ordner ausserhalb des echten Exports).
    policy.toml liegt daneben (oder fehlt: Standards).

.PARAMETER LiveConfig
    Konfigurationsdatei des Dauerbetriebs (nur zum Abgleich: -Config darf
    nicht dieselbe sein).

.PARAMETER WorkDir
    Ordner fuer das Protokoll T1_windows.txt.

.PARAMETER SearchFile
    Optional: Textdatei mit einer Suchanfrage je Zeile (die festen Suchen
    des Betreibers). Ohne Datei: has:attachment, is:unread, is:flagged,
    in:inbox. Die Anfragen stehen nicht im Protokoll.

.PARAMETER Sync
    Schritt 3 ausfuehren (lesender Sync der INBOX von -SyncAccount).

.PARAMETER SyncAccount
    Konto fuer Schritt 3.

.PARAMETER SkipCopy
    Schritt 1 ueberspringen: -DataDir enthaelt schon eine Kopie, die noch
    NIE mit dem neuen Programm geoeffnet wurde.

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File abnahme_T1.ps1 `
      -OldExe C:\PK\rc8\postkauz.exe -NewExe C:\PK\alpha2\postkauz.exe `
      -LiveDataDir C:\PostKauz\data -LiveConfig C:\PostKauz\config.toml `
      -DataDir C:\PK\t1\data -Config C:\PK\t1\config.toml -WorkDir C:\PK\t1
#>
param(
    [Parameter(Mandatory = $true)] [string] $OldExe,
    [Parameter(Mandatory = $true)] [string] $NewExe,
    [Parameter(Mandatory = $true)] [string] $LiveDataDir,
    [Parameter(Mandatory = $true)] [string] $DataDir,
    [Parameter(Mandatory = $true)] [string] $Config,
    [Parameter(Mandatory = $true)] [string] $LiveConfig,
    [Parameter(Mandatory = $true)] [string] $WorkDir,
    [string] $SearchFile = '',
    [switch] $Sync,
    [string] $SyncAccount = '',
    [switch] $SkipCopy
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

# Mailadressen und Schluessel nie ins Protokoll.
function Hide-Private {
    param([string] $Text)
    if ($null -eq $Text) { return '' }
    $t = [regex]::Replace($Text, 'pk_key_[A-Za-z0-9_-]+', 'pk_key_********')
    return [regex]::Replace($t, '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}', '<adresse>')
}

function Write-Log {
    param([string] $Text)
    $t = Hide-Private $Text
    if ($script:Log) { [System.IO.File]::AppendAllText($script:Log, $t + "`r`n", $script:Utf8) }
    try { Write-Host $t } catch { }
}

function Limit-Text {
    param([string] $Text, [int] $MaxLines = 20, [int] $MaxChars = 1500)
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

function Add-Skip {
    param([string] $Nr, [string] $Desc)
    $script:NSkip++
    Write-Log ('SKIP ' + $Nr + ' ' + $Desc)
}

function Stop-Run {
    param([string] $Why)
    Write-Log ('ABBRUCH: ' + $Why)
    Write-Log ('T1: ' + $script:NOk + ' OK, ' + ($script:NFail + 1) + ' FAIL, ' + $script:NSkip + ' SKIP')
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
    param([string] $File, [string[]] $ArgList, [int] $TimeoutSec = 1800)
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
    $psi.WorkingDirectory = $WorkDir
    # Nichts aus der Umgebung darf auf das Live-Verzeichnis zeigen.
    foreach ($k in @('POSTKAUZ_KEY', 'POSTKAUZ_KEY_FILE', 'POSTKAUZ_DATA_DIR', 'POSTKAUZ_CONFIG', 'POSTKAUZ_POLICY')) {
        if ($psi.EnvironmentVariables.ContainsKey($k)) { $psi.EnvironmentVariables.Remove($k) }
    }
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
    return New-Object PSObject -Property @{ Exit = $code; Out = [string] $to.Result; Err = [string] $te.Result; TimedOut = $timedOut;
        Ms = $sw.ElapsedMilliseconds }
}

# Programm (alt oder neu) immer mit Datenverzeichnis und Konfiguration der Kopie.
function Invoke-Pk {
    param([string] $Exe, [string[]] $A, [string] $Shown = '')
    $all = @('--data-dir', $script:Data, '--config', $script:Cfg) + $A
    $r = Invoke-Native $Exe $all
    if (-not $Shown) { $Shown = ($A -join ' ') }
    $text = '> postkauz ' + $Shown + "`n" + $r.Out
    if ($r.Err) { $text = $text.TrimEnd() + "`n[stderr] " + $r.Err }
    if ($r.TimedOut) { $text = $text.TrimEnd() + "`n[Zeitgrenze erreicht, Prozess beendet]" }
    $text = $text.TrimEnd() + "`n[exit " + $r.Exit + ', ' + $r.Ms + ' ms]'
    $r | Add-Member -NotePropertyName Text -NotePropertyValue $text
    return $r
}

function Get-Json {
    param([string] $Text)
    try { return ($Text.Trim() | ConvertFrom-Json) } catch { return $null }
}

# NDJSON-Zeilen mit Feld id (search --json).
function Get-Ids {
    param([string] $Text)
    $ids = New-Object System.Collections.Generic.List[string]
    foreach ($line in ($Text -split "`r?`n")) {
        $t = $line.Trim()
        if (-not $t.StartsWith('{')) { continue }
        $o = Get-Json $t
        if ($null -ne $o -and $null -ne $o.id) { $ids.Add([string] $o.id) }
    }
    return ($ids -join ',')
}

function Get-FullPath {
    param([string] $P)
    return [System.IO.Path]::GetFullPath($P).TrimEnd('\').ToLowerInvariant()
}

function Test-Inside {
    param([string] $Child, [string] $Parent)
    $c = Get-FullPath $Child
    $p = Get-FullPath $Parent
    return ($c -eq $p) -or $c.StartsWith($p + '\')
}

# Zustand einer Kopie: Konten, Ordner je Konto, Suchen, audit verify.
function Get-State {
    param([string] $Exe, [string] $Nr, [string] $Label)
    $st = @{}
    $v = Invoke-Pk $Exe @('audit', 'verify', '--json')
    $j = Get-Json $v.Out
    $st.Verify = $j
    Add-Check ($Nr + '.1') ($Label + ': audit verify ok') (($v.Exit -eq 0) -and ($null -ne $j) -and ($j.status -eq 'ok')) $v.Text

    $a = Invoke-Pk $Exe @('accounts', 'list', '--json')
    $aj = Get-Json $a.Out
    $names = @()
    if ($null -ne $aj) { foreach ($x in $aj.accounts) { $names += ([string] $x.name + '=' + [string] $x.email) } }
    $st.Accounts = ($names | Sort-Object) -join ';'
    $st.AccountNames = @()
    if ($null -ne $aj) { foreach ($x in $aj.accounts) { $st.AccountNames += [string] $x.name } }
    Add-Check ($Nr + '.2') ($Label + ': accounts list') (($a.Exit -eq 0) -and ($names.Count -gt 0)) $a.Text

    $boxes = @()
    $okBoxes = $true
    foreach ($n in $st.AccountNames) {
        $m = Invoke-Pk $Exe @('mailbox', 'list', '--account', $n, '--json')
        $mj = Get-Json $m.Out
        if (($m.Exit -ne 0) -or ($null -eq $mj)) { $okBoxes = $false; Write-Log (Limit-Text $m.Text); continue }
        foreach ($b in $mj.mailboxes) { $boxes += ($n + '|' + [string] $b.path + '|' + [string] $b.mirrored + '|' + [string] $b.unread) }
    }
    $st.Boxes = ($boxes | Sort-Object) -join ';'
    Add-Check ($Nr + '.3') ($Label + ': Zaehler je Ordner (' + $boxes.Count + ' Ordner)') ($okBoxes -and ($boxes.Count -gt 0)) ''

    $st.Searches = @()
    $i = 0
    $okSearch = $true
    foreach ($q in $script:Queries) {
        $i++
        # "--" vor der Anfrage: Eine Anfrage wie -is:read ist kein Flag.
        $s = Invoke-Pk $Exe @('search', '--limit', '100', '--json', '--', $q) ('search --limit 100 --json -- <Suche ' + $i + '>')
        if ($s.Exit -ne 0) { $okSearch = $false; Write-Log ('      Suche ' + $i + ': exit ' + $s.Exit) }
        $st.Searches += (Get-Ids $s.Out)
    }
    $hits = 0
    foreach ($x in $st.Searches) { if ($x) { $hits += ($x -split ',').Count } }
    Add-Check ($Nr + '.4') ($Label + ': ' + $script:Queries.Count + ' feste Suchen (' + $hits + ' Treffer)') $okSearch ''
    return $st
}

# ---------------------------------------------------------------- Pruefungen vor dem Start


$defaultData = ''
if ($env:LOCALAPPDATA) { $defaultData = Join-Path $env:LOCALAPPDATA 'postkauz' }
$defaultCfg = ''
if ($env:APPDATA) { $defaultCfg = Join-Path (Join-Path $env:APPDATA 'postkauz') 'config.toml' }

if ((Test-Inside $DataDir $LiveDataDir) -or (Test-Inside $LiveDataDir $DataDir)) { Stop-Run '-DataDir ist das Live-Datenverzeichnis oder liegt darin (bzw. umgekehrt).' }
if ($defaultData -and (Test-Inside $DataDir $defaultData)) { Stop-Run ('-DataDir liegt im Standard-Datenverzeichnis ' + $defaultData + '.') }
if ((Get-FullPath $Config) -eq (Get-FullPath $LiveConfig)) { Stop-Run '-Config ist die Konfiguration des Dauerbetriebs (-LiveConfig).' }
if ($defaultCfg -and ((Get-FullPath $Config) -eq (Get-FullPath $defaultCfg))) { Stop-Run ('-Config ist die Standard-Konfiguration ' + $defaultCfg + '.') }
if (Test-Inside $Config $LiveDataDir) { Stop-Run '-Config liegt im Live-Datenverzeichnis.' }
foreach ($f in @($OldExe, $NewExe, $Config)) {
    if (-not (Test-Path -LiteralPath $f)) { Stop-Run ('Datei fehlt: ' + $f) }
}
if ((Test-Inside $WorkDir $LiveDataDir) -or ($defaultData -and (Test-Inside $WorkDir $defaultData))) { Stop-Run '-WorkDir liegt im Live- oder Standard-Datenverzeichnis.' }
if ($Sync -and -not $SyncAccount) { Stop-Run '-Sync braucht -SyncAccount.' }

# Protokoll erst nach den Pruefungen der Pfade anlegen.
if (-not (Test-Path -LiteralPath $WorkDir)) { New-Item -ItemType Directory -Path $WorkDir | Out-Null }
$script:Log = Join-Path $WorkDir 'T1_windows.txt'
[System.IO.File]::WriteAllText($script:Log, '', $script:Utf8)
Write-Log ('Abnahme T1 ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + ', Rechner ' + $env:COMPUTERNAME)
Write-Log ('Programm alt: ' + $OldExe)
Write-Log ('Programm neu: ' + $NewExe)
$script:Data = [System.IO.Path]::GetFullPath($DataDir)
$script:Cfg = [System.IO.Path]::GetFullPath($Config)

$script:Queries = @('has:attachment', 'is:unread', 'is:flagged', 'in:inbox')
if ($SearchFile) {
    if (-not (Test-Path -LiteralPath $SearchFile)) { Stop-Run ('Suchdatei fehlt: ' + $SearchFile) }
    $script:Queries = @(Get-Content -LiteralPath $SearchFile -Encoding UTF8 | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne '' -and -not $_.StartsWith('#') })
    if ($script:Queries.Count -eq 0) { Stop-Run 'Suchdatei ohne Anfragen.' }
}

$vo = Invoke-Native $OldExe @('version')
$vn = Invoke-Native $NewExe @('version')
Add-Check '0.0' 'Versionen: alt 0.2.0-rc8, neu 0.3.0-alpha.2' (($vo.Out -match '0\.2\.0-rc8') -and ($vn.Out -match '0\.3\.0-alpha\.2')) ('alt: ' + $vo.Out.Trim() + "`nneu: " + $vn.Out.Trim())

# ---------------------------------------------------------------- Schritt 0

$procs = @(Get-Process -Name postkauz -ErrorAction SilentlyContinue)
Add-Check '0.4' 'kein postkauz-Prozess laeuft (Aufgaben und Claude-App gestoppt, md Schritt 0)' ($procs.Count -eq 0) (($procs | ForEach-Object { 'PID ' + $_.Id }) -join "`n")
if ($procs.Count -gt 0) { Stop-Run 'Erst alle PostKauz-Prozesse beenden (docs/abnahme_T1.md, Schritt 0).' }

# ---------------------------------------------------------------- Schritt 1

$liveDb = Join-Path $LiveDataDir 'postkauz.db'
$copyDb = Join-Path $script:Data 'postkauz.db'
if (-not $SkipCopy) {
    if (-not (Test-Path -LiteralPath $liveDb)) { Stop-Run ('keine Datenbank in -LiveDataDir: ' + $liveDb) }
    if ((Test-Path -LiteralPath $script:Data) -and @(Get-ChildItem -LiteralPath $script:Data -Force).Count -gt 0) {
        Stop-Run '-DataDir ist nicht leer (fuer eine vorhandene, unberuehrte Kopie: -SkipCopy).'
    }
    New-Item -ItemType Directory -Path $script:Data -Force | Out-Null
    $size = 0
    foreach ($s in @('', '-wal', '-shm')) {
        $f = $liveDb + $s
        if (Test-Path -LiteralPath $f) { $size += (Get-Item -LiteralPath $f).Length }
    }
    $drive = [System.IO.Path]::GetPathRoot($script:Data)
    $free = (New-Object System.IO.DriveInfo $drive).AvailableFreeSpace
    Add-Check '1.1' ('freier Platz mindestens dreimal die Datenbank (' + [math]::Round($size / 1MB) + ' MB, frei ' + [math]::Round($free / 1MB) + ' MB)') ($free -ge 3 * $size) ''
    if ($free -lt 3 * $size) { Stop-Run 'zu wenig freier Platz.' }
    $copied = $true
    foreach ($s in @('', '-wal', '-shm')) {
        $f = $liveDb + $s
        if (Test-Path -LiteralPath $f) {
            Copy-Item -LiteralPath $f -Destination ($copyDb + $s)
            if ((Get-Item -LiteralPath $f).Length -ne (Get-Item -LiteralPath ($copyDb + $s)).Length) { $copied = $false }
        }
    }
    # Nach dem Kopieren: lief in der Zwischenzeit ein Prozess an, ist die Kopie unbrauchbar.
    $procs = @(Get-Process -Name postkauz -ErrorAction SilentlyContinue)
    Add-Check '1.2' 'Datenbank, -wal und -shm zusammen kopiert, kein Prozess dazwischen' ($copied -and $procs.Count -eq 0) ''
    if (-not $copied -or $procs.Count -gt 0) { Stop-Run 'Kopie unvollstaendig oder ein Prozess lief an.' }
} else {
    if (-not (Test-Path -LiteralPath $copyDb)) { Stop-Run ('-SkipCopy, aber keine Kopie in ' + $copyDb) }
    if (@(Get-ChildItem -LiteralPath $script:Data -Filter 'postkauz.db.pre-*.bak').Count -gt 0) { Stop-Run 'Die Kopie wurde schon migriert (Snapshot vorhanden); neu kopieren.' }
    Add-Skip '1.1' 'Kopie vorhanden (-SkipCopy)'
}

# ---------------------------------------------------------------- Schritt 2: alter Stand

Write-Log ''
Write-Log '--- Schritt 2a: Kopie mit dem alten Programm (0.2.0-rc8)'
$old = Get-State $OldExe '2a' 'alt'

# ---------------------------------------------------------------- Schritt 2: neuer Stand

Write-Log ''
Write-Log '--- Schritt 2b: dieselbe Kopie mit dem neuen Programm (0.3.0-alpha.2), migriert beim ersten Kommando'
$m = Invoke-Pk $NewExe @('accounts', 'list')
$snaps = @(Get-ChildItem -LiteralPath $script:Data -Filter 'postkauz.db.pre-*.bak' -ErrorAction SilentlyContinue)
Add-Check '2b.0' ('Migrationen laufen (Dauer samt Snapshot ' + $m.Ms + ' ms), Snapshot angelegt') (($m.Exit -eq 0) -and ($snaps.Count -ge 1)) ($m.Text + "`n" + (($snaps | ForEach-Object { $_.Name }) -join "`n"))
if ($m.Exit -ne 0) { Stop-Run 'Migration gescheitert; der Snapshot (falls vorhanden) und die Live-Datenbank sind unberuehrt.' }

$d = Invoke-Pk $NewExe @('doctor', '--json')
$dj = Get-Json $d.Out
$dOk = ($null -ne $dj) -and ($dj.database -eq 'ok') -and ([int] $dj.schema_version -eq [int] $dj.schema_known) -and ([int] $dj.schema_version -ge 19) -and ([int] $dj.audit_unfinished_calls -eq 0)
Add-Check '2b.5' 'doctor: Datenbank ok, Schema 19 (= bekannt), kein Aufruf ohne Abschluss' $dOk $d.Text

$new = Get-State $NewExe '2b' 'neu'

Write-Log ''
Write-Log '--- Vergleich alt und neu'
Add-Check '2c.1' 'audit verify: gleiche Zahl Tage, gleiches Kettenende' (($null -ne $old.Verify) -and ($null -ne $new.Verify) -and
    ($old.Verify.days_checked -eq $new.Verify.days_checked) -and ([string] $old.Verify.chain_head -eq [string] $new.Verify.chain_head)) ('alt: ' + $old.Verify.days_checked + ' Tage, Kette ' + $old.Verify.chain_head + "`nneu: " + $new.Verify.days_checked + ' Tage, Kette ' + $new.Verify.chain_head)
Add-Check '2c.2' 'accounts list gleich' ($old.Accounts -eq $new.Accounts) ''
Add-Check '2c.3' 'Zaehler je Ordner gleich (gespiegelt, ungelesen)' ($old.Boxes -eq $new.Boxes) ''
$same = $true
for ($i = 0; $i -lt $old.Searches.Count; $i++) {
    if ($old.Searches[$i] -ne $new.Searches[$i]) { $same = $false; Write-Log ('      Suche ' + ($i + 1) + ' liefert andere Treffer') }
}
Add-Check '2c.4' 'feste Suchen: gleiche Treffer in gleicher Reihenfolge' $same ''

# Neue Audit-Ansicht: je Aufruf Start und Abschluss. Das neue Programm hat
# accounts list zweimal aufgerufen (Migration, Zustand); alte Eintraege
# (rc8, Dauerbetrieb) sind einzelne "ok" ohne Verweis und zaehlen nicht.
$au = Invoke-Pk $NewExe @('audit', '--since', '6h', '--operation', 'accounts.list', '--raw', '--limit', '1000', '--json')
$started = 0
$ended = 0
foreach ($line in ($au.Out -split "`r?`n")) {
    $o = Get-Json $line
    if ($null -eq $o) { continue }
    if ($o.result -eq 'started') { $started++ }
    if ($null -ne $o.ref_id -and [int] $o.ref_id -gt 0) { $ended++ }
}
Add-Check '2c.5' ('Audit neu: je Aufruf ein Start und ein Abschluss (accounts list: ' + $started + ' Starts, ' + $ended + ' Abschluesse)') (($au.Exit -eq 0) -and ($started -eq 2) -and ($ended -eq 2)) ''

# ---------------------------------------------------------------- Schritt 3

Write-Log ''
if (-not $Sync) {
    Add-Skip '3' 'lesender Sync (nur mit -Sync -SyncAccount <konto> und Zustimmung des Betreibers)'
} else {
    $dl = Invoke-Pk $NewExe @('draft', 'list', '--account', $SyncAccount, '--status', 'draft', '--status', 'pending_approval', '--status', 'ready', '--limit', '500', '--json')
    $dlj = Get-Json $dl.Out
    $waiting = 0
    if ($null -ne $dlj) { foreach ($x in $dlj.drafts) { if ($x.remote -eq 'pending') { $waiting++ } } }
    if (($dl.Exit -ne 0) -or ($null -eq $dlj)) {
        Add-Check '3.1' 'draft list lesbar' $false $dl.Text
    } elseif ($waiting -gt 0) {
        Add-Skip '3' ('lesender Sync: ' + $waiting + ' Entwurf/Entwuerfe warten auf ihre Kopie; ein Sync wuerde sie im echten Postfach ablegen')
    } else {
        Add-Check '3.1' 'kein Entwurf wartet auf seine Kopie' $true ''
        $sy = Invoke-Pk $NewExe @('sync', '--account', $SyncAccount, '--mailbox', 'INBOX')
        Add-Check '3.2' ('lesender Sync der INBOX (' + $sy.Ms + ' ms)') ($sy.Exit -eq 0) $sy.Text
        $v2 = Invoke-Pk $NewExe @('audit', 'verify', '--json')
        $v2j = Get-Json $v2.Out
        Add-Check '3.3' 'audit verify nach dem Sync ok' (($v2.Exit -eq 0) -and ($null -ne $v2j) -and ($v2j.status -eq 'ok')) $v2.Text
    }
}

# ---------------------------------------------------------------- Schritt 4 (nur Hinweis)

Write-Log ''
Write-Log '--- Schritt 4 (Umstellung des Dauerbetriebs) fuehrt dieses Skript nicht aus: erst nach dem Ja des Betreibers, md Schritt 4.'
if ($snaps.Count -ge 1) { Write-Log ('    Rueckweg der KOPIE: Snapshot ' + $snaps[0].Name + ' (gilt nur bis zum ersten Audit-Export mit der neuen Version).') }

Write-Log ''
foreach ($f in $script:Fails) { Write-Log ('FAIL-Liste: ' + $f) }
Write-Log ('T1: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL, ' + $script:NSkip + ' SKIP')
if ($script:NFail -gt 0) { exit 1 }
exit 0
