<#
.SYNOPSIS
    Abnahme D4 (PostKauz, Schnitt 2): Entwuerfe, Freigabe, Pruef-Hook, MCP.

.DESCRIPTION
    Prueft die Punkte 1 bis 7 und 9 der Checkliste D4 (auftraege/A02_s2.md,
    Teil D) auf Windows 11 mit Windows PowerShell 5.1, ohne Docker, ohne Go,
    ohne Mailserver. Punkt 8 (Gespraech mit dem eigenen Claude) bereitet das
    Skript nur vor: MCP-Eintrag und Schluesseldatei. Anleitung:
    docs/abnahme_D4.md.

    Das Skript
      - legt unter -WorkDir einen frischen Ordner lauf-<Datum-Uhrzeit> an
        (Datenverzeichnis, config.toml, policy.toml, attach\, Schluessel);
        das Produktiv-Datenverzeichnis wird nie beruehrt,
      - baut die Testdaten selbst: Konto demo ohne Server (Remote bleibt
        pending; das ist erwartet), zwei synthetische Mails per
        import mbox, Identitaet demo mit Signatur, Policy-Datei mit dem
        Beispiel-Hook check-example.ps1, attach_dir mit Beispieldatei,
        Schluessel test-agent (reader) und test-assistant (assistant),
      - ruft postkauz.exe immer mit --data-dir und --config auf, ohne
        Fenster, und vergleicht jede Ausgabe mit dem Erwarteten,
      - prueft MCP (Punkte 7 und 9) mit abnahme_D4_mcp.py (Python, stdio),
      - schreibt das Protokoll (Schluessel maskiert) nach
        <WorkDir>\D4_windows.txt und in den Laufordner.

    Jede Pruefung ist eine Zeile "OK <Nr> <Beschreibung>" oder
    "FAIL <Nr> <Beschreibung>", darunter eingerueckt die Ist-Ausgabe
    (gekuerzt). Letzte Zeile: "D4: <n> OK, <m> FAIL". Exit 0 nur ohne FAIL.

    Geprueft wird an stabilen Merkmalen (Status, Exit-Code, Zaehlungen,
    Schluesselwoerter wie human_only, approval_stale, hook: pass|fail),
    nicht an ganzen Hinweissaetzen.

.PARAMETER Exe
    Pfad zu postkauz.exe (der zu pruefende Stand).

.PARAMETER WorkDir
    Arbeitsordner; darin entsteht je Lauf ein neuer Unterordner.

.PARAMETER Python
    Pfad zu python.exe (3.8 oder neuer) fuer die MCP-Probe.

.PARAMETER Hook
    Pfad zu check-example.ps1. Standard: neben diesem Skript, sonst
    ..\deploy\hooks\check-example.ps1 (Repository).

.PARAMETER Probe
    Pfad zu abnahme_D4_mcp.py. Standard: neben diesem Skript.

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File abnahme_D4.ps1 -Exe C:\Users\Admin\AI\Cowork_Temp\PostKauz\d4\postkauz.exe
#>
param(
    [Parameter(Mandatory = $true)]
    [string] $Exe,
    [string] $WorkDir = 'C:\Users\Admin\AI\Cowork_Temp\PostKauz\d4',
    [string] $Python = 'C:\Users\Admin\AppData\Local\Programs\Python\Python312\python.exe',
    [string] $Hook = '',
    [string] $Probe = ''
)

$ErrorActionPreference = 'Stop'
$script:Utf8 = New-Object System.Text.UTF8Encoding $false
$script:NOk = 0
$script:NFail = 0
$script:Log = $null
$script:LogCopy = $null
$script:Fails = New-Object System.Collections.Generic.List[string]
try { [Console]::OutputEncoding = $script:Utf8 } catch { }

# ---------------------------------------------------------------- Hilfen

function Hide-Keys {
    param([string] $Text)
    if ($null -eq $Text) { return '' }
    return [regex]::Replace($Text, 'pk_key_[A-Za-z0-9_-]+', 'pk_key_********')
}

function Write-Log {
    param([string] $Text)
    $t = Hide-Keys $Text
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

# Startet ein Programm ohne Fenster, liest stdout/stderr als UTF-8.
function Invoke-Native {
    param([string] $File, [string[]] $ArgList, [hashtable] $EnvExtra = @{}, [int] $TimeoutSec = 180)
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
    $psi.WorkingDirectory = $script:Run
    foreach ($k in @('POSTKAUZ_KEY', 'POSTKAUZ_KEY_FILE', 'POSTKAUZ_DATA_DIR', 'POSTKAUZ_CONFIG', 'POSTKAUZ_POLICY', 'POSTKAUZ_PASSWORD_DEMO')) {
        if ($psi.EnvironmentVariables.ContainsKey($k)) { $psi.EnvironmentVariables.Remove($k) }
    }
    foreach ($k in $EnvExtra.Keys) { $psi.EnvironmentVariables[$k] = [string] $EnvExtra[$k] }
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
    [void] $to.Wait(10000)
    [void] $te.Wait(10000)
    $code = -1
    if (-not $timedOut) { $code = $p.ExitCode }
    return New-Object PSObject -Property @{ Exit = $code; Out = [string] $to.Result; Err = [string] $te.Result; TimedOut = $timedOut }
}

# postkauz.exe mit festem Datenverzeichnis und fester Konfiguration.
function Invoke-Pk {
    param([string[]] $A, [hashtable] $EnvExtra = @{})
    $all = @('--data-dir', $script:Data, '--config', $script:Cfg) + $A
    $r = Invoke-Native $script:ExePath $all $EnvExtra
    $shown = 'postkauz ' + ($A -join ' ')
    $text = '> ' + $shown + "`n" + $r.Out
    if ($r.Err) { $text = $text.TrimEnd() + "`n[stderr] " + $r.Err }
    if ($r.TimedOut) { $text = $text.TrimEnd() + "`n[Zeitgrenze erreicht, Prozess beendet]" }
    $text = $text.TrimEnd() + "`n[exit " + $r.Exit + ']'
    $r | Add-Member -NotePropertyName Text -NotePropertyValue $text
    $r | Add-Member -NotePropertyName All -NotePropertyValue ($r.Out + "`n" + $r.Err)
    return $r
}

function Test-Has {
    param([string] $Text, [string] $Pattern)
    return [regex]::IsMatch($Text, $Pattern)
}

function Get-Json {
    param([string] $Text)
    try { return ($Text.Trim() | ConvertFrom-Json) } catch { return $null }
}

function Get-DraftStatus {
    param([int] $Id)
    $r = Invoke-Pk @('draft', 'show', [string] $Id, '--json')
    $j = Get-Json $r.Out
    if ($null -eq $j) { return '(keine JSON-Ausgabe)' }
    return [string] $j.status
}

function Get-AuditEvents {
    param([string] $Since)
    $r = Invoke-Pk @('audit', '--since', $Since, '--limit', '1000', '--json')
    $events = New-Object System.Collections.Generic.List[object]
    foreach ($line in ($r.Out -split "`r?`n")) {
        $t = $line.Trim()
        if (-not $t.StartsWith('{')) { continue }
        $o = Get-Json $t
        if ($null -ne $o -and $null -ne $o.operation) { $events.Add($o) }
    }
    return New-Object PSObject -Property @{ Run = $r; Events = $events }
}

function ConvertTo-JsonString {
    param([string] $S)
    return '"' + $S.Replace('\', '\\').Replace('"', '\"') + '"'
}

function Find-Beside {
    param([string] $Given, [string[]] $Candidates)
    if ($Given) { return $Given }
    foreach ($c in $Candidates) { if (Test-Path -LiteralPath $c) { return $c } }
    return $Candidates[0]
}

# ---------------------------------------------------------------- Lauf

$exitCode = 1
$started = Get-Date
try {
    $here = $PSScriptRoot
    if (-not $here) { $here = (Get-Location).Path }
    $script:ExePath = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Exe)
    $WorkDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($WorkDir)
    $Hook = Find-Beside $Hook @((Join-Path $here 'check-example.ps1'), (Join-Path $here '..\deploy\hooks\check-example.ps1'))
    $Probe = Find-Beside $Probe @((Join-Path $here 'abnahme_D4_mcp.py'))

    # Nie das Produktiv-Datenverzeichnis und nie die umgeleiteten
    # App-Ordner (MSIX-Falle A7): der Laufordner liegt unter -WorkDir.
    $wd = $WorkDir.TrimEnd('\') + '\'
    $forbidden = @('C:\Claude\postkauz\')
    if ($env:LOCALAPPDATA) { $forbidden += @(($env:LOCALAPPDATA.TrimEnd('\') + '\postkauz\'), ($env:LOCALAPPDATA.TrimEnd('\') + '\Packages\')) }
    if ($env:APPDATA) { $forbidden += ($env:APPDATA.TrimEnd('\') + '\') }
    foreach ($f in $forbidden) {
        if ($wd.StartsWith($f, [System.StringComparison]::OrdinalIgnoreCase)) { throw ('WorkDir liegt in einem gesperrten Ordner: ' + $f) }
    }
    if ($WorkDir.Contains("'")) { throw 'WorkDir darf kein einfaches Anfuehrungszeichen enthalten (TOML)' }

    New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    $script:Run = Join-Path $WorkDir ('lauf-' + $stamp)
    if (Test-Path -LiteralPath $script:Run) { throw ('Laufordner existiert schon: ' + $script:Run) }
    New-Item -ItemType Directory -Path $script:Run | Out-Null
    foreach ($sub in @('data', 'attach', 'keys', 'hooks', 'input')) { New-Item -ItemType Directory -Path (Join-Path $script:Run $sub) | Out-Null }
    $script:Data = Join-Path $script:Run 'data'
    $script:Cfg = Join-Path $script:Run 'config.toml'
    $policyFile = Join-Path $script:Run 'policy.toml'
    $attach = Join-Path $script:Run 'attach'
    $keys = Join-Path $script:Run 'keys'
    $inp = Join-Path $script:Run 'input'
    $script:Log = Join-Path $script:Run 'D4_windows.txt'
    $script:LogCopy = Join-Path $WorkDir 'D4_windows.txt'

    Write-Log ('Abnahme D4 gestartet ' + (Get-Date -Format 'dd.MM.yyyy HH:mm:ss') + ' auf ' + $env:COMPUTERNAME)
    Write-Log ('PowerShell ' + $PSVersionTable.PSVersion.ToString() + ', Skript abnahme_D4.ps1')
    Write-Log ('Programm:  ' + $script:ExePath)
    Write-Log ('Laufordner: ' + $script:Run)

    # ------------------------------------------------ 0 Vorbereitung
    Write-Log ''
    Write-Log '===== 0 Vorbereitung (Testdaten ohne Server)'
    $ok = (Test-Path -LiteralPath $script:ExePath -PathType Leaf) -and (Test-Path -LiteralPath $Hook -PathType Leaf)
    $script:HaveProbe = (Test-Path -LiteralPath $Probe -PathType Leaf) -and (Test-Path -LiteralPath $Python -PathType Leaf)
    $det = 'postkauz.exe: ' + (Test-Path -LiteralPath $script:ExePath) + "`nHook: " + $Hook + ' ' + (Test-Path -LiteralPath $Hook) + "`nMCP-Probe: " + $Probe + ' ' + (Test-Path -LiteralPath $Probe) + "`nPython: " + $Python + ' ' + (Test-Path -LiteralPath $Python) + ' (fehlt eins davon: nur die Punkte 7 und 9 scheitern)'
    if ($ok) {
        $det += "`nSHA-256 postkauz.exe:  " + (Get-FileHash -LiteralPath $script:ExePath -Algorithm SHA256).Hash
        $det += "`nSHA-256 check-example: " + (Get-FileHash -LiteralPath $Hook -Algorithm SHA256).Hash
    }
    if ($script:HaveProbe) { $det += "`nSHA-256 MCP-Probe:     " + (Get-FileHash -LiteralPath $Probe -Algorithm SHA256).Hash }
    Add-Check '0.1' 'Dateien vorhanden (postkauz.exe, check-example.ps1; fuer MCP abnahme_D4_mcp.py und python.exe)' ($ok -and $script:HaveProbe) $det
    if (-not $ok) { throw 'postkauz.exe oder check-example.ps1 fehlt; Abbruch' }

    # Hook in den Laufordner kopieren: Die Policy-Datei zeigt auf diese Kopie.
    $hookCopy = Join-Path $script:Run 'hooks\check-example.ps1'
    Copy-Item -LiteralPath $Hook -Destination $hookCopy
    if ($hookCopy.Contains("'")) { throw 'Hook-Pfad darf kein einfaches Anfuehrungszeichen enthalten (TOML)' }

    $cfgText = "[paths]`r`nattach_dir = '" + $attach + "'`r`n[operator]`r`nname = 'D4-Abnahme'`r`n"
    [System.IO.File]::WriteAllText($script:Cfg, $cfgText, $script:Utf8)
    $polText = "[draft]`r`ncheck_hook = ['powershell.exe', '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', '" + $hookCopy + "', '{meta_json}', '{body_text}']`r`ncheck_hook_timeout = '30s'`r`n"
    [System.IO.File]::WriteAllText($policyFile, $polText, $script:Utf8)
    [System.IO.File]::WriteAllText((Join-Path $attach 'beispiel.txt'), "Beispieldatei fuer attach_dir (Abnahme D4)`r`n", $script:Utf8)
    [System.IO.File]::WriteAllText((Join-Path $inp 'signatur.txt'), "Viele Gruesse`nDemo Buero (D4-Signatur)`n", $script:Utf8)
    [System.IO.File]::WriteAllText((Join-Path $inp 'antwort.md'), "Hallo Frau Beispiel,`n`nFreitag um 10 Uhr passt. KANARIENVOGEL`n", $script:Utf8)
    [System.IO.File]::WriteAllText((Join-Path $inp 'anders.md'), "Hallo Frau Beispiel,`n`nFreitag um 10 Uhr passt leider doch nicht. ANDERSVOGEL`n", $script:Utf8)

    # Zwei synthetische Mails (Adressen nur example.org): Anfrage und Antwort
    # mit References; die Antwort darauf muss beide Message-IDs tragen.
    $mbox = @'
From MAILER-DAEMON Mon Oct 19 09:00:00 2026
Message-ID: <d4.anfrage@corpus.example.org>
Date: Mon, 19 Oct 2026 09:00:00 +0200
From: Demo Buero <demo@example.org>
To: Erika Beispiel <erika@example.org>
Subject: Termin Heizungswartung
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8
Content-Transfer-Encoding: quoted-printable

Guten Tag Frau Beispiel,

passt Ihnen Donnerstag, 9 Uhr, f=C3=BCr die Wartung der Heizung?

Viele Gr=C3=BC=C3=9Fe
Demo Buero

From MAILER-DAEMON Tue Oct 20 10:15:00 2026
Message-ID: <d4.antwort@corpus.example.org>
In-Reply-To: <d4.anfrage@corpus.example.org>
References: <d4.anfrage@corpus.example.org>
Date: Tue, 20 Oct 2026 10:15:00 +0200
From: Erika Beispiel <erika@example.org>
To: Demo Buero <demo@example.org>
Subject: Re: Termin Heizungswartung
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8
Content-Transfer-Encoding: quoted-printable

Hallo,

Donnerstag geht leider nicht. W=C3=A4re Freitag um 10 Uhr m=C3=B6glich?
Der Schl=C3=BCssel liegt beim Hausmeister.

Erika Beispiel

'@
    $mboxFile = Join-Path $inp 'korpus.mbox'
    [System.IO.File]::WriteAllText($mboxFile, ($mbox -replace "`r`n", "`n"), $script:Utf8)

    $r = Invoke-Pk @('version')
    Add-Check '0.2' 'postkauz version laeuft (Exit 0)' ($r.Exit -eq 0 -and (Test-Has $r.Out '^postkauz ')) $r.Text

    $r = Invoke-Pk @('account', 'add', 'demo', '--imap', '127.0.0.1:1', '--user', 'demo@example.org', '--insecure-test-server', '--password-from-env') @{ 'POSTKAUZ_PASSWORD_DEMO' = 'unbenutzt-kein-server' }
    Add-Check '0.3' 'Konto demo ohne Server angelegt (Passwort nur aus der Umgebung, nicht gespeichert)' ($r.Exit -eq 0 -and (Test-Has $r.Out 'account demo added')) $r.Text

    $r = Invoke-Pk @('import', 'mbox', $mboxFile, '-a', 'demo', '--mailbox', 'INBOX', '--quiet')
    Add-Check '0.4' 'import mbox: 2 neue Nachrichten im Spiegel' ($r.Exit -eq 0 -and (Test-Has $r.Out 'new: 2, existing: 0, skipped: 0, errors: 0')) $r.Text

    $r = Invoke-Pk @('show', 'd4.antwort@corpus.example.org', '--json')
    $j = Get-Json $r.Out
    $mailId = 0
    if ($null -ne $j -and $null -ne $j.id) { $mailId = [int] $j.id }
    Add-Check '0.5' 'Korpusmail fuer die Antwort gefunden (lokale ID)' ($r.Exit -eq 0 -and $mailId -gt 0) ('lokale ID ' + $mailId + "`n" + (Limit-Text $r.Text 3 400))

    $r = Invoke-Pk @('identity', 'add', '-a', 'demo', '--email', 'demo@example.org', '--name', 'Demo Buero', '--signature-file', (Join-Path $inp 'signatur.txt'), '--default')
    Add-Check '0.6' 'Identitaet demo mit Signatur angelegt' ($r.Exit -eq 0 -and (Test-Has $r.Out 'identity 1 of account demo')) $r.Text

    $r = Invoke-Pk @('doctor')
    $ok = $r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^policy:.*\(found') -and (Test-Has $r.Out ('(?m)^attach:\s+' + [regex]::Escape($attach))) -and (Test-Has $r.Out ('(?m)^data dir:\s+' + [regex]::Escape($script:Data))) -and -not (Test-Has $r.Out '(?m)^\s*really:')
    Add-Check '0.7' 'doctor: Datenverzeichnis des Laufs (nicht umgeleitet), Policy-Datei gefunden, attach_dir' $ok $r.Text

    $keyFiles = @{}
    $ok = $true
    $det = ''
    foreach ($kv in @(@('test-agent', 'reader'), @('test-assistant', 'assistant'))) {
        $r = Invoke-Pk @('key', 'create', '--name', $kv[0], '--role', $kv[1])
        $m = [regex]::Match($r.Out, 'pk_key_[A-Za-z0-9_-]+')
        $kf = Join-Path $keys ($kv[0] + '.key')
        if ($r.Exit -eq 0 -and $m.Success) {
            [System.IO.File]::WriteAllText($kf, $m.Value, [System.Text.Encoding]::ASCII)
            $keyFiles[$kv[0]] = $kf
            $det += 'Schluessel ' + $kv[0] + ' (' + $kv[1] + '): erhalten, Laenge ' + $m.Value.Length + ', Datei ' + $kf + "`n"
        } else {
            $ok = $false
            $det += $r.Text + "`n"
        }
    }
    $r = Invoke-Pk @('key', 'list')
    $ok = $ok -and $r.Exit -eq 0 -and -not (Test-Has $r.Out 'pk_key_')
    Add-Check '0.8' 'Schluessel test-agent (reader) und test-assistant (assistant) angelegt; key list zeigt keinen Schluessel' $ok ($det + $r.Text)
    if (-not $ok) { throw 'Schluessel fehlen; Abbruch' }

    # ------------------------------------------------ Punkt 1
    Write-Log ''
    Write-Log '===== D4.1 draft reply und draft show --eml'
    $r = Invoke-Pk @('draft', 'reply', [string] $mailId, '--body-file', (Join-Path $inp 'antwort.md'), '--attach', 'beispiel.txt')
    $m = [regex]::Match($r.Out, 'draft (\d+) created \(status draft, remote pending\)')
    $draft = 0
    if ($m.Success) { $draft = [int] $m.Groups[1].Value }
    Add-Check '1.1' 'draft reply -> "draft 1 created (status draft, remote pending)", Exit 0' ($r.Exit -eq 0 -and $draft -eq 1) $r.Text
    if ($draft -eq 0) { $draft = 1 }
    $d = [string] $draft

    $r = Invoke-Pk @('draft', 'show', $d, '--eml')
    $eml = $r.Out
    Add-Check '1.2' 'draft show --eml: In-Reply-To und References der Ausgangsnachricht' ($r.Exit -eq 0 -and (Test-Has $eml '(?m)^In-Reply-To: <d4\.antwort@corpus\.example\.org>\r?$') -and (Test-Has $eml '(?m)^References: <d4\.anfrage@corpus\.example\.org> <d4\.antwort@corpus\.example\.org>\r?$')) (Limit-Text $r.Text 12)
    Add-Check '1.3' 'draft show --eml: Betreff mit Re:, eigene Message-ID' ((Test-Has $eml '(?m)^Subject: Re: Termin Heizungswartung\r?$') -and (Test-Has $eml '(?mi)^Message-Id: <pk\.[0-9a-f]{20}@example\.org>')) ([regex]::Match($eml, '(?m)^Subject:.*$').Value + "`n" + [regex]::Match($eml, '(?mi)^Message-Id:.*$').Value)
    Add-Check '1.4' 'draft show --eml: Zitat (Einleitungszeile und "> "-Zeilen)' ((Test-Has $eml 'On Tue, 20 Oct 2026 10:15 \+0200, Erika Beispiel <erika@example\.org> wrote:') -and (Test-Has $eml '(?m)^> Der Schl=C3=BCssel liegt beim Hausmeister\.')) ([regex]::Match($eml, '(?m)^On Tue.*$').Value + "`n" + [regex]::Match($eml, '(?m)^> Der.*$').Value)
    Add-Check '1.5' 'draft show --eml: Signatur der Identitaet, Anhang aus attach_dir' ((Test-Has $eml '(?m)^Demo Buero \(D4-Signatur\)') -and (Test-Has $eml 'filename=beispiel\.txt')) ([regex]::Match($eml, '(?m)^Demo Buero \(D4.*$').Value + "`n" + [regex]::Match($eml, 'Content-Disposition:.*').Value)

    $r = Invoke-Pk @('draft', 'show', $d)
    $ue = [string] [char] 0x00FC
    $ae = [string] [char] 0x00E4
    $ok = $r.Exit -eq 0 -and (Test-Has $r.Out ('Der Schl' + $ue + 'ssel liegt beim Hausmeister')) -and (Test-Has $r.Out ('W' + $ae + 're Freitag')) -and (Test-Has $r.Out '(?m)^remote:\s+pending') -and (Test-Has $r.Out 'status draft')
    Add-Check '1.6' 'draft show: Umlaute im Zitat korrekt (UTF-8), status draft, remote pending' $ok $r.Text

    # ------------------------------------------------ Punkt 2
    Write-Log ''
    Write-Log '===== D4.2 draft request mit Beispiel-Hook'
    $r = Invoke-Pk @('draft', 'request', $d)
    $m = [regex]::Match($r.Out, 'status pending_approval, approval (\d+)')
    $ap1 = 0
    if ($m.Success) { $ap1 = [int] $m.Groups[1].Value }
    Add-Check '2.1' 'draft request -> hook: pass, status pending_approval, approval 1' ($r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^hook: pass') -and $ap1 -eq 1) $r.Text

    # ------------------------------------------------ Punkt 3
    Write-Log ''
    Write-Log '===== D4.3 Betreff mit TODO: Hook scheitert'
    $r = Invoke-Pk @('draft', 'edit', $d, '--subject', 'TODO Re: Termin Heizungswartung')
    Add-Check '3.1' 'draft edit --subject "TODO Re: ..." -> status pending_approval -> draft, Freigabe 1 superseded' ($r.Exit -eq 0 -and (Test-Has $r.Out 'status pending_approval -> draft') -and (Test-Has $r.Out 'approval 1 superseded')) (Limit-Text $r.Text 4)
    $r = Invoke-Pk @('draft', 'request', $d)
    Add-Check '3.2' 'draft request -> hook: fail mit Begruendung des Beispiel-Hooks ("subject contains TODO"), status bleibt draft' ($r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^hook: fail') -and (Test-Has $r.Out 'subject contains TODO') -and (Test-Has $r.Out 'status stays draft') -and -not (Test-Has $r.Out 'pending_approval')) $r.Text
    $st = Get-DraftStatus $draft
    Add-Check '3.3' 'draft show --json: status draft' ($st -eq 'draft') ('status ' + $st)
    $left = @()
    if ($env:TEMP -and (Test-Path -LiteralPath $env:TEMP)) {
        $left = @(Get-ChildItem -LiteralPath $env:TEMP -Directory -Filter 'postkauz-hook-*' -ErrorAction SilentlyContinue | Where-Object { $_.CreationTime -ge $started })
    }
    Add-Check '3.4' 'nach den Hook-Laeufen kein Temp-Ordner postkauz-hook-* uebrig' ($left.Count -eq 0) ('TEMP: ' + $env:TEMP + '; neue Ordner postkauz-hook-*: ' + $left.Count)

    # ------------------------------------------------ Punkt 4
    Write-Log ''
    Write-Log '===== D4.4 Freigabe und Aenderung danach (approval_stale)'
    $r = Invoke-Pk @('draft', 'edit', $d, '--subject', 'Re: Termin Heizungswartung')
    Add-Check '4.1' 'draft edit --subject "Re: ..." -> Exit 0, status draft' ($r.Exit -eq 0 -and (Test-Has $r.Out ('draft ' + $d + ' changed \(status draft -> draft'))) (Limit-Text $r.Text 3)
    $r = Invoke-Pk @('draft', 'request', $d)
    $m = [regex]::Match($r.Out, 'status pending_approval, approval (\d+)')
    $ap2 = 0
    if ($m.Success) { $ap2 = [int] $m.Groups[1].Value }
    Add-Check '4.2' 'draft request -> hook: pass, approval 2' ($r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^hook: pass') -and $ap2 -eq 2) $r.Text
    if ($ap2 -eq 0) { $ap2 = 2 }
    $r = Invoke-Pk @('approve', [string] $ap2)
    $st = Get-DraftStatus $draft
    Add-Check '4.3' 'approve 2 -> Status ready' ($r.Exit -eq 0 -and (Test-Has $r.Out 'is ready') -and $st -eq 'ready') ($r.Text + "`nstatus laut draft show --json: " + $st)
    $r = Invoke-Pk @('draft', 'edit', $d, '--body-file', (Join-Path $inp 'anders.md'))
    $st = Get-DraftStatus $draft
    Add-Check '4.4' 'draft edit --body-file anders.md -> status ready -> draft, Freigabe 2 superseded' ($r.Exit -eq 0 -and (Test-Has $r.Out 'status ready -> draft') -and (Test-Has $r.Out ('approval ' + $ap2 + ' superseded')) -and $st -eq 'draft') ((Limit-Text $r.Text 3) + "`nstatus laut draft show --json: " + $st)
    $r = Invoke-Pk @('approve', [string] $ap2)
    $st = Get-DraftStatus $draft
    Add-Check '4.5' 'approve 2 nach der Aenderung -> denied (approval_stale), Exit 3, Entwurf bleibt draft' ($r.Exit -eq 3 -and (Test-Has $r.All 'approval_stale') -and $st -eq 'draft') ($r.Text + "`nstatus laut draft show --json: " + $st)

    # ------------------------------------------------ Punkt 5
    Write-Log ''
    Write-Log '===== D4.5 human_only fuer Schluessel'
    $r = Invoke-Pk @('policy', 'show', '--key', 'test-agent')
    Add-Check '5.1' 'policy show --key test-agent zeigt "drafts.approve: human_only"' ($r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^\s*drafts\.approve: human_only\s*$')) $r.Text
    $r = Invoke-Pk @('draft', 'request', $d)
    $m = [regex]::Match($r.Out, 'status pending_approval, approval (\d+)')
    $ap3 = 0
    if ($m.Success) { $ap3 = [int] $m.Groups[1].Value }
    Add-Check '5.2' 'draft request -> hook: pass, approval 3 (Vorbereitung)' ($r.Exit -eq 0 -and (Test-Has $r.Out '(?m)^hook: pass') -and $ap3 -eq 3) $r.Text
    if ($ap3 -eq 0) { $ap3 = 3 }
    $r = Invoke-Pk @('approve', [string] $ap3, '--key', 'test-agent')
    Add-Check '5.3' 'approve 3 --key test-agent -> denied (human_only), Exit 3' ($r.Exit -eq 3 -and (Test-Has $r.All 'denied \(human_only\)')) $r.Text
    $r2 = Invoke-Pk @('approval', 'show', [string] $ap3, '--json')
    $j = Get-Json $r2.Out
    $aps = ''
    if ($null -ne $j) { $aps = [string] $j.status + ' / ' + [string] $j.draft_status }
    Add-Check '5.4' 'danach unveraendert: Freigabe 3 pending, Entwurf pending_approval' ($aps -eq 'pending / pending_approval') ('approval show 3 --json: ' + $aps)

    # ------------------------------------------------ Punkt 6
    Write-Log ''
    Write-Log '===== D4.6 Audit ohne Entwurfsinhalt'
    $r = Invoke-Pk @('audit', '--since', '10m')
    Add-Check '6.1' 'audit --since 10m laeuft (Exit 0)' ($r.Exit -eq 0 -and (Test-Has $r.Out 'transition')) (Limit-Text $r.Text 40 4000)
    $a = Get-AuditEvents '10m'
    $tr = @($a.Events | Where-Object { $_.reason -eq 'transition' -and $_.params.draft_id -eq $draft })
    $human = @($tr | Where-Object { $_.actor -eq 'human' })
    $hooks = @($tr | ForEach-Object { [string] $_.params.hook } | Where-Object { $_ }) | Sort-Object -Unique
    $tos = @($tr | ForEach-Object { [string] $_.params.to }) | Sort-Object -Unique
    $ok = $tr.Count -ge 9 -and $human.Count -eq $tr.Count -and ($hooks -contains 'pass') -and ($hooks -contains 'fail') -and ($tos -contains 'pending_approval') -and ($tos -contains 'ready') -and ($tos -contains 'draft')
    $det = 'Uebergaenge fuer draft ' + $d + ': ' + $tr.Count + ' (Akteur human: ' + $human.Count + '); hook: ' + ($hooks -join ',') + '; Zielstatus: ' + ($tos -join ',')
    foreach ($e in $tr) { $det += "`n  " + $e.id + ' ' + $e.actor + ' ' + $e.key + ' ' + $e.operation + ' ' + $e.params.from + ' -> ' + $e.params.to + ' hook=' + $e.params.hook + ' approval=' + $e.params.approval_id }
    Add-Check '6.2' 'je Uebergang eine Audit-Zeile (transition) mit Akteur human, Draft-ID, Status, Hook-Ergebnis (pass und fail)' $ok $det
    $words = @('KANARIENVOGEL', 'ANDERSVOGEL', 'Heizungswartung', 'erika@example.org', 'Erika Beispiel', 'beispiel.txt', 'D4-Signatur', 'Hausmeister')
    $hit = @($words | Where-Object { $a.Run.Out.IndexOf($_, [System.StringComparison]::OrdinalIgnoreCase) -ge 0 })
    Add-Check '6.3' 'audit --since 10m --json enthaelt kein Wort aus dem Entwurf (Text, Betreff, Adresse, Signatur, Anhang)' ($a.Run.Exit -eq 0 -and $a.Events.Count -gt 0 -and $hit.Count -eq 0) ('Eintraege: ' + $a.Events.Count + '; gesucht: ' + ($words -join ', ') + '; Treffer: [' + ($hit -join ', ') + '] (leer = gut)')
    $logText = ''
    $logPath = Join-Path $script:Data 'postkauz.log'
    if (Test-Path -LiteralPath $logPath) { $logText = [System.IO.File]::ReadAllText($logPath, $script:Utf8) }
    $hit = @($words + @('subject contains') | Where-Object { $logText.IndexOf($_, [System.StringComparison]::OrdinalIgnoreCase) -ge 0 })
    Add-Check '6.4' 'postkauz.log enthaelt weder Entwurfsinhalt noch Hook-Ausgabe' ($logText.Length -gt 0 -and $hit.Count -eq 0) ('Log: ' + $logText.Length + ' Zeichen; Treffer: [' + ($hit -join ', ') + '] (leer = gut)')

    # ------------------------------------------------ Punkt 7
    Write-Log ''
    Write-Log '===== D4.7 MCP (stdio-Probe mit Python)'
    $probeArgs = @('-I', $Probe, 'tools', '--exe', $script:ExePath, '--data-dir', $script:Data, '--config', $script:Cfg,
        '--reader-key-file', $keyFiles['test-agent'], '--assistant-key-file', $keyFiles['test-assistant'],
        '--reply-to', [string] $mailId, '--canary', 'MCPVOGEL')
    $pr = New-Object PSObject -Property @{ Exit = -1; Out = ''; Err = 'MCP-Probe oder python.exe fehlt (siehe 0.1)'; TimedOut = $false }
    if ($script:HaveProbe) { $pr = Invoke-Native $Python $probeArgs @{} 300 }
    $mcpDraft = 0
    $seen = 0
    foreach ($line in ($pr.Out -split "`r?`n")) {
        if ($line -match '^(OK|FAIL) (\S+) (.*)$') {
            $seen++
            if ($Matches[1] -eq 'OK') { $script:NOk++ } else { $script:NFail++; $script:Fails.Add($Matches[2] + ' ' + $Matches[3]) }
            Write-Log $line
        } elseif ($line -match '^VALUE draft_id=(\d+)') {
            $mcpDraft = [int] $Matches[1]
        } elseif ($line -match '^\s+\S') {
            Write-Log $line
        }
    }
    if ($pr.Err.Trim()) { Write-Log ('      [stderr der Probe] ' + (Limit-Text $pr.Err 8 800)) }
    Add-Check '7.0' 'MCP-Probe lief vollstaendig (6 Pruefungen, Exit 0)' ($pr.Exit -eq 0 -and $seen -eq 6 -and $mcpDraft -gt 0) ('exit ' + $pr.Exit + ', Pruefungen ' + $seen + ', Entwurf ueber MCP: ' + $mcpDraft + "`n" + ($pr.Out -split "`r?`n" | Where-Object { $_ -match '^MCP-Probe' }))
    $md = [string] $mcpDraft

    $r = Invoke-Pk @('draft', 'list')
    $ok = $r.Exit -eq 0 -and (Test-Has $r.Out ('(?m)^' + $md + '\s+demo\s+reply\s+draft\s+-\s+pending\s'))
    Add-Check '7.7' ('draft list zeigt den Entwurf ' + $md + ' aus MCP (reply, draft, remote pending)') ($mcpDraft -gt 0 -and $ok) $r.Text
    $a = Get-AuditEvents '1h'
    $agent = @($a.Events | Where-Object { $_.key -eq 'key:test-assistant' })
    $nonAgent = @($agent | Where-Object { $_.actor -ne 'agent' })
    $created = @($agent | Where-Object { $_.operation -eq 'drafts.create' -and $_.reason -eq 'transition' -and $_.params.draft_id -eq $mcpDraft })
    $dry = @($agent | Where-Object { $_.operation -eq 'drafts.create' -and $_.dry_run -eq $true })
    $hit = @(@('MCPVOGEL', 'Hausmeister', 'erika@example.org') | Where-Object { $a.Run.Out.IndexOf($_, [System.StringComparison]::OrdinalIgnoreCase) -ge 0 })
    $ok = $agent.Count -gt 0 -and $nonAgent.Count -eq 0 -and $created.Count -eq 1 -and $dry.Count -ge 1 -and $hit.Count -eq 0
    $det = 'Eintraege mit key:test-assistant: ' + $agent.Count + ' (davon nicht agent: ' + $nonAgent.Count + '); drafts.create transition fuer ' + $md + ': ' + $created.Count + '; Trockenlauf drafts.create: ' + $dry.Count + '; Inhaltswoerter: [' + ($hit -join ', ') + '] (leer = gut)'
    foreach ($e in ($agent | Select-Object -First 12)) { $det += "`n  " + $e.id + ' ' + $e.actor + ' ' + $e.key + ' ' + $e.operation + ' ' + $e.result + ' ' + $e.reason + ' dry_run=' + $e.dry_run }
    Add-Check '7.8' 'Audit: MCP-Aufrufe mit Akteur agent und key:test-assistant, Trockenlauf und echter Entwurf, kein Text' $ok $det

    # ------------------------------------------------ Punkt 8 (vorbereiten)
    Write-Log ''
    Write-Log '===== D4.8 Gespraech mit dem eigenen Claude (Betreiber; nur vorbereitet)'
    $snippet = "{`r`n  `"mcpServers`": {`r`n    `"postkauz-d4`": {`r`n      `"command`": " + (ConvertTo-JsonString $script:ExePath) + ",`r`n" +
        "      `"args`": [`"--data-dir`", " + (ConvertTo-JsonString $script:Data) + ", `"--config`", " + (ConvertTo-JsonString $script:Cfg) + ", `"mcp`"],`r`n" +
        "      `"env`": { `"POSTKAUZ_KEY_FILE`": " + (ConvertTo-JsonString $keyFiles['test-assistant']) + " }`r`n    }`r`n  }`r`n}"
    $snipFile = Join-Path $script:Run 'mcp-eintrag-d4.json'
    [System.IO.File]::WriteAllText($snipFile, $snippet + "`r`n", $script:Utf8)
    $sj = Get-Json $snippet
    $ok = $null -ne $sj -and -not (Test-Has $snippet 'pk_key_')
    Add-Check '8.0' 'MCP-Eintrag fuer test-assistant geschrieben (gueltiges JSON, Schluessel nur als Datei, nicht im Eintrag; derselbe Weg wie 7.2)' $ok ('Datei: ' + $snipFile + "`n" + $snippet)
    Write-Log ('      Satz fuer den Claude des Betreibers: "Beantworte Mail ' + $mailId + ' kurz, dass der Termin passt."')
    Write-Log ('      Erwartet: mail_create_draft mit dry_run: false; Ergebnis mit draft_id, message_id, remote: pending; danach "draft list" und "audit --since 10m --key-id key:test-assistant" (Akteur agent, kein Text)')

    # ------------------------------------------------ Punkt 9
    Write-Log ''
    Write-Log '===== D4.9 draft abandon, danach mail_update_draft'
    $r = Invoke-Pk @('draft', 'abandon', $md)
    Add-Check '9.1' ('draft abandon ' + $md + ' -> abandoned') ($mcpDraft -gt 0 -and $r.Exit -eq 0 -and (Test-Has $r.Out ('draft ' + $md + ': abandoned'))) $r.Text
    $probeArgs = @('-I', $Probe, 'closed', '--exe', $script:ExePath, '--data-dir', $script:Data, '--config', $script:Cfg,
        '--reader-key-file', $keyFiles['test-agent'], '--assistant-key-file', $keyFiles['test-assistant'], '--draft', $md)
    $pr = New-Object PSObject -Property @{ Exit = -1; Out = ''; Err = 'MCP-Probe oder python.exe fehlt (siehe 0.1)'; TimedOut = $false }
    if ($script:HaveProbe -and $mcpDraft -gt 0) { $pr = Invoke-Native $Python $probeArgs @{} 120 }
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
    Add-Check '9.0' 'MCP-Probe (Phase closed) lief vollstaendig (1 Pruefung, Exit 0)' ($pr.Exit -eq 0 -and $seen -eq 1) ('exit ' + $pr.Exit + ', Pruefungen ' + $seen)
    $a = Get-AuditEvents '1h'
    $den = @($a.Events | Where-Object { $_.key -eq 'key:test-assistant' -and $_.actor -eq 'agent' -and $_.operation -eq 'drafts.update' -and $_.result -eq 'denied' -and $_.reason -eq 'draft_closed' })
    Add-Check '9.3' 'Audit: drafts.update denied (draft_closed) mit Akteur agent und key:test-assistant' ($den.Count -ge 1) ('Treffer: ' + $den.Count)
    $st = Get-DraftStatus $mcpDraft
    Add-Check '9.4' ('Entwurf ' + $md + ' bleibt abandoned') ($st -eq 'abandoned') ('status ' + $st)

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
        # Noch kein Laufordner: Meldung nach %TEMP%, nie in ein Datenverzeichnis.
        try {
            $fallback = Join-Path $env:TEMP 'postkauz-D4_windows.txt'
            [System.IO.File]::WriteAllText($fallback, ('FAIL X Skript abgebrochen: ' + (Hide-Keys $msg) + "`r`nD4: 0 OK, 1 FAIL`r`n"), $script:Utf8)
            Write-Host ('FAIL X Skript abgebrochen: ' + $msg + ' (Meldung in ' + $fallback + ')')
        } catch { }
    }
} finally {
    if ($script:Log) {
        Write-Log ''
        Write-Log ('Abnahme D4 beendet ' + (Get-Date -Format 'dd.MM.yyyy HH:mm:ss'))
        if ($script:Fails.Count -gt 0) {
            Write-Log 'Abweichungen:'
            foreach ($f in $script:Fails) { Write-Log ('  FAIL ' + $f) }
        }
        Write-Log ('D4: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL')
        try { Copy-Item -LiteralPath $script:Log -Destination $script:LogCopy -Force } catch { }
    }
}
if ($script:NFail -gt 0) { exit 1 }
exit $exitCode
