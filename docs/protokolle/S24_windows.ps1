$ErrorActionPreference = 'Continue'
$base = 'C:\Users\Admin\AI\Cowork_Temp\PostKauz\s24'
Set-Location $base
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$log = Join-Path $base 's24_run.log'
if (Test-Path $log) { Remove-Item $log -Force }
$exe = Join-Path $base 'postkauz-s24.exe'
$d = 'C:\Claude\postkauz-s24probe'
$env:POSTKAUZ_DATA_DIR = $d
$env:POSTKAUZ_CONFIG = Join-Path $base 'cfg\config.toml'
function Write-Log { param($Text) Add-Content -LiteralPath $log -Value $Text -Encoding UTF8 }
function Invoke-Step { param($Label, [scriptblock]$Block)
  Write-Log ''; Write-Log ('> ' + $Label)
  $sw = [Diagnostics.Stopwatch]::StartNew()
  $out = (& $Block 2>&1 | Out-String -Width 400)
  $code = $LASTEXITCODE
  Write-Log $out.TrimEnd(); Write-Log ('[exit ' + $code + ', ' + [int]$sw.Elapsed.TotalSeconds + ' s]')
  return $out
}
function Set-Policy { param($Name) $env:POSTKAUZ_POLICY = Join-Path $base $Name }
function Get-NewPowerShell { param($Since) @(Get-Process powershell -ErrorAction SilentlyContinue | Where-Object { $_.StartTime -gt $Since }).Count }
Write-Log ('S2.4-Probe gestartet ' + (Get-Date -Format 'dd.MM.yyyy HH:mm:ss'))
if (Test-Path $d) { Write-Log 'Probe-Ordner existiert schon -> Abbruch'; Write-Log 'ENDE'; exit 1 }
Expand-Archive 'C:\PostKauz\abnahme-d2-daten.zip' -DestinationPath $d
$stale = Join-Path $env:TEMP 'postkauz-hook-a7probe-alt'
New-Item -ItemType Directory -Force -Path $stale | Out-Null
(Get-Item $stale).LastWriteTime = (Get-Date).AddHours(-2)
Set-Policy 'pass.toml'
Invoke-Step '01 version' { & $exe version } | Out-Null
Invoke-Step '02 accounts list (Migration D2 -> Schema 14)' { & $exe accounts list } | Out-Null
Invoke-Step '03 doctor (erwartet Schema 14, Policy-Datei pass.toml geladen)' { & $exe doctor } | Out-Null
$sr = Invoke-Step '04 search Rechnung --limit 1 --json' { & $exe search 'Rechnung' -a demo --limit 1 --json }
$mid = $null
foreach ($line in ($sr -split "`n")) { $t = $line.Trim(); if ($t.StartsWith('{')) { try { $o = $t | ConvertFrom-Json; if ($o.PSObject.Properties['id']) { $mid = $o.id; break } } catch {} } }
Write-Log ('Nachricht: id ' + $mid)
Invoke-Step '05 identity add' { & $exe identity add -a demo --email demo@example.org --name 'Demo Buero' --default } | Out-Null
Invoke-Step '06 draft reply mit Anrede (erwartet deutsche Zitatzeile "Am ... schrieb ...:")' { & $exe draft reply $mid --body-file (Join-Path $base 'antwort_ok.md') } | Out-Null
$t0 = Get-Date
Invoke-Step '07 draft request 1 mit Beispiel-Hook (Pruefpunkt 1: PowerShell 5.1 mit knapper Umgebung; erwartet hook: pass)' { & $exe draft request 1 } | Out-Null
Invoke-Step '07b Temp-Ordner postkauz-hook-* danach (erwartet keiner; alter Probe-Ordner ebenfalls weg)' { @(Get-ChildItem $env:TEMP -Directory -Filter 'postkauz-hook-*' -ErrorAction SilentlyContinue).Count } | Out-Null
Invoke-Step '08 draft edit 1 --subject TODO' { & $exe draft edit 1 --subject 'TODO Re: Rechnung' } | Out-Null
Invoke-Step '09 draft request 1 (erwartet hook: fail, subject contains TODO, Entwurf bleibt draft)' { & $exe draft request 1 } | Out-Null
Invoke-Step '10 draft reply ohne Anrede + request (erwartet hook: fail, Anrede fehlt)' { & $exe draft reply $mid --body-file (Join-Path $base 'antwort_ohne.md') | Out-Null; & $exe draft request 2 } | Out-Null
Set-Policy 'umlaut.toml'
Invoke-Step '11 Umlaut-Hook (Pruefpunkt 2: erwartet "Anrede fehlt: Gruesse" mit echten Umlauten aeoeuess)' { & $exe draft request 2 } | Out-Null
Set-Policy 'timeout.toml'
$t1 = Get-Date
Invoke-Step '12 Zeitueberschreitung 3 s (erwartet hook: timeout nach ca. 3 s)' { & $exe draft request 2 } | Out-Null
Start-Sleep -Seconds 1
Invoke-Step '12b neue powershell-Prozesse seit 12 (erwartet 0)' { Get-NewPowerShell -Since $t1 } | Out-Null
Set-Policy 'child.toml'
$t2 = Get-Date
Invoke-Step '13 Hook startet Kindprozess und endet normal (Pruefpunkt 3; erwartet hook: pass)' { & $exe draft request 2 } | Out-Null
Start-Sleep -Seconds 2
Invoke-Step '13b neue powershell-Prozesse seit 13 (erwartet 0: Kind mit beendet)' { Get-NewPowerShell -Since $t2 } | Out-Null
Set-Policy 'missing.toml'
Invoke-Step '14 Programm fehlt (erwartet hook: error, ohne Pfad)' { & $exe draft request 2 } | Out-Null
Set-Policy 'bat.toml'
Invoke-Step '15 lint.bat. (Pruefpunkt 5; erwartet hook: error)' { & $exe draft request 2 } | Out-Null
Set-Policy 'batsp.toml'
Invoke-Step '16 lint.bat mit Leerzeichen (erwartet hook: error)' { & $exe draft request 2 } | Out-Null
Set-Policy 'pass.toml'
Invoke-Step '17 Audit: Hook-Ausgabe und Betreff (erwartet 0)' { $a = & $exe audit --since 30m --json | Out-String; ([regex]::Matches($a, 'subject contains|Anrede|Rechnung|TODO')).Count } | Out-Null
Invoke-Step '18 Log: Hook-Ausgabe (erwartet 0)' { $l = Get-Content (Join-Path $d 'postkauz.log') -Raw; ([regex]::Matches($l, 'subject contains|Anrede|TODO')).Count } | Out-Null
Invoke-Step '19 draft list' { & $exe draft list } | Out-Null
Invoke-Step '20 Temp-Ordner am Ende (erwartet 0)' { @(Get-ChildItem $env:TEMP -Directory -Filter 'postkauz-hook-*' -ErrorAction SilentlyContinue).Count } | Out-Null
Write-Log ('S2.4-Probe beendet ' + (Get-Date -Format 'dd.MM.yyyy HH:mm:ss'))
Write-Log 'ENDE'
