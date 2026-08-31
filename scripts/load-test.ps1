[CmdletBinding()]
param(
    [string]$BaseUrl = "http://127.0.0.1:19000",
    [string]$WebSocketUrl = "ws://127.0.0.1:19000/api/v1/ws",
    [string]$Origin = "http://localhost:5173",

    [ValidateRange(1, 1000000)]
    [int]$Connections = 10,

    [ValidateRange(0.0, 1.0)]
    [double]$ActiveRatio = 1.0,

    [string]$SendInterval = "2s",
    [string]$RampInterval = "10ms",
    [string]$Duration = "30s",

    [switch]$StartTunnel,
    [string]$SshTarget = "",
    [ValidateRange(1, 65535)]
    [int]$LocalPort = 19000,
    [ValidateRange(1, 65535)]
    [int]$RemotePort = 9000,

    [switch]$SkipCodeChecks,
    [switch]$SkipHistoryCheck,
    [switch]$KeepRoomLive
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$script:TunnelProcess = $null
$script:RoomStarted = $false
$script:RoomId = [uint64]0
$script:Sid = ""

function Write-Step {
    param([string]$Message)
    Write-Host "`n==> $Message" -ForegroundColor Cyan
}

function Test-TcpPort {
    param(
        [string]$HostName,
        [int]$Port,
        [int]$TimeoutMilliseconds = 1000
    )

    $client = [System.Net.Sockets.TcpClient]::new()
    try {
        $task = $client.ConnectAsync($HostName, $Port)
        if (-not $task.Wait($TimeoutMilliseconds)) {
            return $false
        }
        return $client.Connected
    }
    catch {
        return $false
    }
    finally {
        $client.Dispose()
    }
}

function Wait-TcpPort {
    param(
        [string]$HostName,
        [int]$Port,
        [int]$TimeoutSeconds = 15
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        if (Test-TcpPort -HostName $HostName -Port $Port) {
            return
        }
        if ($null -ne $script:TunnelProcess -and $script:TunnelProcess.HasExited) {
            throw "SSH tunnel exited early with code $($script:TunnelProcess.ExitCode). Configure SSH key authentication or create the tunnel manually."
        }
        Start-Sleep -Milliseconds 500
    }
    throw "Timed out waiting for ${HostName}:$Port"
}

function Invoke-JsonApi {
    param(
        [ValidateSet("Get", "Post", "Patch")]
        [string]$Method,
        [string]$Uri,
        [object]$Body = $null,
        [string]$Sid = ""
    )

    $request = @{
        Method      = $Method
        Uri         = $Uri
        ErrorAction = "Stop"
    }
    if ($null -ne $Body) {
        $request.ContentType = "application/json"
        $request.Body = $Body | ConvertTo-Json -Compress
    }
    if (-not [string]::IsNullOrWhiteSpace($Sid)) {
        $request.Headers = @{ Cookie = "sid=$Sid" }
    }
    return Invoke-RestMethod @request
}

function Start-SshTunnel {
    if ([string]::IsNullOrWhiteSpace($SshTarget)) {
        throw "-StartTunnel requires -SshTarget, for example yujun@119.91.119.208"
    }

    if (Test-TcpPort -HostName "127.0.0.1" -Port $LocalPort) {
        Write-Host "Local port $LocalPort is already listening; reusing the existing tunnel."
        return
    }

    $forward = "127.0.0.1:${LocalPort}:127.0.0.1:${RemotePort}"
    $sshArguments = @(
        "-N",
        "-o", "BatchMode=yes",
        "-o", "ExitOnForwardFailure=yes",
        "-o", "ServerAliveInterval=30",
        "-L", $forward,
        $SshTarget
    )

    Write-Step "Starting SSH tunnel $forward through $SshTarget"
    $script:TunnelProcess = Start-Process `
        -FilePath "ssh.exe" `
        -ArgumentList $sshArguments `
        -PassThru `
        -WindowStyle Hidden

    Wait-TcpPort -HostName "127.0.0.1" -Port $LocalPort
}

function Invoke-CodeChecks {
    Write-Step "Running Go compile tests"
    & go test ./...
    if ($LASTEXITCODE -ne 0) {
        throw "go test ./... failed"
    }

    Write-Step "Running go vet"
    & go vet ./...
    if ($LASTEXITCODE -ne 0) {
        throw "go vet ./... failed"
    }
}

function Invoke-Benchmark {
    param(
        [uint64]$RoomId,
        [string]$Sid
    )

    $activeText = $ActiveRatio.ToString([System.Globalization.CultureInfo]::InvariantCulture)
    $arguments = @(
        "run", "./cmd/benchmark",
        "-urls", $WebSocketUrl,
        "-origin", $Origin,
        "-room", $RoomId.ToString(),
        "-sid", $Sid,
        "-c", $Connections.ToString(),
        "-active", $activeText,
        "-r", $SendInterval,
        "-ramp", $RampInterval,
        "-d", $Duration
    )

    Write-Step "Running WebSocket benchmark"
    $benchmarkLines = @()
    & go @arguments 2>&1 |
        Tee-Object -Variable benchmarkLines |
        ForEach-Object { Write-Host $_ }
    if ($LASTEXITCODE -ne 0) {
        throw "WebSocket benchmark exited with code $LASTEXITCODE"
    }

    $finalLine = $benchmarkLines |
        ForEach-Object { $_.ToString() } |
        Where-Object { $_ -match "\[FINAL\]" } |
        Select-Object -Last 1

    if ([string]::IsNullOrWhiteSpace($finalLine)) {
        throw "Benchmark did not print a [FINAL] metrics line"
    }

    $metricsMatch = [regex]::Match(
        $finalLine,
        "sent=(?<sent>\d+) received=(?<received>\d+) frames=(?<frames>\d+) expected=(?<expected>\d+) errors=(?<errors>\d+)"
    )
    if (-not $metricsMatch.Success) {
        throw "Unable to parse benchmark metrics: $finalLine"
    }

    $sent = [int64]$metricsMatch.Groups["sent"].Value
    $received = [int64]$metricsMatch.Groups["received"].Value
    $errors = [int64]$metricsMatch.Groups["errors"].Value

    if ($errors -ne 0) {
        throw "Benchmark reported $errors errors"
    }
    if ($ActiveRatio -gt 0 -and $sent -eq 0) {
        throw "Benchmark did not send any messages"
    }
    if ($ActiveRatio -gt 0 -and $received -eq 0) {
        throw "Benchmark sent messages but received no broadcasts"
    }

    return [pscustomobject]@{
        Sent      = $sent
        Received  = $received
        Errors    = $errors
        FinalLine = $finalLine
    }
}

function Test-History {
    param([uint64]$RoomId)

    Write-Step "Checking persisted danmaku history"
    for ($attempt = 1; $attempt -le 10; $attempt++) {
        $history = Invoke-JsonApi `
            -Method Get `
            -Uri "$BaseUrl/api/v1/rooms/$RoomId/messages?limit=100"

        $items = @($history.data.items)
        $benchMessages = @($items | Where-Object {
            $null -ne $_ -and
            $null -ne $_.content -and
            $_.content.ToString().StartsWith("bench-")
        })

        if ($benchMessages.Count -gt 0) {
            Write-Host "Found $($benchMessages.Count) benchmark messages in history."
            return $benchMessages.Count
        }
        Start-Sleep -Seconds 1
    }

    throw "No bench-* message appeared in history within 10 seconds"
}

$projectRoot = Split-Path -Parent $PSScriptRoot
$testSucceeded = $false
$benchmarkResult = $null
$historyCount = 0

Push-Location $projectRoot
try {
    if ($StartTunnel) {
        Start-SshTunnel
    }

    if (-not $SkipCodeChecks) {
        Invoke-CodeChecks
    }

    Write-Step "Checking backend health"
    $baseUri = [Uri]$BaseUrl
    $basePort = if ($baseUri.IsDefaultPort) {
        if ($baseUri.Scheme -eq "https") { 443 } else { 80 }
    }
    else {
        $baseUri.Port
    }
    Wait-TcpPort -HostName $baseUri.Host -Port $basePort
    $null = Invoke-JsonApi -Method Get -Uri "$BaseUrl/api/v1/rooms"

    $stamp = Get-Date -Format "yyyyMMddHHmmssfff"
    $userName = "bench$stamp"
    $email = "$userName@example.com"
    $password = "Benchmark123!"

    Write-Step "Registering $email"
    $registerResult = Invoke-JsonApi `
        -Method Post `
        -Uri "$BaseUrl/api/v1/auth/register" `
        -Body @{
            username = $userName
            email    = $email
            password = $password
        }
    if ($null -eq $registerResult.data.user_id -or [uint64]$registerResult.data.user_id -eq 0) {
        $registerJson = $registerResult | ConvertTo-Json -Depth 10 -Compress
        throw "Registration response did not contain a valid user_id. Response: $registerJson"
    }

    Write-Step "Logging in and extracting sid"
    $loginResponse = Invoke-WebRequest `
        -UseBasicParsing `
        -Method Post `
        -Uri "$BaseUrl/api/v1/auth/login" `
        -ContentType "application/json" `
        -Body (@{ email = $email; password = $password } | ConvertTo-Json -Compress)

    $setCookie = $loginResponse.Headers["Set-Cookie"] -join ";"
    $sidMatch = [regex]::Match($setCookie, "sid=([^;]+)")
    if (-not $sidMatch.Success) {
        throw "Login response did not contain a sid cookie"
    }
    $script:Sid = $sidMatch.Groups[1].Value

    $me = Invoke-JsonApi `
        -Method Get `
        -Uri "$BaseUrl/api/v1/auth/me" `
        -Sid $script:Sid
    if ($null -eq $me.data.user_id -or [uint64]$me.data.user_id -eq 0) {
        throw "Session verification failed"
    }

    Write-Step "Creating benchmark room"
    $createRoomResult = Invoke-JsonApi `
        -Method Post `
        -Uri "$BaseUrl/api/v1/rooms" `
        -Sid $script:Sid `
        -Body @{ title = "Benchmark Room $stamp" }
    $script:RoomId = [uint64]$createRoomResult.data.room_id
    if ($script:RoomId -eq 0) {
        throw "Create-room response did not contain a valid room_id"
    }

    Write-Step "Starting room $($script:RoomId)"
    $null = Invoke-JsonApi `
        -Method Post `
        -Uri "$BaseUrl/api/v1/rooms/$($script:RoomId)/start" `
        -Sid $script:Sid `
        -Body @{ room_title = "Benchmark Room $stamp" }
    $script:RoomStarted = $true

    $benchmarkResult = Invoke-Benchmark -RoomId $script:RoomId -Sid $script:Sid

    if (-not $SkipHistoryCheck -and $ActiveRatio -gt 0) {
        $historyCount = Test-History -RoomId $script:RoomId
    }

    $testSucceeded = $true
}
finally {
    if ($script:RoomStarted -and -not $KeepRoomLive) {
        Write-Step "Stopping room $($script:RoomId)"
        try {
            $null = Invoke-JsonApi `
                -Method Post `
                -Uri "$BaseUrl/api/v1/rooms/$($script:RoomId)/stop" `
                -Sid $script:Sid
        }
        catch {
            Write-Warning "Failed to stop room $($script:RoomId): $($_.Exception.Message)"
        }
    }

    if ($null -ne $script:TunnelProcess -and -not $script:TunnelProcess.HasExited) {
        Write-Step "Stopping SSH tunnel"
        Stop-Process -Id $script:TunnelProcess.Id
        $script:TunnelProcess.WaitForExit(5000)
    }

    Pop-Location
}

if (-not $testSucceeded) {
    throw "Automated load test failed"
}

Write-Host "`nAUTOMATED LOAD TEST PASSED" -ForegroundColor Green
Write-Host "Room ID: $($script:RoomId)"
Write-Host "Sent: $($benchmarkResult.Sent)"
Write-Host "Received: $($benchmarkResult.Received)"
Write-Host "History matches: $historyCount"
