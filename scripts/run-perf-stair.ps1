[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][ValidateSet("guest-login", "password-login")][string]$Scenario,
    [Parameter(Mandatory = $true)][int]$Connections,
    [string]$LoadtestPath = ".\loadtest-local.exe",
    [int]$RampPerSecond = 25,
    [int]$DurationSeconds = 20,
    [string]$Target = "ws://127.0.0.1:18080/ws",
    [string]$UsernamePrefix = "stair",
    [string]$RunId = "",
    [string]$Password = "PerfPassword123!"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $LoadtestPath)) {
    throw "loadtest executable was not found: $LoadtestPath"
}

$resetScript = Join-Path $PSScriptRoot "reset-perf-stair.ps1"
& powershell -ExecutionPolicy Bypass -File $resetScript
if ($LASTEXITCODE -ne 0) {
    throw "performance environment reset failed"
}

if ([string]::IsNullOrWhiteSpace($RunId)) {
    $RunId = "stair-$Scenario-$Connections-$(Get-Date -Format yyyyMMddHHmmss)"
}

& $LoadtestPath `
    -scenario $Scenario `
    -target $Target `
    -connections $Connections `
    -ramp-per-second $RampPerSecond `
    -duration ("{0}s" -f $DurationSeconds) `
    -dial-retries 1 `
    -username-prefix $UsernamePrefix `
    -run-id $RunId `
    -password $Password

if ($LASTEXITCODE -ne 0) {
    throw "loadtest failed for $Scenario at $Connections connections"
}
