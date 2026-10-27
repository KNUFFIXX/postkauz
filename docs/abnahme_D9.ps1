<#
.SYNOPSIS
    Abnahme D9 (PostKauz, Schnitt 3, Teil T4): Microsoft-Postfach per OAuth2, nur lesend.

.DESCRIPTION
    Prueft S3.7 (auftraege/A03_s3.md, Teil D, D9) auf Windows 11 mit
    Windows PowerShell 5.1, ohne Go und ohne Docker, nur mit dem Programm
    aus dist/. Anleitung: docs/abnahme_D9.md, App-Registrierung:
    docs/betrieb_microsoft.md.

    Das Skript
      - verlangt -DataDir und -Config und bricht ab, wenn einer davon das
        Standard-Datenverzeichnis bzw. die Standard-Konfiguration ist, in
        einem mit -Live genannten Pfad liegt oder (DataDir) nicht leer ist;
        das Live-Datenverzeichnis des Dauerbetriebs wird nie beruehrt,
      - ruft postkauz.exe immer mit --data-dir und --config auf,
      - prueft zuerst offline (Teil 1: Eingabefehler, human_only, ohne
        Microsoft), dann am echten Konto nur lesend (Teil 2): Konto anlegen,
        Anmeldung im Browser (der Betreiber meldet sich an), account test
        mit XOAUTH2, sync nur INBOX mit Fenster 200 und max_message_bytes
        1 MiB, Suche, doctor mit Token-Ablauf, Audit oauth.refresh, zweite
        Anmeldung mit --device-code, doctor --secrets,
      - legt nie einen Entwurf an und fuehrt keine Aktion im Konto aus,
      - schreibt das Protokoll ohne Adresse, Client-ID, API-Schluessel und
        Token nach -LogFile.

    Jede Pruefung ist eine Zeile "OK <Nr> <Beschreibung>" oder
    "FAIL <Nr> <Beschreibung>", darunter eingerueckt die Ist-Ausgabe
    (gekuerzt, maskiert). Letzte Zeile: "D9: <n> OK, <m> FAIL".
    Exit 0 nur ohne FAIL.

.PARAMETER Exe
    Pfad zu postkauz.exe (Stand 0.3.0-ms.1 aus dist/).

.PARAMETER DataDir
    Frisches Datenverzeichnis nur fuer D9 (darf nicht existieren oder muss
    leer sein).

.PARAMETER Config
    Konfigurationsdatei nur fuer D9. Fehlt sie, legt das Skript sie an
    ([imap] max_message_bytes = 1048576). Eine vorhandene Datei wird nicht
    veraendert.

.PARAMETER Live
    Pfade des Dauerbetriebs (Datenverzeichnis, Konfiguration), die nie
    benutzt werden duerfen. Mehrere mit Komma.

.PARAMETER User
    Adresse des Microsoft-Postfachs (Anmeldename). Steht nie im Protokoll.

.PARAMETER ClientId
    Anwendungs-ID (Client-ID) der eigenen App-Registrierung. Steht nie im
    Protokoll.

.PARAMETER Tenant
    consumers (Standard, private Konten), organizations, common oder
    Mandanten-ID.

.PARAMETER SearchWord
    Ein Wort aus einer eigenen Mail im Posteingang (fuer die Suche).
    Leer: Schritt 2.8 wird uebersprungen (Vermerk im Protokoll).

.PARAMETER Zustimmung
    Zustimmung des Betreibers zu D9 gegen sein echtes Postfach, mit Datum
    und Uhrzeit, z. B. "ja, 28.10.2026 10:15". Ohne "ja" am Anfang laeuft
    nur Teil 1.

.PARAMETER Entfernen
    Am Ende das Konto mit account remove entfernen (loescht im
    Schluesselbund Haupteintrag und alle Teile des Tokens) und das
    Datenverzeichnis loeschen (sonst bleibt beides fuer weitere Pruefungen).

.PARAMETER LogFile
    Protokolldatei. Standard: D9_windows.txt neben dem Datenverzeichnis.

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File abnahme_D9.ps1 -Exe C:\PostKauz\d9\postkauz.exe -DataDir C:\PostKauz\d9\daten -Config C:\PostKauz\d9\config.toml -Live C:\PostKauz\daten -User <adresse> -ClientId <client-id> -SearchWord Rechnung -Zustimmung "ja, 28.10.2026 10:15"
#>
param(
    [Parameter(Mandatory = $true)] [string] $Exe,
    [Parameter(Mandatory = $true)] [string] $DataDir,
    [Parameter(Mandatory = $true)] [string] $Config,
    [string[]] $Live = @(),
    [Parameter(Mandatory = $true)] [string] $User,
    [Parameter(Mandatory = $true)] [string] $ClientId,
    [string] $Tenant = 'consumers',
    [string] $SearchWord = '',
    [Parameter(Mandatory = $true)] [string] $Zustimmung,
    [string] $ExpectVersion = '0.3.0-ms.1',
    [switch] $Entfernen,
    [string] $LogFile = ''
)

$ErrorActionPreference = 'Stop'
$script:Utf8 = New-Object System.Text.UTF8Encoding $false
$script:NOk = 0
$script:NFail = 0
$script:Log = $null
$script:Fails = New-Object System.Collections.Generic.List[string]
$script:Account = 'outlook'
try { [Console]::OutputEncoding = $script:Utf8 } catch { }

# ---------------------------------------------------------------- Hilfen

# Maskiert alles, was nicht ins Protokoll darf: Adresse, Client-ID,
# API-Schluessel, JWT-foermige Token, Token-Felder in JSON.
function Hide-Secrets {
    param([string] $Text)
    if ($null -eq $Text) { return '' }
    $t = $Text
    if ($User) { $t = $t.Replace($User, '<adresse>') }
    if ($ClientId) { $t = [regex]::Replace($t, [regex]::Escape($ClientId), '<client-id>', 'IgnoreCase') }
    $t = [regex]::Replace($t, 'pk_key_[A-Za-z0-9_-]+', 'pk_key_********')
    $t = [regex]::Replace($t, 'eyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]*', '<jwt>')
    $t = [regex]::Replace($t, '("?(access_token|refresh_token|id_token|device_code)"?\s*[:=]\s*"?)[^"&,\s}]+', '$1<token>')
    $t = [regex]::Replace($t, 'enter the code [A-Z0-9-]+', 'enter the code <code>')
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

function New-StartInfo {
    param([string[]] $ArgList, [hashtable] $EnvExtra)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $script:ExePath
    $psi.Arguments = ConvertTo-ArgString (@('--data-dir', $script:Data, '--config', $script:Cfg) + $ArgList)
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.StandardOutputEncoding = $script:Utf8
    $psi.StandardErrorEncoding = $script:Utf8
    foreach ($k in @('POSTKAUZ_KEY', 'POSTKAUZ_KEY_FILE', 'POSTKAUZ_DATA_DIR', 'POSTKAUZ_CONFIG', 'POSTKAUZ_POLICY', 'POSTKAUZ_PROFILE', 'POSTKAUZ_SECRETS_BACKEND')) {
        if ($psi.EnvironmentVariables.ContainsKey($k)) { $psi.EnvironmentVariables.Remove($k) }
    }
    foreach ($k in $EnvExtra.Keys) { $psi.EnvironmentVariables[$k] = [string] $EnvExtra[$k] }
    return $psi
}

function New-Result {
    param([string[]] $A, [int] $Code, [string] $Out, [string] $Err, [bool] $TimedOut)
    $text = '> postkauz ' + ($A -join ' ') + "`n" + $Out
    if ($Err) { $text = $text.TrimEnd() + "`n[stderr] " + $Err }
    if ($TimedOut) { $text = $text.TrimEnd() + "`n[Zeitgrenze erreicht, Prozess beendet]" }
    $text = $text.TrimEnd() + "`n[exit " + $Code + ']'
    return New-Object PSObject -Property @{ Exit = $Code; Out = $Out; Err = $Err; TimedOut = $TimedOut; Text = $text; All = ($Out + "`n" + $Err) }
}

# postkauz.exe ohne Fenster, stdout/stderr als UTF-8, mit Zeitgrenze.
function Invoke-Pk {
    param([string[]] $A, [hashtable] $EnvExtra = @{}, [int] $TimeoutSec = 600)
    $p = [System.Diagnostics.Process]::Start((New-StartInfo $A $EnvExtra))
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
    return New-Result $A $code ([string] $to.Result) ([string] $te.Result) $timedOut
}

# Wie Invoke-Pk, zeigt aber stderr sofort an (Adresse und Code der
# Anmeldung muss der Betreiber sehen, waehrend das Programm wartet).
function Invoke-PkLive {
    param([string[]] $A, [int] $TimeoutSec = 420)
    $p = [System.Diagnostics.Process]::Start((New-StartInfo $A @{}))
    $p.StandardInput.Close()
    $to = $p.StandardOutput.ReadToEndAsync()
    $err = New-Object System.Text.StringBuilder
    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    $timedOut = $false
    $line = $p.StandardError.ReadLineAsync()
    while ($true) {
        if ($line.Wait(500)) {
            if ($null -eq $line.Result) { break }
            [void] $err.AppendLine($line.Result)
            # Unmaskiert nur auf den Bildschirm: Adresse und Code braucht der Betreiber.
            try { Write-Host ('      | ' + $line.Result) } catch { }
            $line = $p.StandardError.ReadLineAsync()
        }
        if ((Get-Date) -gt $deadline) { $timedOut = $true; try { $p.Kill() } catch { }; break }
    }
    $p.WaitForExit()
    [void] $to.Wait(10000)
    $code = -1
    if (-not $timedOut) { $code = $p.ExitCode }
    return New-Result $A $code ([string] $to.Result) $err.ToString() $timedOut
}

function Test-Has {
    param([string] $Text, [string] $Pattern)
    return [regex]::IsMatch($Text, $Pattern)
}

function Get-Json {
    param([string] $Text)
    try { return ($Text.Trim() | ConvertFrom-Json) } catch { return $null }
}

function Get-FullPath {
    param([string] $P)
    return $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($P).TrimEnd('\')
}

# a liegt in b (oder ist b), ohne Gross-/Kleinschreibung.
function Test-Inside {
    param([string] $A, [string] $B)
    if (-not $A -or -not $B) { return $false }
    $a = $A.TrimEnd('\').ToLowerInvariant() + '\'
    $b = $B.TrimEnd('\').ToLowerInvariant() + '\'
    return $a.StartsWith($b) -or $b.StartsWith($a)
}

function Confirm-Next {
    param([string] $Text)
    Write-Log ''
    Write-Log ('[Du] ' + $Text)
    try { [void] (Read-Host '      Weiter mit Enter (Strg+C bricht ab)') } catch { }
}

# ---------------------------------------------------------------- Lauf

$exitCode = 1
try {
    $script:ExePath = Get-FullPath $Exe
    $script:Data = Get-FullPath $DataDir
    $script:Cfg = Get-FullPath $Config
    if (-not $LogFile) { $LogFile = Join-Path (Split-Path -Parent $script:Data) 'D9_windows.txt' }
    $LogFile = Get-FullPath $LogFile

    # ---- Schutz: nie das Live- oder Standard-Datenverzeichnis (Rahmen 5)
    $forbidden = @()
    if ($env:LOCALAPPDATA) { $forbidden += @((Join-Path $env:LOCALAPPDATA 'postkauz'), (Join-Path $env:LOCALAPPDATA 'Packages')) }
    if ($env:APPDATA) { $forbidden += (Join-Path $env:APPDATA 'postkauz') }
    # Mit -File kommt "-Live a,b" als ein Text an: selbst trennen.
    foreach ($l in ($Live | ForEach-Object { $_ -split ',' })) { if ($l.Trim()) { $forbidden += (Get-FullPath $l.Trim()) } }
    foreach ($f in $forbidden) {
        if (Test-Inside $script:Data $f) { throw ('-DataDir liegt im Standard- oder Live-Pfad ' + $f + '; abgebrochen, nichts veraendert') }
        if (Test-Inside $script:Cfg $f) { throw ('-Config liegt im Standard- oder Live-Pfad ' + $f + '; abgebrochen, nichts veraendert') }
    }
    if ((Test-Path -LiteralPath $script:Data) -and @(Get-ChildItem -LiteralPath $script:Data -Force).Count -gt 0) {
        throw ('-DataDir ' + $script:Data + ' ist nicht leer; D9 braucht ein frisches Datenverzeichnis')
    }
    if (-not (Test-Path -LiteralPath $script:ExePath)) { throw ('postkauz.exe nicht gefunden: ' + $script:ExePath) }

    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $LogFile) | Out-Null
    $script:Log = $LogFile
    [System.IO.File]::WriteAllText($script:Log, '', $script:Utf8)
    Write-Log ('Abnahme D9 gestartet ' + (Get-Date -Format 'dd.MM.yyyy HH:mm:ss') + ' auf ' + $env:COMPUTERNAME)
    Write-Log ('Programm: ' + $script:ExePath)
    Write-Log ('Datenverzeichnis: ' + $script:Data + ' (frisch), Konfiguration: ' + $script:Cfg + ', Tenant: ' + $Tenant)

    # ------------------------------------------------ Teil 0: Vorbereitung
    Write-Log ''
    Write-Log '===== D9.0 Vorbereitung'
    $cfgNote = 'vorhanden, unveraendert'
    if (-not (Test-Path -LiteralPath $script:Cfg)) {
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $script:Cfg) | Out-Null
        [System.IO.File]::WriteAllText($script:Cfg, "# Abnahme D9: nur dieses Datenverzeichnis`r`n[imap]`r`nmax_message_bytes = 1048576`r`n", $script:Utf8)
        $cfgNote = 'angelegt'
    }
    $cfgText = [System.IO.File]::ReadAllText($script:Cfg)
    Add-Check '0.1' ('Konfiguration ' + $cfgNote + ', max_message_bytes = 1048576 (1 MiB)') (Test-Has $cfgText '(?m)^\s*max_message_bytes\s*=\s*1048576\s*$') ''

    $r = Invoke-Pk @('version')
    Add-Check '0.2' ('version nennt ' + $ExpectVersion) ($r.Exit -eq 0 -and (Test-Has $r.Out ([regex]::Escape($ExpectVersion)))) $r.Text

    $sums = Join-Path (Split-Path -Parent $script:ExePath) 'SHA256SUMS'
    if (Test-Path -LiteralPath $sums) {
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $script:ExePath).Hash.ToLowerInvariant()
        $line = @(Get-Content -LiteralPath $sums | Where-Object { $_ -match 'postkauz-windows-amd64\.exe' }) | Select-Object -First 1
        Add-Check '0.3' 'SHA256 von postkauz.exe passt zu SHA256SUMS (postkauz-windows-amd64.exe)' ($line -and $line.ToLowerInvariant().StartsWith($hash)) ('ist ' + $hash + "`nSHA256SUMS: " + $line)
    } else {
        Write-Log ('INFO 0.3 kein SHA256SUMS neben dem Programm; Pruefsumme nicht verglichen')
    }

    $r = Invoke-Pk @('doctor', '--json')
    $d = Get-Json $r.Out
    Add-Check '0.4' 'doctor: Schluesselbund erreichbar (keyring available), Datenbank ok nach erstem Kommando' ($null -ne $d -and $d.keyring -eq 'available') $r.Text

    # ------------------------------------------------ Teil 1: offline
    Write-Log ''
    Write-Log '===== D9.1 Offline-Pruefungen (kein Kontakt zu Microsoft)'
    $r = Invoke-Pk @('account', 'add', 'probe-ohne-id', '--imap', 'outlook.office365.com:993', '--user', 'probe@example.org', '--auth', 'oauth2', '--json')
    Add-Check '1.1' 'account add --auth oauth2 ohne --client-id -> Exit 2, nennt client_id' ($r.Exit -eq 2 -and (Test-Has $r.Err 'client_id')) $r.Text
    $r = Invoke-Pk @('account', 'add', 'probe-pw', '--imap', 'outlook.office365.com:993', '--user', 'probe@example.org', '--auth', 'oauth2', '--client-id', $ClientId, '--password-stdin')
    Add-Check '1.2' 'account add --auth oauth2 mit --password-stdin -> Exit 2 (kein Passwort bei OAuth2)' ($r.Exit -eq 2) $r.Text
    $r = Invoke-Pk @('account', 'add', 'probe', '--imap', '127.0.0.1:1', '--user', 'probe', '--tls', 'none', '--insecure-test-server', '--password-from-env') @{ POSTKAUZ_PASSWORD_PROBE = 'x' }
    $r2 = Invoke-Pk @('account', 'login', 'probe')
    Add-Check '1.3' 'account login auf einem Passwort-Konto -> Exit 2' ($r.Exit -eq 0 -and $r2.Exit -eq 2) ($r.Text + "`n" + $r2.Text)
    $r = Invoke-Pk @('account', 'remove', 'probe')
    Add-Check '1.4' 'Probekonto wieder entfernt' ($r.Exit -eq 0) $r.Text
    $r = Invoke-Pk @('key', 'create', '--name', 'test-agent', '--role', 'admin', '--expires', '1d')
    Add-Check '1.5' 'Schluessel test-agent (Rolle admin, 1 Tag) fuer die human_only-Probe' ($r.Exit -eq 0) $r.Text

    $live = $Zustimmung -match '^\s*ja\b'
    if (-not $live) {
        Write-Log ''
        Write-Log 'INFO Teil 2 uebersprungen: keine Zustimmung des Betreibers (-Zustimmung "ja, <Datum Uhrzeit>")'
    }

    # ------------------------------------------------ Teil 2: echtes Konto, nur lesend
    if ($live) {
        Write-Log ''
        Write-Log ('Zustimmung des Betreibers zu D9 gegen sein echtes Microsoft-Postfach: ' + $Zustimmung)
        Write-Log '===== D9.2 Microsoft-Postfach, nur lesend'
        $acc = $script:Account
        $r = Invoke-Pk @('account', 'add', $acc, '--imap', 'outlook.office365.com:993', '--user', $User, '--auth', 'oauth2', '--client-id', $ClientId, '--tenant', $Tenant)
        Add-Check '2.1' 'account add outlook --auth oauth2 -> angelegt, noch kein Token' ($r.Exit -eq 0 -and (Test-Has $r.Out 'no token yet, sign in with: account login outlook')) $r.Text

        $r = Invoke-Pk @('doctor', '--json')
        $d = Get-Json $r.Out
        $da = $null
        if ($d) { $da = @($d.accounts | Where-Object { $_.name -eq $acc }) | Select-Object -First 1 }
        Add-Check '2.2' 'doctor vor der Anmeldung: token missing' ($null -ne $da -and $da.token -eq 'missing') $r.Text

        $r = Invoke-Pk @('account', 'login', $acc, '--key', 'test-agent')
        Add-Check '2.3' 'account login --key test-agent -> Exit 3 human_only (kein Browser)' ($r.Exit -eq 3 -and (Test-Has $r.All 'human_only')) $r.Text

        $r = Invoke-Pk @('account', 'login', $acc, '--dry-run', '--json')
        $j = Get-Json $r.Out
        $script:Ref = ''
        if ($j) { $script:Ref = [string] $j.credential_ref }
        Add-Check '2.4' 'account login --dry-run -> would_store, kein Browser' ($r.Exit -eq 0 -and $j -and $j.refresh_token -eq 'would_store' -and $script:Ref -match '^account/[0-9a-f]{8}/outlook$') $r.Text

        Confirm-Next 'Gleich oeffnet sich der Browser mit der Anmeldeseite von Microsoft. Melde dich mit dem Postfach an und stimme beim ersten Mal den Berechtigungen zu. Geht kein Browser auf, oeffne die Adresse, die unten erscheint. Frist: 5 Minuten.'
        $r = Invoke-PkLive @('account', 'login', $acc)
        Add-Check '2.5' 'account login (Browser) -> signed in, Refresh-Token im Schluesselbund' ($r.Exit -eq 0 -and (Test-Has $r.Out 'signed in; refresh token stored in the OS keyring as account/')) $r.Text

        $r = Invoke-Pk @('account', 'test', $acc)
        Add-Check '2.6' 'account test -> login ok (XOAUTH2)' ($r.Exit -eq 0 -and (Test-Has $r.Out 'login:\s+ok \(XOAUTH2\)')) $r.Text

        $r = Invoke-Pk @('sync', '-a', $acc, '--mailbox', 'INBOX', '--window', '200') @{} 1800
        Add-Check '2.7' 'sync -a outlook --mailbox INBOX --window 200 -> Exit 0 (nur lesen; groessere Mails als 1 MiB werden uebersprungen)' ($r.Exit -eq 0) $r.Text

        if ($SearchWord) {
            $r = Invoke-Pk @('search', $SearchWord, '-a', $acc, '--limit', '5', '--json')
            $hits = @(($r.Out -split "`r?`n") | Where-Object { $_.Trim().StartsWith('{') -and $_ -match '"id"' })
            # Suchtreffer enthalten Mailinhalt: im Protokoll nur die Zahl.
            Add-Check '2.8' 'search mit einem Wort aus einer eigenen Mail -> mindestens 1 Treffer' ($r.Exit -eq 0 -and $hits.Count -ge 1) ('exit ' + $r.Exit + ', Treffer ' + $hits.Count)
        } else {
            Write-Log 'INFO 2.8 uebersprungen: kein -SearchWord angegeben'
        }

        $r = Invoke-Pk @('doctor', '--json')
        $d = Get-Json $r.Out
        $da = $null
        if ($d) { $da = @($d.accounts | Where-Object { $_.name -eq $acc }) | Select-Object -First 1 }
        Add-Check '2.9' 'doctor: OAuth2 token stored, token_expiry und token_updated_at gesetzt' ($null -ne $da -and $da.token -eq 'stored' -and $da.token_expiry -and $da.token_updated_at) $r.Text

        $r = Invoke-Pk @('audit', '--since', '2h', '--limit', '1000', '--json')
        $ev = @(($r.Out -split "`r?`n") | Where-Object { $_.Trim().StartsWith('{') } | ForEach-Object { Get-Json $_ } | Where-Object { $null -ne $_ })
        $refresh = @($ev | Where-Object { $_.operation -eq 'oauth.refresh' -and $_.result -eq 'ok' })
        $logins = @($ev | Where-Object { $_.operation -eq 'accounts.login' -and $_.result -eq 'ok' })
        $denied = @($ev | Where-Object { $_.operation -eq 'accounts.login' -and $_.result -eq 'denied' -and $_.reason -eq 'human_only' })
        Add-Check '2.10' 'Audit: accounts.login ok, human_only-Ablehnung, oauth.refresh ok (Systemereignis)' ($logins.Count -ge 1 -and $denied.Count -ge 1 -and $refresh.Count -ge 1) ('accounts.login ok ' + $logins.Count + ', denied ' + $denied.Count + ', oauth.refresh ok ' + $refresh.Count)
        Add-Check '2.11' 'Audit enthaelt kein Token (keine Token-Felder, kein JWT)' (-not (Test-Has $r.Out '(access_token|refresh_token)"\s*:\s*"[^"\[]|eyJ[A-Za-z0-9_-]{8,}\.')) ''

        Confirm-Next 'Jetzt die Anmeldung mit Code (--device-code): Unten erscheinen eine Adresse und ein Code. Oeffne die Adresse auf einem beliebigen Geraet (auch Handy), gib den Code ein und melde dich an.'
        $r = Invoke-PkLive @('account', 'login', $acc, '--device-code')
        Add-Check '2.12' 'account login --device-code -> Code angezeigt, signed in' ($r.Exit -eq 0 -and (Test-Has $r.Err 'enter the code') -and (Test-Has $r.Out 'signed in; refresh token stored')) $r.Text
        $r = Invoke-Pk @('account', 'test', $acc)
        Add-Check '2.13' 'account test nach der Code-Anmeldung -> login ok (XOAUTH2)' ($r.Exit -eq 0 -and (Test-Has $r.Out 'login:\s+ok \(XOAUTH2\)')) $r.Text

        $r = Invoke-Pk @('doctor', '--secrets')
        Add-Check '2.14' 'doctor --secrets: Refresh-Token weder in Log noch Konfiguration noch Datenbank (kein FAIL)' ($r.Exit -eq 0 -and -not (Test-Has $r.Out '(?m)^\s*FAIL')) $r.Text

        $r = Invoke-Pk @('draft', 'list', '-a', $acc, '--json')
        Add-Check '2.15' 'kein Entwurf im Microsoft-Konto angelegt (draft list leer)' ($r.Exit -eq 0 -and -not (Test-Has $r.Out '"draft_id"\s*:\s*[0-9]')) $r.Text

        if ($Entfernen) {
            Write-Log ''
            Write-Log '===== D9.3 Abbau'
            # Ueber das Programm, nicht cmdkey: account remove loescht den
            # Haupteintrag und alle Teile (jede Generation, bis 32 Teile).
            $r = Invoke-Pk @('account', 'remove', $acc, '--json')
            $j = Get-Json $r.Out
            Add-Check '3.1' 'account remove -> Konto entfernt, Token samt Teilen geloescht (secret_deleted true)' ($r.Exit -eq 0 -and $j -and $j.secret_deleted -eq $true) $r.Text
            if ($script:Ref) {
                # Ganze Liste (der Filter /list:<ziel> wiederholt das Ziel in
                # der Kopfzeile); gesucht wird "/<kennung>/<konto>" am
                # Zeilenende, das trifft Haupteintrag und Teile jeder Generation.
                $tail = $script:Ref -replace '^account', ''
                $o = (& cmdkey.exe '/list') -join "`n"
                Add-Check '3.2' 'Schluesselbund: kein Eintrag des Kontos mehr (Haupteintrag und Teile; cmdkey /list, nur lesend)' (-not ($o -match ('(?m)' + [regex]::Escape($tail) + '\s*$'))) ''
            }
            Remove-Item -LiteralPath $script:Data -Recurse -Force
            Add-Check '3.3' 'Datenverzeichnis geloescht' (-not (Test-Path -LiteralPath $script:Data)) ''
            Write-Log 'INFO Zustimmung fuer die App im Microsoft-Konto entziehen: https://account.live.com/consent/Manage (siehe docs/betrieb_microsoft.md)'
        } else {
            Write-Log 'INFO Konto und Token bleiben (ohne -Entfernen); Abbau siehe docs/abnahme_D9.md, Abschnitt 5'
        }
    }
    $exitCode = 0
} catch {
    $script:NFail++
    $msg = $_.Exception.Message
    $script:Fails.Add('X Skript abgebrochen')
    if ($_.InvocationInfo) { $msg += ' (Zeile ' + $_.InvocationInfo.ScriptLineNumber + ')' }
    if ($script:Log) {
        Write-Log ''
        Write-Log ('FAIL X Skript abgebrochen: ' + $msg)
    } else {
        try { Write-Host ('FAIL X Skript abgebrochen: ' + (Hide-Secrets $msg)) } catch { }
    }
} finally {
    if ($script:Log) {
        Write-Log ''
        Write-Log ('Abnahme D9 beendet ' + (Get-Date -Format 'dd.MM.yyyy HH:mm:ss'))
        if ($script:Fails.Count -gt 0) {
            Write-Log 'Abweichungen:'
            foreach ($f in $script:Fails) { Write-Log ('  FAIL ' + $f) }
        }
        Write-Log ('D9: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL')
    }
}
if ($script:NFail -gt 0) { exit 1 }
exit $exitCode
