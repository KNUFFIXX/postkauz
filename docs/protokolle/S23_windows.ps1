$ErrorActionPreference = 'Continue'
$base = 'C:\Users\Admin\AI\Cowork_Temp\PostKauz\s23'
Set-Location $base
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$log = Join-Path $base 's23_run.log'
if (Test-Path $log) { Remove-Item $log -Force }
$exe = Join-Path $base 'postkauz-s23.exe'
$d = 'C:\Claude\postkauz-s23probe'
$env:POSTKAUZ_DATA_DIR = $d
$env:POSTKAUZ_CONFIG = 'C:\Claude\postkauz-s23probe-config\config.toml'
Remove-Item Env:\POSTKAUZ_POLICY -ErrorAction SilentlyContinue
function Write-Log { param($Text) Add-Content -LiteralPath $log -Value $Text -Encoding UTF8 }
function Invoke-Step { param($Label, [scriptblock]$Block)
  Write-Log ''; Write-Log ('> ' + $Label)
  $out = (& $Block 2>&1 | Out-String -Width 400)
  $code = $LASTEXITCODE
  Write-Log $out.TrimEnd(); Write-Log ('[exit ' + $code + ']')
  return $out
}
Write-Log ('S2.3-Probe gestartet ' + (Get-Date -Format 'dd.MM.yyyy HH:mm:ss'))
if (Test-Path $d) { Write-Log 'Probe-Ordner existiert schon -> Abbruch'; Write-Log 'ENDE'; exit 1 }
Expand-Archive 'C:\PostKauz\abnahme-d2-daten.zip' -DestinationPath $d
New-Item -ItemType Directory -Force -Path (Join-Path $d 'attach') | Out-Null
Copy-Item (Join-Path $base 'antwort.md') (Join-Path $d 'attach\notiz.txt')
Invoke-Step '01 version' { & $exe version } | Out-Null
Invoke-Step '02 accounts list (migriert D2-Bestand auf Schema 12)' { & $exe accounts list } | Out-Null
Invoke-Step '03 doctor (erwartet schema 12, attach_dir, keine Policy-Datei)' { & $exe doctor } | Out-Null
$sr = Invoke-Step '04 search Rechnung --limit 1 --json' { & $exe search 'Rechnung' -a demo --limit 1 --json }
$mid = $null
foreach ($line in ($sr -split "`n")) { $t = $line.Trim(); if ($t.StartsWith('{')) { try { $o = $t | ConvertFrom-Json; if ($o.PSObject.Properties['id']) { $mid = $o.id; break } } catch {} } }
Write-Log ('Nachricht fuer die Antwort: id ' + $mid)
Invoke-Step '05 identity add (erwartet ok)' { & $exe identity add -a demo --email demo@example.org --name 'Demo Buero' --signature-file (Join-Path $base 'sig.txt') --default } | Out-Null
Invoke-Step '06 draft reply (erwartet: draft 1 created, status draft, remote pending)' { & $exe draft reply $mid --body-file (Join-Path $base 'antwort.md') } | Out-Null
Invoke-Step '07 draft show 1 (erwartet Re:, Zitat, Signatur)' { & $exe draft show 1 } | Out-Null
Invoke-Step '08 draft request 1 (erwartet hook pass, pending_approval, approval 1)' { & $exe draft request 1 } | Out-Null
Invoke-Step '09 approve 1 (erwartet ready)' { & $exe approve 1 } | Out-Null
Invoke-Step '10 draft edit 1 --subject TODO (erwartet Status draft, Freigabe superseded)' { & $exe draft edit 1 --subject 'TODO Re: Rechnung' } | Out-Null
Invoke-Step '11 approve 1 nach Aenderung (erwartet denied approval_stale oder approval_decided, Exit 3)' { & $exe approve 1 } | Out-Null
Invoke-Step '12 key create test-agent reader' { & $exe key create --name test-agent --role reader } | Out-Null
Invoke-Step '13 policy show --key test-agent (erwartet Liste human_only mit drafts.approve)' { & $exe policy show --key test-agent } | Out-Null
Invoke-Step '14 draft request 1 (erwartet approval 2)' { & $exe draft request 1 } | Out-Null
Invoke-Step '15 approve 2 --key test-agent (erwartet denied human_only, Exit 3)' { & $exe approve 2 --key test-agent } | Out-Null
Invoke-Step '16 draft list --json (erwartet Entwurf 1 pending_approval)' { & $exe draft list --json } | Out-Null
Invoke-Step '17 reject 2 --reason (erwartet ok, Entwurf draft)' { & $exe reject 2 --reason 'zu knapp' } | Out-Null
Invoke-Step '18 draft new --dry-run (erwartet Vorschau, nichts angelegt)' { & $exe draft new -a demo --to 'a@example.org' --subject 'Probe' --body-file (Join-Path $base 'anders.md') --dry-run } | Out-Null
Invoke-Step '19 draft list (erwartet nur Entwurf 1)' { & $exe draft list } | Out-Null
Invoke-Step '20 draft new mit verstecktem Text (erwartet denied hidden_text_in_draft, Exit 3)' { & $exe draft new -a demo --to 'a@example.org' --subject 'Versteckt' --html-file (Join-Path $base 'versteckt.html') } | Out-Null
Invoke-Step '21 draft new mit Anhang ausserhalb attach_dir (erwartet denied attach_outside_dir, Exit 3)' { & $exe draft new -a demo --to 'a@example.org' --subject 'Anhang' --body-file (Join-Path $base 'anders.md') --attach 'C:\Windows\win.ini' } | Out-Null
Invoke-Step '22 draft new mit Anhang in attach_dir (erwartet created)' { & $exe draft new -a demo --to 'a@example.org' --subject 'Anhang ok' --body-file (Join-Path $base 'anders.md') --attach 'notiz.txt' } | Out-Null
Invoke-Step '23 draft abandon 1 (erwartet abandoned)' { & $exe draft abandon 1 } | Out-Null
Invoke-Step '24 draft edit 1 nach abandon (erwartet denied draft_closed, Exit 3)' { & $exe draft edit 1 --subject 'x' } | Out-Null
Invoke-Step '25 draft edit 2 --key test-agent (erwartet denied, reader hat kein mail:draft)' { & $exe draft edit 2 --subject 'y' --key test-agent } | Out-Null
$au = Invoke-Step '26 audit --since 30m --json (nur Zaehlung)' { $a = & $exe audit --since 30m --json; ($a | Measure-Object).Count }
Invoke-Step '27 Kanarienwort im Audit (erwartet 0 Treffer)' { $a = & $exe audit --since 30m --json | Out-String; ([regex]::Matches($a, 'KANARIENWORT|Rechnung|demo@example|a@example')).Count } | Out-Null
Invoke-Step '28 Kanarienwort im Log (erwartet 0 Treffer)' { $l = Get-Content (Join-Path $d 'postkauz.log') -Raw; ([regex]::Matches($l, 'KANARIENWORT|Ignoriere')).Count } | Out-Null
Invoke-Step '29 doctor am Ende' { & $exe doctor } | Out-Null
Write-Log ('S2.3-Probe beendet ' + (Get-Date -Format 'dd.MM.yyyy HH:mm:ss'))
Write-Log 'ENDE'
