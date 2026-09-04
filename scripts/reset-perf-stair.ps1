[CmdletBinding()]
param(
    [string]$ComposeProject = "game01-perf",
    [string]$ComposeFile = "docker-compose.yml",
    [string]$PerfComposeFile = "docker-compose.perf.yml",
    [string]$MongoDatabase = "game01_perf"
)

$ErrorActionPreference = "Stop"

function Invoke-Compose {
    param([Parameter(Mandatory = $true)][string[]]$Arguments)

    & docker compose -p $ComposeProject -f $ComposeFile -f $PerfComposeFile @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "docker compose command failed: $($Arguments -join ' ')"
    }
}

function Wait-Healthy {
    param(
        [Parameter(Mandatory = $true)][string[]]$Services,
        [int]$TimeoutSeconds = 120
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $allHealthy = $true
        foreach ($service in $Services) {
            $containerID = (& docker compose -p $ComposeProject -f $ComposeFile -f $PerfComposeFile ps -q $service).Trim()
            if ([string]::IsNullOrWhiteSpace($containerID)) {
                throw "service container is not present: $service"
            }
            $health = (& docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' $containerID).Trim()
            if ($health -ne "healthy") {
                $allHealthy = $false
            }
        }
        if ($allHealthy) {
            return
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)

    throw "services did not become healthy within $TimeoutSeconds seconds"
}

Write-Host "Resetting local performance project '$ComposeProject'."
Write-Host "Only the game01-perf Compose project will be modified."

Invoke-Compose @("up", "-d", "--no-deps", "mongo", "redis", "etcd")
Wait-Healthy @("mongo", "redis", "etcd")

Write-Host "Stopping game services before clearing state."
Invoke-Compose @("stop", "gateway", "gateway-2", "usercenter", "lobby", "nginx")

Write-Host "Dropping MongoDB database '$MongoDatabase'."
& docker compose -p $ComposeProject -f $ComposeFile -f $PerfComposeFile exec -T mongo mongosh --quiet $MongoDatabase --eval "db.dropDatabase()"
if ($LASTEXITCODE -ne 0) {
    throw "MongoDB database cleanup failed"
}

Write-Host "Flushing the performance Redis database."
& docker compose -p $ComposeProject -f $ComposeFile -f $PerfComposeFile exec -T redis redis-cli FLUSHDB
if ($LASTEXITCODE -ne 0) {
    throw "Redis cleanup failed"
}

Write-Host "Starting game services."
Invoke-Compose @("up", "-d", "--no-deps", "gateway", "gateway-2", "usercenter", "lobby", "nginx")
Wait-Healthy @("gateway", "gateway-2", "usercenter", "lobby", "nginx")

Write-Host "Local performance environment is ready for the next staircase gradient."
