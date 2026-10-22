<#
.SYNOPSIS
    Registers a daily Windows scheduled task that runs "postkauz.exe audit export".

.DESCRIPTION
    The audit log of PostKauz is append-only. Its real tamper protection is a
    daily export with checksums (SHA-256 hash chain and manifest) to a second
    location, for example a Google Drive folder or another disk. This script
    registers a scheduled task for the current user that runs

        postkauz.exe audit export --quiet [--to <ExportDir>] [--profile <Profile>]

    once a day. Missed runs (PC switched off) are started as soon as possible.

    The task gets the data directory and the configuration file that
    "postkauz.exe doctor" shows right now, as --data-dir and --config. So it
    works on exactly the same data as your PowerShell window, even if
    POSTKAUZ_DATA_DIR is only set in that window. If Windows redirects the
    data directory into the folder of an app package (this happens when
    PostKauz is started from a packaged app such as the Claude desktop app
    and the data directory is under %LOCALAPPDATA% or %APPDATA%), the script
    stops: the Task Scheduler would see a different, empty folder. Use a data
    directory outside these folders, for example with -DataDir C:\PostKauz\data.
    The task runs only while you are logged on, so that drive letters of
    Google Drive and network shares are available. By default it runs without
    a console window (conhost.exe --headless).

    After registering, the script starts the task once and checks with
    "postkauz.exe doctor --json" that the export really happened.

    Without -ExportDir the export goes to [audit] export_dir from the
    PostKauz configuration file (see "postkauz.exe doctor", line "audit").

    Requires Windows PowerShell 5.1 or later and no administrator rights.

.PARAMETER ExePath
    Full path to postkauz.exe. Default: postkauz.exe next to this script,
    otherwise C:\PostKauz\postkauz.exe.

.PARAMETER DataDir
    PostKauz data directory (--data-dir). Default: the data directory that
    "postkauz.exe doctor" shows (POSTKAUZ_DATA_DIR, data_dir in the
    configuration, otherwise %LOCALAPPDATA%\postkauz).

.PARAMETER ExportDir
    Folder for the export files (a second location, not the data directory).
    Default: [audit] export_dir from the configuration.

.PARAMETER At
    Time of day for the daily run (24-hour clock, for example 12:30).

.PARAMETER TaskName
    Name of the scheduled task.

.PARAMETER ProfileName
    PostKauz profile (--profile), if you use one.

.PARAMETER ShowWindow
    Start postkauz.exe directly (a console window appears briefly). Use this
    if the check after registering fails with the hidden window.

.PARAMETER NoTest
    Do not start the task once after registering.

.PARAMETER Unregister
    Remove the scheduled task again (the export files stay).

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\Register-AuditExport.ps1 -ExportDir "G:\My Drive\PostKauz-Audit"

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\Register-AuditExport.ps1 -DataDir "C:\PostKauz\data" -ExportDir "G:\My Drive\PostKauz-Audit"

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\Register-AuditExport.ps1 -Unregister
#>
[CmdletBinding()]
param(
    [string]$ExePath = "",
    [string]$DataDir = "",
    [string]$ExportDir = "",
    [string]$At = "12:30",
    [string]$TaskName = "PostKauz audit export",
    [string]$ProfileName = "",
    [switch]$ShowWindow,
    [switch]$NoTest,
    [switch]$Unregister
)

# Hinweis zum Format dieser Datei: UTF-8 mit BOM, Inhalt nur ASCII (Windows
# PowerShell 5.1 liest Dateien ohne BOM als ANSI). Keine Aliase, nur
# Cmdlet-Namen (Verb-Noun).
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
    if ($Value -match '[\s;&]') {
        return '"' + $Value + '"'
    }
    return $Value
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
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
    Write-Step ("Task '" + $TaskName + "' removed. The export files stay where they are.")
    exit 0
}

# 1. Programm finden
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

# 2. Uhrzeit pruefen
$time = [datetime]::MinValue
if (-not [datetime]::TryParseExact($At, "HH:mm", [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::None, [ref]$time)) {
    throw ("-At must be a time like 12:30, not '" + $At + "'")
}

# 3. Datenverzeichnis und Konfiguration aufloesen (A7, Befund D3.3c): Die
# Aufgabe bekommt genau die Pfade, die doctor jetzt zeigt, als --data-dir und
# --config. Leitet Windows sie in einen Paketordner um (MSIX), sieht die
# Aufgabenplanung einen anderen Ordner: dann abbrechen.
$pathArgs = @()
if ($ProfileName -ne "") {
    $pathArgs += @("--profile", $ProfileName)
}
if ($DataDir -ne "") {
    $pathArgs += @("--data-dir", $DataDir)
}
$report = Get-DoctorReport -Exe $ExePath -PathArgs $pathArgs
if ((Get-ReportValue -Report $report -Name "config_error") -ne "") {
    throw ("PostKauz configuration error: " + (Get-ReportValue -Report $report -Name "config_error"))
}
$redirectedData = Get-ReportValue -Report $report -Name "data_dir_real"
$redirectedConfig = Get-ReportValue -Report $report -Name "config_path_real"
$appPackage = Get-ReportValue -Report $report -Name "app_package"
if ($redirectedData -ne "" -or $redirectedConfig -ne "" -or $appPackage -ne "") {
    if ($redirectedData -eq "" -and $redirectedConfig -eq "") {
        Write-Warning ("This window runs inside the app package " + $appPackage + "; a new data directory " + (Get-ReportValue -Report $report -Name "data_dir") + " would be redirected into the package folder.")
    }
    if ($redirectedData -ne "") {
        Write-Warning ("Windows redirects the data directory " + (Get-ReportValue -Report $report -Name "data_dir") + " to " + $redirectedData)
    }
    if ($redirectedConfig -ne "") {
        Write-Warning ("Windows redirects the configuration file " + (Get-ReportValue -Report $report -Name "config_path") + " to " + $redirectedConfig)
    }
    Write-Warning "The Task Scheduler would see a different folder. Use a data directory (and configuration file) outside %LOCALAPPDATA% and %APPDATA%, for example -DataDir C:\PostKauz\data, or run this script from a normal PowerShell window."
    throw "Not registered: data directory or configuration is redirected (see warnings above)"
}
$resolvedDataDir = Get-ReportValue -Report $report -Name "data_dir"
$resolvedConfig = Get-ReportValue -Report $report -Name "config_path"
if ($resolvedDataDir -eq "" -or $resolvedConfig -eq "") {
    throw "postkauz.exe doctor --json did not report data_dir and config_path (program too old? use 0.1.0-rc2 or later)"
}
# Ab hier immer mit den aufgeloesten Pfaden, ohne --profile (es steckt schon
# in den Pfaden).
$pathArgs = @("--data-dir", $resolvedDataDir, "--config", $resolvedConfig)
Write-Step ("Data directory: " + $resolvedDataDir)
if ($report.config_exists) {
    Write-Step ("Configuration: " + $resolvedConfig)
} else {
    Write-Step ("Configuration: " + $resolvedConfig + " (does not exist; defaults)")
}

# 4. Exportziel pruefen: Parameter oder [audit] export_dir
if ($ExportDir -ne "") {
    if (-not (Test-Path -LiteralPath $ExportDir -PathType Container)) {
        New-Item -ItemType Directory -Path $ExportDir -Force | Out-Null
    }
    $ExportDir = (Resolve-Path -LiteralPath $ExportDir).ProviderPath
    if ($ExportDir.TrimEnd("\").ToLowerInvariant() -eq $resolvedDataDir.TrimEnd("\").ToLowerInvariant()) {
        throw "-ExportDir must be a second location, not the PostKauz data directory"
    }
    Write-Step ("Export folder: " + $ExportDir)
} elseif ((Get-ReportValue -Report $report -Name "audit_export_dir") -ne "") {
    Write-Step ("Export folder from the configuration: " + (Get-ReportValue -Report $report -Name "audit_export_dir"))
} else {
    throw "No export folder: give -ExportDir or set [audit] export_dir in the PostKauz configuration"
}

# 5. Aufgabe anlegen (ersetzt eine vorhandene gleichen Namens)
$exportArgs = "audit export --quiet --data-dir " + (Get-QuotedArgument -Value $resolvedDataDir) + " --config " + (Get-QuotedArgument -Value $resolvedConfig)
if ($ExportDir -ne "") {
    $exportArgs = $exportArgs + " --to " + (Get-QuotedArgument -Value $ExportDir)
}
if ($ShowWindow) {
    $action = New-ScheduledTaskAction -Execute $ExePath -Argument $exportArgs
} else {
    # conhost.exe --headless startet das Konsolenprogramm ohne Fenster.
    $action = New-ScheduledTaskAction -Execute "conhost.exe" -Argument ("--headless " + (Get-QuotedArgument -Value $ExePath) + " " + $exportArgs)
}
$trigger = New-ScheduledTaskTrigger -Daily -At $time
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Minutes 30) -MultipleInstances IgnoreNew
$user = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited
$description = "PostKauz: daily export of the audit log with checksums to a second location (audit export). Check with: postkauz.exe audit verify"
Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Settings $settings -Principal $principal -Description $description -Force | Out-Null
Write-Step ("Task '" + $TaskName + "' registered: daily at " + $At + " for " + $user + " (missed runs start as soon as possible)")

if ($NoTest) {
    exit 0
}

# 6. Einmal starten und pruefen, dass der Export wirklich lief
Write-Step "Starting the task once to check it ..."
$startLocal = (Get-Date).AddSeconds(-5)
$before = $startLocal.ToUniversalTime()
Start-ScheduledTask -TaskName $TaskName
# Warten, bis der Lauf begonnen (LastRunTime) und geendet hat (nicht mehr
# Running); hoechstens zwei Minuten.
$deadline = (Get-Date).AddSeconds(120)
do {
    Start-Sleep -Seconds 1
    $state = (Get-ScheduledTask -TaskName $TaskName).State
    $info = Get-ScheduledTaskInfo -TaskName $TaskName
    $started = ($info.LastRunTime -ge $startLocal)
} while ((-not $started -or $state -eq "Running") -and (Get-Date) -lt $deadline)
$report = Get-DoctorReport -Exe $ExePath -PathArgs $pathArgs
$last = [datetime]::MinValue
$ok = $false
$lastText = Get-ReportValue -Report $report -Name "audit_last_export"
if ($lastText -ne "") {
    $last = [datetime]::Parse($lastText, [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::AdjustToUniversal)
    $ok = ($last -ge $before)
}
if ($ok) {
    Write-Step ("OK: export written to " + (Get-ReportValue -Report $report -Name "audit_export_location") + " (task result " + $info.LastTaskResult + ")")
    Write-Step ("Check it any time with: " + (Get-QuotedArgument -Value $ExePath) + " audit verify --data-dir " + (Get-QuotedArgument -Value $resolvedDataDir) + " --config " + (Get-QuotedArgument -Value $resolvedConfig))
    exit 0
}
Write-Warning ("The task ran (result " + $info.LastTaskResult + ") but no new export was found.")
Write-Warning "Run the export by hand to see the error message:"
Write-Warning ("  " + (Get-QuotedArgument -Value $ExePath) + " " + $exportArgs.Replace(" --quiet", ""))
if (-not $ShowWindow) {
    Write-Warning "If that works, register again with -ShowWindow (the hidden console may be blocked on this PC)."
}
exit 1
