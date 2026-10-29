<#
.SYNOPSIS
    Abnahme D8 (PostKauz, Schnitt 3, Teil T5): HTTP-MCP (mcp --http) mit
    Schluessel und mit OAuth, lokal und ueber das private Netz.

.DESCRIPTION
    Gehoert zu docs/abnahme_D8.md und auftraege/A03_teile.md, Teil T5,
    Haltepunkt D8 (A03 Teil D, D8), vorlaeufig mit einem PC des Betreibers
    als Knoten im privaten Netz. Laeuft auf Windows mit Windows PowerShell
    5.1, ohne Go, ohne Docker, nur mit dem Programm aus dist/ (-ExePath).
    Teil C (aus der Cloud-Sitzung) steht in abnahme_D8_cloud.sh.

    Teil A (immer, offline, ohne Tailscale): frisches Datenverzeichnis
    -DataDir mit Konfiguration -Config (beide legt das Skript an), [http]
    im Modus key auf 127.0.0.1:-Port. Schluessel cloud-reader (reader, 1
    Tag). Geprueft: doctor zeigt [http], 0.0.0.0 ohne allow_non_loopback
    bricht ab, mcp --http startet, /healthz, Metadaten, 401 mit
    WWW-Authenticate, falscher Schluessel 401, GET 405, fremder Host 421,
    zu grosse Anfrage 413, MCP-Sitzung mit der Probe (abnahme_D8_probe.py,
    falls Python vorhanden: 12 Tools, Suche in der Huelle, Audit mit
    transport http), Widerruf -> 403 denied (key_revoked), Audit und Log
    ohne Schluessel.

    Teil B (nur mit -NodeName): derselbe Server im Modus key fuer den
    Namen des PCs im Tailnet (-NodeName, z. B. pc.<tailnet>.ts.net). Zuerst
    liest das Skript tailscale serve status --json; ist HTTPS 443 schon
    belegt, bricht es mit Hinweis ab (nichts wird ueberschrieben). Mit
    Zustimmung je Schritt: tailscale serve --bg <Port>, Pruefung ueber
    https://<NodeName>/, dann laeuft der Server, bis der Betreiber Enter
    drueckt (in dieser Zeit fuehrt die Pruefsitzung Teil C aus der
    Cloud-Sitzung aus). Danach tailscale serve --https=443 off (mit
    Zustimmung) und Widerruf des Schluessels. Der Schluessel fuer Teil C
    liegt waehrenddessen in <WorkDir>\lauf-...\keys\cloud-reader-c.txt
    (danach geloescht).

    Teil D (nur mit -Issuer und -NodeName): Modus oauth gegen den
    Autorisierungsserver (Authelia, docs/betrieb.md 4.6). Der Betreiber
    meldet sich im Browser an (Probe, Rueckleitung auf
    http://localhost:-CallbackPort/callback). Ohne -Subject endet Teil D
    nach der Anmeldung mit 403 und nennt das Subjekt; dann mit -Subject
    wiederholen. Mit -Subject: Tools nach dem Schluessel, Audit mit iss/sub,
    Ablauf des Tokens (5 min) abwarten -> 401, Refresh -> 200, Widerruf ->
    403 denied (key_revoked). tailscale serve wie in Teil B.

    Aufraeumen: Teile A, B und D laufen in try/finally. Am Ende jedes
    Teils und bei jedem Abbruch (Fehler, Strg+C) schaltet das Skript
    tailscale serve wieder aus (bei Abbruch ohne weitere Frage; das sagt
    schon die Frage vor dem Einschalten), beendet mcp --http, widerruft die
    angelegten Schluessel und loescht Schluessel- und Refresh-Dateien.

    Sicherheitsregeln (A03_teile.md, Rahmen 5): -DataDir, -Config und
    -LivePath sind Pflicht (wie bei D7). Abbruch ohne Aenderung, wenn
    -DataDir, -Config oder -WorkDir im Standard-Datenverzeichnis, im
    Standard-Konfigurationsordner oder in einem mit -LivePath genannten
    Pfad liegen (oder umgekehrt); ein Datei-Eintrag in -LivePath (etwa die
    Live-Konfiguration) schuetzt auch seinen Ordner. Dazu eine Markerdatei
    gegen Junction und subst (auch fuer den Ordner von -Config) und ein
    Abbruch, wenn neben -Config schon eine policy.toml liegt. Nie die
    Live-Datenbank. Kein Mailkonto, kein Passwort: mcp --http kennt keins.

    Jede Pruefung ist eine Zeile "OK <Nr> <Beschreibung>",
    "FAIL <Nr> <Beschreibung>" oder "SKIP <Nr> <Grund>", darunter
    eingerueckt die Ist-Ausgabe (gekuerzt; Schluessel, Token, Pfade,
    -NodeName (<knoten>), -Issuer (<issuer>, <issuer-host>), jeder Name
    unter ts.net (<name>.ts.net) und Tailnet-Adressen (<tailnet-ip>,
    <tailnet-ip6>) maskiert). Letzte Zeile: "D8: <n> OK, <m> FAIL, <k> SKIP".
    Exit 0 nur ohne FAIL. Endet die Probe ohne ihre Schlusszeile oder mit
    falschem Exit-Code (Abbruch, Syntax- oder Importfehler), steht
    "FAIL <Nr>.ende" im Protokoll.

.PARAMETER ExePath
    postkauz.exe des zu pruefenden Stands (aus dist/, umbenannt).

.PARAMETER DataDir
    Neues, leeres (oder noch nicht vorhandenes) Datenverzeichnis.

.PARAMETER Config
    Pfad der Konfigurationsdatei, die das Skript neu anlegt (darf noch nicht
    existieren). Teil B und D schreiben daneben eigene Dateien
    (<Config>.tailnet.toml, <Config>.oauth.toml).

.PARAMETER WorkDir
    Ordner fuer das Protokoll D8_windows.txt (exklusiv neu angelegt) und den
    Laufordner lauf-<Datum-Uhrzeit>. Kein Reparse Point.

.PARAMETER LivePath
    Pflicht: Pfade des Dauerbetriebs (Live-Datenverzeichnis,
    Live-Konfiguration), durch Komma getrennt; Schutz gegen Verwechslung.
    Ein Datei-Eintrag schuetzt auch den Ordner, in dem die Datei liegt.

.PARAMETER ExpectVersion
    Erwartete Version von -ExePath (Standard 0.3.0-alpha.8c). Verglichen
    wird bis zum Ende der Versionsangabe: 0.3.0-alpha.8 passt nicht auf
    0.3.0-alpha.8b oder 0.3.0-alpha.8c.

.PARAMETER Port
    Port fuer mcp --http auf 127.0.0.1 (Standard 18787; nicht der
    Standardport 8787, damit kein laufender Dienst gestoert wird).

.PARAMETER Python
    python.exe fuer die Probe. Ohne Angabe sucht das Skript python.exe bzw.
    py.exe im PATH; ohne Python: SKIP der Probe-Schritte mit Grund.

.PARAMETER Probe
    abnahme_D8_probe.py. Standard: neben diesem Skript.

.PARAMETER NodeName
    Teil B und D: MagicDNS-Name dieses PCs im Tailnet.

.PARAMETER Issuer
    Teil D: Aussteller des Autorisierungsservers ([http] issuer), z. B.
    https://<knoten>.<tailnet>.ts.net:8443.

.PARAMETER Subject
    Teil D: Subjekt (sub) des Testkontos im Autorisierungsserver; ohne
    Angabe nennt Teil D es nach der Anmeldung.

.PARAMETER ClientId
    Teil D: Client-ID im Autorisierungsserver (Standard claude-code).

.PARAMETER CallbackPort
    Teil D: Port der Rueckleitung http://localhost:<Port>/callback
    (Standard 33418, wie im Authelia-Client eingetragen).

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File abnahme_D8.ps1 `
      -ExePath C:\PK\d8\postkauz.exe -DataDir C:\PK\d8\daten -Config C:\PK\d8\config.toml `
      -WorkDir C:\PK\d8 -LivePath C:\PostKauz\data,C:\PostKauz\config.toml

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File abnahme_D8.ps1 `
      -ExePath C:\PK\d8\postkauz.exe -DataDir C:\PK\d8b\daten -Config C:\PK\d8b\config.toml `
      -WorkDir C:\PK\d8b -LivePath C:\PostKauz\data,C:\PostKauz\config.toml -NodeName pc.example.ts.net
#>
param(
    [Parameter(Mandatory = $true)] [string] $ExePath,
    [Parameter(Mandatory = $true)] [string] $DataDir,
    [Parameter(Mandatory = $true)] [string] $Config,
    [Parameter(Mandatory = $true)] [string] $WorkDir,
    [Parameter(Mandatory = $true)] [string[]] $LivePath,
    [string] $ExpectVersion = '0.3.0-alpha.8c',
    [int] $Port = 18787,
    [string] $Python = '',
    [string] $Probe = '',
    [string] $NodeName = '',
    [string] $Issuer = '',
    [string] $Subject = '',
    [string] $ClientId = 'claude-code',
    [int] $CallbackPort = 33418
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

# Platzhalter fuer Hostnamen und URLs (-NodeName, -Issuer): woertlich, ohne
# GetFullPath (das machte aus pc.<tailnet>.ts.net einen Pfad unter dem
# aktuellen Ordner, und der blanke Name blieb stehen). Bei einer URL auch
# der Host mit und ohne Port. Laengste zuerst.
$script:HostMask = New-Object System.Collections.Generic.List[object]
function Add-HostMask {
    param([string] $Text, [string] $Name, [string] $HostName = '')
    if (-not $Text) { return }
    if (-not $HostName) { $HostName = $Name }
    $script:HostMask.Add((New-Object PSObject -Property @{ Text = $Text.TrimEnd('/'); Name = $Name }))
    $u = $null
    if ([System.Uri]::TryCreate($Text, [System.UriKind]::Absolute, [ref] $u) -and $u.Host) {
        foreach ($v in @($u.Authority, $u.Host)) {
            if ($v) { $script:HostMask.Add((New-Object PSObject -Property @{ Text = $v; Name = $HostName })) }
        }
    }
    $sorted = @($script:HostMask | Sort-Object -Property @{ Expression = { $_.Text.Length }; Descending = $true })
    $script:HostMask = New-Object System.Collections.Generic.List[object]
    foreach ($x in $sorted) { $script:HostMask.Add($x) }
}

# Schluessel, Token, Auth-Keys, Mailadressen, Pfade und Knotenname nie ins
# Protokoll.
function Hide-Private {
    param([string] $Text)
    if ($null -eq $Text) { return '' }
    $t = [regex]::Replace($Text, 'pk_key_[A-Za-z0-9_-]+', 'pk_key_********')
    $t = [regex]::Replace($t, 'tskey-[A-Za-z0-9_-]+', 'tskey-********')
    $t = [regex]::Replace($t, 'eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+', '<token>')
    $t = [regex]::Replace($t, '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}', '<adresse>')
    foreach ($m in $script:HostMask) {
        $t = [regex]::Replace($t, [regex]::Escape($m.Text), $m.Name, 'IgnoreCase')
    }
    foreach ($m in $script:PathMask) {
        $t = [regex]::Replace($t, [regex]::Escape($m.Text), $m.Name, 'IgnoreCase')
    }
    # Allgemeine Muster wie in abnahme_D8_cloud.sh: jeder MagicDNS-Name
    # (*.ts.net), Tailnet-Adressen IPv6 (fd7a:115c:a1e0::/48) und IPv4
    # (100.64.0.0/10).
    $t = [regex]::Replace($t, '(?i)\b[a-z0-9-]+(\.[a-z0-9-]+)*\.ts\.net\b', '<name>.ts.net')
    $t = [regex]::Replace($t, '(?i)\bfd7a:115c:a1e0:[0-9a-f:]*', '<tailnet-ip6>')
    $t = [regex]::Replace($t, '\b100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.\d{1,3}\.\d{1,3}\b', '<tailnet-ip>')
    return $t
}

function Write-Log {
    param([string] $Text)
    $t = Hide-Private $Text
    if ($script:Log) { [System.IO.File]::AppendAllText($script:Log, $t + "`r`n", $script:Utf8) }
    try { Write-Host $t } catch { }
}

# Kuerzt Text. Immer erst maskieren (Hide-Private), dann kuerzen: Ein
# abgeschnittenes Token passt nicht mehr auf das Muster und bliebe teilweise
# stehen.
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

# Abbruch. Waehrend der Teile A, B und D ($script:Main) wirft Stop-Run nur;
# der Block um die Teile raeumt im finally auf (Clear-Part) und ruft
# Stop-Run danach noch einmal fuer Protokoll und Exit 2.
$script:Main = $false
function Stop-Run {
    param([string] $Why)
    if ($script:Main) { throw ('D8-ABBRUCH: ' + $Why) }
    if ($script:Log) {
        Write-Log ('ABBRUCH: ' + $Why)
        Write-Log ('D8: ' + $script:NOk + ' OK, ' + ($script:NFail + 1) + ' FAIL, ' + $script:NSkip + ' SKIP')
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
    [System.IO.File]::WriteAllText($marker, 'Abnahme D8: Marker, darf geloescht werden', $script:Utf8)
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

# ---------------------------------------------------------------- Hilfen fuer HTTP

# Eine HTTP-Anfrage ohne Proxy-Raten: HttpWebRequest (Windows PowerShell 5.1
# kann den Host-Kopf mit Invoke-WebRequest nicht setzen). Liefert Status
# (0 = keine Verbindung), Kopfzeilen und Text.
# $Body bleibt absichtlich ohne Typ: [string] macht aus $null einen leeren
# Text, und dann schickte jede GET-Anfrage einen Inhalt
# (ProtocolViolationException). Ein Inhalt geht nur mit, wenn die Methode
# nicht GET oder HEAD ist und $Body bzw. $Bytes etwas enthaelt.
function Invoke-Http {
    param([string] $Method, [string] $Url, [hashtable] $Headers = @{}, $Body = $null, [string] $HostName = '', [byte[]] $Bytes = $null)
    $req = [System.Net.HttpWebRequest]::Create($Url)
    $req.Method = $Method
    $req.Timeout = 30000
    $req.ReadWriteTimeout = 30000
    $req.AllowAutoRedirect = $false
    if ($HostName) { $req.Host = $HostName }
    foreach ($k in $Headers.Keys) {
        switch ($k) {
            'Content-Type' { $req.ContentType = [string] $Headers[$k] }
            'Accept' { $req.Accept = [string] $Headers[$k] }
            default { $req.Headers[$k] = [string] $Headers[$k] }
        }
    }
    $withBody = ($Method -ne 'GET' -and $Method -ne 'HEAD')
    if ($withBody -and $null -eq $Bytes -and $null -ne $Body -and ([string] $Body) -ne '') { $Bytes = $script:Utf8.GetBytes([string] $Body) }
    if ($null -ne $Bytes -and $Bytes.Length -eq 0) { $Bytes = $null }
    $status = 0
    $hdr = ''
    $text = ''
    try {
        if ($withBody -and $null -ne $Bytes) {
            $req.ContentLength = $Bytes.Length
            $s = $req.GetRequestStream()
            $s.Write($Bytes, 0, $Bytes.Length)
            $s.Close()
        } elseif ($withBody) {
            $req.ContentLength = 0
        }
        $resp = $req.GetResponse()
    } catch [System.Net.WebException] {
        $resp = $_.Exception.Response
        if ($null -eq $resp) { $text = 'error: ' + $_.Exception.Status }
    } catch {
        $resp = $null
        $text = 'error: ' + $_.Exception.GetType().Name
    }
    if ($null -ne $resp) {
        $status = [int] $resp.StatusCode
        foreach ($k in $resp.Headers.AllKeys) { $hdr += $k + ': ' + $resp.Headers[$k] + "`n" }
        try {
            $rd = New-Object System.IO.StreamReader($resp.GetResponseStream(), $script:Utf8)
            $text = $rd.ReadToEnd()
            $rd.Close()
        } catch { }
        $resp.Close()
    }
    $show = 'status ' + $status + "`n" + $hdr + $text
    return New-Object PSObject -Property @{ Status = $status; Headers = $hdr; Body = $text; Show = $show }
}

$script:InitBody = '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"abnahme-d8","version":"1"}}}'
$script:JsonHdr = @{ 'Content-Type' = 'application/json'; 'Accept' = 'application/json, text/event-stream' }

function Get-McpHeaders {
    param([string] $Bearer)
    $h = @{ 'Content-Type' = 'application/json'; 'Accept' = 'application/json, text/event-stream' }
    if ($Bearer) { $h['Authorization'] = 'Bearer ' + $Bearer }
    return $h
}

# Startet mcp --http im Hintergrund (ohne Fenster), Ausgabe in eine Datei.
# Traegt den Prozess gleich nach dem Start in $script:Srv ein (Clear-Part
# beendet ihn so auch bei einem Abbruch waehrend der Warteschleife) und
# wartet dann hoechstens 30 s auf /healthz.
function Start-McpHttp {
    param([string] $Cfg, [string] $LogName)
    $out = Join-Path $script:Run $LogName
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $script:ExeFull
    $psi.Arguments = ConvertTo-ArgString @('--data-dir', $script:Data, '--config', $Cfg, 'mcp', '--http')
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.WorkingDirectory = $script:Run
    foreach ($k in @('POSTKAUZ_KEY', 'POSTKAUZ_KEY_FILE', 'POSTKAUZ_DATA_DIR', 'POSTKAUZ_CONFIG', 'POSTKAUZ_POLICY', 'POSTKAUZ_PROFILE')) {
        if ($psi.EnvironmentVariables.ContainsKey($k)) { $psi.EnvironmentVariables.Remove($k) }
    }
    $script:McpOutFile = $out
    $script:McpErr = $null
    $script:McpOut = $null
    $p = [System.Diagnostics.Process]::Start($psi)
    $script:Srv = $p
    $p.StandardInput.Close()
    $script:McpErr = $p.StandardError.ReadToEndAsync()
    $script:McpOut = $p.StandardOutput.ReadToEndAsync()
    $base = 'http://127.0.0.1:' + $Port
    for ($i = 0; $i -lt 60; $i++) {
        if ($p.HasExited) { break }
        $r = Invoke-Http 'GET' ($base + '/healthz')
        if ($r.Status -eq 204) { break }
        Start-Sleep -Milliseconds 500
    }
}

# Beendet den laufenden mcp --http ($script:Srv) und legt seine Ausgabe
# (maskiert) im Laufordner ab.
function Stop-McpHttp {
    $P = $script:Srv
    $script:Srv = $null
    if ($null -eq $P) { return '' }
    if (-not $P.HasExited) { try { $P.Kill() } catch { } }
    [void] $P.WaitForExit(15000)
    $t = ''
    try { [void] $script:McpErr.Wait(5000); $t = [string] $script:McpErr.Result } catch { }
    try { [void] $script:McpOut.Wait(5000); $t += [string] $script:McpOut.Result } catch { }
    [System.IO.File]::WriteAllText($script:McpOutFile, (Hide-Private $t), $script:Utf8)
    return $t
}

# Pruefnummer der Probe (Wert nach --nr), fuer eigene Zeilen dieses Skripts.
function Get-ProbeNr {
    param([string[]] $ProbeArgs)
    for ($i = 0; $i -lt $ProbeArgs.Count - 1; $i++) {
        if ($ProbeArgs[$i] -eq '--nr') { return $ProbeArgs[$i + 1] }
    }
    return $ProbeArgs[0]
}

# Die Probe muss sauber enden: Schlusszeile "probe: <n> OK, <m> FAIL,
# <k> SKIP" passend zu den Zeilen, die sie geschrieben hat, und Exit 0 (ohne
# FAIL) bzw. 1 (mit FAIL). Sonst (Syntax- oder Importfehler, Absturz,
# Strg+C in der Probe, eine alte Probe bei falschen Argumenten) eine
# FAIL-Zeile <Nr>.ende. Ein sauberes Ende zaehlt nicht als eigene Pruefung.
function Test-ProbeEnd {
    param([string] $Nr, [string[]] $Lines, $Exit, [string] $Err = '')
    $n = 0
    $f = 0
    $k = 0
    $final = ''
    foreach ($line in $Lines) {
        if ($line -match '^OK ') { $n++ }
        elseif ($line -match '^FAIL ') { $f++ }
        elseif ($line -match '^SKIP ') { $k++ }
        elseif ($line -match '^probe: ') { $final = $line.Trim() }
    }
    $want = 'probe: ' + $n + ' OK, ' + $f + ' FAIL, ' + $k + ' SKIP'
    $wantExit = 0
    if ($f -gt 0) { $wantExit = 1 }
    if ($final -eq $want -and $null -ne $Exit -and $Exit -eq $wantExit) { return }
    $shown = $final
    if (-not $shown) { $shown = 'keine' }
    $det = 'Schlusszeile: ' + $shown + "`n" + 'erwartet: ' + $want + ', Exit ' + $wantExit + "`n" + 'Exit: ' + $Exit
    if ($Err -and $Err.Trim()) { $det += "`n" + '[stderr] ' + (Limit-Text (Hide-Private $Err) 5 400) }
    Add-Check ($Nr + '.ende') 'Probe endet sauber (Schlusszeile und Exit-Code)' $false $det
}

# Probe (Python) mit Pruefnummern; Ergebnisse zaehlen in dieses Protokoll.
function Invoke-Probe {
    param([string[]] $ProbeArgs, [string] $Why = '', [int] $TimeoutSec = 900)
    if (-not $script:Py) {
        Add-Skip ($ProbeArgs[0] + '-Probe') ('kein Python (' + $Why + '); -Python angeben')
        return
    }
    $env:PYTHONIOENCODING = 'utf-8'
    $r = Invoke-Native $script:Py (@('-I', $script:ProbeFull) + $ProbeArgs) @{ 'NO_PROXY' = '127.0.0.1,localhost'; 'PYTHONIOENCODING' = 'utf-8' } $TimeoutSec
    foreach ($line in ($r.Out -split "`r?`n")) {
        if ($line -match '^OK ') { $script:NOk++ }
        elseif ($line -match '^FAIL ') { $script:NFail++; $script:Fails.Add($line.Substring(5)) }
        elseif ($line -match '^SKIP ') { $script:NSkip++ }
        if ($line -match '^probe: ') { continue }
        if ($line.Trim()) { Write-Log $line }
    }
    if ($r.Err.Trim()) { Write-Log ('      [stderr] ' + (Limit-Text (Hide-Private $r.Err) 5 400)) }
    if ($r.TimedOut) {
        Add-Check ($ProbeArgs[0] + '-Probe') 'Probe innerhalb der Zeitgrenze' $false ''
    } else {
        Test-ProbeEnd (Get-ProbeNr $ProbeArgs) ($r.Out -split "`r?`n") $r.Exit
    }
}

# Probe, die der Betreiber im Fenster sieht (Teil D: Adresse fuer den Browser).
function Invoke-ProbeInteractive {
    param([string[]] $ProbeArgs)
    if (-not $script:Py) {
        Add-Skip 'D-Probe' 'kein Python; Teil D braucht die Probe (-Python angeben)'
        return
    }
    $old = $env:PYTHONIOENCODING
    $env:PYTHONIOENCODING = 'utf-8'
    $seen = New-Object System.Collections.Generic.List[string]
    $global:LASTEXITCODE = $null
    & $script:Py -I $script:ProbeFull @ProbeArgs | ForEach-Object {
        $line = [string] $_
        $seen.Add($line)
        if ($line -match '^OK ') { $script:NOk++ }
        elseif ($line -match '^FAIL ') { $script:NFail++; $script:Fails.Add($line.Substring(5)) }
        elseif ($line -match '^SKIP ') { $script:NSkip++ }
        if ($line -match '^probe: ') { return }
        # Die Adresse fuer den Browser nur auf den Bildschirm, nicht ins Protokoll.
        if ($line -match '^\s+https?://\S+\?response_type=') { try { Write-Host $line } catch { }; return }
        Write-Log $line
    }
    $code = $LASTEXITCODE
    $env:PYTHONIOENCODING = $old
    Test-ProbeEnd (Get-ProbeNr $ProbeArgs) ($seen.ToArray()) $code
}

# Datei exklusiv neu anlegen (nie ueberschreiben, nie einem Link folgen).
function New-ExclusiveFile {
    param([string] $Path, [string] $Text)
    $b = $script:Utf8.GetBytes($Text)
    try {
        $fs = New-Object System.IO.FileStream($Path, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
        $fs.Write($b, 0, $b.Length)
        $fs.Close()
    } catch {
        Stop-Run ('Datei existiert schon (oder ist ein Link): ' + $Path)
    }
}

# Schluessel anlegen; der Klartext geht nur in eine Datei im Laufordner.
function New-TestKey {
    param([string] $Name, [string] $Nr)
    $r = Invoke-Pk @('key', 'create', '--name', $Name, '--role', 'reader', '--expires', '1d', '--json')
    $j = Get-Json $r.Out
    $ok = ($r.Exit -eq 0 -and $null -ne $j -and ([string] $j.secret).StartsWith('pk_key_'))
    Add-Check $Nr ('key create --name ' + $Name + ' --role reader --expires 1d') $ok $r.Text
    # Angelegt ist angelegt: am Ende des Teils (auch bei Abbruch) widerrufen.
    if ($r.Exit -eq 0) { $script:OpenKeys.Add($Name) }
    if (-not $ok) { return '' }
    $f = Join-Path $script:Run ('keys\' + $Name + '.txt')
    New-ExclusiveFile $f ([string] $j.secret)
    $script:OpenFiles.Add($f)
    $script:Secrets.Add([string] $j.secret)
    return $f
}

# Schluessel ist in einem Pruefschritt widerrufen worden: nicht noch einmal.
function Set-KeyRevoked {
    param([string] $Name)
    [void] $script:OpenKeys.Remove($Name)
}

# ---------------------------------------------------------------- tailscale serve

# Liest "tailscale serve status --json" (die ServeConfig von tailscaled;
# "null" oder leer, wenn nichts eingestellt ist). State: 'frei', 'belegt'
# (HTTPS/TCP auf Port 443 dieses Knotens: Eintrag "443" unter TCP oder
# "<name>:443" unter Web, auch in Vordergrund-Sitzungen unter Foreground)
# oder 'unbekannt' (Fehler, kein JSON).
function Get-Serve443 {
    $r = Invoke-Native $script:TsExe @('serve', 'status', '--json') @{} 60
    $txt = ([string] $r.Out).Trim()
    $state = 'unbekannt'
    if ($r.Exit -eq 0 -and ($txt -eq '' -or $txt -eq 'null' -or $txt -eq '{}')) {
        $state = 'frei'
    } elseif ($r.Exit -eq 0) {
        $j = Get-Json $txt
        if ($null -ne $j) {
            $state = 'frei'
            $cfgs = New-Object System.Collections.Generic.List[object]
            $cfgs.Add($j)
            if ($null -ne $j.Foreground) { foreach ($p in $j.Foreground.PSObject.Properties) { $cfgs.Add($p.Value) } }
            foreach ($c in $cfgs) {
                if ($null -eq $c) { continue }
                if ($null -ne $c.TCP) { foreach ($p in $c.TCP.PSObject.Properties) { if ($p.Name -eq '443') { $state = 'belegt' } } }
                if ($null -ne $c.Web) { foreach ($p in $c.Web.PSObject.Properties) { if ($p.Name -match ':443$') { $state = 'belegt' } } }
            }
        }
    }
    return New-Object PSObject -Property @{ State = $state; Text = ('> tailscale serve status --json' + "`n" + $r.Out + "`n" + $r.Err) }
}

# Schaltet tailscale serve fuer -Port ein. Vorher muss HTTPS 443 frei sein;
# sonst Abbruch mit Hinweis (D8 ueberschreibt keine fremde Einstellung).
# Zustimmung je Schritt; die Zustimmung deckt auch das Ausschalten bei
# Abbruch (Strg+C, Fehler), das ohne weitere Frage laeuft. Liefert $true,
# wenn serve auf diesen Server zeigt.
function Enable-Serve {
    param([string] $CheckNr, [string] $Nr, [string] $OffNr)
    $script:ServeOffNr = $OffNr
    if ($script:ServeKept) {
        # Teil B hat serve auf Wunsch des Betreibers angelassen (gleicher Port).
        $script:ServeKept = $false
        $script:ServeOn = $true
        Write-Log ('      ' + $Nr + ': tailscale serve aus Teil B ist noch an (gleicher Port); am Ende dieses Teils wieder aus')
        return $true
    }
    $s = Get-Serve443
    if ($s.State -ne 'frei') {
        foreach ($l in ((Limit-Text (Hide-Private $s.Text)) -split "`r?`n")) { Write-Log ('      ' + $l) }
        Stop-Run ($CheckNr + ' tailscale serve: HTTPS auf Port 443 ist schon belegt oder nicht lesbar (' + $s.State + '). D8 ueberschreibt keine fremde Einstellung. Bitte mit "tailscale serve status" pruefen und, nur wenn sie nicht mehr gebraucht wird, von Hand ausschalten ("tailscale serve --https=443 off"); dann D8 mit neuen Pfaden wiederholen.')
    }
    Add-Check $CheckNr 'tailscale serve status --json: HTTPS 443 ist frei (nichts wird ueberschrieben)' $true $s.Text
    if (-not (Confirm-Step $Nr ('tailscale serve --bg ' + $Port + ' (macht https://<knoten>/ im Tailnet auf diesen Server; kein Funnel). Am Ende des Teils fragt das Skript vor dem Ausschalten; bei Abbruch schaltet es ohne Frage aus (tailscale serve --https=443 off).'))) { return $false }
    # Schon vor dem Aufruf als an vermerken: Ein Abbruch waehrend des Aufrufs
    # (oder ein Fehler, nach dem doch etwas eingestellt ist) soll im finally
    # ebenfalls ausschalten. Disable-Serve vertraegt einen nicht
    # eingeschalteten Serve.
    $script:ServeOn = $true
    $r = Invoke-Native $script:TsExe @('serve', '--bg', [string] $Port) @{} 60
    Add-Check $Nr ('tailscale serve --bg ' + $Port) ($r.Exit -eq 0) ($r.Out + "`n" + $r.Err)
    return ($r.Exit -eq 0)
}

# Schaltet tailscale serve auf 443 wieder aus und prueft danach, dass 443
# frei ist. Ask: am normalen Ende eines Teils mit Zustimmung (wie bisher);
# im finally (Abbruch) ohne Frage, wie in Enable-Serve angekuendigt. Beim
# Aufraeumen ohne Frage reicht ein freies 443 danach: War serve nie wirklich
# an (Abbruch waehrend serve --bg), scheitert off; das steht nur im Detail.
function Disable-Serve {
    param([bool] $Ask)
    if (-not $script:ServeOn) { return }
    $nr = $script:ServeOffNr
    if ($Ask -and -not (Confirm-Step $nr 'tailscale serve wieder ausschalten (tailscale serve --https=443 off)')) {
        $script:ServeOn = $false
        $script:ServeKept = $true
        Add-Skip $nr 'tailscale serve bleibt an (von Hand: tailscale serve --https=443 off)'
        return
    }
    $script:ServeOn = $false
    $r = Invoke-Native $script:TsExe @('serve', '--https=443', 'off') @{} 60
    $s = Get-Serve443
    $det = '> tailscale serve --https=443 off' + "`n" + $r.Out + "`n" + $r.Err + "`n" + '[exit ' + $r.Exit + ']' + "`n" + $s.Text
    if ($Ask) {
        Add-Check $nr 'tailscale serve --https=443 off, danach HTTPS 443 frei (status --json)' ($r.Exit -eq 0 -and $s.State -eq 'frei') $det
    } else {
        Add-Check $nr 'Aufraeumen: tailscale serve aus, danach HTTPS 443 frei (status --json)' ($s.State -eq 'frei') $det
        if ($s.State -ne 'frei') { Write-Log '      Aufraeumen: bitte von Hand pruefen und ausschalten: tailscale serve status, tailscale serve --https=443 off' }
    }
}

# ---------------------------------------------------------------- Aufraeumen

# Was ein Teil angelegt oder gestartet hat und am Ende wieder weg muss, auch
# bei Abbruch: tailscale serve, der laufende mcp --http, Schluessel (werden
# widerrufen) und Dateien (Schluessel, Refresh-Token).
$script:Srv = $null
$script:OpenKeys = New-Object System.Collections.Generic.List[string]
$script:OpenFiles = New-Object System.Collections.Generic.List[string]
$script:TsExe = ''
$script:ServeOn = $false
$script:ServeKept = $false
$script:ServeOffNr = ''

# Laeuft am Ende von Teil A, B und D und im finally. Fehler hier brechen
# nichts ab; was liegen bleibt, steht im Protokoll.
function Clear-Part {
    if ($script:ServeOn) {
        try { Disable-Serve $false } catch { try { Write-Log ('      Aufraeumen: tailscale serve --https=443 off fehlgeschlagen, bitte von Hand ausfuehren') } catch { } }
    }
    if ($null -ne $script:Srv) {
        try { [void] (Stop-McpHttp) } catch { try { Write-Log '      Aufraeumen: mcp --http liess sich nicht beenden, bitte im Task-Manager beenden' } catch { } }
    }
    foreach ($n in @($script:OpenKeys)) {
        try {
            $r = Invoke-Pk @('key', 'revoke', $n)
            Write-Log ('      Aufraeumen: key revoke ' + $n + ' (exit ' + $r.Exit + ')')
        } catch { try { Write-Log ('      Aufraeumen: key revoke ' + $n + ' fehlgeschlagen') } catch { } }
    }
    $script:OpenKeys.Clear()
    foreach ($f in @($script:OpenFiles)) {
        try {
            if (Test-Path -LiteralPath $f) { Remove-Item -LiteralPath $f -Force -ErrorAction Stop }
        } catch { try { Write-Log ('      Aufraeumen: Datei liess sich nicht loeschen, bitte von Hand: ' + $f) } catch { } }
    }
    $script:OpenFiles.Clear()
}

# Stellt sicher, dass weder ein Schluessel noch der Knotenname im Text steht.
function Test-NoSecret {
    param([string] $Text)
    foreach ($s in $script:Secrets) { if ($s -and $Text.Contains($s.Substring(7))) { return $false } }
    return $true
}

# ---------------------------------------------------------------- Pfade pruefen

$script:ExeFull = Get-FullPath $ExePath
$script:Data = Get-FullPath $DataDir
$script:Cfg = Get-FullPath $Config
$script:Work = Get-FullPath $WorkDir
$script:Secrets = New-Object System.Collections.Generic.List[string]

$forbidden = New-Object System.Collections.Generic.List[string]
if ($env:LOCALAPPDATA) { $forbidden.Add((Join-Path $env:LOCALAPPDATA 'postkauz')); $forbidden.Add((Join-Path $env:LOCALAPPDATA 'Packages')) }
if ($env:APPDATA) { $forbidden.Add((Join-Path $env:APPDATA 'postkauz')) }
$livePaths = New-Object System.Collections.Generic.List[string]
# Elternordner von Live-Dateien: verboten fuer -DataDir, -Config, -WorkDir,
# bei der Markersuche aber nur flach geprueft (wie abnahme_D7.ps1).
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
    Stop-Run '-DataDir ist nicht leer; D8 braucht ein frisches Datenverzeichnis.'
}
foreach ($c in @($script:Cfg, ($script:Cfg + '.tailnet.toml'), ($script:Cfg + '.oauth.toml'))) {
    if (Test-Path -LiteralPath $c) { Stop-Run ($c + ' gibt es schon; D8 legt die Konfiguration selbst an (neuen Pfad waehlen).') }
}
if (Test-Path -LiteralPath (Join-Path (Split-Path -Parent $script:Cfg) 'policy.toml')) {
    Stop-Run 'Neben -Config liegt schon eine policy.toml (Ordner einer anderen Konfiguration, vielleicht der Live-Konfiguration); bitte einen neuen, leeren Ordner fuer -Config nehmen.'
}
if ($Port -lt 1024 -or $Port -gt 65535 -or $Port -eq 8787) { Stop-Run '-Port muss zwischen 1024 und 65535 liegen und darf nicht 8787 sein (Standard eines laufenden Dienstes).' }
if (-not (Test-Path -LiteralPath $script:ExeFull -PathType Leaf)) { Stop-Run ('Programm nicht gefunden: ' + $script:ExeFull) }
if ($NodeName -and $NodeName -notmatch '^[A-Za-z0-9.-]+$') { Stop-Run '-NodeName: nur der Name, z. B. pc.<tailnet>.ts.net (ohne https:// und ohne Pfad).' }
if ($Issuer -and $Issuer -notmatch '^https://[A-Za-z0-9.:-]+(/[A-Za-z0-9._/-]*)?$') { Stop-Run '-Issuer: https://<name>[:port][/pfad]' }
if ($Issuer -and -not $NodeName) { Stop-Run 'Teil D (-Issuer) braucht -NodeName.' }

$markerDirs = @($script:Data, $script:Work, (Split-Path -Parent $script:Cfg))
foreach ($d in $markerDirs) {
    $where = Test-Marker $d $markerPlaces $markerShallow
    if ($where) { Stop-Run ('Eine Markerdatei aus ' + $d + ' ist in ' + $where + ' sichtbar (Junction, subst oder derselbe Ordner).') }
}

[void] [System.IO.Directory]::CreateDirectory($script:Work)
if ((Get-Item -LiteralPath $script:Work -Force).Attributes -band [System.IO.FileAttributes]::ReparsePoint) {
    Stop-Run ('-WorkDir ist ein Reparse Point (Junction oder Link): ' + $script:Work)
}
$script:Log = Join-Path $script:Work 'D8_windows.txt'
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
foreach ($sub in @('', 'keys')) { [void] [System.IO.Directory]::CreateDirectory((Join-Path $script:Run $sub)) }
Add-PathMask $script:Data '<DataDir>'
Add-PathMask $script:Cfg '<Config>'
Add-PathMask $script:Work '<WorkDir>'
$i = 0
foreach ($l in $livePaths) { $i++; Add-PathMask $l ('<Live' + $i + '>') }
Add-PathMask (Split-Path -Parent $script:ExeFull) '<ExeDir>'
Add-PathMask $env:USERPROFILE '<USERPROFILE>'
# Knotenname und Aussteller von Anfang an maskieren (auch im Kopf des
# Protokolls und in resource, authorization_servers, aud).
Add-HostMask $NodeName '<knoten>'
Add-HostMask $Issuer '<issuer>' '<issuer-host>'

$script:Py = ''
if ($Python) { $script:Py = Get-FullPath $Python } else { $script:Py = Find-Python }
$script:ProbeFull = $Probe
if (-not $script:ProbeFull) { $script:ProbeFull = Join-Path $PSScriptRoot 'abnahme_D8_probe.py' }
if ($script:Py -and -not (Test-Path -LiteralPath $script:ProbeFull -PathType Leaf)) { $script:Py = '' }

Write-Log ('Abnahme D8 ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + ', PowerShell ' + $PSVersionTable.PSVersion.ToString())
Write-Log ('Programm: ' + $script:ExeFull)
Write-Log ('DataDir ' + $script:Data + ', Config ' + $script:Cfg + ', Laufordner ' + $script:Run + ', Port ' + $Port)
if ($script:Py) { Write-Log ('Python: ' + $script:Py) } else { Write-Log 'Python: keins (Probe-Schritte SKIP)' }
if ($NodeName) { Write-Log 'Teil B: ja (-NodeName)' } else { Write-Log 'Teil B: nein (nur mit -NodeName)' }
if ($Issuer) { Write-Log 'Teil D: ja (-Issuer)' } else { Write-Log 'Teil D: nein (nur mit -Issuer und -NodeName)' }

$base = 'http://127.0.0.1:' + $Port
$mcpUrl = $base + '/mcp'
$hostLocal = '127.0.0.1:' + $Port
if ($NodeName) {
    $ts = @(Get-Command 'tailscale.exe' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1)
    if ($ts.Count -gt 0) { $script:TsExe = $ts[0].Path }
}

# Teile A, B und D in einem Block: Was ein Teil anlegt (Schluessel,
# Schluessel- und Refresh-Dateien, mcp --http, tailscale serve), raeumt
# Clear-Part am Ende des Teils weg und im finally auch bei Abbruch
# (Stop-Run, Fehler, Strg+C).
$script:Main = $true
$abort = ''
$done = $false
try {

    # ============================================================ Teil A

    Write-Log ''
    Write-Log '===== Teil A: mcp --http lokal im Modus key (offline)'

    $det = 'SHA-256 postkauz.exe: ' + (Get-FileHash -LiteralPath $script:ExeFull -Algorithm SHA256).Hash
    $r = Invoke-Native $script:ExeFull @('version')
    Add-Check 'A0.1' ('version nennt ' + $ExpectVersion) ($r.Exit -eq 0 -and (Test-Has $r.Out ([regex]::Escape($ExpectVersion) + '(?![0-9A-Za-z.])'))) ($r.Out.Trim() + "`n" + $det)

    [void] [System.IO.Directory]::CreateDirectory($script:Data)
    [void] [System.IO.Directory]::CreateDirectory((Split-Path -Parent $script:Cfg))
    New-ExclusiveFile $script:Cfg ("# Abnahme D8, Teil A: nur dieses Datenverzeichnis`r`n[http]`r`nlisten = '127.0.0.1:" + $Port + "'`r`nauth = 'key'`r`n")

    $r = Invoke-Pk @('doctor', '--json')
    $j = Get-Json $r.Out
    $hok = ($null -ne $j -and $null -ne $j.http -and $j.http.listen -eq $hostLocal -and $j.http.auth -eq 'key' -and $j.http.listen_allowed -eq $true)
    Add-Check 'A1.1' ('doctor --json: http.listen ' + $hostLocal + ', auth key, listen_allowed') $hok $r.Text

    $keyFile = New-TestKey 'cloud-reader' 'A1.2'

    $r = Invoke-PkIn $script:Data $script:Cfg @('mcp', '--http', '--listen', ('0.0.0.0:' + $Port)) '' 30
    Add-Check 'A1.3' 'mcp --http --listen 0.0.0.0:<Port> -> Abbruch vor dem Start (Exit 2, allow_non_loopback)' ($r.Exit -eq 2 -and (Test-Has $r.Err 'allow_non_loopback')) $r.Text

    Start-McpHttp $script:Cfg 'mcp-http-a.txt'
    $r = Invoke-Http 'GET' ($base + '/healthz')
    Add-Check 'A2.1' 'mcp --http laeuft: GET /healthz -> 204 ohne Inhalt' ($r.Status -eq 204 -and -not $r.Body) $r.Show
    if ($r.Status -eq 204) {
        $r = Invoke-Http 'GET' ($base + '/.well-known/oauth-protected-resource/mcp')
        $j = Get-Json $r.Body
        Add-Check 'A2.2' 'Metadaten (RFC 9728): resource = http://127.0.0.1:<Port>/mcp, keine authorization_servers' ($r.Status -eq 200 -and $null -ne $j -and $j.resource -eq $mcpUrl -and $null -eq $j.authorization_servers) $r.Show
        $r = Invoke-Http 'POST' $mcpUrl $script:JsonHdr $script:InitBody
        Add-Check 'A2.3' 'POST /mcp ohne Bearer -> 401 mit WWW-Authenticate resource_metadata' ($r.Status -eq 401 -and (Test-Has $r.Headers '(?i)WWW-Authenticate: Bearer resource_metadata="http://127\.0\.0\.1:\d+/\.well-known/oauth-protected-resource/mcp"')) $r.Show
        $r = Invoke-Http 'POST' $mcpUrl (Get-McpHeaders ('pk_key_' + ('x' * 43))) $script:InitBody
        Add-Check 'A2.4' 'POST /mcp mit unbekanntem Schluessel -> 401' ($r.Status -eq 401) $r.Show
        $key = ''
        if ($keyFile) { $key = [System.IO.File]::ReadAllText($keyFile).Trim() }
        $r = Invoke-Http 'GET' $mcpUrl (Get-McpHeaders $key)
        Add-Check 'A2.5' 'GET /mcp -> 405' ($r.Status -eq 405) $r.Show
        $r = Invoke-Http 'POST' $mcpUrl (Get-McpHeaders $key) $script:InitBody 'evil.example.org'
        Add-Check 'A2.6' 'fremder Host-Kopf -> 421' ($r.Status -eq 421) $r.Show
        $big = New-Object byte[] (1048576 + 100)
        for ($k = 0; $k -lt $big.Length; $k++) { $big[$k] = 32 }
        $r = Invoke-Http 'POST' $mcpUrl (Get-McpHeaders $key) $null '' $big
        if ($r.Status -eq 0) {
            Add-Skip 'A2.7' 'Anfrage ueber 1 MiB: Server schloss die Verbindung vor der Antwort (Windows liest dann keinen Status; 413 belegt der Unit-Test TestHTTPHardening)'
        } else {
            Add-Check 'A2.7' 'Anfrage ueber 1 MiB -> 413' ($r.Status -eq 413) ('status ' + $r.Status)
        }
        $r = Invoke-Http 'POST' $mcpUrl (Get-McpHeaders $key) $script:InitBody
        Add-Check 'A2.8' 'initialize mit Schluessel -> 200, keine Session-ID, keine Server-Kennung' ($r.Status -eq 200 -and -not (Test-Has $r.Headers '(?im)^(Mcp-Session-Id|Server):')) $r.Show
        if ($keyFile) {
            Invoke-Probe @('key', '--url', $mcpUrl, '--key-file', $keyFile, '--nr', 'A3', '--principal', 'cloud-reader', '--expect-tools', '12') 'MCP-Sitzung'
        }
        $r = Invoke-Pk @('key', 'revoke', 'cloud-reader')
        Add-Check 'A4.1' 'key revoke cloud-reader' ($r.Exit -eq 0 -and (Test-Has $r.Out 'revoked')) $r.Text
        if ($r.Exit -eq 0) { Set-KeyRevoked 'cloud-reader' }
        Write-Log '      warte 31 s (Zwischenspeicher der Anmeldung 30 s) ...'
        Start-Sleep -Seconds 31
        $r = Invoke-Http 'POST' $mcpUrl (Get-McpHeaders $key) $script:InitBody
        $j = Get-Json $r.Body
        Add-Check 'A4.2' 'widerrufener Schluessel -> 403 {"status":"denied","code":"key_revoked"}' ($r.Status -eq 403 -and $null -ne $j -and $j.status -eq 'denied' -and $j.code -eq 'key_revoked') $r.Show
    } else {
        Add-Skip 'A2-A4' 'mcp --http laeuft nicht'
    }
    $srvText = Stop-McpHttp
    Add-Check 'A5.1' 'mcp --http meldet den Start auf stderr' (Test-Has $srvText 'mcp --http: listening on 127\.0\.0\.1:\d+ \(auth key') (Limit-Text (Hide-Private $srvText) 5 600)

    $r = Invoke-Pk @('audit', '--raw', '--since', '1h', '--limit', '1000', '--json')
    $ev = @(Get-JsonLines $r.Out)
    $http = @($ev | Where-Object { $null -ne $_.params -and $null -ne $_.params.via -and $_.params.via.transport -eq 'http' })
    $auth = @($ev | Where-Object { $_.operation -eq 'keys.authenticate' -and $_.result -eq 'denied' })
    $detail = 'Eintraege mit params.via.transport=http: ' + $http.Count + "`n" + 'keys.authenticate denied: ' + (@($auth | ForEach-Object { $_.reason }) -join ', ')
    $needHttp = 1
    if (-not $script:Py) { $needHttp = 0 }
    Add-Check 'A5.2' 'Audit: Aufrufe mit transport http und Principal; gescheiterte Anmeldungen (key_invalid, key_revoked)' ($r.Exit -eq 0 -and $http.Count -ge $needHttp -and @($auth | Where-Object { $_.reason -eq 'key_invalid' }).Count -ge 1 -and @($auth | Where-Object { $_.reason -eq 'key_revoked' }).Count -ge 1) $detail
    $logText = ''
    $logFile = Join-Path $script:Data 'postkauz.log'
    if (Test-Path -LiteralPath $logFile) { $logText = [System.IO.File]::ReadAllText($logFile) }
    Add-Check 'A5.3' 'Schluessel weder im Log noch im Audit noch in der Ausgabe von mcp --http' ((Test-NoSecret $logText) -and (Test-NoSecret $r.Out) -and (Test-NoSecret $srvText)) ''
    $r = Invoke-Pk @('audit', 'verify', '--json')
    $j = Get-Json $r.Out
    Add-Check 'A5.4' 'audit verify -> status ok' ($r.Exit -eq 0 -and $null -ne $j -and $j.status -eq 'ok') $r.Text
    Clear-Part

    # ============================================================ Teil B

    if ($NodeName) {
        Write-Log ''
        Write-Log '===== Teil B: dieser PC als Knoten im Tailnet (Modus key)'
        $res = 'https://' + $NodeName + '/mcp'
        $cfgB = $script:Cfg + '.tailnet.toml'
        New-ExclusiveFile $cfgB ("# Abnahme D8, Teil B`r`n[http]`r`nlisten = '127.0.0.1:" + $Port + "'`r`nauth = 'key'`r`nresource = '" + $res + "'`r`n")
        $keyC = New-TestKey 'cloud-reader-c' 'B1.1'
        Start-McpHttp $cfgB 'mcp-http-b.txt'
        $r = Invoke-Http 'GET' ($base + '/healthz')
        Add-Check 'B1.2' 'mcp --http laeuft (Resource https://<knoten>/mcp)' ($r.Status -eq 204) $r.Show
        $r = Invoke-Http 'POST' $mcpUrl $script:JsonHdr $script:InitBody
        Add-Check 'B1.3' 'Host 127.0.0.1 gilt mit eigener Resource nicht mehr -> 421' ($r.Status -eq 421) $r.Show
        if (-not $script:TsExe) {
            Add-Skip 'B2' 'tailscale.exe nicht im PATH (Tailscale fuer Windows installieren)'
        } elseif (Enable-Serve 'B2.0' 'B2.1' 'B5.1') {
            $r = Invoke-Http 'GET' ('https://' + $NodeName + '/.well-known/oauth-protected-resource/mcp')
            $j = Get-Json $r.Body
            Add-Check 'B2.2' 'https://<knoten>/.well-known/oauth-protected-resource/mcp -> resource https://<knoten>/mcp (Zertifikat gueltig)' ($r.Status -eq 200 -and $null -ne $j -and $j.resource -eq $res) $r.Show
            if ($keyC) {
                Invoke-Probe @('key', '--url', $res, '--key-file', $keyC, '--nr', 'B3', '--principal', 'cloud-reader-c', '--expect-tools', '12') 'Sitzung ueber das Tailnet'
            }
            Write-Log ''
            Write-Log '[Du] B4 Der Server laeuft. Jetzt fuehrt die Pruefsitzung Teil C aus der Cloud-Sitzung aus (abnahme_D8_cloud.sh, D8_MODE=key).'
            Write-Log ('     Schluessel fuer Teil C (als Geheimnis der Cloud-Sitzung uebertragen, nie ins Repository): ' + $keyC)
            Write-Log '     Teil C wartet in Schritt C5 auf die Datei go in seinem Arbeitsordner. Erst hier Enter (Widerruf), dann legt die Pruefsitzung dort go an.'
            try { [void] (Read-Host '      Enter, sobald Teil C bei Schritt C5 wartet (das Skript widerruft dann cloud-reader-c)') } catch { }
            $r = Invoke-Pk @('key', 'revoke', 'cloud-reader-c')
            Add-Check 'B4.1' 'key revoke cloud-reader-c (fuer Teil C, Schritt C5)' ($r.Exit -eq 0) $r.Text
            if ($r.Exit -eq 0) { Set-KeyRevoked 'cloud-reader-c' }
            try { [void] (Read-Host '      Enter, sobald Teil C fertig ist (Server wird danach beendet)') } catch { }
        } else {
            Add-Skip 'B2.2-B4.1' 'tailscale serve ist nicht an (ohne Zustimmung oder Fehler in B2.1)'
        }
        Disable-Serve $true
        Clear-Part
    }

    # ============================================================ Teil D

    if ($Issuer) {
        Write-Log ''
        Write-Log '===== Teil D: Modus oauth mit dem Autorisierungsserver'
        $res = 'https://' + $NodeName + '/mcp'
        $cfgD = $script:Cfg + '.oauth.toml'
        $sub = $Subject
        if (-not $sub) { $sub = 'noch-unbekannt' }
        $subToml = $sub.Replace('\', '\\').Replace('"', '\"')
        New-ExclusiveFile $cfgD ("# Abnahme D8, Teil D`r`n[http]`r`nlisten = '127.0.0.1:" + $Port + "'`r`nauth = 'oauth'`r`nresource = '" + $res + "'`r`nissuer = '" + $Issuer + "'`r`n[http.principals]`r`n""" + $subToml + """ = 'cloud-oauth'`r`n")
        $keyD = New-TestKey 'cloud-oauth' 'D1.1'
        $tok = Join-Path $script:Run 'keys\oauth-refresh.json'
        $script:OpenFiles.Add($tok)
        Start-McpHttp $cfgD 'mcp-http-d.txt'
        $r = Invoke-Http 'GET' ($base + '/healthz')
        Add-Check 'D1.2' 'mcp --http laeuft im Modus oauth' ($r.Status -eq 204) $r.Show
        if ($script:TsExe) {
            [void] (Enable-Serve 'D1.3' 'D1.4' 'D5.1')
        } else {
            Add-Skip 'D1.3-D1.4' 'tailscale.exe nicht im PATH'
        }
        Write-Log '[Du] D2 Gleich erscheint eine Adresse. Im Browser oeffnen, beim Autorisierungsserver anmelden und zustimmen.'
        $pa = @('oauth', '--url', $res, '--nr', 'D3', '--principal', 'cloud-oauth', '--expect-tools', '12', '--client-id', $ClientId, '--callback-port', [string] $CallbackPort, '--token-file', $tok)
        if ($keyD) { $pa += @('--key-file', $keyD) }
        if ($Subject) { $pa += '--wait-expiry' }
        Invoke-ProbeInteractive $pa
        if ($Subject -and (Test-Path -LiteralPath $tok)) {
            $r = Invoke-Pk @('key', 'revoke', 'cloud-oauth')
            Add-Check 'D4.1' 'key revoke cloud-oauth' ($r.Exit -eq 0) $r.Text
            if ($r.Exit -eq 0) { Set-KeyRevoked 'cloud-oauth' }
            Start-Sleep -Seconds 3
            Invoke-ProbeInteractive @('revoked', '--url', $res, '--token-file', $tok, '--nr', 'D4')
        } elseif (-not $Subject) {
            Add-Skip 'D4' 'ohne -Subject: Teil D mit dem oben genannten Subjekt wiederholen (-Subject "<sub>")'
        }
        Disable-Serve $true
        Clear-Part
        Write-Log '[Du] D6 Danach von Hand: Claude Code am PC (docs/betrieb.md 4.6 Schritt 6) und das Testkonto im Autorisierungsserver deaktivieren.'
    }
    $done = $true
} catch {
    $m = [string] $_.Exception.Message
    if ($m.StartsWith('D8-ABBRUCH: ')) { $abort = $m.Substring(12) } else { $abort = 'unerwarteter Fehler: ' + $m }
} finally {
    $script:Main = $false
    if (-not $done) {
        try { Write-Log '' } catch { }
        try { Write-Log 'Abbruch: raeume auf (tailscale serve, mcp --http, Schluessel, Dateien)' } catch { }
    }
    Clear-Part
    if ($script:ServeKept) {
        try { Write-Log '[Du] tailscale serve ist noch an (deine Wahl). Von Hand ausschalten: tailscale serve --https=443 off' } catch { }
    }
}
if ($abort) { Stop-Run $abort }

Write-Log ''
foreach ($f in $script:Fails) { Write-Log ('FAIL-Liste: ' + $f) }
Write-Log ('D8: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL, ' + $script:NSkip + ' SKIP')
if ($script:NFail -gt 0) { exit 1 }
exit 0
