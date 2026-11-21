param([Parameter(Mandatory = $true)][string]$Directory, [Parameter(Mandatory = $true)][guid]$PeerId, [ValidateSet('Healthy','Blocked','NoDns','Crash','FastSwitch')][string]$Scenario = 'Healthy')
$ErrorActionPreference = 'Stop'

# Connect the installed worker to a loopback-only test node through VLESS.
$config = @{
    inbounds = @(@{ tag = '26x-tun'; port = 0; protocol = 'tun'; settings = @{ name = '26x-tun'; desc = '26x VPN'; mtu = 1500; gateway = @('10.26.0.1/30', 'fd26:26::1/126') } })
    outbounds = @(@{ tag = '26x-proxy'; protocol = 'vless'; settings = @{ address = '127.0.0.1'; port = 25436; id = $PeerId.ToString(); encryption = 'none' } }, @{ tag = 'block'; protocol = 'blackhole' })
    routing = @{ domainStrategy = 'AsIs'; rules = @(@{ type = 'field'; inboundTag = @('26x-tun'); outboundTag = '26x-proxy' }) }
}
if ($Scenario -eq 'Blocked') {
    # Leave outbound preflight and tunneled DNS working, then drop HTTPS from
    # ordinary OS sockets. Startup must roll back without a disconnect command.
    $config.routing.rules[0].outboundTag = 'block'
}
if ($Scenario -eq 'NoDns') { $config.outbounds[0].settings.port = 25438 }
function Get-UplinkDns {
    $routes = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0'
    return @($routes | Sort-Object InterfaceIndex -Unique | ForEach-Object {
        $dns = Get-DnsClientServerAddress -InterfaceIndex $_.InterfaceIndex -AddressFamily IPv4
        "$($_.InterfaceIndex):$($_.NextHop):$($dns.ServerAddresses -join ',')"
    }) -join '|'
}
$before = Get-UplinkDns
if (-not $before) { throw 'No IPv4 uplink for this test' }
$start = [Diagnostics.ProcessStartInfo]::new((Join-Path $Directory '26x-tunnel.exe'))
$start.WorkingDirectory = $Directory
$start.UseShellExecute = $false
$start.CreateNoWindow = $true
$start.RedirectStandardInput = $true
$start.RedirectStandardOutput = $true
$start.RedirectStandardError = $true
$worker = [Diagnostics.Process]::new()
$worker.StartInfo = $start
$started = $false
$diagnostics = $null
$tailOutput = $null
try {
    $started = $worker.Start()
    $diagnostics = $worker.StandardError.ReadToEndAsync()
    $worker.StandardInput.WriteLine(($config | ConvertTo-Json -Depth 12 -Compress))
    $worker.StandardInput.Flush()
    $deadline = [DateTime]::UtcNow.AddSeconds(55)
    while ($true) {
        $remaining = [int]($deadline - [DateTime]::UtcNow).TotalMilliseconds
        $ready = $worker.StandardOutput.ReadLineAsync()
        if ($remaining -le 0 -or -not $ready.Wait($remaining)) { throw 'Windows TUN startup timed out' }
        $line = $ready.Result
        if ($Scenario -eq 'Blocked' -and $line -eq 'tunnel_data_failed') { break }
        if ($Scenario -eq 'NoDns' -and $line -eq 'tunnel_dns_failed') { break }
        if ($line -eq 'connected') {
            if ($Scenario -eq 'NoDns') { throw 'Worker reported connected despite blocked DNS' }
            if ($Scenario -eq 'Blocked') { throw 'Worker reported connected despite blocked HTTPS' }
            break
        }
        if ($null -eq $line -or $line -in @('vpn_permission_denied', 'engine_unavailable', 'profile_invalid', 'node_unreachable', 'tunnel_start_failed')) { throw "Windows TUN failed: $line" }
        Write-Host "Worker startup: $line"
    }
    $tailOutput = $worker.StandardOutput.ReadToEndAsync()
    if ($Scenario -in @('Blocked', 'NoDns')) {
        if (-not $worker.WaitForExit(12000) -or $worker.ExitCode -ne 1) { throw 'Broken data path did not roll back' }
    } elseif ($Scenario -eq 'FastSwitch') {
        $worker.Kill()
        if (-not $worker.WaitForExit(10000)) { throw 'First worker did not terminate' }
        $worker.Dispose()
        $worker = [Diagnostics.Process]::new()
        $worker.StartInfo = $start
        $started = $worker.Start()
        $diagnostics = $worker.StandardError.ReadToEndAsync()
        $config.outbounds[0].settings.port = 25437
        $worker.StandardInput.WriteLine(($config | ConvertTo-Json -Depth 12 -Compress))
        $worker.StandardInput.Flush()
        $ready = $worker.StandardOutput.ReadLineAsync()
        if (-not $ready.Wait(42000) -or $ready.Result -ne 'connected') { throw 'Immediate switch to the second endpoint failed' }
        $tailOutput = $worker.StandardOutput.ReadToEndAsync()
    } elseif ($Scenario -eq 'Crash') {
        $worker.Kill()
        if (-not $worker.WaitForExit(10000)) { throw 'Worker did not terminate' }
    } else {
        Write-Host 'Installed Windows worker connected.'
        function Query-TunnelDns([byte]$Type) {
            [byte[]]$query = @(0x26, $Type, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 3, 119, 119, 119, 6, 103, 111, 111, 103, 108, 101, 3, 99, 111, 109, 0, 0, $Type, 0, 1)
            $udp = [Net.Sockets.UdpClient]::new()
            try {
                $udp.Client.ReceiveTimeout = 15000
                $udp.Connect('10.26.0.2', 53)
                [void]$udp.Send($query, $query.Length)
                $peer = [Net.IPEndPoint]::new([Net.IPAddress]::Any, 0)
                $answer = $udp.Receive([ref]$peer)
                if ($answer.Length -lt 12 -or $answer[0] -ne 0x26 -or $answer[1] -ne $Type -or ($answer[3] -band 15) -ne 0) { throw 'Invalid tunnel DNS response' }
                return ([int]$answer[6] * 256 + [int]$answer[7])
            } finally { $udp.Dispose() }
        }
        $dnsRoute = Get-NetRoute -DestinationPrefix '10.26.0.2/32' -InterfaceAlias '26x-tun' -ErrorAction Stop
        if (-not $dnsRoute) { throw 'Missing tunnel DNS host route' }
        $a = Query-TunnelDns 1
        $aaaa = Query-TunnelDns 28
        Write-Host "Windows DNS answers: A=$a AAAA=$aaaa"
        if ($a -lt 1) { throw 'Windows TUN IPv4 DNS returned no addresses' }
        if ($aaaa -ne 0) { throw 'Windows TUN returned unsupported IPv6 addresses' }
        if ((Invoke-WebRequest 'https://cp.cloudflare.com/generate_204' -TimeoutSec 15).StatusCode -ne 204) { throw 'Windows TUN HTTPS probe failed' }
    }
} catch {
    Write-Host "Windows tunnel verification error: $_"
    throw
} finally {
    if ($started -and -not $worker.HasExited) {
        $worker.StandardInput.Close()
        if (-not $worker.WaitForExit(15000)) {
            $worker.Kill(); $worker.WaitForExit()
            if ($diagnostics) { Write-Host $diagnostics.GetAwaiter().GetResult() }
            throw 'Windows TUN did not shut down cleanly'
        }
        if ($diagnostics) { Write-Host $diagnostics.GetAwaiter().GetResult() }
    }
    $worker.Dispose()
}
# The OS may enumerate the just-removed Wintun device for another moment.
for ($n = 0; $n -lt 20; $n++) {
    $remaining = @(Get-NetRoute -InterfaceAlias '26x-tun' -ErrorAction SilentlyContinue)
    $adapter = @(Get-NetAdapter -Name '26x-tun' -ErrorAction SilentlyContinue | Where-Object Status -eq Up)
    if ($remaining.Count -eq 0 -and $adapter.Count -eq 0) { break }
    Start-Sleep -Milliseconds 250
}
if ($remaining.Count -or $adapter.Count) { throw "Tunnel routes or active adapter remained after $Scenario" }
if ((Get-UplinkDns) -ne $before) { throw "Uplink gateway or DNS changed after $Scenario" }
$answer = Resolve-DnsName www.github.com -Type A -DnsOnly -ErrorAction Stop
if (-not ($answer | Where-Object Type -eq A)) { throw "No IPv4 DNS after $Scenario" }
if ((Invoke-WebRequest 'https://cp.cloudflare.com/generate_204' -NoProxy -TimeoutSec 15).StatusCode -ne 204) { throw "No HTTPS after $Scenario" }
Write-Host "Windows $Scenario passed. Normal gateway, DNS and HTTPS recovered without reboot."
