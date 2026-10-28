<#
.SYNOPSIS
    Registers a Windows scheduled task that keeps "postkauz.exe sync --watch
    --all" running while you are logged on.

.DESCRIPTION
    PostKauz mirrors your mailboxes only while a sync runs. This script
    registers a scheduled task for the current user that starts at logon and
    runs Run-SyncWatch.ps1 (next to this script). Run-SyncWatch.ps1 runs

        postkauz.exe sync --watch --all --data-dir <DataDir> [--config <ConfigFile>]

    one process for all accounts, restarts it after a crash or an end (wait 60
    seconds, doubled up to 30 minutes; after exit code 6, a rejected login,
    always the longest wait) and writes its output to
    <DataDir>\logs\sync-watch.log (rotated at 10 MB, 5 old files). With
    --all a rejected login does not end the program: it tries that account
    again every 30 minutes, and the other accounts go on.

    No password is stored in the task or in the scripts: postkauz.exe reads
    the passwords (or OAuth2 tokens) from the Windows Credential Manager, as
    when you run it in a PowerShell window. Accounts whose password is only
    in an environment variable (POSTKAUZ_PASSWORD_<NAME>) work only if that
    variable is set for your user, not only in one window.

    The task gets the data directory and the configuration file that
    "postkauz.exe doctor" shows right now (like Register-AuditExport.ps1).
    The script stops without registering anything if
    - Windows redirects the data directory or the configuration file into
      the folder of an app package (for example when PostKauz was started from
      the Claude desktop app and the folder is under %LOCALAPPDATA% or
      %APPDATA%): the Task Scheduler would see a different folder. Use a data
      directory outside these folders, for example -DataDir C:\PostKauz\data;
    - the data directory has no database yet, or no account.

    The task runs only while you are logged on (network drives, Credential
    Manager). By default it runs without a console window
    (conhost.exe --headless). Only one copy runs at a time.

    After registering, the script starts the task once and checks that it is
    running and writing its log (unless -NoStart). It then watches the log
    for another 15 seconds: if postkauz.exe ended in that time (for example
    a wrong data directory or a database that needs a migration) or no
    postkauz.exe is running, it warns and ends with exit code 1.

    Stop it: Stop-ScheduledTask -TaskName "PostKauz sync watch". Remove it:
    this script with -Unregister. See docs/betrieb.md, part "Windows".

    Requires Windows PowerShell 5.1 or later and no administrator rights.

.PARAMETER ExePath
    Full path to postkauz.exe. Default: postkauz.exe next to this script,
    otherwise C:\PostKauz\postkauz.exe.

.PARAMETER DataDir
    PostKauz data directory (--data-dir). Default: the data directory that
    "postkauz.exe doctor" shows. Together with -ProfileName the task uses
    <DataDir>\profiles\<ProfileName>.

.PARAMETER ConfigFile
    PostKauz configuration file (--config). Default: the one that
    "postkauz.exe doctor" shows, if it exists.

.PARAMETER TaskName
    Name of the scheduled task.

.PARAMETER ProfileName
    PostKauz profile (--profile), if you use one.

.PARAMETER WrapperPath
    Full path to Run-SyncWatch.ps1. Default: next to this script.

.PARAMETER LogDir
    Folder for the log files. Default: <DataDir>\logs.

.PARAMETER ShowWindow
    Start PowerShell with a minimized window instead of the hidden console.
    Use this if the check after registering fails with the hidden window.

.PARAMETER NoStart
    Do not start the task after registering (it starts at the next logon).

.PARAMETER Unregister
    Stop and remove the scheduled task (data and logs stay).

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\Register-SyncWatch.ps1 -DataDir "C:\PostKauz\data"

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\Register-SyncWatch.ps1 -DataDir "C:\PostKauz\data" -ConfigFile "C:\PostKauz\config.toml"

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\Register-SyncWatch.ps1 -Unregister
#>
[CmdletBinding()]
param(
    [string]$ExePath = "",
    [string]$DataDir = "",
    [string]$ConfigFile = "",
    [string]$TaskName = "PostKauz sync watch",
    [string]$ProfileName = "",
    [string]$WrapperPath = "",
    [string]$LogDir = "",
    [switch]$ShowWindow,
    [switch]$NoStart,
    [switch]$Unregister
)

# Hinweis zum Format dieser Datei: UTF-8 mit BOM, Inhalt nur ASCII (Windows
# PowerShell 5.1 liest Dateien ohne BOM als ANSI). Keine Aliase, nur
# Cmdlet-Namen (Verb-Noun). Regeln wie Register-AuditExport.ps1.
Set-StrictMode -Version 2.0
$ErrorActionPreference = "Stop"

function Write-Step {
    param([string]$Text)
    Write-Output ("== " + $Text)
}

function Get-QuotedArgument {
    param([string]$Value)
    # Argument fuer die Kommandozeile der Aufgabe; Anfuehrungszeichen im Pfad
    # sind unter Windows nicht erlaubt, deshalb reicht einfaches Einschliessen.
    # Abschliessende Backslashes vor dem schliessenden Anfuehrungszeichen
    # muessen verdoppelt werden (sonst wird \" zu einem Zeichen im Wert).
    if ($Value -match '[\s;&]') {
        $trail = $Value.Length - $Value.TrimEnd('\').Length
        return '"' + $Value + ('\' * $trail) + '"'
    }
    return $Value
}

function Get-TrimmedPath {
    param([string]$Value)
    # Abschliessenden Backslash entfernen, ausser bei einer Laufwerkswurzel.
    if ($Value -match '^[A-Za-z]:\\$') {
        return $Value
    }
    return $Value.TrimEnd('\')
}

# Read-LogFrom liest eine Logdatei ab Byte From (UTF-8; mit Max hoechstens
# so viele Bytes). Run-SyncWatch.ps1
# schreibt und rotiert weiter, deshalb FileShare ReadWrite und Delete; geht
# das Lesen einmal nicht, gilt der Teil als leer (naechster Versuch 1 s
# spaeter).
function Read-LogFrom {
    param([string]$Path, [long]$From, [int]$Max = 0)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return ""
    }
    try {
        $share = [System.IO.FileShare]::ReadWrite -bor [System.IO.FileShare]::Delete
        $stream = [System.IO.File]::Open($Path, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, $share)
    } catch {
        return ""
    }
    try {
        if ($stream.Length -lt $From) {
            $From = 0
        }
        [void]$stream.Seek($From, [System.IO.SeekOrigin]::Begin)
        if ($Max -gt 0) {
            $buf = New-Object byte[] $Max
            $n = $stream.Read($buf, 0, $Max)
            return [System.Text.Encoding]::UTF8.GetString($buf, 0, $n)
        }
        $reader = New-Object System.IO.StreamReader($stream, (New-Object System.Text.UTF8Encoding($false)))
        return $reader.ReadToEnd()
    } finally {
        $stream.Dispose()
    }
}

# Get-LogHead liefert den Anfang einer Logdatei (erste Zeile, hoechstens
# 200 Zeichen; "" ohne Datei). Daran erkennt Get-NewLogPart eine Rotation
# (das Erstelldatum taugt nicht: NTFS uebernimmt es bei Neuanlage unter
# gleichem Namen).
function Get-LogHead {
    param([string]$Path)
    $text = Read-LogFrom -Path $Path -From 0 -Max 4096
    $line = ($text -split "`r?`n")[0]
    if ($line.Length -gt 200) {
        $line = $line.Substring(0, 200)
    }
    return $line
}

# Get-NewLogPart liefert, was seit der Messung (Groesse From, Anfang Head)
# ins Log kam, auch ueber eine Rotation hinweg (Nachpruefung zu fd70f19,
# Punkt 5): Hat die Datei einen anderen Anfang als bei der Messung, liegt
# der alte Inhalt jetzt in sync-watch.1.log; der neue Teil ist dann dessen
# Rest ab From plus die ganze neue Datei.
function Get-NewLogPart {
    param([string]$Path, [long]$From, [string]$Head)
    $current = Get-LogHead -Path $Path
    if ($Head -ne "" -and $current -eq "") {
        # Kopf gerade nicht lesbar oder neue Datei noch leer: in dieser
        # Sekunde nichts Neues (sonst zaehlten alte Zeilen mit).
        return ""
    }
    if ($Head -ne "" -and $current -ne $Head) {
        $rotated = Join-Path (Split-Path -Parent $Path) ([System.IO.Path]::GetFileNameWithoutExtension($Path) + ".1.log")
        $part = ""
        if ((Get-LogHead -Path $rotated) -eq $Head) {
            $part = Read-LogFrom -Path $rotated -From $From
        }
        return $part + (Read-LogFrom -Path $Path -From 0)
    }
    return (Read-LogFrom -Path $Path -From $From)
}

function Get-DoctorReport {
    param([string]$Exe, [string[]]$PathArgs)
    $doctorArgs = @("doctor", "--json") + $PathArgs
    $raw = & $Exe @doctorArgs
    if ($null -eq $raw) {
        throw "postkauz.exe doctor --json printed nothing"
    }
    return (($raw | Out-String) | ConvertFrom-Json)
}

function Get-ReportValue {
    param($Report, [string]$Name)
    # Felder ohne Wert fehlen in der JSON-Ausgabe (omitempty); unter
    # StrictMode waere der Zugriff darauf ein Fehler.
    $property = $Report.PSObject.Properties[$Name]
    if ($null -eq $property -or $null -eq $property.Value) {
        return ""
    }
    return [string]$property.Value
}

if ($Unregister) {
    $existing = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($null -eq $existing) {
        Write-Step ("Task '" + $TaskName + "' does not exist; nothing to do.")
        exit 0
    }
    if ($existing.State -eq "Running") {
        Stop-ScheduledTask -TaskName $TaskName
    }
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
    Write-Step ("Task '" + $TaskName + "' stopped and removed. Data and logs stay where they are.")
    $left = Get-Process -Name "postkauz" -ErrorAction SilentlyContinue
    if ($null -ne $left) {
        Write-Warning "postkauz.exe is still running (maybe started by hand or by the Claude app); check with: Get-Process postkauz"
    }
    exit 0
}

# 1. Programm und Hilfsskript finden
if ($ExePath -eq "") {
    $candidate = Join-Path $PSScriptRoot "postkauz.exe"
    if (Test-Path -LiteralPath $candidate -PathType Leaf) {
        $ExePath = $candidate
    } else {
        $ExePath = "C:\PostKauz\postkauz.exe"
    }
}
if (-not (Test-Path -LiteralPath $ExePath -PathType Leaf)) {
    throw ("postkauz.exe not found: " + $ExePath + " (give the full path with -ExePath)")
}
$ExePath = (Resolve-Path -LiteralPath $ExePath).ProviderPath
Write-Step ("Program: " + $ExePath)
if ($WrapperPath -eq "") {
    $WrapperPath = Join-Path $PSScriptRoot "Run-SyncWatch.ps1"
}
if (-not (Test-Path -LiteralPath $WrapperPath -PathType Leaf)) {
    throw ("Run-SyncWatch.ps1 not found: " + $WrapperPath + " (keep it next to this script or give -WrapperPath)")
}
$WrapperPath = (Resolve-Path -LiteralPath $WrapperPath).ProviderPath
Write-Step ("Restart script: " + $WrapperPath)

# 2. Datenverzeichnis und Konfiguration aufloesen (wie
# Register-AuditExport.ps1): Die Aufgabe bekommt genau die Pfade, die doctor
# jetzt zeigt. Leitet Windows sie in einen Paketordner um, abbrechen.
$pathArgs = @()
if ($ProfileName -ne "") {
    $pathArgs += @("--profile", $ProfileName)
}
if ($DataDir -ne "") {
    $DataDir = Get-TrimmedPath -Value $DataDir
    $pathArgs += @("--data-dir", $DataDir)
}
if ($ConfigFile -ne "") {
    if (-not (Test-Path -LiteralPath $ConfigFile -PathType Leaf)) {
        throw ("-ConfigFile: no such file: " + $ConfigFile)
    }
    $ConfigFile = (Resolve-Path -LiteralPath $ConfigFile).ProviderPath
    $pathArgs += @("--config", $ConfigFile)
}
$report = Get-DoctorReport -Exe $ExePath -PathArgs $pathArgs
if ((Get-ReportValue -Report $report -Name "config_error") -ne "") {
    throw ("PostKauz configuration error: " + (Get-ReportValue -Report $report -Name "config_error"))
}
$redirectedData = Get-ReportValue -Report $report -Name "data_dir_real"
$redirectedConfig = Get-ReportValue -Report $report -Name "config_path_real"
if ($redirectedData -ne "" -or $redirectedConfig -ne "") {
    if ($redirectedData -ne "") {
        Write-Warning ("Windows redirects the data directory " + (Get-ReportValue -Report $report -Name "data_dir") + " to " + $redirectedData)
    }
    if ($redirectedConfig -ne "") {
        Write-Warning ("Windows redirects the configuration file " + (Get-ReportValue -Report $report -Name "config_path") + " to " + $redirectedConfig)
    }
    Write-Warning "The Task Scheduler would see a different folder. Use a data directory (and configuration file) outside %LOCALAPPDATA% and %APPDATA%, for example -DataDir C:\PostKauz\data. To keep the existing data, copy the redirected folder there first."
    throw "Not registered: data directory or configuration is redirected (see warnings above)"
}
$shadow = Get-ReportValue -Report $report -Name "data_dir_shadow"
if ($shadow -ne "") {
    Write-Warning ("Another data directory with a database exists in an app package folder and is not seen here: " + $shadow + " (copy it if you want to keep that data)")
}
$resolvedDataDir = Get-ReportValue -Report $report -Name "data_dir"
$resolvedConfig = Get-ReportValue -Report $report -Name "config_path"
if ($resolvedDataDir -eq "" -or $resolvedConfig -eq "") {
    throw "postkauz.exe doctor --json did not report data_dir and config_path (program too old? use 0.3.0-alpha.3 or later)"
}
if ((Get-ReportValue -Report $report -Name "database") -eq "missing") {
    throw ("Not registered: no database in " + $resolvedDataDir + " yet. Add an account first, for example: " + (Get-QuotedArgument -Value $ExePath) + " account add <name> ... --data-dir " + (Get-QuotedArgument -Value $resolvedDataDir))
}
Write-Step ("Data directory: " + $resolvedDataDir)
$configArgs = @()
if ($report.config_exists) {
    $configArgs = @("-ConfigFile", (Get-QuotedArgument -Value $resolvedConfig))
    Write-Step ("Configuration: " + $resolvedConfig)
} else {
    $resolvedConfig = ""
    Write-Step "Configuration: none (defaults)"
}

# 3. Konten pruefen: ohne Konto nichts zu beobachten; fehlende Zugangsdaten
# beenden nur dieses Konto, deshalb hier nur warnen.
$accounts = @()
$property = $report.PSObject.Properties["accounts"]
if ($null -ne $property -and $null -ne $property.Value) {
    $accounts = @($property.Value)
}
if ($accounts.Count -eq 0) {
    throw ("Not registered: no account in " + $resolvedDataDir + ". Add one first with: " + (Get-QuotedArgument -Value $ExePath) + " account add ...")
}
foreach ($account in $accounts) {
    $password = Get-ReportValue -Report $account -Name "password"
    $token = Get-ReportValue -Report $account -Name "token"
    switch ($password) {
        "stored" { Write-Step ("Account " + $account.name + ": password in the Windows Credential Manager") }
        "oauth2" {
            if ($token -eq "stored") {
                Write-Step ("Account " + $account.name + ": OAuth2 token in the Windows Credential Manager")
            } else {
                Write-Warning ("Account " + $account.name + ": no OAuth2 token (" + $token + "); sign in with: postkauz.exe account login " + $account.name + ". Until then only this account is not watched.")
            }
        }
        "environment" {
            Write-Warning ("Account " + $account.name + ": the password comes from the environment variable " + (Get-ReportValue -Report $account -Name "env_var") + ". The task sees it only if it is set for your user (not only in this window); otherwise store it with: postkauz.exe account set " + $account.name + " --password-stdin")
        }
        default {
            Write-Warning ("Account " + $account.name + ": password " + $password + "; store it with: postkauz.exe account set " + $account.name + " --password-stdin. Until then only this account is not watched.")
        }
    }
}

# 4. Aufgabe anlegen (ersetzt eine vorhandene gleichen Namens)
if ($LogDir -eq "") {
    $LogDir = Join-Path $resolvedDataDir "logs"
}
$wrapperArgs = "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File " + (Get-QuotedArgument -Value $WrapperPath) +
    " -ExePath " + (Get-QuotedArgument -Value $ExePath) + " -DataDir " + (Get-QuotedArgument -Value $resolvedDataDir) +
    " -LogDir " + (Get-QuotedArgument -Value $LogDir)
if ($configArgs.Count -gt 0) {
    $wrapperArgs = $wrapperArgs + " " + ($configArgs -join " ")
}
$powershell = Join-Path $env:SystemRoot "System32\WindowsPowerShell\v1.0\powershell.exe"
if ($ShowWindow) {
    $action = New-ScheduledTaskAction -Execute $powershell -Argument ("-WindowStyle Minimized " + $wrapperArgs)
} else {
    # conhost.exe --headless startet das Konsolenprogramm ohne Fenster.
    $action = New-ScheduledTaskAction -Execute "conhost.exe" -Argument ("--headless " + (Get-QuotedArgument -Value $powershell) + " " + $wrapperArgs)
}
$user = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $user
# Ohne Zeitgrenze (Dauerlaeufer); nur eine Instanz; nach einem Absturz des
# Hilfsskripts selbst startet die Aufgabenplanung es bis zu dreimal neu (die
# Neustarts von postkauz.exe macht Run-SyncWatch.ps1).
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable `
    -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 5)
$principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited
$description = "PostKauz: keeps sync --watch --all running while you are logged on (Run-SyncWatch.ps1, restart with backoff). Log: " + (Join-Path $LogDir "sync-watch.log")
Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Settings $settings -Principal $principal -Description $description -Force | Out-Null
Write-Step ("Task '" + $TaskName + "' registered: starts at logon of " + $user)
Write-Step ("Log: " + (Join-Path $LogDir "sync-watch.log"))

if ($NoStart) {
    Write-Step "Not started now (-NoStart); it starts at the next logon, or with: Start-ScheduledTask -TaskName '$TaskName'"
    exit 0
}

# 5. Starten und pruefen: Aufgabe laeuft und das Log bekommt eine neue
# Startzeile von postkauz.exe.
Write-Step "Starting the task to check it ..."
$logPath = Join-Path $LogDir "sync-watch.log"
$existing = Get-ScheduledTask -TaskName $TaskName
if ($existing.State -eq "Running") {
    Write-Step "The task was already running; restarting it with the new settings."
    Stop-ScheduledTask -TaskName $TaskName
    Start-Sleep -Seconds 2
}
# Erst nach dem Stoppen messen: Endzeilen der alten Instanz gehoeren nicht
# zum neuen Teil des Logs.
$before = 0
if (Test-Path -LiteralPath $logPath -PathType Leaf) {
    $before = (Get-Item -LiteralPath $logPath).Length
}
$beforeHead = Get-LogHead -Path $logPath
Start-ScheduledTask -TaskName $TaskName
$deadline = (Get-Date).AddSeconds(30)
$ok = $false
do {
    Start-Sleep -Seconds 2
    $state = (Get-ScheduledTask -TaskName $TaskName).State
    # Nur der neue Teil des Logs zaehlt (rotationsfest; eine alte
    # "starting:"-Zeile frueherer Laeufe zaehlt nicht).
    if ((Get-NewLogPart -Path $logPath -From $before -Head $beforeHead).Contains("starting:")) {
        $ok = ($state -eq "Running")
    }
} while (-not $ok -and (Get-Date) -lt $deadline)
# V6 (Nachbesserung T2): "starting:" schreibt Run-SyncWatch.ps1 vor dem
# Start von postkauz.exe. Ob das Programm laeuft, zeigt erst die Zeit danach:
# 15 s lang darf im neuen Teil des Logs kein "ended with exit code" stehen,
# und am Ende muss ein postkauz.exe mit diesem Pfad laufen.
$ended = $false
$running = $false
if ($ok) {
    Write-Step "Watching the log for 15 seconds ..."
    $settle = (Get-Date).AddSeconds(15)
    do {
        Start-Sleep -Seconds 1
        $ended = (Get-NewLogPart -Path $logPath -From $before -Head $beforeHead).Contains("ended with exit code")
    } while (-not $ended -and (Get-Date) -lt $settle)
    $running = $null -ne (Get-Process -Name "postkauz" -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $ExePath })
}
if ($ok -and -not $ended -and $running) {
    Write-Step ("OK: the task is running, postkauz.exe runs and writes " + $logPath)
    Write-Step ("Look at the log with: Get-Content -LiteralPath " + (Get-QuotedArgument -Value $logPath) + " -Tail 20 -Wait")
    exit 0
}
$info = Get-ScheduledTaskInfo -TaskName $TaskName
if ($ok) {
    if ($ended) {
        Write-Warning "postkauz.exe started but ended within 15 seconds; the task restarts it with a growing wait, but it will most likely end again."
    } else {
        Write-Warning "The task is running, but no postkauz.exe from this path is running."
    }
    Write-Warning "Last lines of the log:"
    (Get-NewLogPart -Path $logPath -From $before -Head $beforeHead) -split "`r?`n" | Where-Object { $_ -ne "" } | Select-Object -Last 10 | ForEach-Object { Write-Warning ("  " + $_) }
} else {
    Write-Warning ("The task did not start as expected (state " + (Get-ScheduledTask -TaskName $TaskName).State + ", last result " + $info.LastTaskResult + ").")
}
Write-Warning "Try the restart script by hand to see the error message (Ctrl+C stops it):"
Write-Warning ("  powershell -ExecutionPolicy Bypass -File " + (Get-QuotedArgument -Value $WrapperPath) + " -ExePath " + (Get-QuotedArgument -Value $ExePath) + " -DataDir " + (Get-QuotedArgument -Value $resolvedDataDir) + " -MaxRuns 1")
if (-not $ShowWindow) {
    Write-Warning "If that works, register again with -ShowWindow (the hidden console may be blocked on this PC)."
}
exit 1
