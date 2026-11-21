$ErrorActionPreference = 'Stop'
$installer = Join-Path $PSScriptRoot '..\windows\26x-VPN-0.1.11-Setup-x64.exe'
$directory = Join-Path $env:ProgramFiles '26x VPN'
$registry = 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN'
$process = Start-Process -FilePath $installer -ArgumentList '/S' -PassThru -Wait
if ($process.ExitCode -ne 0) { throw "Installer failed: $($process.ExitCode)" }
try {
    foreach ($file in @('twentysixx_client.exe', '26x-tunnel.exe', 'wintun.dll', 'flutter_windows.dll', 'data\app.so', 'direct-ipv4-cidrs.txt', 'Uninstall.exe')) {
        if (-not (Test-Path (Join-Path $directory $file))) { throw "Missing installed file: $file" }
    }
    $entry = Get-ItemProperty $registry
    if ($entry.DisplayVersion -ne '0.1.11') { throw 'Wrong installed version' }
    $shortcut = Join-Path ([Environment]::GetFolderPath('CommonStartMenu')) 'Programs\26x VPN.lnk'
    if (-not (Test-Path $shortcut)) { throw 'Start menu shortcut missing' }
    Add-Type -AssemblyName System.Drawing
    $icon = [System.Drawing.Icon]::ExtractAssociatedIcon((Join-Path $directory 'twentysixx_client.exe'))
    $bitmap = $icon.ToBitmap()
    $bitmap.Save((Join-Path $PSScriptRoot '..\windows\windows-icon-preview.png'), [System.Drawing.Imaging.ImageFormat]::Png)
    if ($bitmap.GetPixel(0, 0).A -ne 0) { throw 'Application icon has an opaque outer corner' }
    $bitmap.Dispose()
    $icon.Dispose()
    $test = Start-Process -FilePath (Join-Path $directory '26x-tunnel.exe') -PassThru -Wait -NoNewWindow -RedirectStandardInput (Join-Path $PSScriptRoot 'invalid-profile.json') -RedirectStandardOutput (Join-Path $env:RUNNER_TEMP '26x-worker-result.txt')
    $reply = (Get-Content (Join-Path $env:RUNNER_TEMP '26x-worker-result.txt') -Raw).Trim()
    if ($test.ExitCode -ne 1 -or $reply -ne 'profile_invalid') { throw "Unexpected worker response: $reply" }
    # The relay stands in for a remote node. Only its outbound uses the runner's
    # uplink, so all client DNS and HTTPS traffic exercises the installed TUN.
    $uplink = (Find-NetRoute -RemoteIPAddress '1.1.1.1' | Select-Object -First 1).InterfaceAlias
    if (-not $uplink) { throw 'Unable to identify the test uplink' }
    $peerId = [guid]::NewGuid()
    $relayConfig = @{
        inbounds = @(25436, 25437, 25438 | ForEach-Object {
            @{ tag = "relay-$_"; listen = '127.0.0.1'; port = $_; protocol = 'vless'; settings = @{ clients = @(@{ id = $peerId.ToString() }); decryption = 'none' } }
        })
        outbounds = @(@{ tag = 'direct'; protocol = 'freedom'; settings = @{ domainStrategy = 'UseIPv4' }; streamSettings = @{ sockopt = @{ interface = $uplink } } }, @{ tag = 'drop'; protocol = 'blackhole' })
        routing = @{ rules = @(@{ type = 'field'; inboundTag = @('relay-25438'); ip = @('1.1.1.1/32','1.0.0.1/32'); port = '443'; outboundTag = 'drop' }) }
    }
    $configPath = Join-Path $env:RUNNER_TEMP '26x-relay.json'
    $relayConfig | ConvertTo-Json -Depth 10 | Set-Content -Encoding utf8NoBOM $configPath
    $relayPath = Join-Path $env:GITHUB_WORKSPACE 'work/26x-test-relay.exe'
    $relay = Start-Process $relayPath -ArgumentList @('run','-c',('"' + $configPath + '"')) -PassThru -NoNewWindow
    try {
        for ($i = 0; $i -lt 40; $i++) {
            if ($relay.HasExited) { throw 'Loopback test node exited' }
            if (Get-NetTCPConnection -LocalPort 25436 -State Listen -ErrorAction SilentlyContinue | Where-Object OwningProcess -eq $relay.Id) { break }
            Start-Sleep -Milliseconds 250
        }
        foreach ($scenario in @('Blocked', 'NoDns', 'Crash', 'FastSwitch', 'Healthy')) {
            & (Join-Path $PSScriptRoot 'test_windows_tunnel.ps1') -Directory $directory -PeerId $peerId -Scenario $scenario
        }
    } finally {
        if (-not $relay.HasExited) { $relay.Kill(); $relay.WaitForExit() }
        $relay.Dispose()
        Remove-Item $configPath
    }
    $signature = Get-AuthenticodeSignature $installer
    $report = [ordered]@{ version = '0.1.11'; build = 27; installation = 'passed'; runtimeAssets = 'passed'; workerInputValidation = 'passed'; postTunnelFailureRollback = 'passed'; dnsFailureRollback = 'passed'; immediateSwitchAfterExit = 'passed'; forcedExitRecovery = 'passed'; nativeTunnelDnsAndRouteCleanup = 'passed'; authenticodeStatus = $signature.Status.ToString(); sha256 = (Get-FileHash $installer -Algorithm SHA256).Hash.ToLower() }
} finally {
    $uninstaller = Join-Path $directory 'Uninstall.exe'
    if (Test-Path $uninstaller) {
        Start-Process -FilePath $uninstaller -ArgumentList '/S' -Wait
        for ($attempt = 0; $attempt -lt 30 -and ((Test-Path $registry) -or (Test-Path (Join-Path $directory 'twentysixx_client.exe'))); $attempt++) { Start-Sleep -Milliseconds 500 }
        if (Test-Path $registry) { throw 'Uninstall registry entry was not removed' }
        if (Test-Path (Join-Path $directory 'twentysixx_client.exe')) { throw 'Installed application was not removed' }
    }
}
$report.uninstallation = 'passed'
$report | ConvertTo-Json | Set-Content (Join-Path $PSScriptRoot '..\windows\windows-package-report.json')
