[CmdletBinding()]
param(
    [string]$BaseUrl = "http://127.0.0.1:19000",
    [string]$WebSocketUrl = "ws://127.0.0.1:19000/api/v1/ws",
    [string]$Origin = "http://localhost:5173",

    [ValidateRange(1, 1000000)]
    [int]$Connections = 10,

    [ValidateRange(0.0, 1.0)]
    [double]$ActiveRatio = 1.0,

    [ValidateRange(-1, 1000000)]
    [int]$ActiveClients = -1,

    [ValidateRange(0.0, 100.0)]
    [double]$MaxLossPercent = 0.0,
	[string]$MaxP95Latency = "",

    [string]$SendInterval = "2s",
    [string]$RampInterval = "10ms",
    [string]$Warmup = "2s",
    [string]$Duration = "30s",
	[string]$Drain = "5s",
	[ValidateRange(0, 1000000)]
	[int]$AckEvery = 0,

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
$script:WebSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$sendingEnabled = if ($ActiveClients -ge 0) { $ActiveClients -gt 0 } else { $ActiveRatio -gt 0 }
if ($ActiveClients -gt $Connections) {
    throw "-ActiveClients cannot be greater than -Connections"
}

function Write-Step {
    param([string]$Message)
    Write-Host "`n==> $Message" -ForegroundColor Cyan
}

function Convert-GoDurationToMilliseconds {
	param([string]$DurationText)

	if ([string]::IsNullOrWhiteSpace($DurationText)) {
		return $null
	}
	$parts = [regex]::Matches($DurationText, "(?<value>\d+(?:\.\d+)?)(?<unit>ms|us|µs|ns|h|m|s)")
	if ($parts.Count -eq 0) {
		throw "Invalid Go duration '$DurationText'. Examples: 500ms, 1s, 1.5s."
	}
	$reconstructed = ($parts | ForEach-Object { $_.Value }) -join ""
	if ($reconstructed -ne $DurationText) {
		throw "Invalid Go duration '$DurationText'. Examples: 500ms, 1s, 1.5s."
	}
	$totalMilliseconds = 0.0
	foreach ($part in $parts) {
		$value = [double]::Parse(
			$part.Groups["value"].Value,
			[System.Globalization.CultureInfo]::InvariantCulture
		)
		switch ($part.Groups["unit"].Value) {
			"h"  { $totalMilliseconds += $value * 3600000.0 }
			"m"  { $totalMilliseconds += $value * 60000.0 }
			"s"  { $totalMilliseconds += $value * 1000.0 }
			"ms" { $totalMilliseconds += $value }
			"us" { $totalMilliseconds += $value / 1000.0 }
			"µs" { $totalMilliseconds += $value / 1000.0 }
			"ns" { $totalMilliseconds += $value / 1000000.0 }
		}
	}
	return $totalMilliseconds
}

$maxP95LatencyMilliseconds = Convert-GoDurationToMilliseconds -DurationText $MaxP95Latency

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
        $request.WebSession = $script:WebSession
    }
    try {
        return Invoke-RestMethod @request
    }
    catch {
        $statusCode = "unknown"
        if ($null -ne $_.Exception.Response) {
            $statusCode = [int]$_.Exception.Response.StatusCode
        }
        $responseBody = ""
        if ($null -ne $_.ErrorDetails) {
            $responseBody = $_.ErrorDetails.Message
        }
        if ([string]::IsNullOrWhiteSpace($responseBody)) {
            $responseBody = $_.Exception.Message
        }
        throw "API request failed: $Method $Uri; status=$statusCode; response=$responseBody"
    }
}

function Start-SshTunnel {
    if ([string]::IsNullOrWhiteSpace($SshTarget)) {
        throw "-StartTunnel requires -SshTarget, for example <SSH_USER>@<SERVER_IPV4>"
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
		"-warmup", $Warmup,
		"-d", $Duration,
		"-drain", $Drain,
		"-replay-base-url", $BaseUrl
	)
	if ($AckEvery -gt 0) {
		$arguments += @("-ack-every", $AckEvery.ToString())
	}
    if ($ActiveClients -ge 0) {
        $arguments += @("-active-clients", $ActiveClients.ToString())
    }

    Write-Step "Running WebSocket benchmark"
    $benchmarkLines = @()
    $benchmarkExitCode = 0
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        # Go 的 log 包会将正常的基准测试输出写入 stderr。当全局首选项为 Stop 时，
        # Windows PowerShell 5.1 会将原生命令的 stderr 包装为 NativeCommandError，
        # 因此这里临时允许原生日志继续输出。
        $ErrorActionPreference = "Continue"
        & go @arguments 2>&1 |
            Tee-Object -Variable benchmarkLines |
            ForEach-Object { Write-Host $_ }
        $benchmarkExitCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    if ($benchmarkExitCode -ne 0) {
        throw "WebSocket benchmark exited with code $benchmarkExitCode"
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
    $connectionMatch = [regex]::Match(
        $finalLine,
        "peak_conns=(?<peak>\d+) dial_attempted=(?<attempted>\d+) dial_succeeded=(?<succeeded>\d+) dial_failed=(?<failed>\d+)"
    )
    if (-not $connectionMatch.Success) {
        throw "Unable to parse benchmark connection metrics: $finalLine"
    }
	$protocolMatch = [regex]::Match(
		$finalLine,
		"ack_sent=(?<ack>\d+) resync_required=(?<resync>\d+) recovered=(?<recovered>\d+)"
	)
	if (-not $protocolMatch.Success) {
		throw "Unable to parse benchmark protocol metrics: $finalLine"
	}
	$latencyMatch = [regex]::Match(
		$finalLine,
		"latency_samples=(?<samples>\d+) latency_p50=(?<p50>\S+) latency_p95=(?<p95>\S+) latency_p99=(?<p99>\S+)"
	)
	if (-not $latencyMatch.Success) {
		throw "Unable to parse benchmark latency metrics: $finalLine"
	}

    $sent = [int64]$metricsMatch.Groups["sent"].Value
    $received = [int64]$metricsMatch.Groups["received"].Value
    $expected = [int64]$metricsMatch.Groups["expected"].Value
    $errors = [int64]$metricsMatch.Groups["errors"].Value
	$missing = [Math]::Max([int64]0, $expected - $received)
	$lossPercent = 0.0
	if ($expected -gt 0) {
		$lossPercent = $missing * 100.0 / $expected
	}
    $peakConnections = [int64]$connectionMatch.Groups["peak"].Value
    $dialAttempted = [int64]$connectionMatch.Groups["attempted"].Value
    $dialSucceeded = [int64]$connectionMatch.Groups["succeeded"].Value
    $dialFailed = [int64]$connectionMatch.Groups["failed"].Value
	$ackSent = [int64]$protocolMatch.Groups["ack"].Value
	$resyncRequired = [int64]$protocolMatch.Groups["resync"].Value
	$recovered = [int64]$protocolMatch.Groups["recovered"].Value
	$latencySamples = [int64]$latencyMatch.Groups["samples"].Value
	$latencyP50 = $latencyMatch.Groups["p50"].Value
	$latencyP95 = $latencyMatch.Groups["p95"].Value
	$latencyP99 = $latencyMatch.Groups["p99"].Value
	$latencyP95Milliseconds = Convert-GoDurationToMilliseconds -DurationText $latencyP95

    if ($errors -ne 0) {
        throw "Benchmark reported $errors errors"
    }
    if ($dialAttempted -ne $Connections -or $dialSucceeded -ne $Connections -or $dialFailed -ne 0 -or $peakConnections -ne $Connections) {
        throw "Connection target was not reached: attempted=$dialAttempted succeeded=$dialSucceeded failed=$dialFailed peak=$peakConnections target=$Connections"
    }
    if ($sendingEnabled -and $sent -eq 0) {
        throw "Benchmark did not send any messages"
    }
    if ($sendingEnabled -and $received -eq 0) {
        throw "Benchmark sent messages but received no broadcasts"
    }
	if ($sendingEnabled -and $received -gt $expected) {
		throw "Duplicate deliveries detected: received=$received expected=$expected"
	}
	if ($sendingEnabled -and $lossPercent -gt $MaxLossPercent) {
		$lossText = $lossPercent.ToString("0.######", [System.Globalization.CultureInfo]::InvariantCulture)
		$limitText = $MaxLossPercent.ToString("0.######", [System.Globalization.CultureInfo]::InvariantCulture)
		throw "Delivery loss exceeded limit: missing=$missing loss=$lossText% limit=$limitText%"
	}
	if ($sendingEnabled -and $null -ne $maxP95LatencyMilliseconds -and $latencyP95Milliseconds -gt $maxP95LatencyMilliseconds) {
		throw "P95 latency exceeded limit: measured=$latencyP95 limit=$MaxP95Latency"
	}

    return [pscustomobject]@{
        Sent            = $sent
        Received        = $received
		Expected        = $expected
		Missing         = $missing
		LossPercent     = $lossPercent
        Errors          = $errors
        PeakConnections = $peakConnections
        DialAttempted   = $dialAttempted
        DialSucceeded   = $dialSucceeded
        DialFailed      = $dialFailed
		AckSent         = $ackSent
		ResyncRequired  = $resyncRequired
		Recovered       = $recovered
		LatencySamples  = $latencySamples
		LatencyP50      = $latencyP50
		LatencyP95      = $latencyP95
		LatencyP99      = $latencyP99
        FinalLine       = $finalLine
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
    Wait-TcpPort -HostName $baseUri.DnsSafeHost -Port $basePort
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
    if ($script:Sid -notmatch "^[0-9a-fA-F]{64}$") {
        throw "Login response contained an invalid sid value (length=$($script:Sid.Length))"
    }

    # Windows PowerShell 可能会丢弃手动提供的 Cookie 请求头。
    # 将 sid 放入 CookieContainer，并把客户端副本标记为非 Secure，
    # 以便通过本地 HTTP SSH 隧道发送。服务端 Cookie 仍保持 Secure，供浏览器使用。
    $sessionCookie = New-Object System.Net.Cookie("sid", $script:Sid, "/")
    $sessionCookie.Secure = $false
    $script:WebSession.Cookies.Add([Uri]$BaseUrl, $sessionCookie)

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
        $createRoomJson = $createRoomResult | ConvertTo-Json -Depth 10 -Compress
        throw "Create-room response did not contain a valid room_id. Response: $createRoomJson"
    }

    Write-Step "Starting room $($script:RoomId)"
    $null = Invoke-JsonApi `
        -Method Post `
        -Uri "$BaseUrl/api/v1/rooms/$($script:RoomId)/start" `
        -Sid $script:Sid `
        -Body @{ room_title = "Benchmark Room $stamp" }
    $script:RoomStarted = $true

    $benchmarkResult = Invoke-Benchmark -RoomId $script:RoomId -Sid $script:Sid

    if (-not $SkipHistoryCheck -and $sendingEnabled) {
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
Write-Host "Peak connections: $($benchmarkResult.PeakConnections)"
Write-Host "Dial succeeded: $($benchmarkResult.DialSucceeded)/$($benchmarkResult.DialAttempted)"
Write-Host "Sent: $($benchmarkResult.Sent)"
Write-Host "Received: $($benchmarkResult.Received)"
Write-Host "Expected deliveries: $($benchmarkResult.Expected)"
Write-Host "Missing deliveries: $($benchmarkResult.Missing)"
Write-Host "Loss: $($benchmarkResult.LossPercent.ToString('0.######', [System.Globalization.CultureInfo]::InvariantCulture))%"
Write-Host "ACK sent: $($benchmarkResult.AckSent)"
Write-Host "Resync required: $($benchmarkResult.ResyncRequired)"
Write-Host "Recovered by replay: $($benchmarkResult.Recovered)"
Write-Host "Latency samples: $($benchmarkResult.LatencySamples)"
Write-Host "Latency P50/P95/P99: $($benchmarkResult.LatencyP50) / $($benchmarkResult.LatencyP95) / $($benchmarkResult.LatencyP99)"
Write-Host "History matches: $historyCount"
