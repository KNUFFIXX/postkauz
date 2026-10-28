<#
.SYNOPSIS
    Keeps "postkauz.exe sync --watch --all" running: restarts it after a crash
    or an end, with a growing wait, and writes its output to a rotating log.

.DESCRIPTION
    Register-SyncWatch.ps1 registers a scheduled task that starts this script
    when you log on. You do not need to start it yourself; to try it, run it
    in a PowerShell window and stop it with Ctrl+C.

    The script runs

        postkauz.exe sync --watch --all --data-dir <DataDir> [--config <ConfigFile>]

    and waits until it ends. Then it waits and starts it again:
    - the first wait is 60 seconds, each further one twice as long, at most
      1800 seconds (30 minutes);
    - after a run that lasted at least 30 minutes, the wait starts again at
      60 seconds;
    - if postkauz.exe ended with exit code 6 (the mail server rejected the
      login, or an OAuth2 token could not be renewed), the script always waits
      the longest time and writes a log line: trying again and again could
      lock the account at the provider. Fix the password (postkauz.exe account
      set <name> --password-stdin) or sign in again (account login <name>).
      With --all (as this script runs it) a rejected login does not end
      postkauz.exe: the program tries that account again every 30 minutes
      itself and the other accounts go on; exit code 6 then only comes from
      a run that watches a single account.

    Log: <DataDir>\logs\sync-watch.log (UTF-8). When it is larger than
    -MaxLogBytes (10 MB), it is renamed to sync-watch.1.log, older files move
    up to sync-watch.<LogFiles>.log, and the oldest is deleted. The log holds
    what postkauz.exe prints (counts, mailbox names, errors) and the lines of
    this script, never a password: the passwords stay in the Windows
    Credential Manager, postkauz.exe reads them itself.

    Requires Windows PowerShell 5.1 or later and no administrator rights.

.PARAMETER ExePath
    Full path to postkauz.exe.

.PARAMETER DataDir
    PostKauz data directory (--data-dir).

.PARAMETER ConfigFile
    PostKauz configuration file (--config); empty = none (defaults).

.PARAMETER LogDir
    Folder for the log files. Default: <DataDir>\logs.

.PARAMETER MaxLogBytes
    Size at which the log file is rotated (default 10 MB).

.PARAMETER LogFiles
    Number of old log files to keep (default 5).

.PARAMETER FirstWaitSeconds
    First wait before a restart (default 60).

.PARAMETER MaxWaitSeconds
    Longest wait before a restart (default 1800).

.PARAMETER StableSeconds
    A run that lasted at least this long resets the wait to FirstWaitSeconds
    (default 1800).

.PARAMETER MaxRuns
    Stop after this many runs (0 = never; for tests).

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\Run-SyncWatch.ps1 -ExePath C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ExePath,
    [Parameter(Mandatory = $true)][string]$DataDir,
    [string]$ConfigFile = "",
    [string]$LogDir = "",
    [long]$MaxLogBytes = 10485760,
    [int]$LogFiles = 5,
    [int]$FirstWaitSeconds = 60,
    [int]$MaxWaitSeconds = 1800,
    [int]$StableSeconds = 1800,
    [int]$MaxRuns = 0
)

# Hinweis zum Format dieser Datei: UTF-8 mit BOM, Inhalt nur ASCII (Windows
# PowerShell 5.1 liest Dateien ohne BOM als ANSI). Keine Aliase, nur
# Cmdlet-Namen (Verb-Noun).
Set-StrictMode -Version 2.0
$ErrorActionPreference = "Stop"

# Exit-Code von postkauz.exe fuer eine abgelehnte Anmeldung (docs/cli.md,
# Abschnitt 4, ExitAuth).
$ExitAuth = 6

if ($LogDir -eq "") {
    $LogDir = Join-Path $DataDir "logs"
}
if (-not (Test-Path -LiteralPath $LogDir -PathType Container)) {
    New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
}
$LogPath = Join-Path $LogDir "sync-watch.log"
$Utf8 = New-Object System.Text.UTF8Encoding($false)

function Move-LogFile {
    # Rotation: sync-watch.log -> .1.log -> .2.log ...; die aelteste faellt weg.
    $oldest = Join-Path $LogDir ("sync-watch." + $LogFiles + ".log")
    if (Test-Path -LiteralPath $oldest) {
        Remove-Item -LiteralPath $oldest -Force
    }
    for ($i = $LogFiles - 1; $i -ge 1; $i--) {
        $from = Join-Path $LogDir ("sync-watch." + $i + ".log")
        if (Test-Path -LiteralPath $from) {
            Move-Item -LiteralPath $from -Destination (Join-Path $LogDir ("sync-watch." + ($i + 1) + ".log")) -Force
        }
    }
    if ($LogFiles -ge 1) {
        Move-Item -LiteralPath $LogPath -Destination (Join-Path $LogDir "sync-watch.1.log") -Force
    } else {
        Remove-Item -LiteralPath $LogPath -Force
    }
}

function Write-Log {
    param([string]$Text)
    try {
        if (Test-Path -LiteralPath $LogPath -PathType Leaf) {
            if ((Get-Item -LiteralPath $LogPath).Length -ge $MaxLogBytes) {
                Move-LogFile
            }
        }
        $line = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss") + "  " + $Text + "`r`n"
        [System.IO.File]::AppendAllText($LogPath, $line, $Utf8)
    } catch {
        # Ein voller Datentraeger oder eine gesperrte Datei haelt den
        # Dauerlaeufer nicht an.
        Write-Warning ("log not written: " + $_.Exception.Message)
    }
    Write-Output $Text
}

function Get-QuotedArgument {
    param([string]$Value)
    if ($Value -match '[\s;&]') {
        $trail = $Value.Length - $Value.TrimEnd('\').Length
        return '"' + $Value + ('\' * $trail) + '"'
    }
    return $Value
}

if (-not (Test-Path -LiteralPath $ExePath -PathType Leaf)) {
    Write-Log ("postkauz.exe not found: " + $ExePath + "; stopping (register the task again with the right -ExePath)")
    exit 1
}

# postkauz.exe schreibt UTF-8; ohne diese Einstellung kaemen Umlaute in
# Ordnernamen im Log falsch an. Ohne Konsole kann das scheitern.
try {
    [Console]::OutputEncoding = $Utf8
} catch {
    Write-Verbose "console encoding unchanged"
}

$syncArgs = @("sync", "--watch", "--all", "--data-dir", $DataDir)
if ($ConfigFile -ne "") {
    $syncArgs += @("--config", $ConfigFile)
}
$commandText = (Get-QuotedArgument -Value $ExePath) + " " + (($syncArgs | ForEach-Object { Get-QuotedArgument -Value $_ }) -join " ")

$wait = $FirstWaitSeconds
$runs = 0
Write-Log ("Run-SyncWatch started (process " + $PID + "); log " + $LogPath)
while ($true) {
    $runs++
    Write-Log ("starting: " + $commandText)
    $started = Get-Date
    $code = -1
    # Die Fehlerausgabe (stderr) des Programms kommt ueber 2>&1 als
    # ErrorRecord; mit "Stop" braeche PowerShell 5.1 daran ab.
    $ErrorActionPreference = "Continue"
    try {
        & $ExePath @syncArgs 2>&1 | ForEach-Object { Write-Log ("  " + [string]$_) }
        if ($null -ne $LASTEXITCODE) {
            $code = $LASTEXITCODE
        }
    } catch {
        Write-Log ("could not run postkauz.exe: " + $_.Exception.Message)
    }
    $ErrorActionPreference = "Stop"
    $ranSeconds = [int]((Get-Date) - $started).TotalSeconds
    if ($ranSeconds -ge $StableSeconds) {
        $wait = $FirstWaitSeconds
    }
    if ($code -eq $ExitAuth) {
        $thisWait = $MaxWaitSeconds
        Write-Log ("postkauz.exe ended with exit code " + $code + " after " + $ranSeconds + " s: the mail server rejected the login " +
            "(or an OAuth2 token could not be renewed). Not retrying quickly, to avoid locking the account; next try in " +
            $thisWait + " s. Fix it with: postkauz.exe account set <name> --password-stdin, or account login <name>.")
    } else {
        $thisWait = $wait
        Write-Log ("postkauz.exe ended with exit code " + $code + " after " + $ranSeconds + " s; restarting in " + $thisWait + " s")
        $wait = [Math]::Min($wait * 2, $MaxWaitSeconds)
    }
    if ($MaxRuns -gt 0 -and $runs -ge $MaxRuns) {
        Write-Log ("stopping after " + $runs + " runs (-MaxRuns)")
        exit 0
    }
    Start-Sleep -Seconds $thisWait
}
