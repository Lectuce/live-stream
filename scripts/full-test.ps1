[CmdletBinding()]
param(
    [string]$BaseUrl = "http://127.0.0.1:9000",
    [string]$WebSocketUrl = "ws://127.0.0.1:9000/api/v1/ws",
    [string]$Origin = "http://localhost:5173",

    [ValidateSet("Functional", "Smoke", "Standard", "Stress", "Extreme")]
    [string]$Profile = "Smoke",

    [ValidateRange(0.0, 100.0)]
    [double]$MaxLossPercent = 0.0,
    [string]$MaxP95Latency = "",
	[ValidateRange(0, 1000000)]
	[int]$LikeRateLimit = 10,

    [switch]$SkipCodeChecks,
    [switch]$SkipFrontendChecks,
    [switch]$SkipHistoryChecks,
    [switch]$FailOnKnownIssues,

    [string]$KafkaBrokers = "",
    [string]$KafkaTopic = "danmu_topic",
    [ValidateRange(1, 100000000)]
    [int]$KafkaMessages = 100000,
    [ValidateRange(1, 1024)]
    [int]$KafkaWorkers = 8,

    [string]$DiagnosticsBaseUrl = "",
    [string]$ReportDirectory = "test-results"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
$script:Checks = New-Object System.Collections.Generic.List[object]
$script:KnownIssues = New-Object System.Collections.Generic.List[object]
$script:LoadResults = New-Object System.Collections.Generic.List[object]
$script:StartedRoom = [uint64]0
$script:OwnerSession = $null
$script:OwnerSid = ""
$script:SuiteError = $null
$script:StartedAt = Get-Date

function Write-Step {
    param([string]$Message)
    Write-Host "`n==> $Message" -ForegroundColor Cyan
}

function Add-Check {
    param(
        [string]$Name,
        [ValidateSet("PASS", "FAIL", "WARN", "SKIP")]
        [string]$Status,
        [string]$Detail = ""
    )
    $script:Checks.Add([pscustomobject]@{
        Name   = $Name
        Status = $Status
        Detail = $Detail
    })
    $color = switch ($Status) {
        "PASS" { "Green" }
        "WARN" { "Yellow" }
        "SKIP" { "DarkYellow" }
        default { "Red" }
    }
    Write-Host "[$Status] $Name$(if ($Detail) { ': ' + $Detail })" -ForegroundColor $color
}

function Assert-True {
    param(
        [bool]$Condition,
        [string]$Name,
        [string]$Detail = ""
    )
    if (-not $Condition) {
        Add-Check -Name $Name -Status FAIL -Detail $Detail
        throw "Assertion failed: $Name. $Detail"
    }
    Add-Check -Name $Name -Status PASS -Detail $Detail
}

function Add-KnownIssue {
    param(
        [string]$Name,
        [string]$Evidence
    )
    $script:KnownIssues.Add([pscustomobject]@{
        Name     = $Name
        Evidence = $Evidence
    })
    Add-Check -Name $Name -Status WARN -Detail $Evidence
    if ($FailOnKnownIssues) {
        throw "Known issue confirmed and -FailOnKnownIssues was specified: $Name"
    }
}

function Invoke-RawApi {
    param(
        [ValidateSet("GET", "POST", "PATCH")]
        [string]$Method,
        [string]$Uri,
        [object]$Body = $null,
        [Microsoft.PowerShell.Commands.WebRequestSession]$Session = $null
    )

    $request = @{
        UseBasicParsing = $true
        Method          = $Method
        Uri             = $Uri
        ErrorAction     = "Stop"
    }
    if ($null -ne $Session) {
        $request.WebSession = $Session
    }
    if ($null -ne $Body) {
        $request.ContentType = "application/json"
        $request.Body = $Body | ConvertTo-Json -Depth 20 -Compress
    }

    $status = 0
    $content = ""
    $headers = $null
    try {
        $response = Invoke-WebRequest @request
        $status = [int]$response.StatusCode
        $content = [string]$response.Content
        $headers = $response.Headers
    }
    catch {
        $webResponse = $_.Exception.Response
        if ($null -eq $webResponse) {
            throw
        }
        $status = [int]$webResponse.StatusCode
        $headers = $webResponse.Headers
        try {
            $stream = $webResponse.GetResponseStream()
            if ($null -ne $stream) {
                $reader = New-Object System.IO.StreamReader($stream)
                try { $content = $reader.ReadToEnd() }
                finally { $reader.Dispose() }
            }
        }
        catch {
            if ($null -ne $_.ErrorDetails) {
                $content = [string]$_.ErrorDetails.Message
            }
        }
    }

    $json = $null
    if (-not [string]::IsNullOrWhiteSpace($content)) {
        try { $json = $content | ConvertFrom-Json }
        catch { }
    }
    return [pscustomobject]@{
        Status  = $status
        Body    = $content
        Json    = $json
        Headers = $headers
    }
}

function Assert-Status {
    param(
        [object]$Response,
        [int[]]$Expected,
        [string]$Name
    )
    $ok = $Expected -contains [int]$Response.Status
    $detail = "status=$($Response.Status), expected=$($Expected -join '/')"
    if (-not [string]::IsNullOrWhiteSpace($Response.Body) -and -not $ok) {
        $detail += ", body=$($Response.Body)"
    }
    Assert-True -Condition $ok -Name $Name -Detail $detail
}

function Invoke-Login {
    param(
        [string]$Email,
        [string]$Password,
        [string]$Label
    )
    $session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $response = Invoke-RawApi -Method POST -Uri "$BaseUrl/api/v1/auth/login" `
        -Session $session -Body @{ email = $Email; password = $Password }
    Assert-Status -Response $response -Expected 200 -Name "$Label login"
    $cookie = $session.Cookies.GetCookies([Uri]$BaseUrl)["sid"]
    Assert-True -Condition ($null -ne $cookie -and $cookie.Value -match "^[0-9a-fA-F]{64}$") `
        -Name "$Label received random sid" -Detail "64-hex session cookie"
    return [pscustomobject]@{ Session = $session; Sid = $cookie.Value; Response = $response }
}

function Invoke-GoHelper {
    param([string[]]$Arguments)
    $lines = @()
    $exitCode = 0
    $oldPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = "Continue"
        & go run ./cmd/e2e @Arguments 2>&1 |
            Tee-Object -Variable lines |
            ForEach-Object { Write-Host $_ }
        $exitCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $oldPreference
    }
    if ($exitCode -ne 0) {
        throw "cmd/e2e exited with code $exitCode"
    }
    return @($lines | ForEach-Object { $_.ToString() })
}

function Test-HandshakeStatus {
    param(
        [uint64]$RoomId,
        [int]$ExpectedStatus,
        [string]$Name
    )
    $lines = Invoke-GoHelper -Arguments @(
        "-ws-url", $WebSocketUrl,
        "-origin", $Origin,
        "-room", $RoomId.ToString(),
        "-expect-handshake-status", $ExpectedStatus.ToString(),
        "-timeout", "10s"
    )
    Assert-True -Condition (@($lines | Where-Object { $_ -match "\[E2E_HANDSHAKE\] status=$ExpectedStatus" }).Count -gt 0) `
        -Name $Name -Detail "handshake HTTP $ExpectedStatus"
}

function Invoke-ProtocolTests {
    param(
        [uint64]$RoomId,
        [uint64]$UserId,
        [string]$UserName,
        [string]$Sid
    )
    Write-Step "Testing WebSocket authentication, protocol, ordering, ACK, spoof protection, and likes"
    $lines = Invoke-GoHelper -Arguments @(
        "-ws-url", $WebSocketUrl,
        "-origin", $Origin,
        "-room", $RoomId.ToString(),
        "-sid", $Sid,
        "-user", $UserId.ToString(),
        "-username", $UserName,
		"-like-rate-limit", $LikeRateLimit.ToString(),
        "-timeout", "15s"
    )
    $jsonLine = $lines | Where-Object { $_ -match "\[E2E_JSON\]" } | Select-Object -Last 1
    if ([string]::IsNullOrWhiteSpace($jsonLine)) {
        throw "WebSocket protocol helper did not emit [E2E_JSON]"
    }
    $payload = $jsonLine.Substring($jsonLine.IndexOf("[E2E_JSON]") + 10).Trim() | ConvertFrom-Json
    Assert-True ([bool]$payload.origin_blocked) "WebSocket rejects an untrusted Origin"
    Assert-True ([bool]$payload.guest_read_ok) "Guest WebSocket connection is allowed for a live room"
    Assert-True ([bool]$payload.guest_send_blocked) "Guest cannot send danmaku"
    Assert-True ([bool]$payload.generated_id_valid) "Server generates a UUID message_id"
    Assert-True ([bool]$payload.spoofing_blocked) "Server overwrites forged room/user identity"
    Assert-True ([bool]$payload.ordering_ok) "Two viewers observe the same increasing room_seq order"
    Assert-True ([bool]$payload.ack_sent) "Viewer ACK frame is accepted"
    Assert-True ([int64]$payload.like_count -gt 0) "Like reaches the realtime room state" "like_count=$($payload.like_count)"
	if ($LikeRateLimit -gt 0) {
		Assert-True ([bool]$payload.like_rate_limited) "Per-user/per-room like rate limit" "limit=$LikeRateLimit/s"
	}
    if ([bool]$payload.duplicate_like_applied) {
        Add-KnownIssue "Like message_id is not idempotent" `
            "Sending the same like message_id twice increased Redis like_count."
    }
    else {
        Add-Check "Like message_id is idempotent" PASS "Duplicate logical like did not increase the observed count."
    }
    return $payload
}

function Wait-HistoryMessages {
    param(
        [uint64]$RoomId,
        [string[]]$MessageIds
    )
    $deadline = (Get-Date).AddSeconds(20)
    do {
        $response = Invoke-RawApi -Method GET -Uri "$BaseUrl/api/v1/rooms/$RoomId/messages?limit=100"
        if ($response.Status -eq 200 -and $null -ne $response.Json) {
            $items = @($response.Json.data.items)
            $found = @($items | Where-Object { $MessageIds -contains $_.message_id })
            $foundIDs = @($found | ForEach-Object { $_.message_id } | Select-Object -Unique)
            if ($foundIDs.Count -eq $MessageIds.Count) {
                return $found
            }
        }
        Start-Sleep -Milliseconds 500
    } while ((Get-Date) -lt $deadline)
    throw "Timed out waiting for $($MessageIds.Count) protocol messages in MySQL history"
}

function Invoke-CodeChecks {
    if ($SkipCodeChecks) {
        Add-Check "Go tests and vet" SKIP "-SkipCodeChecks"
        return
    }
    Write-Step "Running Go tests"
    & go test ./...
    Assert-True ($LASTEXITCODE -eq 0) "go test ./..."

    Write-Step "Running go vet"
    & go vet ./...
    Assert-True ($LASTEXITCODE -eq 0) "go vet ./..."
}

function Invoke-FrontendChecks {
    $frontend = Join-Path $projectRoot "frontend"
    if ($SkipFrontendChecks -or -not (Test-Path (Join-Path $frontend "package.json"))) {
        Add-Check "Frontend lint and build" SKIP "frontend absent or -SkipFrontendChecks"
        return
    }
    if (-not (Get-Command npm -ErrorAction SilentlyContinue)) {
        Add-Check "Frontend lint and build" SKIP "npm is not available"
        return
    }
    Write-Step "Running frontend lint and production build"
    Push-Location $frontend
    try {
        if (-not (Test-Path "node_modules")) {
            & npm install
            Assert-True ($LASTEXITCODE -eq 0) "npm install"
        }
        & npm run lint
        Assert-True ($LASTEXITCODE -eq 0) "frontend lint"
        & npm run build
        Assert-True ($LASTEXITCODE -eq 0) "frontend production build"
    }
    finally {
        Pop-Location
    }
}

function Invoke-StaticReliabilityAudit {
    Write-Step "Auditing the three like consistency claims against source code"
    $realtimePath = Join-Path $projectRoot "internal/kafka/realtime_consumer.go"
    $redisPath = Join-Path $projectRoot "internal/dao/redis.go"
    $roomRepoPath = Join-Path $projectRoot "internal/repo/room.go"
    $likeRepoPath = Join-Path $projectRoot "internal/repo/like.go"
	$likeModelPath = Join-Path $projectRoot "internal/model/like_batch.go"
    $realtime = Get-Content $realtimePath -Raw
    $redis = Get-Content $redisPath -Raw
    $roomRepo = Get-Content $roomRepoPath -Raw
	$likeRepo = if (Test-Path $likeRepoPath) { Get-Content $likeRepoPath -Raw } else { "" }
	$likeModel = if (Test-Path $likeModelPath) { Get-Content $likeModelPath -Raw } else { "" }

    $durableIdempotency = $realtime -match "applyLikeBatchesUntilSuccess" -and
		$likeRepo -match "lockLikeCheckpoint" -and
		$likeRepo -match "last_offset" -and
		$likeModel -match "LikeAggregationCheckpoint"
    if (-not $durableIdempotency) {
        Add-KnownIssue "Like increment can repeat after consumer restart" `
            "No transactional Kafka partition checkpoint was detected for aggregate likes."
    }
    else {
        Add-Check "Like increment crash idempotency" PASS `
            "like_batches and the Kafka partition checkpoint advance in the same MySQL transaction."
    }

	$rankingUsesHotScore = $roomRepo -match "ReadHotRoomRanking" -and
		$roomRepo -match "ORDER BY room\.start_at DESC"
    $writesRoomLikeCount = $likeRepo -match 'UpdateColumn\("like_count"' -and
		$likeRepo -match 'gorm\.Expr\("like_count \+ \?"'
	if (-not $rankingUsesHotScore) {
		Add-KnownIssue "Room ranking still depends on historical likes" `
			"Redis hot-score ordering with start-time fallback was not detected."
    }
    else {
		Add-Check "Realtime hot room ranking" PASS `
			"Room order comes from the Redis hot-score ZSet and degrades to start_at ordering."
	}
	if (-not $writesRoomLikeCount) {
		Add-KnownIssue "Historical likes are not durably accumulated" `
			"The aggregate like consumer does not update rooms.like_count."
	}
	else {
		Add-Check "Historical like accumulation" PASS
    }

    $hasRecovery = $redis -match "func ReconcileRoomLikeCounts" -and
		$redis -match "Find\(&rooms\)" -and $redis -match "restoreRoomLikeCountScript\.Run"
    if (-not $hasRecovery) {
        Add-KnownIssue "Redis like loss has no MySQL recovery path" `
            "The Redis like DAO only provides INCR/GET, and startup/consumer code has no restore or reconciliation path."
    }
    else {
        Add-Check "Redis like recovery" PASS
    }
}

function Invoke-LoadProfiles {
    if ($Profile -eq "Functional") {
        Add-Check "WebSocket load test" SKIP "Profile=Functional"
        return
    }

    $stages = switch ($Profile) {
        "Smoke" {
            @([pscustomobject]@{ Name = "smoke-100x1"; Connections = 100; Active = 1; Duration = "15s"; Ramp = "5ms"; Drain = "10s" })
        }
        "Standard" {
            @(
                [pscustomobject]@{ Name = "baseline-100x1"; Connections = 100; Active = 1; Duration = "20s"; Ramp = "5ms"; Drain = "10s" },
                [pscustomobject]@{ Name = "standard-1000x10"; Connections = 1000; Active = 10; Duration = "30s"; Ramp = "2ms"; Drain = "15s" }
            )
        }
        "Stress" {
            @(
                [pscustomobject]@{ Name = "standard-1000x10"; Connections = 1000; Active = 10; Duration = "45s"; Ramp = "2ms"; Drain = "20s" },
                [pscustomobject]@{ Name = "stress-5000x10"; Connections = 5000; Active = 10; Duration = "1m"; Ramp = "1ms"; Drain = "30s" }
            )
        }
        "Extreme" {
            @(
                [pscustomobject]@{ Name = "standard-1000x10"; Connections = 1000; Active = 10; Duration = "45s"; Ramp = "2ms"; Drain = "20s" },
                [pscustomobject]@{ Name = "stress-5000x10"; Connections = 5000; Active = 10; Duration = "1m"; Ramp = "1ms"; Drain = "30s" },
                [pscustomobject]@{ Name = "extreme-10000x10"; Connections = 10000; Active = 10; Duration = "2m"; Ramp = "1ms"; Drain = "1m" }
            )
        }
    }

    foreach ($stage in $stages) {
        Write-Step "Load stage $($stage.Name): $($stage.Connections) connections, $($stage.Active) senders"
        $parameters = @{
            BaseUrl          = $BaseUrl
            WebSocketUrl     = $WebSocketUrl
            Origin           = $Origin
            Connections      = $stage.Connections
            ActiveClients    = $stage.Active
            SendInterval     = "1s"
            RampInterval     = $stage.Ramp
            Warmup           = "3s"
            Duration         = $stage.Duration
            Drain            = $stage.Drain
            AckEvery         = 0
            MaxLossPercent   = $MaxLossPercent
            SkipCodeChecks   = $true
            SkipHistoryCheck = $SkipHistoryChecks
        }
        if (-not [string]::IsNullOrWhiteSpace($MaxP95Latency)) {
            $parameters.MaxP95Latency = $MaxP95Latency
        }
        & (Join-Path $PSScriptRoot "load-test.ps1") @parameters
        if ($LASTEXITCODE -ne 0) {
            throw "Load stage $($stage.Name) failed with exit code $LASTEXITCODE"
        }
        $script:LoadResults.Add([pscustomobject]@{
            Name        = $stage.Name
            Connections = $stage.Connections
            Senders     = $stage.Active
            Status      = "PASS"
        })
        Add-Check "Load stage $($stage.Name)" PASS "$($stage.Connections) connections"
    }
}

function Invoke-KafkaBenchmark {
    if ([string]::IsNullOrWhiteSpace($KafkaBrokers)) {
        Add-Check "Kafka pure-write benchmark" SKIP "Pass -KafkaBrokers to enable it"
        return
    }
    Write-Step "Running Kafka pure-write benchmark"
    & go run ./cmd/benchmark/kafka `
        -brokers $KafkaBrokers `
        -topic $KafkaTopic `
        -n $KafkaMessages `
        -w $KafkaWorkers `
        -async=false `
        -batch 100
    Assert-True ($LASTEXITCODE -eq 0) "Kafka pure-write benchmark" `
        "$KafkaMessages messages with $KafkaWorkers workers"
}

function Get-DiagnosticsSnapshot {
    param([string]$Phase)
    if ([string]::IsNullOrWhiteSpace($DiagnosticsBaseUrl)) {
        return
    }
    foreach ($path in @("/debug/ws/metrics", "/debug/kafka/realtime")) {
        $response = Invoke-RawApi -Method GET -Uri ($DiagnosticsBaseUrl.TrimEnd("/") + $path)
        if ($response.Status -eq 200) {
            Add-Check "Diagnostics $Phase $path" PASS ($response.Body -replace "\s+", " ")
        }
        else {
            Add-Check "Diagnostics $Phase $path" WARN "status=$($response.Status)"
        }
    }
}

function Write-TestReport {
    $finishedAt = Get-Date
    $resolvedDirectory = if ([System.IO.Path]::IsPathRooted($ReportDirectory)) {
        $ReportDirectory
    }
    else {
        Join-Path $projectRoot $ReportDirectory
    }
    New-Item -ItemType Directory -Path $resolvedDirectory -Force | Out-Null
    $stamp = $script:StartedAt.ToString("yyyyMMdd-HHmmss")
    $jsonPath = Join-Path $resolvedDirectory "full-test-$stamp.json"
    $markdownPath = Join-Path $resolvedDirectory "full-test-$stamp.md"
    $failed = @($script:Checks | Where-Object { $_.Status -eq "FAIL" }).Count
    $report = [pscustomobject]@{
        started_at   = $script:StartedAt.ToString("o")
        finished_at  = $finishedAt.ToString("o")
        duration_sec = [Math]::Round(($finishedAt - $script:StartedAt).TotalSeconds, 3)
        base_url     = $BaseUrl
        websocket_url = $WebSocketUrl
        profile      = $Profile
        passed       = ($null -eq $script:SuiteError -and $failed -eq 0)
        error        = if ($null -ne $script:SuiteError) { $script:SuiteError.Exception.Message } else { $null }
        checks       = @($script:Checks)
        known_issues = @($script:KnownIssues)
        load_stages  = @($script:LoadResults)
    }
    $report | ConvertTo-Json -Depth 20 | Set-Content -Path $jsonPath -Encoding UTF8

    $markdown = New-Object System.Collections.Generic.List[string]
    $markdown.Add("# live-stream automated test report")
    $markdown.Add("")
    $markdown.Add("- Result: **$(if ($report.passed) { 'PASS' } else { 'FAIL' })**")
    $markdown.Add("- Started: $($report.started_at)")
    $markdown.Add("- Duration: $($report.duration_sec) seconds")
    $markdown.Add("- Target: $BaseUrl")
    $markdown.Add("- Profile: $Profile")
    $markdown.Add("")
    $markdown.Add("## Checks")
    $markdown.Add("")
    $markdown.Add("| Status | Check | Detail |")
    $markdown.Add("|---|---|---|")
    foreach ($check in $script:Checks) {
        $detail = ([string]$check.Detail).Replace("|", "\|").Replace("`r", " ").Replace("`n", " ")
        $markdown.Add("| $($check.Status) | $($check.Name) | $detail |")
    }
    $markdown.Add("")
    $markdown.Add("## Confirmed known issues")
    $markdown.Add("")
    if ($script:KnownIssues.Count -eq 0) {
        $markdown.Add("None detected.")
    }
    else {
        foreach ($issue in $script:KnownIssues) {
            $markdown.Add("- **$($issue.Name)**: $($issue.Evidence)")
        }
    }
    if ($null -ne $script:SuiteError) {
        $markdown.Add("")
        $markdown.Add("## Failure")
        $markdown.Add("")
        $markdown.Add('```')
        $markdown.Add($script:SuiteError.Exception.Message)
        $markdown.Add('```')
    }
    $markdown | Set-Content -Path $markdownPath -Encoding UTF8
    Write-Host "`nReports:" -ForegroundColor Cyan
    Write-Host "  $markdownPath"
    Write-Host "  $jsonPath"
}

Push-Location $projectRoot
try {
    Invoke-CodeChecks
    Invoke-FrontendChecks
    Invoke-StaticReliabilityAudit

    Write-Step "Checking public API and authentication boundaries"
    $health = Invoke-RawApi -Method GET -Uri "$BaseUrl/api/v1/rooms"
    Assert-Status $health 200 "Public room list"
    Assert-Status (Invoke-RawApi GET "$BaseUrl/api/v1/auth/me") 401 "Unauthenticated /auth/me is rejected"
    Assert-Status (Invoke-RawApi GET "$BaseUrl/api/v1/rooms/mine") 401 "Unauthenticated /rooms/mine is rejected"
    Assert-Status (Invoke-RawApi POST "$BaseUrl/api/v1/rooms" @{ title = "unauthorized" }) 401 "Unauthenticated room creation is rejected"
    Assert-Status (Invoke-RawApi POST "$BaseUrl/api/v1/auth/register" @{
        username = "x"; email = "invalid"; password = "short"
    }) 400 "Registration DTO validation"

    $stamp = Get-Date -Format "yyyyMMddHHmmssfff"
    $ownerName = "e2eowner$stamp"
    $otherName = "e2eother$stamp"
    $ownerEmail = "$ownerName@example.com"
    $otherEmail = "$otherName@example.com"
    $password = "E2ePassword123!"

    Write-Step "Testing registration and login"
    $ownerRegister = Invoke-RawApi POST "$BaseUrl/api/v1/auth/register" @{
        username = $ownerName; email = $ownerEmail; password = $password
    }
    Assert-Status $ownerRegister 200 "Owner registration"
    $ownerID = [uint64]$ownerRegister.Json.data.user_id
    Assert-True ($ownerID -gt 0) "Owner receives auto-increment user_id" "user_id=$ownerID"

    $otherRegister = Invoke-RawApi POST "$BaseUrl/api/v1/auth/register" @{
        username = $otherName; email = $otherEmail; password = $password
    }
    Assert-Status $otherRegister 200 "Second-user registration"
    $otherID = [uint64]$otherRegister.Json.data.user_id
    Assert-True ($otherID -gt 0 -and $otherID -ne $ownerID) "Second user has a distinct user_id"
    Assert-Status (Invoke-RawApi POST "$BaseUrl/api/v1/auth/register" @{
        username = "duplicate$stamp"; email = $ownerEmail; password = $password
    }) 409 "Duplicate email is rejected"
    Assert-Status (Invoke-RawApi POST "$BaseUrl/api/v1/auth/login" @{
        email = $ownerEmail; password = "WrongPassword123!"
    }) 401 "Wrong password is rejected"

    $ownerLogin = Invoke-Login $ownerEmail $password "Owner"
    $otherLogin = Invoke-Login $otherEmail $password "Second user"
    $script:OwnerSession = $ownerLogin.Session
    $script:OwnerSid = $ownerLogin.Sid
    $ownerMe = Invoke-RawApi GET "$BaseUrl/api/v1/auth/me" $null $ownerLogin.Session
    Assert-Status $ownerMe 200 "Owner session lookup"
    Assert-True ([uint64]$ownerMe.Json.data.user_id -eq $ownerID) "Session maps to the correct owner"

    Write-Step "Testing room lifecycle and owner permissions"
    $createRoom = Invoke-RawApi POST "$BaseUrl/api/v1/rooms" @{ title = "E2E Room $stamp" } $ownerLogin.Session
    Assert-Status $createRoom 200 "Room creation"
    $roomID = [uint64]$createRoom.Json.data.room_id
    $script:StartedRoom = $roomID
    Assert-True ($roomID -gt 0) "Room receives auto-increment room_id" "room_id=$roomID"
    Assert-Status (Invoke-RawApi POST "$BaseUrl/api/v1/rooms" @{ title = "duplicate room" } $ownerLogin.Session) 409 `
        "One-room-per-owner rule"
    $mine = Invoke-RawApi GET "$BaseUrl/api/v1/rooms/mine" $null $ownerLogin.Session
    Assert-Status $mine 200 "Get my room"
    Assert-True ([uint64]$mine.Json.data.room_id -eq $roomID) "My-room ownership is correct"
    $detail = Invoke-RawApi GET "$BaseUrl/api/v1/rooms/$roomID"
    Assert-Status $detail 200 "Room detail"
    Assert-True ([int]$detail.Json.data.status -eq 0) "New room starts offline"
    Test-HandshakeStatus $roomID 403 "Offline room rejects WebSocket upgrade"

    $updatedTitle = "E2E Updated $stamp"
    $ownerUpdate = Invoke-RawApi PATCH "$BaseUrl/api/v1/rooms/$roomID" @{ room_title = $updatedTitle } $ownerLogin.Session
    Assert-Status $ownerUpdate 200 "Owner can update room"
    Assert-True ([string]$ownerUpdate.Json.data.room_title -eq $updatedTitle) "Updated title is returned"
    $otherUpdate = Invoke-RawApi PATCH "$BaseUrl/api/v1/rooms/$roomID" @{ room_title = "forged" } $otherLogin.Session
    if ($otherUpdate.Status -eq 403) {
        Add-Check "Non-owner cannot update room" PASS "status=403"
    }
    elseif ($otherUpdate.Status -eq 500) {
        Add-KnownIssue "Non-owner room update uses HTTP 500 instead of 403" `
            "Permission is enforced, but handler.UpdateRoom maps ErrNotRoomOwner to Internal Server Error."
    }
    else {
        Assert-Status $otherUpdate 403 "Non-owner cannot update room"
    }
    Assert-Status (Invoke-RawApi POST "$BaseUrl/api/v1/rooms/$roomID/start" `
        @{ room_title = $updatedTitle } $otherLogin.Session) 403 "Non-owner cannot start room"
    $start = Invoke-RawApi POST "$BaseUrl/api/v1/rooms/$roomID/start" @{ room_title = $updatedTitle } $ownerLogin.Session
    Assert-Status $start 200 "Owner starts room"
    $script:StartedRoom = $roomID

    $roomList = Invoke-RawApi GET "$BaseUrl/api/v1/rooms"
    Assert-Status $roomList 200 "Live-room list after start"
    Assert-True (@($roomList.Json.data | Where-Object { [uint64]$_.room_id -eq $roomID }).Count -eq 1) `
        "Started room appears in live-room ranking"

    Get-DiagnosticsSnapshot "before-protocol"
    $protocol = Invoke-ProtocolTests $roomID $ownerID $ownerName $ownerLogin.Sid
    Get-DiagnosticsSnapshot "after-protocol"

    if (-not $SkipHistoryChecks) {
        Write-Step "Testing MySQL history pagination and Redis/MySQL replay"
        $messageIDs = @($protocol.message_ids | ForEach-Object { $_.ToString() })
        $historyItems = Wait-HistoryMessages $roomID $messageIDs
        Assert-True ($historyItems.Count -eq $messageIDs.Count) "Kafka persistence consumer writes all E2E danmaku"
        foreach ($messageID in $messageIDs) {
            Assert-True (@($historyItems | Where-Object { $_.message_id -eq $messageID }).Count -eq 1) `
                "MySQL message_id idempotency" "message_id=$messageID stored once"
        }
        Assert-True (@($historyItems | Where-Object {
            [uint64]$_.room_id -eq $roomID -and [uint64]$_.user_id -eq $ownerID -and $_.user_name -eq $ownerName
        }).Count -eq $messageIDs.Count) "Persisted history keeps server-authoritative identity"
        Assert-Status (Invoke-RawApi GET "$BaseUrl/api/v1/rooms/$roomID/messages?limit=not-a-number") 400 `
            "History rejects invalid limit"
        $page = Invoke-RawApi GET "$BaseUrl/api/v1/rooms/$roomID/messages?limit=2"
        Assert-Status $page 200 "History cursor page"
        Assert-True (@($page.Json.data.items).Count -eq 2) "History limit is enforced"
        Assert-True ($null -ne $page.Json.data.has_more) "History includes has_more"

        $firstSeq = [uint64]$protocol.room_sequences[0]
        $replay = Invoke-RawApi GET "$BaseUrl/api/v1/rooms/$roomID/messages/replay?after_seq=$firstSeq&limit=100"
        Assert-Status $replay 200 "Replay after room_seq"
        $replayItems = @($replay.Json.data.items | Where-Object { $messageIDs -contains $_.message_id })
        Assert-True ($replayItems.Count -ge 2) "Replay returns messages after the acknowledged sequence" `
            "source=$($replay.Json.data.source)"
        $lastSeq = $firstSeq
        foreach ($item in $replayItems) {
            Assert-True ([uint64]$item.room_seq -gt $lastSeq) "Replay room_seq remains increasing"
            $lastSeq = [uint64]$item.room_seq
        }
    }
    else {
        Add-Check "History and replay checks" SKIP "-SkipHistoryChecks"
    }

    $afterLikes = Invoke-RawApi GET "$BaseUrl/api/v1/rooms/$roomID"
    if ([int64]$afterLikes.Json.data.like_count -lt [int64]$protocol.like_count) {
        Add-KnownIssue "Room detail/ranking does not expose latest Redis likes" `
            "WebSocket observed like_count=$($protocol.like_count), but MySQL-backed room detail returned $($afterLikes.Json.data.like_count)."
    }

    Assert-Status (Invoke-RawApi POST "$BaseUrl/api/v1/rooms/$roomID/stop" $null $otherLogin.Session) 403 `
        "Non-owner cannot stop room"
    Assert-Status (Invoke-RawApi POST "$BaseUrl/api/v1/rooms/$roomID/stop" $null $ownerLogin.Session) 200 `
        "Owner stops room"
    $script:StartedRoom = 0
    Test-HandshakeStatus $roomID 403 "Stopped room rejects WebSocket upgrade"

    Write-Step "Testing logout and Session deletion"
    Assert-Status (Invoke-RawApi POST "$BaseUrl/api/v1/auth/logout" $null $ownerLogin.Session) 200 "Owner logout"
    Assert-Status (Invoke-RawApi GET "$BaseUrl/api/v1/auth/me" $null $ownerLogin.Session) 401 `
        "Logged-out sid cannot access /auth/me"
    Assert-Status (Invoke-RawApi POST "$BaseUrl/api/v1/auth/logout" $null $otherLogin.Session) 200 "Second-user logout"

    Invoke-LoadProfiles
    Invoke-KafkaBenchmark
}
catch {
    $script:SuiteError = $_
    Add-Check "Suite execution" FAIL $_.Exception.Message
}
finally {
    if ($script:StartedRoom -gt 0 -and $null -ne $script:OwnerSession) {
        try {
            $null = Invoke-RawApi POST "$BaseUrl/api/v1/rooms/$($script:StartedRoom)/stop" $null $script:OwnerSession
        }
        catch { }
    }
    Pop-Location
    Write-TestReport
}

if ($null -ne $script:SuiteError) {
    throw $script:SuiteError
}

Write-Host "`nFULL AUTOMATED TEST PASSED" -ForegroundColor Green
Write-Host "Profile: $Profile"
Write-Host "Checks: $($script:Checks.Count)"
Write-Host "Known issues confirmed: $($script:KnownIssues.Count)" -ForegroundColor Yellow
