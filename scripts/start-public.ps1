[CmdletBinding()]
param(
    [ValidateRange(1, 65535)]
    [int]$Port = 8080
)

$ErrorActionPreference = 'Stop'
$tourRoot = Split-Path -Parent $PSScriptRoot
$tourCache = Join-Path $tourRoot '.cache\public'
$tourTools = Join-Path $tourRoot '.cache\tools'
$tourBinary = Join-Path $tourCache 'tour-track.exe'
$tourCloudflared = Join-Path $tourTools 'cloudflared.exe'
$tourVersion = '2026.11.0'
$tourExpectedHash = 'd5c1d82031da6253dcdcff64c944cd0fe063016edeb2e4fd088075fc9bd52d0a'
$tourServer = $null

New-Item -ItemType Directory -Path $tourCache, $tourTools -Force | Out-Null

# Pin and verify the official executable; nothing is installed globally.
$tourDownload = -not (Test-Path -LiteralPath $tourCloudflared)
if (-not $tourDownload) {
    $tourDownload = (Get-FileHash -LiteralPath $tourCloudflared -Algorithm SHA256).Hash.ToLowerInvariant() -ne $tourExpectedHash
}
if ($tourDownload) {
    $tourReleaseURL = "https://github.com/cloudflare/cloudflared/releases/download/$tourVersion/cloudflared-windows-amd64.exe"
    Write-Host "Downloading official cloudflared $tourVersion..."
    Invoke-WebRequest -Uri $tourReleaseURL -OutFile $tourCloudflared -UseBasicParsing -TimeoutSec 120
}
if ((Get-FileHash -LiteralPath $tourCloudflared -Algorithm SHA256).Hash.ToLowerInvariant() -ne $tourExpectedHash) {
    throw 'Cloudflared checksum does not match its official release. The executable was not started.'
}

$tourProbe = [System.Net.Sockets.TcpClient]::new()
try {
    $tourProbe.Connect('127.0.0.1', $Port)
    throw "Port $Port is already in use. Stop the existing server or pass -Port with another number."
} catch [System.Net.Sockets.SocketException] {
    # A refused connection means the port is available for this local server.
} finally {
    $tourProbe.Dispose()
}

$tourPreviousCache = $env:GOCACHE
$tourPreviousAddress = $env:TOUR_TRACK_ADDR
$tourPreviousDemo = $env:TOUR_TRACK_DEMO
try {
    $env:GOCACHE = Join-Path $tourRoot '.cache\go-build'
    Push-Location $tourRoot
    try {
        & go build -trimpath -o $tourBinary .
        if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
    } finally {
        Pop-Location
    }

    # Bind only to loopback. Cloudflare forwards public HTTP to this one app.
    $env:TOUR_TRACK_ADDR = "127.0.0.1:$Port"
    $env:TOUR_TRACK_DEMO = '0'
    $tourServer = Start-Process -FilePath $tourBinary -WorkingDirectory $tourRoot -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $tourCache 'server.stdout.log') `
        -RedirectStandardError (Join-Path $tourCache 'server.stderr.log')
    $env:TOUR_TRACK_ADDR = $tourPreviousAddress
    $env:TOUR_TRACK_DEMO = $tourPreviousDemo

    $tourReady = $false
    for ($tourAttempt = 0; $tourAttempt -lt 30; $tourAttempt++) {
        if ($tourServer.HasExited) { throw 'The Go server exited. Check .cache/public/server.stderr.log.' }
        try {
            $tourHealth = Invoke-WebRequest -Uri "http://127.0.0.1:$Port/health" -UseBasicParsing -TimeoutSec 1
            if ($tourHealth.StatusCode -eq 200) { $tourReady = $true; break }
        } catch { }
        Start-Sleep -Milliseconds 200
    }
    if (-not $tourReady) { throw 'The local server did not become ready.' }

    Write-Host "Local server: http://127.0.0.1:$Port"
    Write-Host 'The public HTTPS URL appears below. Keep this terminal open; Ctrl+C stops the tunnel and server.'
    # An explicit local config prevents accidentally using a personal tunnel config.
    $tourConfig = Join-Path $tourCache 'cloudflared.yml'
    [System.IO.File]::WriteAllText($tourConfig, "no-autoupdate: true`n")
    & $tourCloudflared tunnel --config $tourConfig --no-autoupdate --protocol http2 --url "http://127.0.0.1:$Port"
    if ($LASTEXITCODE -ne 0) { throw "Cloudflare Tunnel exited with code $LASTEXITCODE." }
} finally {
    if ($null -ne $tourServer -and -not $tourServer.HasExited) {
        Stop-Process -Id $tourServer.Id -ErrorAction SilentlyContinue
    }
    $env:GOCACHE = $tourPreviousCache
    $env:TOUR_TRACK_ADDR = $tourPreviousAddress
    $env:TOUR_TRACK_DEMO = $tourPreviousDemo
}
