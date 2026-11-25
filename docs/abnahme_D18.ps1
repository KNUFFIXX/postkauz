<#
.SYNOPSIS
    Abnahme D18 (PostKauz, Auftrag A12 "Alltag 6"): Umstellung des
    Dauerbetriebs von 0.3.0-rc9 auf 0.3.0-rc10 mit zwei vollstaendigen
    Laeufen und Rueckweg (Teil A) und Proben zur Uebernahme unter neuer
    Message-ID, zu grossen Entwuerfen, zum Wiederholen gescheiterter
    Textauszuege und zu doctor (Teil B).

.DESCRIPTION
    Laeuft auf dem Buero-PC (Windows 11, Windows PowerShell 5.1) am
    Live-Datenverzeichnis des Dauerbetriebs, ohne Go und ohne Docker.
    Anleitung: docs/abnahme_D18.md. Claude fuehrt das Skript aus.
    Muster: abnahme_D17.ps1 (Tausch, zwei Laeufe, Rueckweg, Teil B ueber den
    MCP-Server), dazu W1 bis W3 aus der Gegenpruefung des Skripts D17
    (abnahme_D18.md, Abschnitt 9). D17 bleibt als Protokoll unveraendert.

    PATH: Alle Aufrufe laufen mit dem PATH der geplanten Aufgaben (Machine
    und User aus der Registry), nicht mit dem der aufrufenden Huelle (D17:
    dort stand das pdftotext von Git fuer Windows zuerst).

    [extract] pdftotext: Das Skript sichert die Konfiguration
    (config.toml.d18-vor-extract), traegt [extract] pdftotext = '<-PdfToText>'
    UNMITTELBAR NACH DEM TAUSCH und vor dem ersten Aufruf von rc10 ein und
    nimmt den Eintrag VOR JEDEM Aufruf von rc9 wieder heraus (Sicherung
    zurueck): E.0/E.0c, Pfad nach 4.1, Rueckweg. 0.3.0-rc9 startet mit
    [extract] nicht. R.0h liest eine temporaere Kopie ohne [extract].

    Teil A "Umstellung" (Standard):
      Teil 0  Voraussetzungen (aendert nichts): wie D17, dazu: -PdfToText
              absolut, ohne ', vorhanden, "-v" meldet Poppler (sonst Abbruch
              vor dem Tausch); welches pdftotext im PATH der Aufgaben zuerst
              steht; Anhaenge failed je Konto; Entwuerfe mit remote
              taken_over und missing.
      Teil 1  Mit rc9: Aufgaben anhalten und abschalten (wie D17), doctor
              vorher, offene Entwuerfe, Marke im Audit, Sicherung nur mit
              -BackupTo.
      Teil 2  Programm tauschen (altes als postkauz-rc9.exe), sofort danach
              [extract] eintragen; doctor nachher: Schema 22, keine neue
              Warnung, pdftotext.probe ok mit dem Pfad aus -PdfToText.
      Teil 3  ZWEI vollstaendige Laeufe mit rc10. Nach dem ersten: Zahlen
              zu failed (extract_failed aus doctor), taken_over, missing und
              moved (Audit); A12 kann bestehende missing auf taken_over
              setzen und moved vergeben (nur genannt, kein Fehler).
      Teil 4  Aufgaben wieder an, nur ohne FAIL. Sonst kommt rc9 zurueck
              (E.0, vorher [extract] heraus); W1: bleiben Aufgaben aus, sagt
              eine Zeile welche und warum.

    Teil B (-TeilB -SelfAccount <konto>, nach Teil A, nichts wird gesendet):
      B.1  (Agentenschluessel) Entwurf, Ablage, der MENSCH aendert die Kopie
           im Webmail (ein Satz): nach einem vollstaendigen Lauf remote
           taken_over mit edited_message_id; drafts.update ->
           draft_taken_over, drafts.discard -> copy_changed, nichts
           verschoben; der Mensch verwirft seine Fassung im Webmail -> nach
           einem weiteren Lauf missing; B.1f drafts.discard -> geschlossen
           ohne Aktion (copies 0).
      B.2  (Agentenschluessel; nur wenn [imap] max_message_bytes reicht)
           Entwurf mit einem vom Skript erzeugten Anhang von etwa 11 MB
           (Nachricht ueber 13 MB), Ablage abwarten, 15 Minuten nichts tun,
           zwei vollstaendige Laeufe: nie taken_over (pushed oder moved im
           Audit); drafts.update -> ok, danach nur die neue Kopie im
           Entwuerfe-Ordner; zuletzt drafts.discard -> Kopie im Papierkorb.
      B.3  (Mensch) postkauz attach retry --account <-RetryAccount>
           --message <-RetryMessage> fuer die Anhaenge aus D17 (B6): nach dem
           naechsten Lauf ok, attach text liefert Text.
      B.4  (Mensch) doctor --json: pdftotext.probe ok, Felder je Konto,
           notes; Exit-Code 0 trotz notes.

    Modus "Rueckweg" (-Rueckweg): wie D17 (keine Aktion draft_update/
      draft_discard offen), dazu [extract] heraus, bevor rc9 startet; W3:
      FAIL in R.1 (nichts getauscht) -> Aufgaben wieder an, wenn rc10 noch
      an seinem Platz liegt.

    Jede Pruefung ist eine Zeile "OK <Nr> ...", "FAIL <Nr> ...", "WARN
    <Nr> ...", "INFO <Nr> ..." oder "SKIP <Nr> ...". Betreff, Adressen,
    Mailtext, Ordnernamen, Dateinamen, Schluessel, Konto- und Aufgabennamen
    stehen nie im Protokoll (Konten als "Konto <n>", Aufgaben als "Aufgabe
    <n>"; W2: in Teil B werden alle Konten aus accounts list vor der ersten
    Protokollzeile maskiert). Letzte Zeile: "D18: <n> OK, <m> FAIL, <w> WARN,
    <s> SKIP". Exit 0 nur ohne FAIL.

.PARAMETER Exe
    Pflicht: das Programm des Dauerbetriebs (postkauz.exe). Kein Standard.

.PARAMETER NewExe
    Teil A: das neue Programm (postkauz-windows-amd64.exe, Build
    0.3.0-rc10); daneben SHA256SUMS. Rueckweg, optional: rc10 fuer die
    Pruefung der Aktionen, wenn postkauz.exe fehlt.

.PARAMETER DataDir
    Pflicht: Live-Datenverzeichnis. Kein Standard.

.PARAMETER Config
    Pflicht: Live-Konfiguration (config.toml). Kein Standard.

.PARAMETER PdfToText
    Teil A, Pflicht: absoluter Pfad zu pdftotext.exe von Poppler (ohne ').
    "-v" muss Poppler melden, sonst Abbruch vor dem Tausch.

.PARAMETER BackupTo
    Optional, empfohlen: Ordner einer Sicherung vor dem Tausch.

.PARAMETER Accounts
    Teil A und Rueckweg: alle Konten (mit Komma).

.PARAMETER Tasks
    Teil A und Rueckweg, Pflicht: alle geplanten Aufgaben, die PostKauz
    dauerhaft betreiben (sync --watch, mcp --http, audit export), mit Komma.
    NICHT die Sicherungsaufgabe.

.PARAMETER ExpectSha256
    SHA-256 der neuen Programmdatei (CHANGELOG, Abschnitt 0.3.0-rc10).

.PARAMETER ExpectVersion
    Version des neuen Programms (Standard 0.3.0-rc10).

.PARAMETER OldVersion
    Version des alten Programms (Standard 0.3.0-rc9). Lesetest: 0.0.0-probe.

.PARAMETER Rueckweg
    Statt der Umstellung den Rueckweg auf das alte Programm.

.PARAMETER KeepStdioMcp
    Der stdio-MCP der Claude-App darf weiterlaufen (Tausch per Umbenennen).

.PARAMETER TeilB
    Teil B statt Teil A (braucht -SelfAccount).

.PARAMETER SelfAccount
    Teil B: das eigene Konto (nie das geteilte ohne Anweisung).

.PARAMETER SelfAddress
    Teil B, optional: eigene Adresse (sonst aus accounts list). Nie im
    Protokoll.

.PARAMETER AgentKeyFile
    Teil B, Pflicht fuer B.1 und B.2: Datei mit dem API-Schluessel eines
    Agenten (pk_key_...). Dann laufen Anlegen, Aendern, discard und das
    Lesen des Entwurfs ueber den MCP-Server. Nie im Protokoll.

.PARAMETER RetryAccount
    Teil B, B.3: Konto der Nachricht mit den gescheiterten Anhaengen.

.PARAMETER RetryMessage
    Teil B, B.3: lokale ID der Nachricht mit den gescheiterten Anhaengen.

.PARAMETER Steps
    Teil B, optional: nur diese Schritte (Standard B.1,B.2,B.3,B.4).

.PARAMETER BigMB
    Teil B, B.2: Groesse des erzeugten Anhangs in MB (Standard 11).

.PARAMETER ExtractDays
    Teil A: Anhaenge der Nachrichten der letzten so vielen Tage zaehlen
    (Standard 30).

.PARAMETER ExtractMaxMessages
    Teil A: hoechstens so viele Nachrichten je Konto lesen (Standard 1000).

.PARAMETER ExtractMessages
    Teil A, optional: lokale Nachrichten-IDs (mit Komma), deren Anhaenge
    aus dem Betrieb als gescheitert bekannt sind (zusaetzlich zur Suche).

.PARAMETER ExtractAttachments
    Nicht mehr benutzt (D17 B.5); bleibt fuer gleiche Aufrufe.

.PARAMETER WaitPushMinutes
    Teil B: Frist fuer das Ablegen einer Kopie (Standard 12).

.PARAMETER WaitCleanupMinutes
    Teil B: Frist fuer die Papierkorb-Aktion (Standard 2).

.PARAMETER WaitRunMinutes
    Teil B: Frist fuer einen weiteren vollstaendigen Lauf (Standard 10,
    hoechstens 5 Minuten bis zum Lauf ueber alle Ordner).

.PARAMETER WaitEditMinutes
    Teil B: Frist fuer einen Handgriff samt Lauf (Standard 20).

.PARAMETER QuietMinutes
    Teil B, B.2: so lange nichts tun nach der Ablage (Standard 15).

.PARAMETER LogFile
    Protokolldatei. Standard: D18_windows.txt (Teil B: D18_teilB.txt) neben
    dem Programm. Fuer den Lesetest eine eigene Datei angeben.

.EXAMPLE
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d18\abnahme_D18.ps1 -Exe C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data -Config C:\PostKauz\config.toml -NewExe C:\PostKauz\d18\postkauz-windows-amd64.exe -ExpectSha256 <sha256> -PdfToText C:\Tools\Poppler\bin\pdftotext.exe -Accounts a,b,c -Tasks "Aufgabe 1,Aufgabe 2,Aufgabe 3,Aufgabe 4,Aufgabe 5" -KeepStdioMcp -BackupTo E:\Sicherung\d18

.EXAMPLE
    Lesetest vor dem Tausch (aendert nichts; erwartet nur 0.4 und E.2 als FAIL):
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d18\abnahme_D18.ps1 -Exe C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data -Config C:\PostKauz\config.toml -NewExe C:\PostKauz\d18\postkauz-windows-amd64.exe -ExpectSha256 <sha256> -PdfToText C:\Tools\Poppler\bin\pdftotext.exe -Accounts a,b,c -Tasks "Aufgabe 1,Aufgabe 2,Aufgabe 3,Aufgabe 4,Aufgabe 5" -KeepStdioMcp -OldVersion 0.0.0-probe -LogFile C:\PostKauz\d18\D18_lesetest.txt

.EXAMPLE
    Rueckweg:
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d18\abnahme_D18.ps1 -Rueckweg -Exe C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data -Config C:\PostKauz\config.toml -NewExe C:\PostKauz\d18\postkauz-windows-amd64.exe -Accounts a,b,c -Tasks "Aufgabe 1,Aufgabe 2,Aufgabe 3,Aufgabe 4,Aufgabe 5" -KeepStdioMcp

.EXAMPLE
    Teil B:
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d18\abnahme_D18.ps1 -TeilB -Exe C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data -Config C:\PostKauz\config.toml -SelfAccount a -AgentKeyFile C:\PostKauz\d18\agent.key -RetryAccount b -RetryMessage 4711
#>
param(
    [string] $Exe = '',
    [string] $NewExe = '',
    [string] $DataDir = '',
    [string] $Config = '',
    [string] $PdfToText = '',
    [string] $BackupTo = '',
    [string[]] $Accounts = @(),
    [string[]] $Tasks = @(),
    [string] $ExpectSha256 = '',
    [string] $ExpectVersion = '0.3.0-rc10',
    [string] $OldVersion = '0.3.0-rc9',
    [switch] $Rueckweg,
    [switch] $KeepStdioMcp,
    [switch] $TeilB,
    [string] $SelfAccount = '',
    [string] $SelfAddress = '',
    [string] $AgentKeyFile = '',
    [string] $RetryAccount = '',
    [string] $RetryMessage = '',
    [string[]] $Steps = @(),
    [int] $BigMB = 11,
    [int] $ExtractDays = 30,
    [int] $ExtractMaxMessages = 1000,
    [string[]] $ExtractMessages = @(),
    [string[]] $ExtractAttachments = @(),
    [int] $WaitPushMinutes = 12,
    [int] $WaitCleanupMinutes = 2,
    [int] $WaitRunMinutes = 10,
    [int] $WaitEditMinutes = 20,
    [int] $QuietMinutes = 15,
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
$Steps = Split-List $Steps
$ExtractMessages = Split-List $ExtractMessages
$ExtractAttachments = Split-List $ExtractAttachments
if ($Steps.Count -eq 0) { $Steps = @('B.1', 'B.2', 'B.3', 'B.4') }
$SelfAccount = $SelfAccount.Trim()
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
$script:Mcp = $null
$script:McpIn = $null
$script:McpDead = ''
$script:AuditMark = [int64] -1
$script:AuditSince = ''
$script:Complete = $false
$script:CompleteB = $false
$script:KeepTasksOff = $false
$script:Changed = $false
# Teil 3 hat mit dem neuen Programm einen sync gestartet (dann kann es
# Aktionen der Herkunft draft_update geben, E.0d).
$script:RanNew = $false
# Rueckweg vor dem Tausch abgebrochen (Aktion offen): Aufgaben mit dem
# neuen Programm wieder an (betrieb.md 1.7c).
$script:RestartNew = $false
$script:SwapAt = $null
$script:TransientList = @()
$script:B1Draft = ''
$script:B2Succ = ''
$script:B4Draft = ''
$script:ProbeDrafts = New-Object System.Collections.Generic.List[string]
# A12: Sicherung der Konfiguration vor dem Eintrag [extract].
$script:ConfigBackup = ''
$script:ExtractSet = $false
$script:RemoteBefore = $null
$script:FailedSwap = $null
# K5: Konto- und Aufgabennamen werden in JEDER Protokollzeile durch Marken
# ersetzt (Steuerzeichen ohne Buchstaben und Ziffern, damit keine Marke
# selbst wie ein Name aussieht) und erst beim Schreiben als "Konto <n>"
# bzw. "Aufgabe <n>" ausgegeben.
$script:NameLabels = @{}
$script:NamePattern = ''
$script:NameSubPattern = ''
$script:NAccLabel = 0
$script:C1 = [string] [char] 1
$script:C2 = [string] [char] 2
$script:CA = [string] [char] 3
$script:CT = [string] [char] 4
# K1: Wartezeit, nach der ein Hilfsskript mit Neustart-Schleife (60 s) ein
# Programm neu gestartet haette.
$script:RestartGuardSec = 75
try { [Console]::OutputEncoding = $script:Utf8 } catch { }

# ---------------------------------------------------------------- Hilfen

function Hide-Secrets {
    param([string] $Text)
    if ($null -eq $Text) { return '' }
    $t = [regex]::Replace($Text, 'pk_key_[A-Za-z0-9_-]+', 'pk_key_********')
    $t = [regex]::Replace($t, '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}', '<adresse>')
    return $t
}

function Get-TaskLabel {
    param([string] $Name)
    $i = [array]::IndexOf([string[]] $Tasks, $Name)
    return ($script:C1 + ($script:CT * ($i + 1)) + $script:C2)
}

# K5: zwei Muster aus allen bekannten Namen, laengste zuerst: (1) als
# ganzes Wort, davor und danach kein Buchstabe und keine Ziffer (anders als
# D16 gelten "-", "_", "." und "@" als Grenze, also auch "postkauz-<konto>");
# (2) ohne Grenze, nur in Pfaden und anderen Folgen mit "\" oder "/".
function Update-NamePattern {
    $keys = @($script:NameLabels.Keys | Sort-Object { $_.Length } -Descending | ForEach-Object { [regex]::Escape([string] $_) })
    if ($keys.Count -eq 0) { $script:NamePattern = ''; $script:NameSubPattern = ''; return }
    $alt = '(?:' + ($keys -join '|') + ')'
    $script:NamePattern = '(?<![A-Za-z0-9])' + $alt + '(?![A-Za-z0-9])'
    $script:NameSubPattern = $alt
}

function Register-TaskNames {
    foreach ($tn in $Tasks) {
        if (-not $tn) { continue }
        $k = $tn.ToLowerInvariant()
        if (-not $script:NameLabels.ContainsKey($k)) { $script:NameLabels[$k] = Get-TaskLabel $tn }
    }
    Update-NamePattern
}

function Register-AccountNames {
    param([string[]] $Names)
    foreach ($n in $Names) {
        if (-not $n) { continue }
        $k = $n.Trim().ToLowerInvariant()
        if (-not $k -or $script:NameLabels.ContainsKey($k)) { continue }
        $script:NAccLabel++
        $script:NameLabels[$k] = $script:C1 + ($script:CA * $script:NAccLabel) + $script:C2
    }
    Update-NamePattern
}

# Ersetzt Namen durch Marken: zuerst in Pfad-Folgen ohne Grenze (K5),
# dann als ganzes Wort. Gelingt das nicht, kommt die Zeile nicht ins
# Protokoll (fail closed).
function Hide-Names {
    param([string] $Text)
    if ($null -eq $Text) { return '' }
    if (-not $script:NamePattern) { return $Text }
    try {
        $eval = [System.Text.RegularExpressions.MatchEvaluator] { param($m) [string] $script:NameLabels[$m.Value.ToLowerInvariant()] }
        $ic = [System.Text.RegularExpressions.RegexOptions]::IgnoreCase
        $pathEval = [System.Text.RegularExpressions.MatchEvaluator] {
            param($m)
            $e2 = [System.Text.RegularExpressions.MatchEvaluator] { param($x) [string] $script:NameLabels[$x.Value.ToLowerInvariant()] }
            [regex]::Replace($m.Value, $script:NameSubPattern, $e2, [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)
        }
        $t = [regex]::Replace($Text, '[^\s"''<>|]*[\\/][^\s"''<>|]*', $pathEval)
        return [regex]::Replace($t, $script:NamePattern, $eval, $ic)
    } catch {
        return '[Zeile nicht maskierbar, weggelassen]'
    }
}

function Hide-TaskNames {
    param([string] $Text)
    return (Hide-Names $Text)
}

# Marken als "Konto <n>" bzw. "Aufgabe <n>" ausgeben.
function Format-Labels {
    param([string] $Text)
    $ea = [System.Text.RegularExpressions.MatchEvaluator] { param($m) 'Konto ' + $m.Groups[1].Value.Length }
    $et = [System.Text.RegularExpressions.MatchEvaluator] { param($m) 'Aufgabe ' + $m.Groups[1].Value.Length }
    $t = [regex]::Replace($Text, [regex]::Escape($script:C1) + '((?:' + [regex]::Escape($script:CA) + ')+)' + [regex]::Escape($script:C2), $ea)
    $t = [regex]::Replace($t, [regex]::Escape($script:C1) + '((?:' + [regex]::Escape($script:CT) + ')+)' + [regex]::Escape($script:C2), $et)
    return $t
}

# Das Zeichen vor dem Text ($Tag: "OK 1.2 " usw.) wird nicht maskiert, damit
# ein Konto namens "ok" oder "info" die Zeile nicht unkenntlich macht.
function Write-Log {
    param([string] $Text, [string] $Tag = '')
    $t = $Tag + (Format-Labels (Hide-Names (Hide-Secrets $Text)))
    if ($script:Log) {
        try { [System.IO.File]::AppendAllText($script:Log, $t + "`r`n", $script:Utf8) } catch { $script:LogBroken = $true }
    }
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

function Write-Detail {
    param([string] $Detail)
    if ($Detail) {
        foreach ($l in ((Limit-Text $Detail) -split "`r?`n")) { Write-Log $l '      ' }
    }
}

function Add-Check {
    param([string] $Nr, [string] $Desc, $Ok, [string] $Detail = '')
    if ($Ok) { $script:NOk++; $tag = 'OK' } else { $script:NFail++; $tag = 'FAIL'; $script:Fails.Add($Nr + ' ' + (Hide-Names (Hide-Secrets $Desc))) }
    Write-Log $Desc ($tag + ' ' + $Nr + ' ')
    Write-Detail $Detail
}

function Add-Warn {
    param([string] $Nr, [string] $Desc, [string] $Detail = '')
    $script:NWarn++
    Write-Log $Desc ('WARN ' + $Nr + ' ')
    Write-Detail $Detail
}

function Add-Info {
    param([string] $Nr, [string] $Desc, [string] $Detail = '')
    Write-Log $Desc ('INFO ' + $Nr + ' ')
    Write-Detail $Detail
}

function Add-Skip {
    param([string] $Nr, [string] $Desc)
    $script:NSkip++
    Write-Log $Desc ('SKIP ' + $Nr + ' ')
}

# Nicht-leere Elemente eines JSON-Feldes als Liste (PS 5.1 macht aus einem
# fehlenden Feld $null und aus @($null) eine Liste mit einem Element).
function Get-Arr {
    param($V)
    return @(@($V) | Where-Object { $null -ne $_ })
}

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

# Programm $Prog ohne Fenster, mit --data-dir und --config, stdout und
# stderr als UTF-8, mit Zeitgrenze; ohne Umgebungsvariablen von PostKauz.
function Invoke-Pk {
    param([string] $Prog, [string[]] $A, [int] $TimeoutSec = 1800, [switch] $NoDirs, [string] $Cfg = '')
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $Prog
    $all = $A
    $cfgUse = $Config
    if ($Cfg) { $cfgUse = $Cfg }
    if (-not $NoDirs) { $all = @('--data-dir', $DataDir, '--config', $cfgUse) + $A }
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

# Grund einer fachlichen Ablehnung (Exit 3); nur der Code, nie die Meldung.
function Get-DenyReason {
    param($R)
    $m = [regex]::Match(($R.Out + "`n" + $R.Err), 'denied \(([a-z_]+)\)')
    if ($m.Success) { return $m.Groups[1].Value }
    $m = [regex]::Match(($R.Out + "`n" + $R.Err), '"code"\s*:\s*"([a-z_]+)"')
    if ($m.Success) { return $m.Groups[1].Value }
    return ''
}

function Stop-D18 {
    param([string] $Why)
    throw ('D18-ABBRUCH: ' + $Why)
}

# ---- Prozesse und Aufgaben (K1)

# Alle Prozesse des Programms (auch umbenannte: postkauz-rc9.exe). Ein
# Fehler von Get-CimInstance ist ein Fehler, nicht "0 Prozesse".
function Get-PkProcesses {
    try {
        return @(Get-CimInstance Win32_Process -Filter "Name LIKE 'postkauz%'" -OperationTimeoutSec 15 -ErrorAction Stop)
    } catch {
        throw ('D18-ABBRUCH: Prozessliste nicht lesbar (Get-CimInstance): ' + $_.Exception.Message)
    }
}

function Get-PsProcesses {
    try {
        return @(Get-CimInstance Win32_Process -Filter "Name = 'powershell.exe' OR Name = 'pwsh.exe'" -OperationTimeoutSec 15 -ErrorAction Stop |
            Where-Object { $_.ProcessId -ne $PID })
    } catch {
        throw ('D18-ABBRUCH: Prozessliste nicht lesbar (Get-CimInstance): ' + $_.Exception.Message)
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

# Steht der Pfad $K in der Befehlszeile $C als ganzer Pfad?
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

# Befehlszeile in Woerter zerlegen ("..." als ein Wort).
function Split-CommandLine {
    param([string] $C)
    $toks = @()
    foreach ($m in [regex]::Matches([string] $C, '"([^"]*)"|(\S+)')) {
        if ($m.Groups[1].Success) { $toks += $m.Groups[1].Value } else { $toks += $m.Groups[2].Value }
    }
    return , $toks
}

function Get-TaskActionLines {
    param([string] $Name)
    $t = Get-ScheduledTask -TaskName $Name -ErrorAction SilentlyContinue
    $lines = @()
    if ($null -eq $t) { return , $lines }
    foreach ($ac in @($t.Actions)) {
        $lines += New-Object PSObject -Property @{ Cmd = ([string] $ac.Execute + ' ' + [string] $ac.Arguments); Dir = [string] $ac.WorkingDirectory }
    }
    return , $lines
}

# K1: alle PowerShell-Skripte (.ps1), die in einer Aktion einer Aufgabe aus
# -Tasks stehen (in Anfuehrungszeichen, mit ' oder ohne), mit vollem Pfad
# (relativ zum Arbeitsordner der Aktion) und Dateiname, kleingeschrieben.
function Get-TaskScripts {
    $list = @()
    $seen = @{}
    foreach ($tn in $Tasks) {
        foreach ($l in (Get-TaskActionLines $tn)) {
            foreach ($m in [regex]::Matches($l.Cmd, '(?i)"([^"]+?\.ps1)"|''([^'']+?\.ps1)''|([^\s"'']+?\.ps1)(?=$|[\s"''])')) {
                $p = ''
                foreach ($g in 1..3) { if ($m.Groups[$g].Success) { $p = $m.Groups[$g].Value } }
                $p = $p.Trim()
                if (-not $p) { continue }
                $full = $p
                if (-not [System.IO.Path]::IsPathRooted($p) -and $l.Dir) { try { $full = [System.IO.Path]::GetFullPath((Join-Path $l.Dir $p)) } catch { } }
                $key = $full.ToLowerInvariant()
                if ($seen.ContainsKey($key)) { continue }
                $seen[$key] = $true
                $leaf = ''
                try { $leaf = [System.IO.Path]::GetFileName($full).ToLowerInvariant() } catch { }
                $list += New-Object PSObject -Property @{ Full = $key; Leaf = $leaf; Rooted = [System.IO.Path]::IsPathRooted($full) }
            }
        }
    }
    return , $list
}

# K1: Fuehrt dieser PowerShell-Prozess ein Skript aus den Aufgaben aus?
# Ja, wenn seine Befehlszeile den vollen Pfad nennt oder den Dateinamen
# ohne Ordner bzw. mit .\ davor (Aufruf im Arbeitsordner der Aufgabe).
function Test-TaskScriptProcess {
    param($P, $Scripts)
    $c = ([string] $P.CommandLine).ToLowerInvariant()
    foreach ($s in $Scripts) {
        if ($s.Rooted -and (Test-PathIn $c $s.Full)) { return $true }
        if ($s.Leaf -and $c -match ('(^|[\s"''=])(\.[\\/])?' + [regex]::Escape($s.Leaf) + '($|[\s"''])')) { return $true }
    }
    return $false
}

# Wie D16 (Nachbesserung 2): run-sync-watch.ps1 dieses Dauerbetriebs (die
# Befehlszeile nennt Datenordner, Konfiguration oder Programm).
function Test-OwnHelper {
    param($P)
    $c = ([string] $P.CommandLine).ToLowerInvariant()
    if ($c -notmatch 'run-?sync-?watch\.ps1') { return $false }
    foreach ($k in @($DataDir, $Config, $script:ExeFull, $script:ExeDir)) {
        if ($k -and (Test-PathIn $c ([string] $k).ToLowerInvariant())) { return $true }
    }
    return $false
}

function Get-HelperProcesses {
    param($Scripts)
    return @(Get-PsProcesses | Where-Object { (Test-TaskScriptProcess $_ $Scripts) -or (Test-OwnHelper $_) })
}

# K1: Aufgaben anhalten und abschalten; JEDES PowerShell beenden, dessen
# Skript in einer Aktion einer Aufgabe aus -Tasks steht (nicht nur
# run-sync-watch.ps1, auch die Huelle der Aufgabe fuer mcp --http);
# danach postkauz.exe (sync, --http, audit) aus dem Ordner von -Exe; dann
# pruefen, dass kein postkauz-Prozess mehr laeuft (ausser dem stdio-MCP
# mit -KeepStdioMcp) und nach 75 s keiner neu gestartet ist.
function Stop-Tasks {
    param([string] $Nr)
    $scripts = Get-TaskScripts
    foreach ($tn in $Tasks) {
        $t = Get-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue
        if ($null -eq $t) { Add-Info $Nr ((Get-TaskLabel $tn) + ' (Nummer in -Tasks) nicht vorhanden'); continue }
        if (-not $script:WasEnabled.ContainsKey($tn)) { $script:WasEnabled[$tn] = ($t.State -ne 'Disabled') }
        try { Stop-ScheduledTask -TaskName $tn -ErrorAction SilentlyContinue } catch { }
        try {
            Disable-ScheduledTask -TaskName $tn -ErrorAction Stop | Out-Null
        } catch {
            Add-Check $Nr ((Get-TaskLabel $tn) + ' (Nummer in -Tasks) abschalten (wahrscheinlich fehlende Rechte: Abschnitt 3 der Anleitung)') $false (Hide-TaskNames $_.Exception.Message)
        }
    }
    $helpers = @(Get-HelperProcesses $scripts)
    $otherPs = @(Get-PsProcesses | Where-Object { ([string] $_.CommandLine) -match '(?i)run-?sync-?watch\.ps1' -and -not ((Test-TaskScriptProcess $_ $scripts) -or (Test-OwnHelper $_)) })
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
    $detail = 'Skripte in den Aktionen der Aufgaben: ' + $scripts.Count
    if ($foreign.Count + $otherPs.Count -gt 0) {
        $detail += "`nnicht beendet (nicht dieser Dauerbetrieb):`n" + ((@($foreign) + @($otherPs | ForEach-Object { 'Hilfsskript PID ' + $_.ProcessId })) -join "`n")
    }
    Add-Info $Nr ('PowerShell mit einem Skript der Aufgaben beendet: ' + $helpers.Count + '; postkauz (sync, --http, audit) aus dem Ordner von -Exe nach 20 s beendet: ' + $killed) $detail
    $deadline = (Get-Date).AddSeconds(30)
    while ((Get-BlockingPk).Count -gt 0 -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 2 }
    $block = @(Get-BlockingPk)
    $others = @($block | ForEach-Object { 'PID ' + $_.ProcessId + ': ' + [string] $_.ExecutablePath })
    $hint = 'Wahrscheinlicher Bedienfehler: eine Aufgabe fehlt in -Tasks'
    if (-not $KeepStdioMcp) { $hint += ', oder die Claude-App laeuft noch (ohne -KeepStdioMcp vorher ganz beenden)' }
    if ($KeepStdioMcp) {
        $stdio = @(Get-PkProcesses | Where-Object { Test-StdioMcp $_ }).Count
        Add-Check $Nr 'Aufgaben angehalten und abgeschaltet, ausser dem stdio-MCP der Claude-App laeuft kein postkauz' ($block.Count -eq 0) ('uebrige postkauz: ' + $block.Count + ', stdio-MCP (darf laufen): ' + $stdio + "`n" + ($others -join "`n") + $(if ($block.Count -gt 0) { "`n" + $hint } else { '' }))
    } else {
        Add-Check $Nr 'Aufgaben angehalten und abgeschaltet, kein postkauz laeuft' ($block.Count -eq 0) ('laufende postkauz: ' + $block.Count + "`n" + ($others -join "`n") + $(if ($block.Count -gt 0) { "`n" + $hint } else { '' }))
    }
    if ($block.Count -gt 0) { return }
    # K1: Neustart-Schleifen (60 s) abwarten; danach darf weder ein
    # Hilfsskript noch ein postkauz (ausser stdio-MCP) laufen.
    Start-Sleep -Seconds $script:RestartGuardSec
    $h2 = @(Get-HelperProcesses $scripts)
    $b2 = @(Get-BlockingPk)
    $d2 = 'Hilfsskripte: ' + $h2.Count + ', postkauz: ' + $b2.Count + "`n" + ((@($h2 | ForEach-Object { 'Hilfsskript PID ' + $_.ProcessId }) + @($b2 | ForEach-Object { 'PID ' + $_.ProcessId + ': ' + [string] $_.ExecutablePath })) -join "`n")
    if ($h2.Count + $b2.Count -gt 0) { $d2 += "`n" + $hint + ' (ein Hilfsskript ausserhalb der Aufgaben startet das Programm neu)' }
    Add-Check ($Nr + 'b') ('nach ' + $script:RestartGuardSec + ' s: kein Hilfsskript der Aufgaben und kein postkauz neu gestartet (K1)') ($h2.Count + $b2.Count -eq 0) $d2
}

# Konten eines sync-Aufrufs aus -a/--account; "*" fuer --all.
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

# Laufende sync --watch genau mit dem Programm -Exe.
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

# Aufgaben aus -Tasks wieder einschalten und (ausser audit export) starten
# (wie D16): nach 20 s noch Running, je Konto ein sync --watch genau mit
# -Exe, das $Version meldet.
function Start-Tasks {
    param([string] $Nr, [string] $Version)
    $hint = 'Claude-App danach wieder starten'
    if ($KeepStdioMcp) { $hint = 'Claude-App nach D18 neu starten, damit ihr MCP das aktuelle Programm nimmt' }
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
    $syncTasks = @($todo | Where-Object { -not (Test-AuditTask $_.Name) }).Count
    if ($syncTasks -gt 0) {
        $watch = @(Get-OwnWatch)
        $without = 0
        foreach ($acc in $Accounts) {
            $has = @($watch | Where-Object { @($_.Accounts) -contains '*' -or @($_.Accounts) -icontains $acc }).Count
            if ($has -eq 0) { $without++ }
        }
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        $ok = ($Accounts.Count -gt 0 -and $without -eq 0 -and $v.Exit -eq 0 -and $v.Out -match [regex]::Escape($Version))
        if (-not $ok) { $all = $false }
        Add-Check $Nr ('je Konto laeuft ein sync --watch genau mit ' + $script:ExeFull + ' (meldet ' + $Version + ')') $ok ('sync --watch: ' + $watch.Count + ', Konten: ' + $Accounts.Count + ', davon ohne sync --watch: ' + $without + ' (gestartete Aufgaben: ' + $syncTasks + ")`n" + $v.Out.Trim())
    }
    return $all
}

# ---- Programmdateien

function Get-FreeName {
    param([string] $Base)
    $n = Join-Path $script:ExeDir ($Base + '.exe')
    if (Test-Path $n) { $n = Join-Path $script:ExeDir ($Base + '-' + (Get-Date -Format 'yyyyMMddHHmmss') + '.exe') }
    return $n
}

function Get-NewCopyName { return (Get-FreeName 'postkauz-rc10') }

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

function Get-VcsRevision {
    param([string] $Path)
    try {
        $text = [System.Text.Encoding]::ASCII.GetString([System.IO.File]::ReadAllBytes($Path))
        $m = [regex]::Match($text, 'vcs\.revision=([0-9a-f]{40})')
        if ($m.Success) { return $m.Groups[1].Value }
    } catch { }
    return ''
}

function Get-ProgramVersion {
    param([string] $Path)
    if (-not $Path -or -not (Test-Path $Path)) { return '' }
    try {
        $v = Invoke-Pk $Path @('version') -NoDirs -TimeoutSec 60
        $m = [regex]::Match($v.Out, '0\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?')
        if ($m.Success) { return $m.Value }
    } catch { }
    return ''
}

function Restore-Program {
    param([string] $Keep, [string] $Nr)
    # A12: rc9 startet mit [extract] nicht; vor dem Zuruecklegen heraus.
    [void] (Clear-ExtractConfig ($Nr + 'x'))
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

# ---- Audit (wie D16)

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

function Format-Rfc3339 {
    param([DateTimeOffset] $T)
    return $T.ToUniversalTime().ToString("yyyy-MM-dd'T'HH:mm:ss'Z'", [System.Globalization.CultureInfo]::InvariantCulture)
}

function Set-AuditMark {
    param([string] $Prog, [string] $Nr, [DateTimeOffset] $At)
    $script:AuditMark = [int64] -1
    $script:AuditSince = Format-Rfc3339 $At
    $au = Get-AuditPaged $Prog '' '15m' 0 -Raw
    if ($au.Exit -eq 0) {
        foreach ($ev in $au.Events) { if ([int64] $ev.id -gt $script:AuditMark) { $script:AuditMark = [int64] $ev.id } }
    }
    if ($script:AuditMark -gt 0) {
        Add-Info $Nr ('Marke im Audit: ID ' + $script:AuditMark + '; es zaehlt nur, was danach im Audit steht')
    } else {
        $script:AuditMark = [int64] -1
        Add-Warn $Nr ('keine Marke im Audit lesbar (exit ' + $au.Exit + '); Grenze ist der Zeitpunkt ' + $script:AuditSince + ' (UTC)')
    }
}

function Get-AuditOp {
    param([string] $Prog, [string] $Op)
    if ($script:AuditMark -gt 0) { return Get-AuditPaged $Prog $Op '36500d' $script:AuditMark }
    $since = $script:AuditSince
    if (-not $since) { $since = Format-Rfc3339 ([DateTimeOffset] $script:Started.AddMinutes(-10)) }
    return Get-AuditPaged $Prog $Op $since
}

# Grund je Entwurf aus den Systemereignissen der Versanderkennung und aus
# drafts.supersede/drafts.discard (A11: Grund discarded).
function Get-CloseReasons {
    param([string] $Prog)
    $reasons = @{}
    $added = @{}
    $ok = $true
    foreach ($op in @('drafts.sent_detected', 'drafts.replaced_detected', 'drafts.copy_gone_detected', 'drafts.supersede', 'drafts.discard')) {
        $au = Get-AuditOp $Prog $op
        if ($au.Exit -ne 0) { $ok = $false }
        foreach ($ev in $au.Events) {
            if ($op -eq 'drafts.supersede' -and [string] $ev.reason -ne 'superseded_by_draft') { continue }
            if ($op -eq 'drafts.discard' -and [string] $ev.reason -ne 'discarded') { continue }
            foreach ($tg in (Get-Arr $ev.targets)) {
                if ([string] $tg -match '^draft:([0-9]+)$') {
                    $id = [int64] $Matches[1]
                    if ($op -eq 'drafts.supersede' -and $null -ne $ev.params -and [int64] $ev.params.draft_id -ne $id) { continue }
                    $reasons[$id] = [string] $ev.reason
                    if ($op -eq 'drafts.sent_detected' -and $null -ne $ev.params -and $null -ne $ev.params.recipients_added) {
                        $added[$id] = [int] $ev.params.recipients_added
                    }
                }
            }
        }
    }
    return New-Object PSObject -Property @{ Ok = $ok; Reasons = $reasons; Added = $added }
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

# ---- sync

# Ein sync -a <Konto> --json mit $Prog; Zeile mit Zaehlern (A11: Nachlauf
# cleanup_actions/cleanup_copies, ausgefuehrte Aktionen, Auszuege).
function Invoke-SyncOnce {
    param([string] $Prog, [string] $Acc)
    $s = Invoke-Pk $Prog @('sync', '-a', $Acc, '--json') -TimeoutSec 3600
    $j = Get-Json $s.Out
    $line = 'exit ' + $s.Exit + ', stderr ' + (Get-LineCount $s.Err) + ' Zeilen'
    $ca = 0; $cc = 0
    if ($null -ne $j) {
        $line += ', neu ' + $j.new + ', Fehler ' + $j.errors
        $dc = $j.draft_check
        if ($null -ne $dc) {
            $ca = [int] $dc.cleanup_actions
            $cc = [int] $dc.cleanup_copies
            $line += ', sent_by_message_id ' + [int] $dc.sent_by_message_id + ', sent_by_fallback ' + [int] $dc.sent_by_fallback +
                ', sent_by_thread ' + [int] $dc.sent_by_thread + ', sent_by_subject ' + [int] $dc.sent_by_subject +
                ', replaced_in_thread ' + [int] $dc.replaced_in_thread + ', copy_gone ' + [int] $dc.copy_gone +
                ', missing ' + [int] $dc.missing + ', taken_over ' + [int] $dc.taken_over +
                ', scheduled ' + [int] $dc.scheduled + ', sent_by_scheduled ' + [int] $dc.sent_by_scheduled +
                ', cleanup_actions ' + $ca + ', cleanup_copies ' + $cc
        }
        if ($null -ne $j.actions) {
            $line += ', Aktionen ausgefuehrt ' + [int] $j.actions.actions + ' (ok ' + [int] $j.actions.ok + ', failed ' + [int] $j.actions.failed + ', stale ' + [int] $j.actions.stale + ')'
        }
        if ($null -ne $j.attachments_extracted) { $line += ', Auszuege ' + [int] $j.attachments_extracted }
    }
    $clean = ($s.Exit -eq 0 -and $null -ne $j -and [int] $j.errors -eq 0)
    return New-Object PSObject -Property @{ Exit = $s.Exit; Json = $j; Line = $line; Clean = $clean; CleanupActions = $ca; CleanupCopies = $cc }
}

# sync je Konto (ein vollstaendiger Lauf); ein Fehler ist nur WARN, wenn
# ein sofortiger zweiter Versuch ohne Fehler endet. Liefert die Summe der
# im Nachlauf angelegten Aktionen und Kopien.
function Invoke-SyncRun {
    param([string] $Prog, [string] $Nr, [string] $Label)
    $sumA = 0; $sumC = 0
    foreach ($acc in $Accounts) {
        $r = Invoke-SyncOnce $Prog $acc
        if (-not $r.Clean) {
            $again = Invoke-SyncOnce $Prog $acc
            if ($again.Clean) {
                Add-Warn $Nr ((Get-AccLabel $acc) + ': sync (' + $Label + '): erster Versuch mit Fehler, sofortige Wiederholung ohne Fehler (voruebergehend)') ("erster Versuch: " + $r.Line + "`nWiederholung: " + $again.Line)
            } else {
                Add-Check $Nr ((Get-AccLabel $acc) + ': sync (' + $Label + '): Exit 0 und errors 0 (auch die Wiederholung scheiterte)') $false ("erster Versuch: " + $r.Line + "`nWiederholung: " + $again.Line)
            }
            $sumA += $r.CleanupActions + $again.CleanupActions; $sumC += $r.CleanupCopies + $again.CleanupCopies
            continue
        }
        Add-Check $Nr ((Get-AccLabel $acc) + ': sync (' + $Label + '): Exit 0 und errors 0') $true $r.Line
        $sumA += $r.CleanupActions; $sumC += $r.CleanupCopies
    }
    return New-Object PSObject -Property @{ Actions = $sumA; Copies = $sumC }
}

# ---- Entwuerfe

function Get-Drafts {
    param([string] $Prog, [string[]] $Status)
    $a = @('draft', 'list', '--json', '--limit', '500')
    foreach ($st in $Status) { $a += @('--status', $st) }
    $r = Invoke-Pk $Prog $a
    $j = Get-Json $r.Out
    $list = @()
    $parsed = ($null -ne $j -and $null -ne $j.drafts)
    if ($parsed) {
        foreach ($d in (Get-Arr $j.drafts)) {
            $list += New-Object PSObject -Property @{ Id = [int64] $d.draft_id; Status = [string] $d.status; Remote = [string] $d.remote; Kind = [string] $d.kind;
                ScheduledFor = $d.scheduled_for; Older = [int] $d.older_copies }
        }
    }
    return New-Object PSObject -Property @{ Exit = $r.Exit; List = $list; Parsed = $parsed; Text = $r.Text }
}

function Format-Drafts {
    param($List)
    if (@($List).Count -eq 0) { return '(keine)' }
    return ((@($List) | Sort-Object Id | ForEach-Object {
        $t = 'draft ' + $_.Id + ': ' + $_.Status + ', ' + $_.Kind + ', remote ' + $_.Remote
        if ($_.Older -gt 0) { $t += ', older_copies ' + $_.Older }
        if ($_.Remote -eq 'scheduled') { $t += ' (Termin ' + (Format-At (ConvertTo-Dto $_.ScheduledFor)) + ')' }
        $t
    }) -join "`n")
}

# draft show <id> --json als Objekt (oder $null); kommt nie ins Protokoll.
function Get-DraftDetail {
    param([string] $Prog, [string] $Id)
    $r = Invoke-Pk $Prog @('draft', 'show', $Id, '--json')
    if ($r.Exit -ne 0) { return $null }
    return Get-Json $r.Out
}

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
            $n = 0
            if ($cr.Added.ContainsKey([int64] $o.Id)) { $n = [int] $cr.Added[[int64] $o.Id] }
            $closed += New-Object PSObject -Property @{ Id = $o.Id; Status = [string] $dd.status; Reason = $r; Added = $n }
        } else {
            $vanished += $o.Id
        }
    }
    $beforeIds = @($script:Before.List | ForEach-Object { $_.Id })
    $foreign = @($cr.Reasons.Keys | Where-Object { $beforeIds -notcontains $_ } | ForEach-Object { [int64] $_ })
    $sent = @(); $withAdded = @(); $bySched = @(); $replaced = @(); $gone = @(); $superseded = @(); $discarded = @(); $odd = @(); $lines = @()
    foreach ($f in $foreign) { $lines += ('draft ' + $f + ': vorher nicht offen, Grund ' + $cr.Reasons[$f]) }
    foreach ($c in $closed) {
        $r = $c.Reason
        $shown = $r
        if (-not $shown) { $shown = '(kein Audit-Eintrag des Systems)' }
        $line = 'draft ' + $c.Id + ': ' + $c.Status + ', Grund ' + $shown
        if ($c.Added -gt 0) { $line += ', recipients_added ' + $c.Added }
        $lines += $line
        if ($c.Status -eq 'sent_by_human' -and @('message_id', 'fallback', 'thread', 'subject', 'scheduled') -contains $r) {
            $sent += $c.Id
            if ($c.Added -gt 0) { $withAdded += $c.Id }
            if ($r -eq 'scheduled') { $bySched += $c.Id }
        }
        elseif ($c.Status -eq 'abandoned' -and $r -eq 'replaced') { $replaced += $c.Id }
        elseif ($c.Status -eq 'abandoned' -and $r -eq 'copy_gone') { $gone += $c.Id }
        elseif ($c.Status -eq 'abandoned' -and $r -eq 'superseded_by_draft') { $superseded += $c.Id }
        elseif ($c.Status -eq 'abandoned' -and $r -eq 'discarded') { $discarded += $c.Id }
        else { $odd += $c.Id }
    }
    return New-Object PSObject -Property @{ After = $after; AuditOk = $cr.Ok; Closed = $closed; Vanished = $vanished; Sent = $sent;
        WithAdded = $withAdded; BySched = $bySched; Replaced = $replaced; Gone = $gone; Superseded = $superseded; Discarded = $discarded; Odd = $odd; Foreign = $foreign; Lines = $lines }
}

function Test-Closed {
    param([string] $Nr, [string] $Label)
    $g = Get-Closed $script:ExeFull
    $ok = ($g.After.Exit -eq 0 -and $g.After.Parsed -and $g.After.List.Count -lt 500 -and $g.AuditOk -and $g.Odd.Count -eq 0 -and
        $g.Vanished.Count -eq 0 -and $g.Foreign.Count -eq 0)
    Add-Check $Nr ('nach dem ' + $Label + ': alle seit der Marke geschlossenen Entwuerfe mit Grund aus dem Audit (' + $g.Closed.Count + ')') $ok ((@($g.Lines) -join "`n") + "`nohne passenden Grund: " + (Format-Ids $g.Odd) + "`nvorher offen, jetzt weder offen noch geschlossen lesbar: " + (Format-Ids $g.Vanished) + "`nim Audit, aber vorher nicht offen: " + (Format-Ids $g.Foreign))
    return $g
}

function Get-GuardLine {
    param([string] $Prog)
    $r = Invoke-Pk $Prog @('doctor')
    $m = [regex]::Match($r.Out, '(?m)^attach_guard: [0-9]+ dir rules, [0-9]+ file rules\s*$')
    if ($m.Success) { return $m.Value.Trim() }
    return ''
}

function Get-ConfigText {
    $cfgText = ''
    try { if ($Config -and (Test-Path -LiteralPath $Config)) { $cfgText = [System.IO.File]::ReadAllText($Config) } } catch { }
    return $cfgText
}

function Test-DenyKeys {
    return ((Get-ConfigText) -match '(?m)^\s*attach_deny_(dirs|files)\s*=')
}

# A11 Teil 1 (c): [draft] previous_copies. rc9 und aelter starten mit dem
# Schluessel nicht; in der Abnahme darf er nicht gesetzt sein (jede nicht
# auskommentierte Zeile previous_copies = ... zaehlt, gleich in welchem
# Abschnitt).
function Test-PreviousCopiesKey {
    return ((Get-ConfigText) -match '(?m)^\s*previous_copies\s*=')
}

function Set-CtrlCGuard {
    param([bool] $On)
    try { [Console]::TreatControlCAsInput = $On; return $true } catch { return $false }
}

function Get-AccLabel {
    param([string] $Name)
    $k = $Name.Trim().ToLowerInvariant()
    if ($script:NameLabels.ContainsKey($k)) { return [string] $script:NameLabels[$k] }
    return 'Konto ?'
}

function Get-MailboxList {
    param([string] $Prog, [string] $Acc)
    $r = Invoke-Pk $Prog @('mailbox', 'list', '-a', $Acc, '--json') -TimeoutSec 300
    $j = $null
    if ($r.Exit -eq 0) { $j = Get-Json $r.Out }
    $boxes = @()
    $ok = ($null -ne $j -and $null -ne $j.mailboxes)
    if ($ok) { $boxes = @(Get-Arr $j.mailboxes) }
    return New-Object PSObject -Property @{ Ok = $ok; Exit = $r.Exit; Boxes = $boxes }
}

# 0.8: Rollen je Konto. Ohne Entwuerfe-Ordner (drafts) oder Papierkorb
# (trash) kann A11 keine Kopie verschieben (no_trash_mailbox): Hinweis.
# Wie D16: Rolle all ohne Rolle sent ist ein Hinweis.
function Test-MailboxRoles {
    param([string] $Prog, [string] $Nr)
    foreach ($acc in $Accounts) {
        $label = Get-AccLabel $acc
        $m = Get-MailboxList $Prog $acc
        if (-not $m.Ok) {
            Add-Warn $Nr ($label + ': mailbox list --json nicht lesbar; Rollen nicht geprueft') ('exit ' + $m.Exit)
            continue
        }
        $roles = @($m.Boxes | ForEach-Object { [string] $_.role })
        $detail = 'Ordner: ' + $m.Boxes.Count + ', Rollen: ' + ((@($roles | Where-Object { $_ } | Sort-Object -Unique)) -join ', ')
        $hint = @()
        if ($roles -contains 'all' -and -not ($roles -contains 'sent')) { $hint += 'Rolle all ohne Rolle sent (Gesendet fuer IMAP ausgeblendet; wie D16 nur ein Hinweis)' }
        if ($roles -contains 'drafts' -and -not ($roles -contains 'trash')) { $hint += 'Rolle drafts ohne Rolle trash: fruehere Kopien bleiben liegen (no_trash_mailbox)' }
        if ($hint.Count -gt 0) {
            Add-Warn $Nr ($label + ': ' + ($hint -join '; ')) $detail
        } else {
            Add-Info $Nr ($label + ': Rollen drafts ' + $(if ($roles -contains 'drafts') { 'ja' } else { 'nein' }) + ', trash ' + $(if ($roles -contains 'trash') { 'ja' } else { 'nein' }) + ' (in Ordnung)') $detail
        }
    }
}

function Test-BackupTask {
    param([string] $Name)
    foreach ($l in (Get-TaskActionLines $Name)) {
        if ($l.Cmd -match '(?i)(^|[\s"''&;])backup($|[\s"'';])') { return 'action' }
        foreach ($tok in (Split-CommandLine $l.Cmd)) {
            $p = ([string] $tok).Trim("'")
            if ($p -notmatch '(?i)\.(ps1|cmd|bat)$') { continue }
            if (-not [System.IO.Path]::IsPathRooted($p) -and $l.Dir) { $p = Join-Path $l.Dir $p }
            try {
                if (-not (Test-Path -LiteralPath $p)) { continue }
                foreach ($line in [System.IO.File]::ReadAllLines($p)) {
                    $s = $line.Trim()
                    if ($s.StartsWith('#') -or $s.StartsWith('::') -or $s -match '^(?i)rem\s') { continue }
                    if ($s -match '(?i)postkauz' -and $s -match '(?i)(^|[\s"''&;])backup($|[\s"'';])') { return 'script' }
                }
            } catch { }
        }
    }
    return ''
}

function Test-NoBackupTasks {
    param([string] $Nr)
    $found = @()
    $named = @()
    foreach ($tn in $Tasks) {
        $kind = Test-BackupTask $tn
        if ($kind) { $found += ((Get-TaskLabel $tn) + ' (' + $(if ($kind -eq 'action') { 'Aktion' } else { 'Skript der Aktion' }) + ' ruft postkauz backup auf)') }
        elseif ($tn -match '(?i)backup|sicherung') { $named += (Get-TaskLabel $tn) }
    }
    Add-Check $Nr 'keine Sicherungsaufgabe in -Tasks (Aktion ruft postkauz backup auf; sie gehoert nicht in -Tasks)' ($found.Count -eq 0) ('Aufgaben: ' + $Tasks.Count + ', Sicherungsaufgaben: ' + $found.Count + "`n" + ($found -join "`n"))
    if ($named.Count -gt 0) {
        Add-Warn $Nr ('Name klingt nach Sicherung, Aktion ruft aber kein postkauz backup auf: ' + ($named -join ', ') + ' (Claude prueft die Aufgabe von Hand)')
    }
}

# ---- Zeiten

function ConvertTo-Dto {
    param($V)
    if ($null -eq $V) { return $null }
    if ($V -is [DateTimeOffset]) { return $V }
    if ($V -is [DateTime]) { return [DateTimeOffset] $V }
    $s = ([string] $V).Trim()
    if (-not $s) { return $null }
    $t = [DateTimeOffset]::MinValue
    if ([DateTimeOffset]::TryParse($s, [System.Globalization.CultureInfo]::InvariantCulture, [System.Globalization.DateTimeStyles]::AssumeUniversal, [ref] $t)) { return $t }
    return $null
}

function Format-At {
    param($T)
    if ($null -eq $T) { return '(keiner)' }
    return (Format-Rfc3339 $T)
}

# ---------------------------------------------------------------- neu in D18

# ---- Aktionen (A11: source, draft_id)

# action show <id> --json als Objekt (oder $null). Zielordner (mailbox)
# kommen nie ins Protokoll.
function Get-ActionView {
    param([string] $Prog, [string] $Id)
    $r = Invoke-Pk $Prog @('action', 'show', $Id, '--json')
    if ($r.Exit -ne 0) { return $null }
    return Get-Json $r.Out
}

function Format-Action {
    param($A)
    if ($null -eq $A) { return 'action show liefert nichts' }
    $res = @{}
    foreach ($t in (Get-Arr $A.targets)) {
        $k = [string] $t.result
        if (-not $k) { $k = '(offen)' }
        if ($res.ContainsKey($k)) { $res[$k]++ } else { $res[$k] = 1 }
    }
    $rs = (@($res.Keys | Sort-Object | ForEach-Object { $_ + ' ' + $res[$_] }) -join ', ')
    if (-not $rs) { $rs = '(keine Ziele gelesen)' }
    return ('Aktion ' + [string] $A.id + ': kind ' + [string] $A.kind + ', state ' + [string] $A.state + ', source ' + [string] $A.source + ', draft_id ' + [string] $A.draft_id + ', Ziele ' + @(Get-Arr $A.targets).Count + ' (' + $rs + ')')
}

# Wartet bis $Minutes, bis die Aktion $Id abgeschlossen ist (done,
# partial, failed, rejected, expired). Liefert das letzte action show.
function Wait-ActionDone {
    param([string] $Id, [double] $Minutes)
    $deadline = (Get-Date).AddMinutes($Minutes)
    $a = $null
    while ($true) {
        $a = Get-ActionView $script:ExeFull $Id
        if ($null -ne $a -and @('done', 'partial', 'failed', 'rejected', 'expired') -contains [string] $a.state) { break }
        if ((Get-Date) -ge $deadline) { break }
        Start-Sleep -Seconds 5
    }
    return $a
}

# Offene Aktionen (requested, queued, running) mit $Prog; je Aktion action
# show fuer source (Rueckweg, betrieb.md 1.7c). Draft: die mit Herkunft
# draft_update oder draft_discard (rc9 kennt sie nicht).
function Get-OpenDraftActions {
    param([string] $Prog)
    $r = Invoke-Pk $Prog @('action', 'list', '--state', 'requested,queued,running', '--limit', '500', '--json')
    $j = Get-Json $r.Out
    $ok = ($r.Exit -eq 0 -and $null -ne $j -and $null -ne $j.actions)
    $all = @()
    $draft = @()
    if ($ok) {
        foreach ($a in (Get-Arr $j.actions)) {
            $src = [string] $a.source
            $v = Get-ActionView $Prog ([string] $a.id)
            if ($null -ne $v) { $src = [string] $v.source } else { $ok = $false }
            $o = New-Object PSObject -Property @{ Id = [int64] $a.id; State = [string] $a.state; Source = $src }
            $all += $o
            if (@('draft_update', 'draft_discard') -contains $src) { $draft += $o }
        }
    }
    return New-Object PSObject -Property @{ Ok = $ok; Exit = $r.Exit; All = $all; Draft = $draft }
}

function Format-ActionList {
    param($L)
    if (@($L).Count -eq 0) { return '(keine)' }
    return ((@($L) | Sort-Object Id | ForEach-Object { 'Aktion ' + $_.Id + ': ' + $_.State + ', source ' + $_.Source }) -join "`n")
}

# Rueckweg (R.2) und E.0d: keine offene Aktion der Herkunft draft_update
# oder draft_discard. Sonst je Konto ein sync mit $Prog (fuehrt sie aus)
# und erneut pruefen. Legt der Nachlauf dieses Laufs eine NEUE Aktion an
# (Wiederholung einer Kopie), folgt hoechstens noch ein Lauf (insgesamt
# drei, so viele Versuche hat eine Kopie); bleibt dieselbe Aktion offen,
# ist das ein FAIL. Liefert $true, wenn keine mehr offen ist.
function Clear-DraftActions {
    param([string] $Prog, [string] $Nr)
    $g = Get-OpenDraftActions $Prog
    if (-not $g.Ok) {
        Add-Check $Nr 'offene Aktionen mit dem neuen Programm lesbar (action list/show --json)' $false ('exit ' + $g.Exit)
        return $false
    }
    if ($g.Draft.Count -eq 0) {
        Add-Check $Nr 'keine Aktion der Herkunft draft_update oder draft_discard wartet oder laeuft (geprueft mit dem neuen Programm)' $true ('offene Aktionen insgesamt: ' + $g.All.Count)
        return $true
    }
    Add-Info $Nr ('offen mit Herkunft draft_update/draft_discard: ' + $g.Draft.Count + '; je Konto ein sync mit dem neuen Programm fuehrt sie aus') (Format-ActionList $g.Draft)
    $seen = @($g.Draft | ForEach-Object { $_.Id })
    for ($round = 1; $round -le 3; $round++) {
        foreach ($acc in $Accounts) {
            $s = Invoke-SyncOnce $Prog $acc
            Add-Info ($Nr + 'a') ((Get-AccLabel $acc) + ': sync mit dem neuen Programm (Durchgang ' + $round + ')') $s.Line
        }
        $g2 = Get-OpenDraftActions $Prog
        if (-not $g2.Ok) {
            Add-Check ($Nr + 'b') 'offene Aktionen nach dem sync lesbar' $false ('exit ' + $g2.Exit)
            return $false
        }
        if ($g2.Draft.Count -eq 0) {
            Add-Check ($Nr + 'b') ('nach dem sync: keine Aktion der Herkunft draft_update oder draft_discard mehr offen (Durchgang ' + $round + ')') $true
            return $true
        }
        $still = @($g2.Draft | Where-Object { $seen -contains $_.Id })
        if ($still.Count -gt 0) {
            Add-Check ($Nr + 'b') 'nach dem sync: eine Aktion der Herkunft draft_update/draft_discard bleibt offen' $false (Format-ActionList $still)
            return $false
        }
        Add-Info ($Nr + 'b') ('neue Aktion aus dem Nachlauf dieses Laufs (Wiederholung): noch ein Durchgang') (Format-ActionList $g2.Draft)
        $seen = @($g2.Draft | ForEach-Object { $_.Id })
    }
    Add-Check ($Nr + 'b') 'nach drei Durchgaengen: weiter Aktionen der Herkunft draft_update/draft_discard offen' $false
    return $false
}

# ---- fruehere Kopien offener Entwuerfe (A11 Teil 1 (d))

# Zaehlt mit $Prog: draft list --json (older_copies: Kopien mit derselben
# Message-ID, die laut Spiegel noch im Entwuerfe-Ordner liegen) und je
# offenem Entwurf draft show (remote_previous_uids: mit rc9 alle vermerkten
# frueheren Kopien, mit rc10 nur die noch im Entwuerfe-Ordner).
function Get-CopyCounts {
    param([string] $Prog)
    $l = Get-Drafts $Prog @('draft', 'pending_approval', 'ready')
    $sum = 0; $cand = 0; $ready = 0; $with = 0; $prev = 0; $shown = 0
    foreach ($d in $l.List) {
        $sum += $d.Older
        if ($d.Older -gt 0) {
            $with++
            if ($d.Status -eq 'ready') { $ready += $d.Older } else { $cand += $d.Older }
        }
        if ($shown -lt 200) {
            $shown++
            $dd = Get-DraftDetail $Prog ([string] $d.Id)
            if ($null -ne $dd) { $prev += @(Get-Arr $dd.remote_previous_uids).Count }
        }
    }
    $line = 'offene Entwuerfe: ' + $l.List.Count + '; fruehere Kopien im Entwuerfe-Ordner laut Spiegel (Summe older_copies): ' + $sum +
        ', davon bei Status draft/pending_approval: ' + $cand + ', bei ready (bleiben liegen): ' + $ready + '; Entwuerfe mit frueheren Kopien: ' + $with +
        '; remote_previous_uids zusammen: ' + $prev
    if ($l.List.Count -gt $shown) { $line += ' (nur die ersten ' + $shown + ' Entwuerfe gelesen)' }
    return New-Object PSObject -Property @{ Ok = ($l.Exit -eq 0 -and $l.Parsed); Sum = $sum; Cand = $cand; Ready = $ready; With = $with; Prev = $prev; Line = $line }
}

# Aktionen mit Herkunft $Sources, angelegt seit $Since; Zustaende und
# Ergebnisse der Ziele.
function Get-DraftOriginActions {
    param([string] $Prog, [string[]] $Sources, $Since)
    $r = Invoke-Pk $Prog @('action', 'list', '--limit', '500', '--json')
    $j = Get-Json $r.Out
    $ok = ($r.Exit -eq 0 -and $null -ne $j -and $null -ne $j.actions)
    $states = @{}; $results = @{}; $n = 0; $copies = 0
    if ($ok) {
        foreach ($a in (Get-Arr $j.actions)) {
            if ($Sources -notcontains [string] $a.source) { continue }
            $at = ConvertTo-Dto $a.created_at
            if ($null -ne $Since -and ($null -eq $at -or $at -lt $Since)) { continue }
            $n++
            $st = [string] $a.state
            if ($states.ContainsKey($st)) { $states[$st]++ } else { $states[$st] = 1 }
            $v = Get-ActionView $Prog ([string] $a.id)
            foreach ($t in (Get-Arr $v.targets)) {
                $copies++
                $k = [string] $t.result
                if (-not $k) { $k = '(offen)' }
                if ($results.ContainsKey($k)) { $results[$k]++ } else { $results[$k] = 1 }
            }
        }
    }
    $fs = (@($states.Keys | Sort-Object | ForEach-Object { $_ + ' ' + $states[$_] }) -join ', ')
    $fr = (@($results.Keys | Sort-Object | ForEach-Object { $_ + ' ' + $results[$_] }) -join ', ')
    return New-Object PSObject -Property @{ Ok = $ok; N = $n; Copies = $copies; States = $states; Results = $results;
        Line = ('Aktionen: ' + $n + ' (' + $(if ($fs) { $fs } else { '-' }) + '), Kopien: ' + $copies + ' (' + $(if ($fr) { $fr } else { '-' }) + ')') }
}

# ---- Textauszug (A11 Teil 3)

# Vorruebergehender Grund wie extract.IsTransientReason: beginnt mit
# "extractor failed: exit status 0xc" oder enthaelt "fork/exec", "exec:"
# oder "signal:".
function Test-TransientReason {
    param([string] $S)
    if (-not $S) { return $false }
    if ($S.ToLowerInvariant().StartsWith('extractor failed: exit status 0xc')) { return $true }
    foreach ($c in @('fork/exec', 'exec:', 'signal:')) { if ($S.Contains($c)) { return $true } }
    return $false
}

# attach list <Nachricht> --json: Anhaenge mit Status und Grund (ohne
# Dateiname); $null = nicht lesbar.
function Get-AttachStates {
    param([string] $Prog, [string] $Mid)
    $r = Invoke-Pk $Prog @('attach', 'list', $Mid, '--json') -TimeoutSec 120
    if ($r.Exit -ne 0) { return $null }
    $j = Get-Json $r.Out
    if ($null -eq $j -or $null -eq $j.attachments) { return $null }
    $list = @()
    foreach ($a in (Get-Arr $j.attachments)) {
        $list += New-Object PSObject -Property @{ Aid = [int64] $a.id; Mid = [int64] $Mid; Status = [string] $a.extract_status; Error = [string] $a.extract_error }
    }
    return , $list
}

# Nachrichten mit Anhang der letzten $Days Tage im Konto (search
# has:attachment after:<Datum> --fields id,cursor, seitenweise mit
# --cursor). Es gibt kein Kommando, das gescheiterte Auszuege je Konto
# zaehlt (Zaehler in doctor folgen mit A12).
function Get-AttachmentMessages {
    param([string] $Prog, [string] $Acc)
    $since = (Get-Date).AddDays(-$ExtractDays).ToString('yyyy-MM-dd', [System.Globalization.CultureInfo]::InvariantCulture)
    $q = 'has:attachment after:' + $since
    $ids = New-Object System.Collections.Generic.List[string]
    $cursor = ''
    $ok = $true
    $capped = $false
    $exit = 0
    for ($page = 0; $page -lt 100; $page++) {
        $a = @('search', $q, '-a', $Acc, '--limit', '500', '--json', '--fields', 'id,cursor')
        if ($cursor) { $a += @('--cursor', $cursor) }
        $r = Invoke-Pk $Prog $a -TimeoutSec 600
        if ($r.Exit -ne 0) { $ok = $false; $exit = $r.Exit; break }
        $n = 0
        $last = ''
        foreach ($line in ($r.Out -split "`r?`n")) {
            $t = $line.Trim()
            if (-not $t.StartsWith('{')) { continue }
            $o = Get-Json $t
            if ($null -eq $o -or $null -eq $o.id) { continue }
            $n++
            if (-not $ids.Contains([string] $o.id)) { $ids.Add([string] $o.id) }
            if ($o.cursor) { $last = [string] $o.cursor }
        }
        if ($ids.Count -ge $ExtractMaxMessages) { $capped = $true; break }
        if ($n -lt 500 -or -not $last) { break }
        $cursor = $last
    }
    return New-Object PSObject -Property @{ Ok = $ok; Exit = $exit; Ids = $ids; Capped = $capped }
}

# 0.10 und 3.1c: je Konto failed und davon voruebergehend, dazu die
# Nachrichten aus -ExtractMessages. Liefert die Liste der voruebergehend
# gescheiterten Anhaenge (Aid, Mid).
function Measure-Extract {
    param([string] $Prog, [string] $Nr, [string] $Label)
    $trans = @()
    $done = @{}
    foreach ($acc in $Accounts) {
        $lab = Get-AccLabel $acc
        $s = Get-AttachmentMessages $Prog $acc
        if (-not $s.Ok) {
            Add-Warn $Nr ($lab + ': Suche nach Nachrichten mit Anhang nicht lesbar (' + $Label + '); nicht gezaehlt') ('exit ' + $s.Exit)
            continue
        }
        $att = 0; $failed = 0; $tr = 0; $unread = 0
        foreach ($mid in $s.Ids) {
            $done[$mid] = $true
            $st = Get-AttachStates $Prog $mid
            if ($null -eq $st) { $unread++; continue }
            foreach ($x in $st) {
                $att++
                if ($x.Status -ne 'failed') { continue }
                $failed++
                if (Test-TransientReason $x.Error) { $tr++; $trans += $x }
            }
        }
        $line = $lab + ' (' + $Label + ', Nachrichten mit Anhang der letzten ' + $ExtractDays + ' Tage: ' + $s.Ids.Count + $(if ($s.Capped) { ', gekappt bei -ExtractMaxMessages' } else { '' }) +
            '): Anhaenge ' + $att + ', failed ' + $failed + ', davon mit voruebergehendem Grund ' + $tr
        if ($unread -gt 0) { $line += ', attach list nicht lesbar: ' + $unread }
        Add-Info $Nr $line
    }
    $extra = @($ExtractMessages | Where-Object { -not $done.ContainsKey([string] $_) })
    if ($extra.Count -gt 0) {
        $att = 0; $failed = 0; $tr = 0; $unread = 0
        foreach ($mid in $extra) {
            $st = Get-AttachStates $Prog $mid
            if ($null -eq $st) { $unread++; continue }
            foreach ($x in $st) {
                $att++
                if ($x.Status -ne 'failed') { continue }
                $failed++
                if (Test-TransientReason $x.Error) { $tr++; $trans += $x }
            }
        }
        Add-Info $Nr ('zusaetzlich aus -ExtractMessages (' + $Label + ', Nachrichten ' + $extra.Count + '): Anhaenge ' + $att + ', failed ' + $failed + ', davon mit voruebergehendem Grund ' + $tr + $(if ($unread -gt 0) { ', nicht lesbar: ' + $unread } else { '' }))
    }
    return , $trans
}

# Stand der vorher voruebergehend gescheiterten Anhaenge: ok, pending,
# failed mit neuem Grund (endgueltig oder nach mehreren Versuchen), failed
# mit altem voruebergehendem Grund (nicht wieder eingereiht), skipped,
# nicht lesbar.
function Get-TransientBreakdown {
    param([string] $Prog, $List)
    $byMid = @{}
    foreach ($x in @($List)) {
        $k = [string] $x.Mid
        if (-not $byMid.ContainsKey($k)) { $byMid[$k] = @() }
        $byMid[$k] += [int64] $x.Aid
    }
    $res = @{ ok = 0; pending = 0; failed_new = 0; failed_old = 0; skipped = 0; unread = 0 }
    $items = @()
    foreach ($mid in $byMid.Keys) {
        $st = Get-AttachStates $Prog $mid
        foreach ($aid in $byMid[$mid]) {
            $x = $null
            if ($null -ne $st) { $x = @($st | Where-Object { $_.Aid -eq $aid }) | Select-Object -First 1 }
            $cls = 'unread'
            if ($null -ne $x) {
                switch ($x.Status) {
                    'ok' { $cls = 'ok' }
                    'pending' { $cls = 'pending' }
                    'skipped' { $cls = 'skipped' }
                    'failed' { if ((Test-TransientReason $x.Error) -and $x.Error -notmatch '\([0-9]+ attempts\)$') { $cls = 'failed_old' } else { $cls = 'failed_new' } }
                    default { $cls = 'unread' }
                }
            }
            $res[$cls]++
            $items += New-Object PSObject -Property @{ Aid = $aid; Mid = [int64] $mid; Class = $cls }
        }
    }
    $line = 'ok ' + $res.ok + ', pending ' + $res.pending + ' (Zaehler der Versuche nur in meta, nicht in attach list), failed mit neuem Grund ' + $res.failed_new +
        ', failed mit altem voruebergehendem Grund ' + $res.failed_old + ', skipped ' + $res.skipped + ', nicht lesbar ' + $res.unread
    return New-Object PSObject -Property @{ Res = $res; Items = $items; Line = $line }
}

# Datei mit den voruebergehend gescheiterten Anhaengen fuer B.5 (nur
# Zahlen: "<anhang> <nachricht>" je Zeile), neben dem Programm.
function Get-ExtractFile { return (Join-Path $script:ExeDir 'D18_anhaenge.txt') }

# ---- vollstaendiger Lauf (K4)

# K4: Wartet bis $Minutes auf einen neueren ABGESCHLOSSENEN Lauf des ganzen
# Kontos. Ein Lauf ueber alle Ordner (sync --watch, Takt 5 min oder sync
# request ohne Ordner) spiegelt jeden Ordner (last_sync_at in mailbox list)
# und prueft danach die Entwuerfe samt Nachlauf. Die Ablage spiegelt seit
# rc10 nur den Entwuerfe-Ordner, IDLE nur den Posteingang; deshalb zaehlt
# erst, wenn JEDER bisher gespiegelte Ordner des Kontos nach dem Beginn
# des Wartens neu gespiegelt ist, und danach 60 s fuer die Pruefung der
# Entwuerfe nach dem letzten Ordner. sync request ohne Ordner am Anfang
# und alle 4 Minuten. Liefert Ok und Why.
function Wait-FullRun {
    param([string] $Acc, [int] $Minutes)
    $t0 = [DateTimeOffset]::Now
    $m0 = Get-MailboxList $script:ExeFull $Acc
    $ids = @()
    if ($m0.Ok) { $ids = @($m0.Boxes | Where-Object { $null -ne (ConvertTo-Dto $_.last_sync_at) } | ForEach-Object { [int64] $_.id }) }
    $deadline = (Get-Date).AddMinutes($Minutes)
    Request-Sync $Acc
    $lastRequest = Get-Date
    $missing = $ids.Count
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 15
        $m = Get-MailboxList $script:ExeFull $Acc
        if ($m.Ok -and $ids.Count -gt 0) {
            $tmax = $null
            $missing = 0
            foreach ($b in $m.Boxes) {
                if ($ids -notcontains [int64] $b.id) { continue }
                $t = ConvertTo-Dto $b.last_sync_at
                if ($null -eq $t -or $t -le $t0) { $missing++; continue }
                if ($null -eq $tmax -or $t -gt $tmax) { $tmax = $t }
            }
            if ($missing -eq 0 -and $null -ne $tmax) {
                $wait = ($tmax.AddSeconds(60) - [DateTimeOffset]::Now).TotalSeconds
                if ($wait -gt 0) { Start-Sleep -Seconds ([int] [math]::Ceiling($wait)) }
                return New-Object PSObject -Property @{ Ok = $true; Why = ('vollstaendiger Lauf: alle ' + $ids.Count + ' Ordner nach ' + (Format-At $t0) + ' gespiegelt (letzter ' + (Format-At $tmax) + '), danach 60 s fuer die Pruefung der Entwuerfe') }
            }
        }
        if (((Get-Date) - $lastRequest).TotalSeconds -ge 240) { Request-Sync $Acc; $lastRequest = Get-Date }
    }
    return New-Object PSObject -Property @{ Ok = $false; Why = ('kein vollstaendiger Lauf in ' + $Minutes + ' Minuten erkennbar: ' + $missing + ' von ' + $ids.Count + ' Ordnern seit ' + (Format-At $t0) + ' nicht neu gespiegelt') }
}

# sync request -a <Konto> (ohne Ordner: Lauf ueber alle Ordner). Fehler
# zaehlen nicht (der regulaere Lauf kommt ohnehin).
function Request-Sync {
    param([string] $Acc)
    try { [void] (Invoke-Pk $script:ExeFull @('sync', 'request', '-a', $Acc, '--json') -TimeoutSec 60) } catch { }
}

# ---------------------------------------------------------------- neu in D18

# ---- PATH der geplanten Aufgaben (A12 Teil 4)

# Alle Aufrufe laufen mit dem PATH, den die geplanten Aufgaben sehen
# (Machine und User aus der Registry), nicht mit dem der aufrufenden
# Huelle (D17: dort fand die Huelle zuerst das pdftotext von Git fuer
# Windows).
function Set-TaskPath {
    $m = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $u = [Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @()
    foreach ($x in @($m, $u)) { if ($x) { $parts += $x } }
    if ($parts.Count -gt 0) { $env:PATH = ($parts -join ';') }
}

# Erstes pdftotext im PATH der Aufgaben (ohne Nachfrage beim Benutzer).
function Get-PathPdfToText {
    try {
        $c = Get-Command pdftotext -CommandType Application -ErrorAction Stop | Select-Object -First 1
        if ($null -ne $c) { return [string] $c.Source }
    } catch { }
    return ''
}

# Erste Zeile von "<Programm> -v" (Poppler und Xpdf schreiben nach stderr).
function Get-ToolVersion {
    param([string] $Path)
    if (-not $Path -or -not (Test-Path -LiteralPath $Path)) { return '' }
    try {
        $psi = New-Object System.Diagnostics.ProcessStartInfo
        $psi.FileName = $Path
        $psi.Arguments = '-v'
        $psi.UseShellExecute = $false
        $psi.CreateNoWindow = $true
        $psi.RedirectStandardOutput = $true
        $psi.RedirectStandardError = $true
        $p = [System.Diagnostics.Process]::Start($psi)
        $o = $p.StandardOutput.ReadToEndAsync()
        $e = $p.StandardError.ReadToEndAsync()
        if (-not $p.WaitForExit(10000)) { try { $p.Kill() } catch { } }
        [void] $o.Wait(5000)
        [void] $e.Wait(5000)
        $all = ([string] $e.Result) + "`n" + ([string] $o.Result)
        foreach ($l in ($all -split "`n")) { if ($l.Trim()) { return $l.Trim() } }
    } catch { }
    return ''
}

# ---- [extract] pdftotext in der Konfiguration (A12 Teil 3 (a))

# Sicherung der Konfiguration vor dem Eintrag (einmal je Lauf).
function Backup-Config {
    if ($script:ConfigBackup -and (Test-Path -LiteralPath $script:ConfigBackup)) { return $true }
    $b = $Config + '.d18-vor-extract'
    if (Test-Path -LiteralPath $b) { $b = $Config + '.d18-vor-extract-' + (Get-Date -Format 'yyyyMMddHHmmss') }
    try {
        Copy-Item -LiteralPath $Config -Destination $b -ErrorAction Stop
        $script:ConfigBackup = $b
        return $true
    } catch {
        return $false
    }
}

# Text ohne die Tabelle [extract] (bis zur naechsten Tabelle).
function Remove-ExtractTable {
    param([string] $Text)
    $out = New-Object System.Collections.Generic.List[string]
    $skip = $false
    foreach ($l in ($Text -split "`r?`n")) {
        if ($l -match '^\s*\[') { $skip = ($l -match '^\s*\[extract\]\s*(#.*)?$') }
        if (-not $skip) { $out.Add($l) }
    }
    return ($out -join "`r`n")
}

function Test-ExtractInConfig {
    try { return ([System.IO.File]::ReadAllText($Config) -match '(?m)^\s*\[extract\]\s*(#.*)?$') } catch { return $false }
}

# [extract] pdftotext = '<Pfad>' eintragen: steht [extract] schon da, wird
# der Schluessel ersetzt bzw. ergaenzt (nie eine zweite Tabelle, das waere
# ein Ladefehler); sonst die Tabelle angehaengt. Literal string.
function Set-ExtractConfig {
    param([string] $Nr)
    $okB = Backup-Config
    $err = ''
    try {
        $text = [System.IO.File]::ReadAllText($Config)
        $line = "pdftotext = '" + $PdfToText + "'"
        $lines = New-Object System.Collections.Generic.List[string]
        foreach ($l in ($text -split "`r?`n")) { $lines.Add($l) }
        $start = -1
        for ($i = 0; $i -lt $lines.Count; $i++) { if ($lines[$i] -match '^\s*\[extract\]\s*(#.*)?$') { $start = $i; break } }
        if ($start -lt 0) {
            while ($lines.Count -gt 0 -and $lines[$lines.Count - 1].Trim() -eq '') { $lines.RemoveAt($lines.Count - 1) }
            $lines.Add('')
            $lines.Add('[extract]')
            $lines.Add($line)
            $lines.Add('')
        } else {
            $end = $lines.Count
            for ($i = $start + 1; $i -lt $lines.Count; $i++) { if ($lines[$i] -match '^\s*\[') { $end = $i; break } }
            $done = $false
            for ($i = $start + 1; $i -lt $end; $i++) {
                if ($lines[$i] -match '^\s*pdftotext\s*=') { $lines[$i] = $line; $done = $true }
            }
            if (-not $done) { $lines.Insert($start + 1, $line) }
        }
        [System.IO.File]::WriteAllText($Config, ($lines -join "`r`n"), $script:Utf8)
        $script:ExtractSet = $true
    } catch {
        $err = $_.Exception.Message
    }
    Add-Check $Nr ('[extract] pdftotext in die Konfiguration eingetragen (Sicherung ' + (Split-Path -Leaf ([string] $script:ConfigBackup)) + ')') ($okB -and -not $err -and (Test-ExtractInConfig)) ('Sicherung: ' + $okB + ', Fehler: ' + $err)
}

# Vor jedem Aufruf von rc9: [extract] heraus (Sicherung zurueck, sonst die
# Tabelle entfernen). Liefert, ob die Konfiguration danach ohne [extract]
# ist.
function Clear-ExtractConfig {
    param([string] $Nr)
    if (-not (Test-ExtractInConfig)) { return $true }
    $err = ''
    try {
        if ($script:ConfigBackup -and (Test-Path -LiteralPath $script:ConfigBackup) -and -not ([System.IO.File]::ReadAllText($script:ConfigBackup) -match '(?m)^\s*\[extract\]')) {
            Copy-Item -LiteralPath $script:ConfigBackup -Destination $Config -Force -ErrorAction Stop
        } else {
            if (-not $script:ConfigBackup) { [void] (Backup-Config) }
            [System.IO.File]::WriteAllText($Config, (Remove-ExtractTable ([System.IO.File]::ReadAllText($Config))), $script:Utf8)
        }
        $script:ExtractSet = $false
    } catch {
        $err = $_.Exception.Message
    }
    $ok = (-not $err -and -not (Test-ExtractInConfig))
    Add-Check $Nr '[extract] vor dem Start von rc9 aus der Konfiguration genommen (rc9 startet damit nicht)' $ok ('Fehler: ' + $err)
    return $ok
}

# Temporaere Kopie der Konfiguration ohne [extract] (R.0h, rc9 vor dem
# Tausch).
function New-ConfigWithoutExtract {
    $t = Join-Path ([System.IO.Path]::GetTempPath()) ('d18-config-' + [guid]::NewGuid().ToString('N') + '.toml')
    [System.IO.File]::WriteAllText($t, (Remove-ExtractTable ([System.IO.File]::ReadAllText($Config))), $script:Utf8)
    return $t
}

# doctor --json mit rc10: pdftotext (A12 Teil 3 (b), (d)).
function Get-DoctorJson {
    param([string] $Prog)
    $r = Invoke-Pk $Prog @('doctor', '--json')
    return New-Object PSObject -Property @{ Exit = $r.Exit; J = (Get-Json $r.Out); Text = $r.Text }
}

# Zahl der offenen Entwuerfe mit remote taken_over und missing.
function Get-RemoteCounts {
    param([string] $Prog)
    $d = Get-Drafts $Prog @('draft', 'pending_approval', 'ready')
    $to = @($d.List | Where-Object { $_.Remote -eq 'taken_over' })
    $mi = @($d.List | Where-Object { $_.Remote -eq 'missing' })
    return New-Object PSObject -Property @{ Ok = $d.Parsed; TakenOver = $to.Count; Missing = $mi.Count;
        Line = ('taken_over ' + $to.Count + ' (' + (Format-Ids @($to | ForEach-Object { $_.Id })) + '), missing ' + $mi.Count + ' (' + (Format-Ids @($mi | ForEach-Object { $_.Id })) + ')') }
}

# extract_failed je Konto aus doctor --json (rc10).
function Get-FailedFromDoctor {
    param($J)
    $parts = @()
    $sum = 0
    foreach ($a in (Get-Arr $J.accounts)) {
        $n = [int] $a.extract_failed
        $sum += $n
        $parts += ([string] $a.name + ': ' + $n + ' (davon nach mehreren Versuchen ' + [int] $a.extract_failed_exhausted + ', wartend ' + [int] $a.extract_waiting + ', skipped wegen pdftotext ' + [int] $a.extract_skipped_tool + ')')
    }
    return New-Object PSObject -Property @{ Sum = $sum; Line = ($parts -join '; ') }
}

# W1: klare Zeile, wenn Aufgaben aus bleiben.
function Write-TasksOff {
    param([string] $Why)
    $labels = @($Tasks | ForEach-Object { $_ })
    Write-Log ('AUFGABEN AUS: ' + ($labels -join ', ') + '. Grund: ' + $Why + '. Default (abnahme_D18.md Abschnitt 8): nicht von Hand einschalten; Claude meldet den Stand, der Betreiber entscheidet, ob rc10 bleibt (Aufgaben mit postkauz.exe einschalten) oder der Rueckweg laeuft (-Rueckweg).')
}

# ---------------------------------------------------------------- Teil A

function Invoke-Umstellung {
    Write-Log '--- Teil 0: Voraussetzungen (aendert nichts)'
    $newFull = ''
    if ($NewExe) { $newFull = [System.IO.Path]::GetFullPath($NewExe) }
    Add-Check '0.1' 'neues Programm angegeben und vorhanden' ($newFull -and (Test-Path $newFull)) $newFull
    if (-not ($newFull -and (Test-Path $newFull))) { Stop-D18 'neues Programm fehlt; nichts geaendert' }
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
    if ($null -ne $aj -and $null -ne $aj.accounts) { $known = @(Get-Arr $aj.accounts | ForEach-Object { [string] $_.name }) }
    Register-AccountNames $known
    $unknown = @($Accounts | Where-Object { $known -notcontains $_ })
    $notGiven = @($known | Where-Object { $Accounts -notcontains $_ })
    Add-Check '0.5' '-Accounts entspricht genau den konfigurierten Konten (accounts list mit -DataDir und -Config)' ($Accounts.Count -gt 0 -and $al.Exit -eq 0 -and $unknown.Count -eq 0 -and $notGiven.Count -eq 0) ('angegeben: ' + $Accounts.Count + ', konfiguriert: ' + $known.Count + ', nicht konfiguriert: ' + $unknown.Count + ', konfiguriert, aber nicht angegeben: ' + $notGiven.Count + ', exit ' + $al.Exit)
    $missingTasks = @($Tasks | Where-Object { $null -eq (Get-ScheduledTask -TaskName $_ -ErrorAction SilentlyContinue) })
    Add-Check '0.6' '-Tasks angegeben (Pflicht) und jede Aufgabe vorhanden' ($Tasks.Count -gt 0 -and $missingTasks.Count -eq 0) ('Aufgaben: ' + $Tasks.Count + ', nicht gefunden: ' + $missingTasks.Count)
    Test-NoBackupTasks '0.6b'
    $scripts = Get-TaskScripts
    Add-Info '0.6c' ('Skripte (.ps1) in den Aktionen der Aufgaben: ' + $scripts.Count + '; 1.1 beendet jedes PowerShell, das eines davon ausfuehrt (K1)')
    Add-Info '0.7' ('[draft] attach_deny_dirs/attach_deny_files in der Konfiguration: ' + $(if (Test-DenyKeys) { 'ja' } else { 'nein' }) + ' (rc9 und rc10 kennen sie)')
    $pc = Test-PreviousCopiesKey
    Add-Info '0.7b' ('[draft] previous_copies gesetzt: ' + $pc + ' (rc9 und rc10 kennen ihn; fuer Teil B nicht setzen)')
    Test-MailboxRoles $script:ExeFull '0.8'
    # A11 Teil 1 (d): Der erste Lauf mit rc10 legt fruehere Kopien offener
    # Entwuerfe in den Papierkorb; mit rc9 zaehlen.
    $c = Get-CopyCounts $script:ExeFull
    if ($c.Ok) { Add-Info '0.9' ('vorher (rc9): ' + $c.Line) } else { Add-Warn '0.9' 'draft list --json nicht lesbar; fruehere Kopien nicht gezaehlt' }
    # A11 Teil 3: Anhaenge mit failed je Konto (kein eigenes Kommando).
    $script:TransientList = Measure-Extract $script:ExeFull '0.10' 'vorher, rc9'
    if ($null -eq $script:TransientList) { $script:TransientList = @() }
    $ids = @($script:TransientList | Select-Object -First 50 | ForEach-Object { [string] $_.Aid })
    Add-Info '0.10b' ('voruebergehend gescheiterte Anhaenge (IDs, hoechstens 50): ' + $script:TransientList.Count + $(if ($ids.Count -gt 0) { ': ' + ($ids -join ', ') } else { '' }) + '; der erste Lauf mit rc10 reiht sie wieder ein')
    # A12 Teil 3 (a): das PDF-Programm (Pflicht), vor dem Tausch geprueft.
    $pdfOk = ($PdfToText -and [System.IO.Path]::IsPathRooted($PdfToText) -and -not $PdfToText.Contains("'") -and (Test-Path -LiteralPath $PdfToText -PathType Leaf))
    $pv = ''
    if ($pdfOk) { $pv = Get-ToolVersion $PdfToText }
    Add-Check '0.11' ('-PdfToText angegeben, absolut, ohne Hochkomma, vorhanden, "-v" meldet Poppler (rc9 kennt die Probe nicht)') ($pdfOk -and $pv -match '(?i)poppler|pdftotext version 2[0-9]\.') ('Pfad: ' + $PdfToText + "`n-v: " + $pv)
    $first = Get-PathPdfToText
    $fv = ''
    if ($first) { $fv = Get-ToolVersion $first }
    Add-Info '0.11b' ('erstes pdftotext im PATH der Aufgaben: ' + $(if ($first) { $first + ' (' + $fv + ')' } else { 'keins' }) + '; nach dem Tausch gilt [extract] pdftotext')
    Add-Info '0.11c' ('[extract] steht in der Konfiguration: ' + (Test-ExtractInConfig) + ' (mit rc9 nicht moeglich; das Skript traegt es nach dem Tausch ein)')
    $rc = Get-RemoteCounts $script:ExeFull
    $script:RemoteBefore = $rc
    if ($rc.Ok) { Add-Info '0.12' ('offene Entwuerfe vorher (rc9): ' + $rc.Line + '; A12 kann missing auf taken_over setzen und moved vergeben (kein Fehler)') } else { Add-Warn '0.12' 'draft list --json nicht lesbar; taken_over/missing nicht gezaehlt' }
    if ($script:NFail -gt 0) { Stop-D18 'Voraussetzungen fehlen; nichts geaendert' }

    Write-Log '--- Teil 1: Stand vorher (altes Programm)'
    $script:Changed = $true
    Stop-Tasks '1.1'
    if ($script:NFail -gt 0) { Stop-D18 'es laeuft noch ein postkauz oder ein Hilfsskript, oder eine Aufgabe liess sich nicht abschalten; nichts getauscht' }
    $script:DocBefore = Get-Doctor $script:ExeFull
    Add-Check '1.2' 'doctor vorher: database ok, Schema 22' ($script:DocBefore.Database -eq 'ok' -and $script:DocBefore.Schema -eq 22) ('database ' + $script:DocBefore.Database + ', schema ' + $script:DocBefore.Schema + "`n" + (($script:DocBefore.Warnings) -join "`n"))
    $script:Before = Get-Drafts $script:ExeFull @('draft', 'pending_approval', 'ready')
    $beforeAt = [DateTimeOffset]::Now
    Add-Check '1.3' ('draft list vorher: ' + $script:Before.List.Count + ' offene Entwuerfe (unter der Grenze von 500)') ($script:Before.Exit -eq 0 -and $script:Before.Parsed -and $script:Before.List.Count -lt 500) ('JSON lesbar: ' + $script:Before.Parsed + "`n" + (Format-Drafts $script:Before.List))
    Set-AuditMark $script:ExeFull '1.3a' $beforeAt
    if ($script:NFail -gt 0) { Stop-D18 'Stand vorher nicht lesbar; nichts getauscht' }
    if ($BackupTo) {
        $b = Invoke-Pk $script:ExeFull @('backup', 'create', '--to', $BackupTo) -TimeoutSec 7200
        Add-Check '1.4' 'Sicherung geschrieben (betrieb.md 2.2): Exit 0 und "backup written to"' ($b.Exit -eq 0 -and $b.Out -match 'backup written to') ('exit ' + $b.Exit)
        if ($script:NFail -gt 0) { Stop-D18 'Sicherung gescheitert; nichts getauscht' }
    } else {
        Add-Info '1.4' 'keine Sicherung (ohne -BackupTo, empfohlen): ein Abschluss oder eine Verschiebung in den Papierkorb in Teil 3 bleibt auch nach dem Rueckweg'
    }
    $c = Get-CopyCounts $script:ExeFull
    $script:CopiesBefore = $c
    if ($c.Ok) {
        Add-Info '1.5' ('vor dem Tausch (rc9, Aufgaben aus): ' + $c.Line + '. Der erste Lauf mit rc10 legt fuer die Kandidaten (draft/pending_approval, unveraendert, vom Anleger zuletzt geaendert, nicht geplant) Papierkorb-Aktionen an; der zweite fuehrt sie aus.')
    } else {
        Add-Warn '1.5' 'draft list --json nicht lesbar; fruehere Kopien nicht gezaehlt'
    }
    try {
        $lines = @($script:TransientList | ForEach-Object { [string] $_.Aid + ' ' + [string] $_.Mid })
        [System.IO.File]::WriteAllText((Get-ExtractFile), (($lines -join "`r`n") + "`r`n"), $script:Utf8)
        Add-Info '1.6' ('Liste der voruebergehend gescheiterten Anhaenge fuer B.5 geschrieben (nur IDs): D18_anhaenge.txt neben dem Programm, ' + $lines.Count + ' Zeilen')
    } catch {
        Add-Warn '1.6' ('Liste fuer B.5 nicht geschrieben: ' + $_.Exception.Message + ' (B.5 dann mit -ExtractAttachments aus 0.10b)')
    }

    Write-Log '--- Teil 2: Programm tauschen'
    $why = ''
    try {
        Install-Program $newFull $OldCopy
        $script:SwapAt = [DateTimeOffset]::Now.AddSeconds(-5)
        # A12: unmittelbar nach dem Tausch und vor dem ersten Aufruf von rc10.
        Set-ExtractConfig '2.0b'
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Check '2.1' ('getauscht: ' + $script:ExeFull + ' meldet ' + $ExpectVersion + ', altes Programm als postkauz-rc9.exe aufgehoben') ($v.Out -match [regex]::Escape($ExpectVersion) -and (Test-Path $OldCopy)) $v.Text
        $d = Get-Doctor $script:ExeFull
        $old = @{}
        foreach ($w in $script:DocBefore.Warnings) { $old[(Get-WarningKey $w)] = $true }
        $new = @($d.Warnings | Where-Object { -not $old.ContainsKey((Get-WarningKey $_)) })
        Add-Check '2.2' ('doctor nachher: database ok, Schema unveraendert ' + $script:DocBefore.Schema + ', keine neue Warnung') ($d.Database -eq 'ok' -and $d.Schema -eq $script:DocBefore.Schema -and $new.Count -eq 0) ('database ' + $d.Database + ', schema ' + $d.Schema + "`nneu: " + (($new) -join "`n"))
        $gl = Get-GuardLine $script:ExeFull
        Add-Check '2.3' 'doctor nachher zeigt die Zeile attach_guard' ([bool] $gl) $gl
        $dj = Get-DoctorJson $script:ExeFull
        $pp = $null
        if ($null -ne $dj.J) { $pp = $dj.J.pdftotext }
        Add-Check '2.5' 'doctor --json nachher: pdftotext.path = -PdfToText, setting path, probe ok' ($null -ne $pp -and [string] $pp.path -eq $PdfToText -and [string] $pp.setting -eq 'path' -and [string] $pp.probe -eq 'ok') $(if ($null -ne $pp) { 'path gleich: ' + ([string] $pp.path -eq $PdfToText) + ', setting ' + [string] $pp.setting + ', version ' + [string] $pp.version + ', probe ' + [string] $pp.probe + ', detail ' + [string] $pp.detail } else { 'doctor --json nicht lesbar' })
        if ($null -ne $dj.J) { $script:FailedSwap = Get-FailedFromDoctor $dj.J; Add-Info '2.6' ('Anhaenge failed je Konto nach dem Tausch (vor dem ersten Lauf): ' + $script:FailedSwap.Line) }
        if ($script:NFail -gt 0) { $why = 'Tausch nicht in Ordnung' }
    } catch {
        Add-Check '2.x' ('Fehler beim Tausch: ' + $_.Exception.Message) $false
        $why = 'Fehler beim Tausch'
    }
    if ($why) { Stop-D18 ($why + '; kein sync mit dem neuen Programm') }
    if ($KeepStdioMcp) {
        Add-Info '2.4' 'Der stdio-MCP der Claude-App laeuft bis zum naechsten Neustart der App mit dem alten Programm (postkauz-rc9.exe).'
    }

    Write-Log '--- Teil 3: zwei vollstaendige Laeufe mit dem neuen Programm'
    $failsBefore = $script:NFail
    $script:RanNew = $true
    $s1 = Invoke-SyncRun $script:ExeFull '3.1' 'erster Lauf'
    $oa = Get-DraftOriginActions $script:ExeFull @('draft_update') $script:SwapAt
    $c1 = Get-CopyCounts $script:ExeFull
    Add-Info '3.1b' ('fruehere Kopien nach dem ersten Lauf: Nachlauf hat ' + $s1.Actions + ' Papierkorb-Aktionen mit ' + $s1.Copies + ' Kopien angelegt (Herkunft draft_update; ausgefuehrt im zweiten Lauf). Vorher (1.5): Summe older_copies ' + $script:CopiesBefore.Sum + ', Kandidaten ' + $script:CopiesBefore.Cand) ('jetzt (rc10): ' + $c1.Line + "`nAktionen draft_update seit dem Tausch: " + $oa.Line)
    if ($script:TransientList.Count -gt 0) {
        $bd = Get-TransientBreakdown $script:ExeFull $script:TransientList
        Add-Info '3.1c' ('vorher voruebergehend gescheiterte Anhaenge (' + $script:TransientList.Count + ') nach dem ersten Lauf: ' + $bd.Line)
        if ($bd.Res.failed_old -gt 0) { Add-Warn '3.1c' ('noch ' + $bd.Res.failed_old + ' mit altem voruebergehendem Grund failed: nicht wieder eingereiht (A11 Teil 3 (b)); Claude prueft das Log des Laufs') }
    } else {
        Add-Info '3.1c' 'keine voruebergehend gescheiterten Anhaenge in 0.10 gefunden'
    }
    [void] (Measure-Extract $script:ExeFull '3.1d' 'nach dem ersten Lauf, rc10')
    $dj = Get-DoctorJson $script:ExeFull
    if ($null -ne $dj.J) { Add-Info '3.1e' ('extract_failed je Konto nach dem ersten Lauf: ' + (Get-FailedFromDoctor $dj.J).Line + '; vorher (2.6): ' + $(if ($script:FailedSwap) { $script:FailedSwap.Line } else { '?' })) }
    $rc = Get-RemoteCounts $script:ExeFull
    $mv = Get-AuditOp $script:ExeFull 'drafts.remote_check'
    $nMoved = @($mv.Events | Where-Object { [string] $_.reason -eq 'moved' }).Count
    $nEdited = @($mv.Events | Where-Object { [string] $_.reason -eq 'taken_over' -and (($_ | ConvertTo-Json -Depth 6 -Compress) -match 'edited_elsewhere') }).Count
    Add-Info '3.1f' ('offene Entwuerfe nach dem ersten Lauf: ' + $rc.Line + '; vorher (0.12): ' + $(if ($script:RemoteBefore) { $script:RemoteBefore.Line } else { '?' }) + '; Audit seit der Marke: moved ' + $nMoved + ', taken_over mit edited_elsewhere ' + $nEdited + ' (nur genannt, kein Fehler)')
    [void] (Test-Closed '3.2' 'ersten Lauf')
    if ($script:NFail -gt $failsBefore) { Stop-D18 'FAIL nach dem ersten Lauf; kein zweiter Lauf mit dem neuen Programm' }
    $s2 = Invoke-SyncRun $script:ExeFull '3.3' 'zweiter Lauf'
    $oa = Get-DraftOriginActions $script:ExeFull @('draft_update') $script:SwapAt
    $c2 = Get-CopyCounts $script:ExeFull
    Add-Info '3.3b' ('Papierkorb-Aktionen draft_update seit dem Tausch nach dem zweiten Lauf: ' + $oa.Line + '; neu angelegt im zweiten Lauf (Wiederholungen): ' + $s2.Actions) ('jetzt (rc10): ' + $c2.Line + "`nvorher (1.5, rc9): " + $script:CopiesBefore.Line)
    $bad = 0
    foreach ($k in @('failed', 'partial')) { if ($oa.States.ContainsKey($k)) { $bad += $oa.States[$k] } }
    if ($bad -gt 0) { Add-Warn '3.3b' ('Aktionen draft_update failed oder partial: ' + $bad + ' (z. B. stale: Kopie im Mailprogramm geaendert; der Nachlauf versucht es bis zu dreimal, danach bleibt die Kopie liegen)') }
    $g = Test-Closed '3.4' 'zweiten Lauf'
    Add-Info '3.4c' 'Summe nach zwei Laeufen' ('sent_by_human: ' + (Format-Ids $g.Sent) + "`n  davon Grund scheduled: " + (Format-Ids $g.BySched) + "`n  davon mit ergaenzten Empfaengern: " + (Format-Ids $g.WithAdded) + "`nersetzt: " + (Format-Ids $g.Replaced) + "`ncopy_gone: " + (Format-Ids $g.Gone) + "`nsupersede: " + (Format-Ids $g.Superseded) + "`ndiscarded: " + (Format-Ids $g.Discarded))
    $sched = @($g.After.List | Where-Object { $_.Remote -eq 'scheduled' })
    Add-Info '3.4d' ('offene Entwuerfe mit remote scheduled: ' + $sched.Count) (Format-Drafts $sched)
    Add-Info '3.5' 'Betreiber: Stimmen die Abschluesse (draft show <id> zeigt Betreff und Grund nur am Bildschirm)? Noch offen:' (Format-Drafts $g.After.List)
    $docAfter = Get-Doctor $script:ExeFull
    $old = @{}
    foreach ($w in $script:DocBefore.Warnings) { $old[(Get-WarningKey $w)] = $true }
    $new = @($docAfter.Warnings | Where-Object { -not $old.ContainsKey((Get-WarningKey $_)) })
    Add-Check '3.6' 'doctor nach den Laeufen: database ok, Schema 22, keine neue Warnung' ($docAfter.Database -eq 'ok' -and $docAfter.Schema -eq 22 -and $new.Count -eq 0) ('database ' + $docAfter.Database + ', schema ' + $docAfter.Schema + "`nneu: " + (($new) -join "`n"))
    $av = Invoke-Pk $script:ExeFull @('audit', 'verify', '--json') -TimeoutSec 1800
    $avj = Get-Json $av.Out
    Add-Check '3.7' 'audit verify: ok' ($av.Exit -eq 0 -and $null -ne $avj -and $avj.status -eq 'ok') ('exit ' + $av.Exit + ', status ' + [string] $avj.status)
    if ($script:NFail -gt $failsBefore) {
        Add-Info '3.8' 'FAIL in Teil 3: Abschluesse und Verschiebungen in den Papierkorb bleiben; offene Aktionen draft_update fuehrt E.0d mit rc10 aus, dann kommt das alte Programm zurueck (E.0).'
    }
    $script:Complete = $true
}

# ---------------------------------------------------------------- Teil B

# ---- MCP (stdio) fuer -AgentKeyFile (wie D16)

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
    $script:McpIn = New-Object System.IO.StreamWriter($p.StandardInput.BaseStream, $script:Utf8)
    $script:McpIn.NewLine = "`n"
    $script:McpIn.AutoFlush = $true
    $init = Send-Mcp 'initialize' @{ protocolVersion = '2025-06-18'; capabilities = @{}; clientInfo = @{ name = 'abnahme-d18'; version = '1' } }
    if ($null -eq $init -or $null -ne $init.error) { return $false }
    $line = (@{ jsonrpc = '2.0'; method = 'notifications/initialized' } | ConvertTo-Json -Compress -Depth 10)
    $script:McpIn.WriteLine($line)
    return $true
}

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

# Tool aufrufen; Ergebnis (structuredContent) oder Fehlerobjekt mit code,
# status und reason (wie D16).
function Invoke-McpTool {
    param([string] $Tool, $Arguments)
    $r = Send-Mcp 'tools/call' @{ name = $Tool; arguments = $Arguments }
    if ($null -eq $r) { return New-Object PSObject -Property @{ code = 'no_answer'; status = 'error'; reason = $script:McpDead } }
    if ($null -ne $r.error) { return New-Object PSObject -Property @{ code = ('rpc_' + [string] $r.error.code); status = 'error'; reason = '' } }
    if (-not [bool] $r.result.isError -and $null -ne $r.result.structuredContent) { return $r.result.structuredContent }
    $txt = ''
    foreach ($c in @($r.result.content)) { if ($null -ne $c) { $txt += [string] $c.text } }
    $e = Get-Json $txt
    if (-not [bool] $r.result.isError -and $null -ne $e -and @('denied', 'needs_approval', 'error') -notcontains [string] $e.status) {
        return $e
    }
    $code = 'error'; $status = ''; $reason = ''
    if ($null -ne $e) {
        if ($e.status) { $status = [string] $e.status; $code = $status }
        if ($e.code) { $code = [string] $e.code }
        if ($e.reason) { $reason = [string] $e.reason }
    } elseif (-not [bool] $r.result.isError) {
        $code = 'no_content'
    }
    return New-Object PSObject -Property @{ code = $code; status = $status; reason = $reason }
}

function Format-McpError {
    param($R)
    if ($null -eq $R) { return 'keine Antwort' }
    if (-not $R.code) { return 'kein Fehler' }
    return ('Fehler ' + [string] $R.code + ', Status ' + [string] $R.status + ', Grund ' + [string] $R.reason)
}

# Grund einer MCP-Ablehnung (reason, sonst code).
function Get-McpReason {
    param($R)
    if ($null -eq $R) { return 'no_answer' }
    if ($R.reason) { return [string] $R.reason }
    return [string] $R.code
}

function Test-McpAlive {
    param([string] $Nr)
    if (-not $script:McpDead) { return }
    Add-Check $Nr ('MCP-Sitzung: ' + $script:McpDead) $false
    Stop-D18 ('MCP-Server antwortet nicht; Teil B beendet (' + $script:McpDead + ')')
}

function Stop-Mcp {
    if ($null -eq $script:Mcp) { return }
    try { if ($null -ne $script:McpIn) { $script:McpIn.Close() } } catch { }
    $script:McpIn = $null
    if ($script:McpDead -or -not $script:Mcp.WaitForExit(15000)) { try { $script:Mcp.Kill() } catch { } }
    $script:Mcp = $null
}

# ---- Schrittsteuerung

function Stop-Step {
    param([string] $Why)
    throw ('D18-SCHRITT: ' + $Why)
}

function Invoke-Step {
    param([string] $Nr, [scriptblock] $Body)
    if ($Steps -notcontains $Nr) { Add-Info $Nr 'nicht gewaehlt (-Steps)'; return }
    try {
        & $Body
    } catch {
        $msg = [string] $_.Exception.Message
        if ($msg.StartsWith('D18-SCHRITT: ')) {
            Write-Log ('Schritt ' + $Nr + ' abgebrochen: ' + $msg.Substring(13))
        } else {
            throw
        }
    }
}

# ---- Spiegel und Probe-Entwuerfe (wie D16)

function Test-CopyInMirror {
    param([string] $MsgId, [int64] $Uid)
    $r = Invoke-Pk $script:ExeFull @('show', $MsgId, '-a', $SelfAccount, '--json')
    $objs = @()
    if ($r.Exit -eq 0) {
        $j = Get-Json $r.Out
        if ($null -ne $j) { $objs += $j }
    } else {
        $m = [regex]::Match(($r.Out + "`n" + $r.Err), 'ambiguous message_id[^(]*\(ids ([0-9, ]+)')
        if ($m.Success) {
            foreach ($n in ($m.Groups[1].Value -split ',')) {
                $n = $n.Trim()
                if (-not $n) { continue }
                $s = Invoke-Pk $script:ExeFull @('show', $n, '--json')
                if ($s.Exit -eq 0) {
                    $sj = Get-Json $s.Out
                    if ($null -ne $sj) { $objs += $sj }
                }
            }
        }
    }
    foreach ($o in $objs) {
        foreach ($l in (Get-Arr $o.mailboxes)) {
            if ([string] $l.role -ne 'drafts') { continue }
            if ([int64] $l.uid -eq $Uid) { return $true }
        }
    }
    return $false
}

# Wartet bis $Minutes, bis die Kopie des Entwurfs $Id im Spiegel des
# Entwuerfe-Ordners liegt (remote pushed mit remote_uid und show nennt die
# UID unter Rolle drafts). $NotUid: eine andere UID als diese (neue Kopie
# nach einer Aenderung). Liefert Ok, Why, Uid, MsgId, Detail.
function Wait-MirrorCopy {
    param([string] $Id, [int] $Minutes, [int64] $NotUid = 0, [switch] $PushedOnly)
    $deadline = (Get-Date).AddMinutes($Minutes)
    $why = 'draft show liefert nichts'
    $lastRequest = Get-Date
    $d = $null
    while ($true) {
        $d = Get-DraftDetail $script:ExeFull $Id
        if ($null -ne $d) {
            $remote = [string] $d.remote
            $uid = [int64] 0
            if ($null -ne $d.remote_uid) { $uid = [int64] $d.remote_uid }
            if (@('sent_by_human', 'abandoned') -contains [string] $d.status) {
                return New-Object PSObject -Property @{ Ok = $false; Why = ('Entwurf ist ' + [string] $d.status); Uid = $uid; MsgId = [string] $d.message_id; Detail = $d }
            }
            if ($remote -eq 'pushed' -and $uid -gt 0 -and $uid -ne $NotUid -and [string] $d.message_id) {
                if ($PushedOnly -or (Test-CopyInMirror ([string] $d.message_id) $uid)) {
                    return New-Object PSObject -Property @{ Ok = $true; Why = ('remote pushed (uid ' + $uid + ')' + $(if ($PushedOnly) { '' } else { ', Kopie im Spiegel des Entwuerfe-Ordners' })); Uid = $uid; MsgId = [string] $d.message_id; Detail = $d }
                }
                $why = 'remote pushed (uid ' + $uid + '), aber die Kopie ist noch nicht im Spiegel'
                if (((Get-Date) - $lastRequest).TotalSeconds -ge 120) { Request-Sync $SelfAccount; $lastRequest = Get-Date }
            } else {
                $why = 'remote ' + $remote + ', uid ' + $uid
            }
        }
        if ((Get-Date) -ge $deadline) { break }
        Start-Sleep -Seconds 2
    }
    return New-Object PSObject -Property @{ Ok = $false; Why = ($why + ' (nach ' + $Minutes + ' Minuten, -WaitPushMinutes)'); Uid = [int64] 0; MsgId = ''; Detail = $d }
}

function New-ProbeDraft {
    param([string] $Subject, [string] $Text, [string] $Nr)
    if ($AgentKeyFile) {
        $r = Invoke-McpTool 'mail_create_draft' @{ account = $SelfAccount; kind = 'new'; to = @($SelfAddress); subject = $Subject; body_markdown = $Text; dry_run = $false }
        Test-McpAlive $Nr
        $id = ''
        if ($null -ne $r -and -not $r.code -and $null -ne $r.draft_id) { $id = [string] $r.draft_id }
        $why = 'draft ' + $id
        if (-not $id) { $why = Format-McpError $r }
        Add-Check $Nr 'Probe-Entwurf ueber MCP mit dem Agentenschluessel angelegt (neue Mail an die eigene Adresse)' ([bool] $id) $why
        if ($id) { $script:ProbeDrafts.Add($id) }
        return $id
    }
    $body = Join-Path ([System.IO.Path]::GetTempPath()) ('d18-teilb-' + [guid]::NewGuid().ToString('N') + '.md')
    [System.IO.File]::WriteAllText($body, $Text, $script:Utf8)
    try {
        $c = Invoke-Pk $script:ExeFull @('draft', 'new', '-a', $SelfAccount, '--to', $SelfAddress, '--subject', $Subject, '--body-file', $body, '--json')
    } finally {
        Remove-Item -LiteralPath $body -Force -ErrorAction SilentlyContinue
    }
    $cj = Get-Json $c.Out
    $id = ''
    if ($null -ne $cj -and $null -ne $cj.draft_id) { $id = [string] $cj.draft_id }
    Add-Check $Nr 'Probe-Entwurf angelegt (neue Mail an die eigene Adresse, Mensch an der CLI)' ($c.Exit -eq 0 -and $id) ('exit ' + $c.Exit + ', draft ' + $id + ', Grund ' + (Get-DenyReason $c))
    if ($id) { $script:ProbeDrafts.Add($id) }
    return $id
}

# drafts.update mit neuem Text (MCP mail_update_draft oder draft edit
# --body-file). $Dry: Trockenlauf. Liefert Ok und Reason.
function Update-ProbeDraft {
    param([string] $Id, [string] $Text, [switch] $Dry, [string] $Nr)
    if ($AgentKeyFile) {
        $r = Invoke-McpTool 'mail_update_draft' @{ draft_id = [int64] $Id; body_markdown = $Text; dry_run = [bool] $Dry }
        Test-McpAlive $Nr
        $ok = ($null -ne $r -and -not $r.code)
        $reason = ''
        if (-not $ok) { $reason = Get-McpReason $r }
        return New-Object PSObject -Property @{ Ok = $ok; Reason = $reason; Exit = 0 }
    }
    $body = Join-Path ([System.IO.Path]::GetTempPath()) ('d18-teilb-' + [guid]::NewGuid().ToString('N') + '.md')
    [System.IO.File]::WriteAllText($body, $Text, $script:Utf8)
    try {
        $a = @('draft', 'edit', $Id, '--body-file', $body, '--json')
        if ($Dry) { $a += '--dry-run' }
        $c = Invoke-Pk $script:ExeFull $a
    } finally {
        Remove-Item -LiteralPath $body -Force -ErrorAction SilentlyContinue
    }
    return New-Object PSObject -Property @{ Ok = ($c.Exit -eq 0); Reason = (Get-DenyReason $c); Exit = $c.Exit }
}

# drafts.supersede bzw. drafts.discard einmal (MCP oder CLI). Liefert Ok,
# Reason, ActionId, Copies, Skipped, Status (nie trash oder next_step: sie
# nennen den Ordner).
function Invoke-DraftClose {
    param([string] $Op, [string] $Id, [string] $By, [string] $Nr)
    $r = $null
    if ($AgentKeyFile) {
        if ($Op -eq 'supersede') {
            $r = Invoke-McpTool 'mail_supersede_draft' @{ draft_id = [int64] $Id; by_draft_id = [int64] $By; dry_run = $false }
        } else {
            $r = Invoke-McpTool 'mail_discard_draft' @{ draft_id = [int64] $Id; dry_run = $false }
        }
        Test-McpAlive $Nr
        if ($null -ne $r -and -not $r.code) {
            return New-Object PSObject -Property @{ Ok = $true; Reason = ''; ActionId = [int64] $r.action_id; Copies = [int] $r.copies; Skipped = [int] $r.skipped; Status = [string] $r.status }
        }
        return New-Object PSObject -Property @{ Ok = $false; Reason = (Get-McpReason $r); ActionId = [int64] 0; Copies = 0; Skipped = 0; Status = '' }
    }
    if ($Op -eq 'supersede') {
        $c = Invoke-Pk $script:ExeFull @('draft', 'supersede', $Id, '--by', $By, '--json')
    } else {
        $c = Invoke-Pk $script:ExeFull @('draft', 'discard', $Id, '--json')
    }
    $j = $null
    if ($c.Exit -eq 0) { $j = Get-Json $c.Out }
    if ($null -ne $j) {
        return New-Object PSObject -Property @{ Ok = $true; Reason = ''; ActionId = [int64] $j.action_id; Copies = [int] $j.copies; Skipped = [int] $j.skipped; Status = [string] $j.status }
    }
    $reason = Get-DenyReason $c
    if (-not $reason) { $reason = 'exit ' + $c.Exit }
    return New-Object PSObject -Property @{ Ok = $false; Reason = $reason; ActionId = [int64] 0; Copies = 0; Skipped = 0; Status = '' }
}

# B.2/B.3: Aufruf, bei copy_not_mirrored nach 10 s erneut, hoechstens bis
# 30 s nach dem ersten Aufruf. Liefert das letzte Ergebnis und die Zahl der
# Aufrufe und der copy_not_mirrored.
function Invoke-DraftCloseRetry {
    param([string] $Op, [string] $Id, [string] $By, [string] $Nr)
    $start = Get-Date
    $calls = 0
    $notMirrored = 0
    while ($true) {
        $calls++
        $r = Invoke-DraftClose $Op $Id $By $Nr
        if ($r.Ok -or $r.Reason -ne 'copy_not_mirrored') { break }
        $notMirrored++
        if (((Get-Date) - $start).TotalSeconds -ge 30) { break }
        Start-Sleep -Seconds 10
    }
    $r | Add-Member -NotePropertyName Calls -NotePropertyValue $calls
    $r | Add-Member -NotePropertyName NotMirrored -NotePropertyValue $notMirrored
    $r | Add-Member -NotePropertyName Seconds -NotePropertyValue ([int] ((Get-Date) - $start).TotalSeconds)
    return $r
}

# Ist die Kopie (lokale Nachricht $Mid) laut show im Papierkorb (Rolle
# trash) und nicht mehr unter Rolle drafts mit UID $OldUid?
function Test-InTrash {
    param([string] $Mid, [int64] $OldUid)
    $r = Invoke-Pk $script:ExeFull @('show', $Mid, '--json')
    $j = $null
    if ($r.Exit -eq 0) { $j = Get-Json $r.Out }
    $roles = @()
    $inDrafts = $false
    if ($null -ne $j) {
        foreach ($l in (Get-Arr $j.mailboxes)) {
            $roles += [string] $l.role
            if ([string] $l.role -eq 'drafts' -and [int64] $l.uid -eq $OldUid) { $inDrafts = $true }
        }
    }
    $ok = ($null -ne $j -and $roles -contains 'trash' -and -not $inDrafts)
    return New-Object PSObject -Property @{ Ok = $ok; Why = ('show ' + $Mid + ': exit ' + $r.Exit + ', Rollen ' + ((@($roles | Where-Object { $_ } | Sort-Object -Unique)) -join ', ') + ', noch im Entwuerfe-Ordner mit alter UID: ' + $inDrafts) }
}

# Aktion zu einem Entwurf pruefen: abgeschlossen binnen $Minutes, kind
# trash, source $Source, draft_id $DraftId, state done, jedes Ziel ok, und
# jede Kopie laut show im Papierkorb.
function Test-CleanupAction {
    param([string] $Nr, [string] $ActionId, [string] $Source, [string] $DraftId, [double] $Minutes, [int64] $OldUid = 0)
    $a = Wait-ActionDone $ActionId $Minutes
    $targets = @(Get-Arr $a.targets)
    $allOk = ($targets.Count -gt 0)
    foreach ($t in $targets) { if ([string] $t.result -ne 'ok') { $allOk = $false } }
    if ($OldUid -gt 0 -and @($targets | Where-Object { [int64] $_.uid -eq $OldUid }).Count -eq 0) { $allOk = $false }
    $ok = ($null -ne $a -and [string] $a.kind -eq 'trash' -and [string] $a.source -eq $Source -and [string] $a.draft_id -eq $DraftId -and [string] $a.state -eq 'done' -and $allOk)
    Add-Check $Nr ('Aktion ' + $ActionId + ': kind trash, source ' + $Source + ', draft_id ' + $DraftId + ', state done, jedes Ziel ok (Frist ' + $Minutes + ' Minuten, -WaitCleanupMinutes)') $ok (Format-Action $a)
    if (-not $ok) { return $false }
    $all = $true
    $why = @()
    foreach ($t in $targets) {
        $tr = Test-InTrash ([string] $t.message_id) ([int64] $t.uid)
        if (-not $tr.Ok) { $all = $false }
        $why += $tr.Why
    }
    Add-Check ($Nr + 'b') 'die Kopie liegt laut Spiegel im Papierkorb (Rolle trash), nicht mehr im Entwuerfe-Ordner' $all ($why -join "`n")
    return $all
}

function Get-DraftAudit {
    param([string] $Op, [string] $Id)
    $au = Get-AuditOp $script:ExeFull $Op
    $list = @()
    foreach ($ev in $au.Events) {
        foreach ($tg in (Get-Arr $ev.targets)) {
            if ([string] $tg -eq ('draft:' + $Id)) { $list += $ev; break }
        }
    }
    return New-Object PSObject -Property @{ Ok = ($au.Exit -eq 0); Events = $list }
}

function Format-DraftState {
    param($D)
    if ($null -eq $D) { return 'draft show liefert nichts' }
    $s = 'Status ' + [string] $D.status + ', remote ' + [string] $D.remote
    if ($null -ne $D.remote_uid) { $s += ', remote_uid ' + [string] $D.remote_uid }
    $prev = @(Get-Arr $D.remote_previous_uids)
    $s += ', remote_previous_uids ' + $prev.Count
    if ($null -ne $D.older_copies) { $s += ', older_copies ' + [string] $D.older_copies }
    $ca = @(Get-Arr $D.cleanup_action_ids)
    $s += ', cleanup_action_ids ' + $(if ($ca.Count -gt 0) { ($ca -join ', ') } else { '(keine)' })
    if ($D.closed_reason) { $s += ', closed_reason ' + [string] $D.closed_reason }
    if ($D.closed_note) { $s += ', closed_note vorhanden' }
    return $s
}

function Test-OpenStatus {
    param([string] $St)
    return (@('draft', 'pending_approval', 'ready') -contains $St)
}

# Betreff einer Probe: synthetisch, mit Zeitstempel; nie im Protokoll.
function Get-ProbeSubject {
    param([string] $Step)
    return ('D18 Probe ' + $Step + ' ' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
}

# Aktionen mit draft_id $Id (action list).
function Get-ActionsOfDraft {
    param([string] $Id)
    $alj = Get-Json (Invoke-Pk $script:ExeFull @('action', 'list', '--limit', '500', '--json')).Out
    if ($null -eq $alj) { return -1 }
    return @(Get-Arr $alj.actions | Where-Object { [string] $_.draft_id -eq $Id }).Count
}

# Wartet bis $Minutes auf einen Zustand des Entwurfs ($Test bekommt das
# Detail und liefert $true); fordert alle 2 Minuten einen Lauf an.
function Wait-DraftState {
    param([string] $Id, [int] $Minutes, [scriptblock] $Test)
    $deadline = (Get-Date).AddMinutes($Minutes)
    $lastRequest = Get-Date
    $d = $null
    while ((Get-Date) -lt $deadline) {
        $d = Get-DraftDetail $script:ExeFull $Id
        if ($null -ne $d -and (& $Test $d)) { return New-Object PSObject -Property @{ Ok = $true; D = $d } }
        if (((Get-Date) - $lastRequest).TotalSeconds -ge 120) { Request-Sync $SelfAccount; $lastRequest = Get-Date }
        Start-Sleep -Seconds 15
    }
    return New-Object PSObject -Property @{ Ok = $false; D = $d }
}

# B.1: Bearbeitung im Webmail unter neuer Message-ID ist eine Uebernahme
# (A12 Teil 1 (a)-(c), Befund B1 aus D17 B.4).
function Invoke-B1 {
    Write-Log '--- B.1: Kopie im Webmail aendern (Mensch): taken_over mit edited_message_id, danach update und discard abgelehnt'
    if (-not $AgentKeyFile) { Add-Skip 'B.1' 'ohne -AgentKeyFile (B.1 prueft die Ablehnungen fuer den Agenten)'; return }
    $id = New-ProbeDraft (Get-ProbeSubject 'B.1') "Hallo,`n`ndas ist Abnahme D18 Teil B.1. Bitte im Webmail diesen Text um einen Satz ergaenzen und den Entwurf schliessen, NICHT senden.`n`nViele Gruesse" 'B.1a'
    if (-not $id) { Stop-Step 'kein Probe-Entwurf' }
    $script:B1Draft = $id
    $w = Wait-MirrorCopy $id $WaitPushMinutes
    Add-Check 'B.1b' ('Entwurf ' + $id + ': Kopie im Spiegel des Entwuerfe-Ordners (im Webmail sichtbar)') $w.Ok $w.Why
    if (-not $w.Ok) { Stop-Step 'Kopie nicht im Spiegel; im Webmail nichts tun' }
    $uid0 = $w.Uid
    Add-Info 'B.1c' ('JETZT: im Webmail den Entwurf ' + $id + ' (Betreff "D18 Probe B.1 ...") oeffnen, den Text um EINEN Satz ergaenzen und den Entwurf schliessen (Gmail speichert ihn). NICHT senden, nicht planen, nicht loeschen. Das Skript wartet bis zu ' + $WaitEditMinutes + ' Minuten (-WaitEditMinutes), bis ein vollstaendiger Lauf die Aenderung erkennt.')
    $deadline = (Get-Date).AddMinutes($WaitEditMinutes)
    $lastRequest = Get-Date
    $mistake = ''
    $reached = $false
    $d = $null
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 15
        $d = Get-DraftDetail $script:ExeFull $id
        if ($null -ne $d) {
            $st = [string] $d.status
            $remote = [string] $d.remote
            $uidNow = [int64] 0
            if ($null -ne $d.remote_uid) { $uidNow = [int64] $d.remote_uid }
            if ($st -eq 'sent_by_human') { $mistake = 'Bedienfehler (kein Programmfehler): der Entwurf wurde im Webmail gesendet statt nur geaendert (eine Mail an die eigene Adresse, harmlos)'; break }
            if ($st -eq 'abandoned') { $mistake = 'Entwurf geschlossen (' + [string] $d.closed_reason + '): Bedienfehler, die Kopie wurde verworfen oder ein Agent hat zu frueh drafts.discard aufgerufen'; break }
            if ($remote -eq 'pending' -or ($remote -eq 'pushed' -and $uidNow -ne $uid0 -and @(Get-Arr $d.cleanup_action_ids).Count -gt 0)) { $mistake = 'Bedienfehler (kein Programmfehler): ein Agent hat drafts.update zu frueh aufgerufen'; break }
            if ($remote -eq 'scheduled') { $mistake = 'Bedienfehler (kein Programmfehler): der Entwurf wurde im Webmail zum Senden geplant; im Ordner "Geplant" "Senden abbrechen"'; break }
            if ($remote -eq 'taken_over') { $reached = $true; break }
        }
        if (((Get-Date) - $lastRequest).TotalSeconds -ge 120) { Request-Sync $SelfAccount; $lastRequest = Get-Date }
    }
    if (-not $reached -and -not $mistake) {
        if ($null -ne $d -and [string] $d.remote -eq 'missing') {
            $mistake = 'remote missing statt taken_over: entweder wurde der Entwurf im Webmail geloescht (Bedienfehler) oder die weiterbearbeitete Fassung wird nicht erkannt (Programmfehler; Claude prueft mit search is:draft, ob eine nach der Ablage datierte Fassung mit gleichem Betreff im Entwuerfe-Ordner liegt)'
        } else {
            $mistake = 'keine Aenderung erkannt (remote ' + $(if ($null -ne $d) { [string] $d.remote } else { '?' }) + '): wahrscheinlich Bedienfehler (nicht geaendert, nicht gespeichert) oder kein vollstaendiger Lauf in der Frist'
        }
    }
    if ($mistake) {
        Add-Check 'B.1d' ('Entwurf ' + $id + ': remote taken_over nach dem Handgriff') $false ((Format-DraftState $d) + "`n" + $mistake)
        Stop-Step ($mistake + '; B.1e bis B.1f entfallen. Wiederholen mit -Steps B.1')
    }
    $edited = [int64] 0
    if ($null -ne $d.edited_message_id) { $edited = [int64] $d.edited_message_id }
    Add-Check 'B.1d' ('Entwurf ' + $id + ': remote taken_over, edited_message_id gesetzt (die Fassung im Webmail unter neuer Message-ID), Status offen') ((Test-OpenStatus ([string] $d.status)) -and $edited -gt 0) ((Format-DraftState $d) + ', edited_message_id ' + $edited)
    $u = Update-ProbeDraft $id "Hallo,`n`ndas ist Abnahme D18 Teil B.1 (Aenderungsversuch des Agenten, Trockenlauf).`n`nViele Gruesse" -Dry -Nr 'B.1e'
    Add-Check 'B.1e' ('drafts.update ' + $id + ' (Trockenlauf) abgelehnt mit draft_taken_over') ((-not $u.Ok) -and $u.Reason -eq 'draft_taken_over') ('angenommen: ' + $u.Ok + ', Grund ' + $u.Reason)
    $r = Invoke-DraftClose 'discard' $id '' 'B.1e2'
    if ($r.Ok) {
        Add-Check 'B.1e2' ('drafts.discard ' + $id + ' abgelehnt mit copy_changed') $false ('Programmfehler: discard angenommen (copies ' + $r.Copies + ', action_id ' + $r.ActionId + '); die Fassung im Webmail nicht loeschen, Claude meldet es')
    } else {
        Add-Check 'B.1e2' ('drafts.discard ' + $id + ' abgelehnt mit copy_changed') ($r.Reason -eq 'copy_changed') ('Grund ' + $r.Reason)
    }
    $d2 = Get-DraftDetail $script:ExeFull $id
    $mine = Get-ActionsOfDraft $id
    Add-Check 'B.1e3' 'nichts verschoben: Entwurf offen und taken_over, keine Aktion mit diesem draft_id' ($null -ne $d2 -and (Test-OpenStatus ([string] $d2.status)) -and [string] $d2.remote -eq 'taken_over' -and $mine -eq 0) ((Format-DraftState $d2) + "`nAktionen mit diesem draft_id: " + $mine)
    Add-Info 'B.1e4' ('JETZT: im Webmail die geaenderte Fassung "D18 Probe B.1 ..." verwerfen (Gmail: Entwurf oeffnen, Papierkorb-Symbol "Entwurf verwerfen"). Das Skript wartet bis zu ' + $WaitEditMinutes + ' Minuten, bis ein vollstaendiger Lauf remote missing zeigt.')
    $m = Wait-DraftState $id $WaitEditMinutes { param($x) ([string] $x.remote -eq 'missing') -or -not (Test-OpenStatus ([string] $x.status)) }
    $okM = ($m.Ok -and $null -ne $m.D -and [string] $m.D.remote -eq 'missing' -and (Test-OpenStatus ([string] $m.D.status)) -and ($null -eq $m.D.edited_message_id -or [int64] $m.D.edited_message_id -eq 0))
    $why = Format-DraftState $m.D
    if (-not $okM -and $null -ne $m.D -and [string] $m.D.remote -eq 'taken_over') { $why += "`nnoch taken_over: wahrscheinlich Bedienfehler (die Fassung liegt noch im Entwuerfe-Ordner) oder kein vollstaendiger Lauf in der Frist" }
    Add-Check 'B.1e5' ('nach dem Verwerfen im Webmail und einem weiteren Lauf: remote missing, edited_message_id leer, Entwurf offen') $okM $why
    if (-not $okM) { Stop-Step 'nicht missing; B.1f entfaellt (der Mensch raeumt mit draft abandon auf)' }
    $r = Invoke-DraftClose 'discard' $id '' 'B.1f'
    $d3 = Get-DraftDetail $script:ExeFull $id
    Add-Check 'B.1f' ('drafts.discard ' + $id + ' -> geschlossen ohne Aktion (copies 0, keine action_id)') ($r.Ok -and $r.Copies -eq 0 -and $r.ActionId -eq 0 -and $null -ne $d3 -and [string] $d3.status -eq 'abandoned') ('ok ' + $r.Ok + ', Grund ' + $r.Reason + ', copies ' + $r.Copies + ', action_id ' + $r.ActionId + "`n" + (Format-DraftState $d3))
}

# Erlaubte Groessen aus der Konfiguration ([imap] max_message_bytes,
# [draft] max_attachment_bytes); Standard 256 MiB bzw. 25 MiB.
function Get-SizeLimits {
    $mm = [int64] 268435456
    $ma = [int64] 26214400
    try {
        $t = [System.IO.File]::ReadAllText($Config)
        $x = [regex]::Match($t, '(?m)^\s*max_message_bytes\s*=\s*([0-9_]+)')
        if ($x.Success) { $mm = [int64] ($x.Groups[1].Value -replace '_', '') }
        $y = [regex]::Match($t, '(?m)^\s*max_attachment_bytes\s*=\s*([0-9_]+)')
        if ($y.Success) { $ma = [int64] ($y.Groups[1].Value -replace '_', '') }
    } catch { }
    return New-Object PSObject -Property @{ Message = $mm; Attachment = $ma }
}

# B.2: grosser Entwurf (Befund B2): nie taken_over, moved oder pushed.
function Invoke-B2 {
    Write-Log ('--- B.2: grosser Entwurf (Anhang etwa ' + $BigMB + ' MB): nie taken_over, danach update und discard')
    if (-not $AgentKeyFile) { Add-Skip 'B.2' 'ohne -AgentKeyFile'; return }
    $lim = Get-SizeLimits
    $bytes = [int64] $BigMB * 1024 * 1024
    $estimate = [int64] ($bytes * 4 / 3 + 200000)
    if ($lim.Message -le $estimate -or $lim.Attachment -lt $bytes) {
        Add-Skip 'B.2' ('[imap] max_message_bytes (' + $lim.Message + ') nicht groesser als die Nachricht (etwa ' + $estimate + ') oder [draft] max_attachment_bytes (' + $lim.Attachment + ') kleiner als der Anhang')
        return
    }
    $doc = Get-DoctorJson $script:ExeFull
    $attachDir = ''
    if ($null -ne $doc.J) { $attachDir = [string] $doc.J.attach_dir }
    if (-not $attachDir -or -not (Test-Path -LiteralPath $attachDir -PathType Container)) {
        Add-Check 'B.2a' 'attach_dir aus doctor vorhanden (dort legt das Skript den Anhang ab)' $false ('attach_dir: ' + [bool] $attachDir)
        Stop-Step 'kein attach_dir'
    }
    $name = 'D18-probe-' + (Get-Date -Format 'yyyyMMddHHmmss') + '.bin'
    $file = Join-Path $attachDir $name
    try {
        $rng = New-Object System.Random
        $buf = New-Object byte[] (1024 * 1024)
        $fs = [System.IO.File]::Create($file)
        try { for ($i = 0; $i -lt $BigMB; $i++) { $rng.NextBytes($buf); $fs.Write($buf, 0, $buf.Length) } } finally { $fs.Dispose() }
        $r = Invoke-McpTool 'mail_create_draft' @{ account = $SelfAccount; kind = 'new'; to = @($SelfAddress); subject = (Get-ProbeSubject 'B.2'); body_markdown = "Hallo,`n`ndas ist Abnahme D18 Teil B.2 (grosser Anhang, Zufallsdaten).`n`nViele Gruesse"; attachments = @(@{ file = $name }); dry_run = $false }
        Test-McpAlive 'B.2a'
    } finally {
        Remove-Item -LiteralPath $file -Force -ErrorAction SilentlyContinue
    }
    $id = ''
    if ($null -ne $r -and -not $r.code -and $null -ne $r.draft_id) { $id = [string] $r.draft_id }
    Add-Check 'B.2a' ('Probe-Entwurf mit Anhang von ' + $BigMB + ' MB ueber MCP angelegt (Datei vom Skript erzeugt, danach geloescht)') ([bool] $id) $(if ($id) { 'draft ' + $id } else { Format-McpError $r })
    if (-not $id) { Stop-Step 'kein Probe-Entwurf' }
    $script:ProbeDrafts.Add($id)
    $w = Wait-MirrorCopy $id $WaitPushMinutes
    Add-Check 'B.2b' ('Entwurf ' + $id + ': Kopie abgelegt und gespiegelt (APPEND grosser Nachrichten 60-100 s)') $w.Ok $w.Why
    if (-not $w.Ok) { Stop-Step 'Kopie nicht abgelegt' }
    $uid0 = $w.Uid
    Add-Info 'B.2c0' ('jetzt ' + $QuietMinutes + ' Minuten nichts tun (im Webmail den Entwurf NICHT oeffnen); das Skript prueft alle 30 s den Stand')
    $seen = @{}
    $until = (Get-Date).AddMinutes($QuietMinutes)
    while ((Get-Date) -lt $until) {
        $dd = Get-DraftDetail $script:ExeFull $id
        if ($null -ne $dd) { $seen[[string] $dd.remote] = $true }
        Start-Sleep -Seconds 30
    }
    $r1 = Wait-FullRun $SelfAccount $WaitRunMinutes
    $dd = Get-DraftDetail $script:ExeFull $id
    if ($null -ne $dd) { $seen[[string] $dd.remote] = $true }
    $r2 = Wait-FullRun $SelfAccount $WaitRunMinutes
    $dd = Get-DraftDetail $script:ExeFull $id
    if ($null -ne $dd) { $seen[[string] $dd.remote] = $true }
    $au = Get-DraftAudit 'drafts.remote_check' $id
    $reasons = @($au.Events | ForEach-Object { [string] $_.reason })
    $moved = @($reasons | Where-Object { $_ -eq 'moved' }).Count
    $uidNow = [int64] 0
    if ($null -ne $dd -and $null -ne $dd.remote_uid) { $uidNow = [int64] $dd.remote_uid }
    $way = 'unveraendert pushed (uid ' + $uid0 + ')'
    if ($moved -gt 0) { $way = 'moved im Audit (' + $moved + 'x; uid ' + $uid0 + ' -> ' + $uidNow + ')' }
    Add-Check 'B.2c' ('nach ' + $QuietMinutes + ' Minuten und zwei vollstaendigen Laeufen: nie taken_over, remote pushed (' + $way + ')') (-not $seen.ContainsKey('taken_over') -and $null -ne $dd -and [string] $dd.remote -eq 'pushed') ('gesehen: ' + (($seen.Keys | Sort-Object) -join ', ') + "`nAudit remote_check: " + (($reasons) -join ', ') + "`nLaeufe: " + $r1.Why + ' / ' + $r2.Why)
    $u = Update-ProbeDraft $id "Hallo,`n`ndas ist Abnahme D18 Teil B.2 (grosser Anhang, Zufallsdaten). Ein Satz mehr.`n`nViele Gruesse" -Nr 'B.2d'
    Add-Check 'B.2d' ('drafts.update ' + $id + ' (ein Satz) ok') $u.Ok ('Grund ' + $u.Reason)
    if (-not $u.Ok) { Stop-Step 'update abgelehnt' }
    $w2 = Wait-MirrorCopy $id $WaitPushMinutes -NotUid $uidNow
    Add-Check 'B.2e' 'neue Kopie abgelegt und gespiegelt' $w2.Ok $w2.Why
    $deadline = (Get-Date).AddMinutes([math]::Max($WaitCleanupMinutes, 2) + $WaitRunMinutes)
    $okC = $false
    $d4 = $null
    while ((Get-Date) -lt $deadline) {
        $d4 = Get-DraftDetail $script:ExeFull $id
        $ca = @()
        if ($null -ne $d4) { $ca = @(Get-Arr $d4.cleanup_action_ids) }
        $waiting = 0
        foreach ($a in $ca) {
            $v = Get-ActionView $script:ExeFull ([string] $a)
            if ($null -ne $v -and @('requested', 'queued', 'running') -contains [string] $v.state) { $waiting++ }
        }
        if ($ca.Count -gt 0 -and $waiting -eq 0 -and @(Get-Arr $d4.remote_previous_uids).Count -eq 0) { $okC = $true; break }
        Start-Sleep -Seconds 15
    }
    Add-Check 'B.2f' 'danach nur die neue Kopie im Entwuerfe-Ordner (fruehere Kopie im Papierkorb), keine wartende Aktion mehr (zog eine Kopie um: erst nach dem naechsten Lauf)' $okC (Format-DraftState $d4)
    $r = Invoke-DraftCloseRetry 'discard' $id '' 'B.2g'
    $ok = ($r.Ok -and $r.ActionId -gt 0)
    Add-Check 'B.2g' ('drafts.discard ' + $id + ': abandoned, Aktion angelegt') $ok ('Grund ' + $r.Reason + ', copies ' + $r.Copies + ', action_id ' + $r.ActionId + ', Aufrufe ' + $r.Calls)
    if ($ok) { [void] (Test-CleanupAction 'B.2h' ([string] $r.ActionId) 'draft_discard' $id ([double] $WaitCleanupMinutes)) }
}

# B.3: attach retry fuer die Anhaenge aus D17 (Befund B6).
function Invoke-B3 {
    Write-Log '--- B.3: attach retry fuer die am falschen PDF-Programm gescheiterten Anhaenge'
    if (-not $RetryAccount -or -not $RetryMessage) { Add-Skip 'B.3' 'ohne -RetryAccount und -RetryMessage (nennt der Betreiber beim Aufruf)'; return }
    $l0 = Invoke-Pk $script:ExeFull @('attach', 'list', $RetryMessage, '--json')
    $lj = Get-Json $l0.Out
    $failed = @()
    if ($null -ne $lj) { $failed = @(Get-Arr $lj.attachments | Where-Object { [string] $_.extract_status -eq 'failed' }) }
    Add-Info 'B.3a' ('vorher: ' + $failed.Count + ' Anhaenge der Nachricht mit extract_status failed (exit ' + $l0.Exit + ')')
    $d = Invoke-Pk $script:ExeFull @('attach', 'retry', '--account', $RetryAccount, '--message', $RetryMessage, '--dry-run', '--json')
    $dj = Get-Json $d.Out
    Add-Check 'B.3b' 'attach retry --dry-run nennt die Zahl' ($d.Exit -eq 0 -and $null -ne $dj -and [bool] $dj.dry_run -and [int] $dj.count -eq $failed.Count) ('exit ' + $d.Exit + ', count ' + $(if ($null -ne $dj) { [string] $dj.count } else { '?' }))
    $x = Invoke-Pk $script:ExeFull @('attach', 'retry', '--account', $RetryAccount, '--message', $RetryMessage, '--json')
    $xj = Get-Json $x.Out
    Add-Check 'B.3c' 'attach retry setzt sie auf pending' ($x.Exit -eq 0 -and $null -ne $xj -and [int] $xj.count -eq $failed.Count) ('exit ' + $x.Exit + ', count ' + $(if ($null -ne $xj) { [string] $xj.count } else { '?' }))
    $run = Wait-FullRun $RetryAccount $WaitRunMinutes
    $l1 = Invoke-Pk $script:ExeFull @('attach', 'list', $RetryMessage, '--json')
    $lj1 = Get-Json $l1.Out
    $st = @()
    $okIds = @()
    if ($null -ne $lj1) {
        foreach ($a in (Get-Arr $lj1.attachments)) {
            if (@($failed | Where-Object { [string] $_.id -eq [string] $a.id }).Count -eq 0) { continue }
            $st += [string] $a.extract_status
            if ([string] $a.extract_status -eq 'ok') { $okIds += [string] $a.id }
        }
    }
    Add-Check 'B.3d' 'nach dem naechsten Lauf: jeder dieser Anhaenge ok' ($st.Count -eq $failed.Count -and $okIds.Count -eq $failed.Count -and $failed.Count -gt 0) ('Stand: ' + (($st) -join ', ') + "`n" + $run.Why)
    if ($okIds.Count -gt 0) {
        $t = Invoke-Pk $script:ExeFull @('attach', 'text', $okIds[0], '--max-chars', '2000', '--json') -TimeoutSec 300
        $tj = Get-Json $t.Out
        Add-Check 'B.3e' ('attach text ' + $okIds[0] + ' liefert Text') ($t.Exit -eq 0 -and $null -ne $tj -and ([string] $tj.text).Length -gt 0) ('exit ' + $t.Exit + ', Textlaenge ' + $(if ($null -ne $tj) { ([string] $tj.text).Length } else { 0 }))
    }
}

# B.4: doctor --json (A12 Teil 3 (d)).
function Invoke-B4 {
    Write-Log '--- B.4: doctor --json (pdftotext, Zaehler, notes)'
    $d = Get-DoctorJson $script:ExeFull
    $j = $d.J
    $probe = ''
    if ($null -ne $j -and $null -ne $j.pdftotext) { $probe = [string] $j.pdftotext.probe }
    Add-Check 'B.4a' 'doctor --json: pdftotext.probe ok' ($probe -eq 'ok') ('probe ' + $probe + ', setting ' + $(if ($null -ne $j -and $null -ne $j.pdftotext) { [string] $j.pdftotext.setting } else { '?' }))
    $fields = $true
    foreach ($a in (Get-Arr $j.accounts)) {
        foreach ($f in @('extract_failed', 'extract_failed_exhausted', 'extract_waiting', 'extract_skipped_tool', 'draft_copies_given_up')) {
            if ($null -eq $a.PSObject.Properties[$f]) { $fields = $false }
        }
    }
    Add-Check 'B.4b' 'doctor --json: je Konto extract_failed, extract_failed_exhausted, extract_waiting, extract_skipped_tool, draft_copies_given_up; oben notes' ($null -ne $j -and $fields -and $null -ne $j.PSObject.Properties['notes']) ('Konten: ' + @(Get-Arr $j.accounts).Count)
    if ($null -ne $j) { Add-Info 'B.4c' ('Zaehler: ' + (Get-FailedFromDoctor $j).Line + '; notes: ' + @(Get-Arr $j.notes).Count) }
    Add-Check 'B.4d' 'doctor Exit-Code 0 (notes aendern ihn nicht)' ($d.Exit -eq 0) ('exit ' + $d.Exit)
}

function Invoke-TeilB {
    Write-Log '--- Teil B: Proben im eigenen Konto (Entwuerfe nur an die eigene Adresse, nichts wird gesendet)'
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    Add-Check 'BV.1' ('Programm meldet ' + $ExpectVersion) ($v.Exit -eq 0 -and $v.Out -match [regex]::Escape($ExpectVersion)) $v.Text
    $al = Invoke-Pk $script:ExeFull @('accounts', 'list', '--json')
    $aj = Get-Json $al.Out
    $known = @()
    if ($null -ne $aj -and $null -ne $aj.accounts) {
        $known = @(Get-Arr $aj.accounts | ForEach-Object { [string] $_.name })
        Register-AccountNames $known
        foreach ($a in (Get-Arr $aj.accounts)) {
            if (-not $SelfAddress -and [string] $a.name -eq $SelfAccount) { $script:SelfAddressFound = [string] $a.email }
        }
    }
    if (-not $SelfAddress -and $script:SelfAddressFound) { $script:SelfAddress = $script:SelfAddressFound }
    Add-Check 'BV.2' '-SelfAccount ist ein konfiguriertes Konto, eigene Adresse bekannt' ($SelfAccount -and ($known -contains $SelfAccount) -and [bool] $script:SelfAddress) ('Konten: ' + $known.Count + ', Konto gefunden: ' + ($known -contains $SelfAccount) + ', Adresse bekannt: ' + [bool] $script:SelfAddress)
    if ($RetryAccount) { Add-Check 'BV.2b' '-RetryAccount ist ein konfiguriertes Konto' ($known -contains $RetryAccount) '' }
    $mb = Get-MailboxList $script:ExeFull $SelfAccount
    $roles = @()
    if ($mb.Ok) { $roles = @($mb.Boxes | ForEach-Object { [string] $_.role }) }
    Add-Check 'BV.3' 'mailbox list -a <Konto> --json: Ordner mit Rolle drafts und trash vorhanden' ($mb.Ok -and $roles -contains 'drafts' -and $roles -contains 'trash') ('lesbar: ' + $mb.Ok + ', Ordner: ' + @($mb.Boxes).Count)
    if ($AgentKeyFile) {
        $mcpOk = (Test-Path -LiteralPath $AgentKeyFile) -and (Start-Mcp $AgentKeyFile)
        Add-Check 'BV.4' 'Agentenschluessel: Datei vorhanden, MCP-Sitzung angemeldet (Schluessel nie im Protokoll)' $mcpOk ([string] $script:McpDead)
    } else {
        Add-Info 'BV.4' 'ohne -AgentKeyFile: B.1 und B.2 entfallen (SKIP)'
    }
    $w = @(Get-OwnWatch)
    $selfWatch = @($w | Where-Object { @($_.Accounts) -contains '*' -or @($_.Accounts) -icontains $SelfAccount }).Count
    Add-Check 'BV.5' 'sync --watch genau mit -Exe laeuft fuer -SelfAccount (legt ab, fuehrt Aktionen aus)' ($selfWatch -gt 0) ('sync --watch: ' + $w.Count + ', davon fuer das Konto: ' + $selfWatch)
    Add-Check 'BV.6' '[draft] previous_copies ist NICHT gesetzt (Standard trash)' (-not (Test-PreviousCopiesKey))
    if ($script:NFail -gt 0) { Stop-D18 'Voraussetzungen fehlen' }
    Set-AuditMark $script:ExeFull 'BV.7' ([DateTimeOffset]::Now)
    Invoke-Step 'B.1' { Invoke-B1 }
    Invoke-Step 'B.2' { Invoke-B2 }
    Invoke-Step 'B.3' { Invoke-B3 }
    Invoke-Step 'B.4' { Invoke-B4 }
    $open = @()
    foreach ($x in @($script:ProbeDrafts)) {
        $dd = Get-DraftDetail $script:ExeFull $x
        if ($null -eq $dd -or -not (Test-OpenStatus ([string] $dd.status))) { continue }
        if ([string] $dd.remote -eq 'pushed') {
            $r = Invoke-DraftClose 'discard' $x '' 'B.9'
            if ($r.Ok) { Add-Info 'B.9' ('Probe-Entwurf ' + $x + ' verworfen (drafts.discard, copies ' + $r.Copies + ', action_id ' + $r.ActionId + ')'); continue }
        }
        $open += ($x + ' (remote ' + [string] $dd.remote + ')')
    }
    if ($open.Count -gt 0) {
        Add-Info 'B.9' ('noch offene Probe-Entwuerfe: ' + ($open -join ', ') + '. Aufraeumen nach abnahme_D18.md Abschnitt 7.')
    } else {
        Add-Info 'B.9' 'keine offenen Probe-Entwuerfe'
    }
    $script:CompleteB = $true
}

# ---------------------------------------------------------------- Rueckweg

function Invoke-Rueckweg {
    Write-Log '--- Rueckweg: altes Programm zurueck (keine Migration in A11)'
    Add-Check 'R.0' 'aufgehobenes altes Programm vorhanden (postkauz-rc9.exe)' (Test-Path $OldCopy) $OldCopy
    if (-not (Test-Path $OldCopy)) { Stop-D18 'postkauz-rc9.exe fehlt; nichts geaendert' }
    $present = Test-Path $script:ExeFull
    $checkProg = ''
    if ($present) {
        $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
        Add-Check 'R.0b' ('laufendes Programm meldet ' + $ExpectVersion) ($v.Out -match [regex]::Escape($ExpectVersion)) $v.Text
        $checkProg = $script:ExeFull
    } else {
        Add-Info 'R.0b' ($script:ExeFull + ' fehlt (harter Abbruch?); das alte Programm kommt an seine Stelle')
        if ($NewExe -and (Test-Path $NewExe)) {
            $nf = [System.IO.Path]::GetFullPath($NewExe)
            if ((Get-ProgramVersion $nf) -eq $ExpectVersion) { $checkProg = $nf }
        }
    }
    $o = Invoke-Pk $OldCopy @('version') -NoDirs
    Add-Check 'R.0c' ('postkauz-rc9.exe meldet ' + $OldVersion) ($o.Out -match [regex]::Escape($OldVersion)) $o.Text
    Add-Check 'R.0d' '-Tasks und -Accounts angegeben (Pflicht)' ($Tasks.Count -gt 0 -and $Accounts.Count -gt 0) ('Aufgaben: ' + $Tasks.Count + ', Konten: ' + $Accounts.Count)
    $missingTasks = @($Tasks | Where-Object { $null -eq (Get-ScheduledTask -TaskName $_ -ErrorAction SilentlyContinue) })
    Add-Check 'R.0e' 'jede Aufgabe aus -Tasks vorhanden' ($missingTasks.Count -eq 0) ('Aufgaben: ' + $Tasks.Count + ', nicht gefunden: ' + $missingTasks.Count)
    Test-NoBackupTasks 'R.0f'
    Add-Info 'R.0g' ('[extract] in der Konfiguration: ' + (Test-ExtractInConfig) + ' (wird vor dem Start von rc9 herausgenommen; R.0h liest eine temporaere Kopie ohne [extract])')
    # K3: vor dem ersten aendernden Schritt accounts list mit dem ALTEN
    # Programm gegen -Accounts (prueft zugleich -DataDir und -Config fuer
    # rc9).
    $tmpCfg = New-ConfigWithoutExtract
    try { $al = Invoke-Pk $OldCopy @('accounts', 'list', '--json') -Cfg $tmpCfg } finally { Remove-Item -LiteralPath $tmpCfg -Force -ErrorAction SilentlyContinue }
    $aj = Get-Json $al.Out
    $known = @()
    if ($null -ne $aj -and $null -ne $aj.accounts) { $known = @(Get-Arr $aj.accounts | ForEach-Object { [string] $_.name }) }
    Register-AccountNames $known
    $unknown = @($Accounts | Where-Object { $known -notcontains $_ })
    $notGiven = @($known | Where-Object { $Accounts -notcontains $_ })
    Add-Check 'R.0h' 'accounts list mit dem alten Programm (mit -DataDir und -Config) entspricht genau -Accounts' ($al.Exit -eq 0 -and $Accounts.Count -gt 0 -and $unknown.Count -eq 0 -and $notGiven.Count -eq 0) ('exit ' + $al.Exit + ', angegeben: ' + $Accounts.Count + ', konfiguriert: ' + $known.Count + ', nicht konfiguriert: ' + $unknown.Count + ', konfiguriert, aber nicht angegeben: ' + $notGiven.Count)
    Add-Check 'R.0i' 'neues Programm fuer die Pruefung der Aktionen da (postkauz.exe meldet rc10, sonst -NewExe)' ([bool] $checkProg) $checkProg
    if ($script:NFail -gt 0) { Stop-D18 'Rueckweg nicht moeglich; nichts geaendert' }
    foreach ($tn in $Tasks) { $script:WasEnabled[$tn] = $true }
    $script:Changed = $true
    Stop-Tasks 'R.1'
    if ($script:NFail -gt 0) {
        # W3: nichts getauscht; laeuft rc10 noch an seinem Platz, Aufgaben
        # wieder an (wie Teil A 1.1).
        if ($present) { $script:RestartNew = $true }
        Stop-D18 'es laeuft noch ein postkauz oder ein Hilfsskript; nichts getauscht'
    }
    # betrieb.md 1.7c: nach dem Anhalten und vor dem Tausch mit dem NEUEN
    # Programm (nur es zeigt source).
    if (-not (Clear-DraftActions $checkProg 'R.2')) {
        if ($present) { $script:RestartNew = $true }
        else { $script:KeepTasksOff = $true }
        Stop-D18 'Aktion der Herkunft draft_update/draft_discard bleibt offen; Rueckweg abgebrochen, nichts getauscht (Aufgaben laufen mit dem neuen Programm weiter, betrieb.md 1.7c)'
    }
    # A12: rc9 startet mit [extract] nicht.
    if (-not (Clear-ExtractConfig 'R.2b')) {
        if ($present) { $script:RestartNew = $true }
        Stop-D18 '[extract] liess sich nicht aus der Konfiguration nehmen; nichts getauscht'
    }
    $newCopy = Get-NewCopyName
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
    Add-Check 'R.3' ('neues Programm als ' + (Split-Path -Leaf $newCopy) + ' beiseite (falls vorhanden), altes an seine Stelle kopiert') (-not $err -and (Test-Path $script:ExeFull) -and (-not $present -or (Test-Path $newCopy))) ('Fehler: ' + $err)
    if ($err) {
        $back = ''
        if ($renamed) {
            try {
                if (Test-Path $script:ExeFull) { Rename-Item -LiteralPath $script:ExeFull -NewName (Split-Path -Leaf (Get-FreeName 'postkauz-teilkopie')) -ErrorAction Stop }
                Rename-Item -LiteralPath $newCopy -NewName (Split-Path -Leaf $script:ExeFull) -ErrorAction Stop
            } catch { $back = $_.Exception.Message }
        }
        Add-Check 'R.3d' 'Zustand vor dem Tausch wiederhergestellt' (-not $back) ('Fehler: ' + $back)
        $script:KeepTasksOff = $true
        Stop-D18 'Rueckweg gescheitert; Aufgaben bleiben AUS (von Hand nach abnahme_D18.md Abschnitt 6 weiter)'
    }
    if ($KeepStdioMcp) {
        Add-Info 'R.3c' 'Der stdio-MCP der Claude-App laeuft bis zum naechsten Neustart der App mit dem bisherigen Programm.'
    }
    $v = Invoke-Pk $script:ExeFull @('version') -NoDirs
    $d = Get-Doctor $script:ExeFull
    Add-Check 'R.4' ('altes Programm laeuft mit der Datenbank: ' + $OldVersion + ', database ok, Schema 22') ($v.Out -match [regex]::Escape($OldVersion) -and $d.Database -eq 'ok' -and $d.Schema -eq 22) ($v.Text + "`ndatabase " + $d.Database + ', schema ' + $d.Schema)
    if ($script:NFail -gt 0) {
        $script:KeepTasksOff = $true
        Stop-D18 'R.4 nicht in Ordnung; kein sync, Aufgaben bleiben AUS (von Hand nach abnahme_D18.md Abschnitt 6 weiter)'
    }
    [void] (Invoke-SyncRun $script:ExeFull 'R.5' ('Rueckweg mit ' + $OldVersion))
    if ($script:NFail -gt 0) {
        $script:KeepTasksOff = $true
        Stop-D18 'R.5 nicht in Ordnung; Aufgaben bleiben AUS (von Hand nach abnahme_D18.md Abschnitt 6 weiter)'
    }
    Add-Info 'R.6' 'Fall: ohne Migration (A12: rc9 laesst den Vermerk draft_edited/<id> in meta liegen und fuehrt die nach moved nachgefuehrte UID wie jede abgelegte Kopie; ein von Gmail umgelegter grosser Entwurf kann unter rc9 wieder taken_over werden). Mit rc10 verschobene Kopien bleiben im Papierkorb, verworfene Entwuerfe bleiben abandoned (rc9 zeigt bei ihnen keinen closed_reason discarded). rc9 kennt kein Aufraeumen nach einer Aenderung: fruehere Kopien bleiben wieder liegen. Vermerke in meta (draft_copy_cleanup/, extract_retry/, extract_requeue/) bleiben liegen; ein Anhang, der auf einen Wiederholversuch wartet, bleibt pending und wird von rc9 ohne Wartezeit erneut versucht.'
    [void] (Start-Tasks 'R.7' $OldVersion)
    $script:Restarted = $true
}

# ---------------------------------------------------------------- Start

# K3: -Exe, -DataDir und -Config haben keinen Standard; ohne sie (oder wenn
# sie fehlen) laeuft nichts.
$missing = @()
if (-not $Exe) { $missing += '-Exe' }
if (-not $DataDir) { $missing += '-DataDir' }
if (-not $Config) { $missing += '-Config' }
if ($missing.Count -gt 0) {
    Write-Host ('Abnahme D18: ' + ($missing -join ', ') + ' fehlt (Pflicht, kein Standard; Beispiele: Get-Help ' + $MyInvocation.MyCommand.Path + ' -Examples). Nichts geaendert.')
    exit 2
}
$script:ExeFull = [System.IO.Path]::GetFullPath($Exe)
$dir = Split-Path -Parent $script:ExeFull
$script:ExeDir = $dir
$script:SelfAddressFound = ''
if (-not $LogFile) {
    if ($TeilB) { $LogFile = Join-Path $dir 'D18_teilB.txt' } else { $LogFile = Join-Path $dir 'D18_windows.txt' }
}
if (Test-Path -LiteralPath $LogFile) {
    $aside = [System.IO.Path]::Combine((Split-Path -Parent $LogFile), [System.IO.Path]::GetFileNameWithoutExtension($LogFile) + '-' +
        (Get-Date -Format 'yyyyMMddHHmmss') + [System.IO.Path]::GetExtension($LogFile))
    try { Rename-Item -LiteralPath $LogFile -NewName (Split-Path -Leaf $aside) -ErrorAction Stop } catch {
        Write-Host ('Protokoll laesst sich nicht beiseitelegen: ' + $_.Exception.Message)
        exit 1
    }
}
# A12: PATH der geplanten Aufgaben fuer alle Aufrufe.
Set-TaskPath
# K5: Namen vor der ersten Protokollzeile bekannt machen.
Register-TaskNames
Register-AccountNames (@($Accounts) + @($SelfAccount) + @($RetryAccount))
# W2: in Teil B ohne -Accounts alle Konten aus accounts list maskieren.
if ($TeilB -and (Test-Path -LiteralPath $script:ExeFull -PathType Leaf)) {
    try {
        $pre = Get-Json (Invoke-Pk $script:ExeFull @('accounts', 'list', '--json')).Out
        if ($null -ne $pre -and $null -ne $pre.accounts) { Register-AccountNames @(Get-Arr $pre.accounts | ForEach-Object { [string] $_.name }) }
    } catch { }
}
$script:Log = $LogFile
$OldCopy = Join-Path $dir 'postkauz-rc9.exe'
$script:Swapped = $false
$script:Restarted = $false
$mode = 'Teil A, Umstellung'
if ($Rueckweg) { $mode = 'Rueckweg' }
if ($TeilB) { $mode = 'Teil B' }
Write-Log ('=== Abnahme D18 (' + $mode + '), ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + ' ===')
Write-Log ('Programm: ' + $script:ExeFull + '; Daten: ' + $DataDir + '; Konfiguration: ' + $Config)
$elevated = $false
try { $elevated = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator) } catch { }
Add-Info '0.0' ('PowerShell mit Administratorrechten: ' + $elevated + ' (Anleitung Abschnitt 3: so wie die Aufgaben angelegt sind)')
$okDirs = ((Test-Path -LiteralPath $script:ExeFull -PathType Leaf) -or $Rueckweg) -and (Test-Path -LiteralPath $DataDir -PathType Container) -and (Test-Path -LiteralPath $Config -PathType Leaf)
Add-Check '0.0a' '-Exe, -DataDir und -Config angegeben und vorhanden (K3: kein Standardwert)' $okDirs ('-Exe vorhanden: ' + (Test-Path -LiteralPath $script:ExeFull -PathType Leaf) + ', -DataDir ist ein Ordner: ' + (Test-Path -LiteralPath $DataDir -PathType Container) + ', -Config ist eine Datei: ' + (Test-Path -LiteralPath $Config -PathType Leaf))

$guard = $false
try {
    if (-not $okDirs) { Stop-D18 'Pfade fehlen; nichts geaendert' }
    if ($TeilB) { Invoke-TeilB } elseif ($Rueckweg) { Invoke-Rueckweg } else { Invoke-Umstellung }
} catch {
    $msg = [string] $_.Exception.Message
    if ($msg.StartsWith('D18-ABBRUCH: ')) {
        Write-Log ('Abbruch: ' + $msg.Substring(13))
    } else {
        Add-Check 'X' ('unerwarteter Fehler im Skript: ' + $msg) $false
    }
} finally {
    $guard = Set-CtrlCGuard $true
    if ($guard -and -not $TeilB) { Add-Info 'E.4' 'Strg+C bis zum Ende des Abschlusses abgeschaltet' }
    try { Stop-Mcp } catch { }
    $umst = (-not $Rueckweg -and -not $TeilB)
    if ($umst -and -not $script:Complete) {
        Add-Check 'E.2' 'Teil 3 lief nicht vollstaendig: Abnahme nicht bestanden' $false
    }
    if ($TeilB -and -not $script:CompleteB) {
        Add-Check 'E.2' 'Teil B lief nicht vollstaendig: Teil B nicht bestanden' $false
    }
    $restoreFailed = $false
    if ($umst -and $script:Swapped -and $script:NFail -gt 0) {
        try {
            # E.0d: Aktionen draft_update/draft_discard mit rc10 ausfuehren,
            # bevor rc9 zurueckkommt (rc9 kennt die Herkunft nicht).
            $clear = $true
            if ($script:RanNew -and (Test-Path $script:ExeFull)) { $clear = Clear-DraftActions $script:ExeFull 'E.0d' }
            if (-not $clear) {
                Add-Check 'E.0' 'altes Programm NICHT zurueckgelegt: eine Aktion der Herkunft draft_update/draft_discard bleibt offen; Aufgaben bleiben aus (abnahme_D18.md Abschnitt 8)' $false
                $restoreFailed = $true
            } elseif (-not (Restore-Program $OldCopy 'E.0')) {
                $restoreFailed = $true
            } else {
                $d = Get-Doctor $script:ExeFull
                $okDb = ($d.Database -eq 'ok' -and $d.Schema -eq 22)
                Add-Check 'E.0c' ('altes Programm laeuft mit der Datenbank: database ok, Schema 22') $okDb ('database ' + $d.Database + ', schema ' + $d.Schema)
                if (-not $okDb) { $restoreFailed = $true }
            }
        } catch {
            Add-Check 'E.0' ('altes Programm zurueck: ' + $_.Exception.Message) $false
            $restoreFailed = $true
        }
    }
    if ($Rueckweg -and $script:RestartNew -and $script:WasEnabled.Count -gt 0) {
        # betrieb.md 1.7c: Rueckweg vor dem Tausch abgebrochen; das neue
        # Programm liegt unveraendert an seinem Platz und laeuft weiter.
        try { [void] (Start-Tasks 'E.1' $ExpectVersion) } catch { Add-Check 'E.1' ('Aufgaben wieder einschalten: ' + $_.Exception.Message) $false }
        $script:Restarted = $true
    }
    if ($Rueckweg -and ($script:NFail -gt 0 -or -not $script:Restarted)) { $script:KeepTasksOff = $true }
    if ($script:WasEnabled.Count -gt 0 -and -not $script:Restarted) {
        if ($restoreFailed -or $script:KeepTasksOff) {
            Add-Check 'E.1' 'Aufgaben NICHT wieder eingeschaltet: FAIL vor dem Wiedereinschalten oder das richtige Programm liegt nicht sicher an seinem Platz (von Hand nach abnahme_D18.md Abschnitt 6)' $false
            Write-TasksOff $(if ($restoreFailed) { 'das alte Programm liess sich nicht zuruecklegen oder eine Aktion draft_update/draft_discard bleibt offen' } else { 'FAIL vor dem Wiedereinschalten (rc9 bleibt im Ziel oder der Rueckweg scheiterte)' })
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
                if (-not $done41) { Write-Log 'Teil 4 nicht vollstaendig: Aufgaben von Hand pruefen (abnahme_D18.md Abschnitt 3, harter Abbruch).' }
            }
            if (-not $started) {
                try {
                    $before = $script:NFail
                    Stop-Tasks 'E.0a'
                    if ($script:NFail -gt $before) {
                        Add-Check 'E.0b' 'Aufgaben nach 4.1 nicht sauber angehalten: altes Programm NICHT zurueckgelegt, Aufgaben bleiben aus (von Hand nach abnahme_D18.md Abschnitt 6)' $false
                    } elseif (-not (Clear-DraftActions $script:ExeFull 'E.0d')) {
                        Add-Check 'E.1' 'Aktion der Herkunft draft_update/draft_discard bleibt offen: altes Programm NICHT zurueckgelegt, Aufgaben bleiben aus' $false
                    } elseif (Restore-Program $OldCopy 'E.0') {
                        [void] (Start-Tasks 'E.1' $OldVersion)
                    } else {
                        Add-Check 'E.1' 'Aufgaben angehalten, altes Programm liess sich nicht zuruecklegen (von Hand nach abnahme_D18.md Abschnitt 6)' $false
                    }
                } catch { Add-Check 'E.1' ('Rueckweg nach 4.1: ' + $_.Exception.Message) $false }
            }
        } else {
            try { [void] (Start-Tasks 'E.1' $OldVersion) } catch { Add-Check 'E.1' ('Aufgaben wieder einschalten: ' + $_.Exception.Message) $false }
        }
    }
    if ($umst) {
        if (-not $script:Changed) { Write-Log 'Ergebnis: nichts geaendert (Teil 1 nicht begonnen: keine Aufgabe angehalten, kein Programm getauscht, keine Sicherung)' }
        $cur = Get-ProgramVersion $script:ExeFull
        if (-not $cur) { $cur = '(fehlt oder meldet keine Version)' }
        $keep = 'fehlt'
        if (Test-Path $OldCopy) { $keep = (Get-ProgramVersion $OldCopy) }
        Write-Log ('Stand: ' + $script:ExeFull + ' meldet ' + $cur + '; postkauz-rc9.exe: ' + $keep)
    }
    Write-Log ''
    if ($script:Fails.Count -gt 0) { Write-Log ('Fehlgeschlagen: ' + ($script:Fails -join '; ')) }
    if ($script:LogBroken) { try { Write-Host 'ACHTUNG: Das Protokoll liess sich nicht vollstaendig schreiben (Datentraeger voll oder gesperrt).' } catch { } }
    Write-Log ('D18: ' + $script:NOk + ' OK, ' + $script:NFail + ' FAIL, ' + $script:NWarn + ' WARN, ' + $script:NSkip + ' SKIP')
    if ($guard) { [void] (Set-CtrlCGuard $false) }
}
if ($script:NFail -gt 0) { exit 1 }
exit 0
