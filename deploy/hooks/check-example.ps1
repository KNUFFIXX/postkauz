<#
.SYNOPSIS
    Example check hook for PostKauz drafts (Windows PowerShell 5.1).

.DESCRIPTION
    PostKauz runs the check hook before a draft is submitted for approval
    ("postkauz.exe draft request <id>"). Exit code 0 = pass; any other exit
    code = fail: the draft stays "draft" and the first 4 KB of the output
    are shown as the reason. The output is never written to the audit log.

    This example fails if
      1. the subject contains "TODO" (upper case, anywhere), or
      2. the greeting is missing: the first non-empty line of the text
         (leading spaces ignored) must start with one of
           Hallo | Guten Tag | Sehr geehrte | Liebe | Lieber | Dear | Hi
         ("Sehr geehrte" also covers "Sehr geehrter"). This is a plain
         prefix check with exact upper/lower case, so "Hilfe" counts as
         "Hi" and "hallo" does not count. It is an example, not a style
         guide.

    Configuration in policy.toml (next to config.toml). Single quotes in
    TOML keep the backslashes of a Windows path as they are:

        [draft]
        check_hook = ['powershell.exe', '-NoProfile', '-NonInteractive',
                      '-ExecutionPolicy', 'Bypass',
                      '-File', 'C:\PostKauz\hooks\check-example.ps1',
                      '{meta_json}', '{body_text}']
        check_hook_timeout = '30s'

    PostKauz starts the program directly (no cmd.exe, no shell) and without
    a console window. It replaces {meta_json} and {body_text} with the paths
    of temporary files (also available as POSTKAUZ_DRAFT_META_JSON and
    POSTKAUZ_DRAFT_BODY_TEXT). The draft content comes only as files, never
    as arguments.

.PARAMETER MetaJson
    Path of meta.json ({meta_json}): headers, attachment names and sizes,
    account, identity.

.PARAMETER BodyText
    Path of the plain text of the draft ({body_text}), UTF-8.
#>
param(
    [Parameter(Position = 0)]
    [string] $MetaJson = $env:POSTKAUZ_DRAFT_META_JSON,
    [Parameter(Position = 1)]
    [string] $BodyText = $env:POSTKAUZ_DRAFT_BODY_TEXT
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrEmpty($MetaJson) -or [string]::IsNullOrEmpty($BodyText)) {
    Write-Output 'check-example: give the paths of {meta_json} and {body_text}'
    exit 2
}

$problems = New-Object -TypeName 'System.Collections.Generic.List[string]'

# 1. Subject
$meta = Get-Content -LiteralPath $MetaJson -Raw -Encoding UTF8 | ConvertFrom-Json
$subject = [string] $meta.subject
if ($subject.Contains('TODO')) {
    $problems.Add('subject contains TODO')
}

# 2. Greeting: first non-empty line of the text, leading spaces removed.
$text = Get-Content -LiteralPath $BodyText -Raw -Encoding UTF8
if ($null -eq $text) {
    $text = ''
}
$first = ''
foreach ($line in ($text -split "`r?`n")) {
    if ($line.Trim() -ne '') {
        $first = $line.TrimStart()
        break
    }
}
$greetings = @('Hallo', 'Guten Tag', 'Sehr geehrte', 'Liebe', 'Lieber', 'Dear', 'Hi')
$found = $false
foreach ($g in $greetings) {
    if ($first.StartsWith($g, [System.StringComparison]::Ordinal)) {
        $found = $true
        break
    }
}
if (-not $found) {
    $problems.Add('greeting missing: the first line of the text must start with Hallo, Guten Tag, Sehr geehrte, Liebe, Lieber, Dear or Hi')
}

if ($problems.Count -gt 0) {
    foreach ($p in $problems) {
        Write-Output $p
    }
    exit 1
}
exit 0
