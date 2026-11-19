param([Parameter(Mandatory = $true)][string]$Directory)
$ErrorActionPreference = 'Stop'

# Exercise the installed Wintun worker on the disposable build host. A direct
# outbound isolates OS routing/DNS from production nodes and needs no secrets.
$config = @{
    inbounds = @(@{ tag = '26x-tun'; port = 0; protocol = 'tun'; settings = @{ name = '26x-tun'; desc = '26x VPN'; mtu = 1500; gateway = @('10.26.0.1/30', 'fd26:26::1/126') } })
    outbounds = @(@{ tag = '26x-proxy'; protocol = 'freedom' }, @{ tag = 'block'; protocol = 'blackhole' })
    routing = @{ domainStrategy = 'AsIs'; rules = @(@{ type = 'field'; inboundTag = @('26x-tun'); outboundTag = '26x-proxy' }) }
}
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
try {
    $started = $worker.Start()
    $worker.StandardInput.WriteLine(($config | ConvertTo-Json -Depth 12 -Compress))
    $worker.StandardInput.Flush()
    $ready = $worker.StandardOutput.ReadLineAsync()
    if (-not $ready.Wait(35000)) { throw 'Windows TUN startup timed out' }
    if ($ready.Result -ne 'connected') { throw "Windows TUN failed: $($ready.Result)" }
    function Query-TunnelDns([byte]$Type) {
        [byte[]]$query = @(0x26, $Type, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 3, 119, 119, 119, 6, 103, 111, 111, 103, 108, 101, 3, 99, 111, 109, 0, 0, $Type, 0, 1)
        $udp = [Net.Sockets.UdpClient]::new()
        try {
            $udp.Client.ReceiveTimeout = 15000
            $udp.Connect('1.1.1.1', 53)
            [void]$udp.Send($query, $query.Length)
            $peer = [Net.IPEndPoint]::new([Net.IPAddress]::Any, 0)
            $answer = $udp.Receive([ref]$peer)
            if ($answer.Length -lt 12 -or $answer[0] -ne 0x26 -or $answer[1] -ne $Type -or ($answer[3] -band 15) -ne 0) { throw 'Invalid tunnel DNS response' }
            return ([int]$answer[6] * 256 + [int]$answer[7])
        } finally { $udp.Dispose() }
    }
    if ((Query-TunnelDns 1) -lt 1) { throw 'Windows TUN IPv4 DNS returned no addresses' }
    if ((Query-TunnelDns 28) -ne 0) { throw 'Windows TUN returned unsupported IPv6 addresses' }
    if ((Invoke-WebRequest 'https://cp.cloudflare.com/generate_204' -TimeoutSec 15).StatusCode -ne 204) { throw 'Windows TUN HTTPS probe failed' }
} finally {
    if ($started) {
        $worker.StandardInput.Close()
        if (-not $worker.WaitForExit(15000)) { $worker.Kill(); $worker.WaitForExit(); throw 'Windows TUN did not shut down cleanly' }
    }
    $worker.Dispose()
}
$remaining = @(Get-NetRoute -ErrorAction SilentlyContinue | Where-Object { $_.InterfaceAlias -eq '26x-tun' })
if ($remaining.Count -ne 0) { throw 'Windows TUN routes remain after disconnect' }
Write-Host 'Windows Wintun, IPv4 DNS, AAAA suppression, HTTPS and route cleanup passed.'
