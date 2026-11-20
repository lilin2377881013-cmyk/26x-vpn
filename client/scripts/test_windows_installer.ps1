$ErrorActionPreference = 'Stop'
$installer = Join-Path $PSScriptRoot '..\windows\26x-VPN-0.1.8-Setup-x64.exe'
$directory = Join-Path $env:ProgramFiles '26x VPN'
$registry = 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN'
$process = Start-Process -FilePath $installer -ArgumentList '/S' -PassThru -Wait
if ($process.ExitCode -ne 0) { throw "Installer failed: $($process.ExitCode)" }
try {
    foreach ($file in @('twentysixx_client.exe', '26x-tunnel.exe', 'wintun.dll', 'flutter_windows.dll', 'data\app.so', 'direct-ipv4-cidrs.txt', 'Uninstall.exe')) {
        if (-not (Test-Path (Join-Path $directory $file))) { throw "Missing installed file: $file" }
    }
    $entry = Get-ItemProperty $registry
    if ($entry.DisplayVersion -ne '0.1.8') { throw 'Wrong installed version' }
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
    & (Join-Path $PSScriptRoot 'test_windows_tunnel.ps1') -Directory $directory
    $signature = Get-AuthenticodeSignature $installer
    $report = [ordered]@{ version = '0.1.8'; build = 24; installation = 'passed'; runtimeAssets = 'passed'; workerInputValidation = 'passed'; nativeTunnelDnsAndRouteCleanup = 'passed'; authenticodeStatus = $signature.Status.ToString(); sha256 = (Get-FileHash $installer -Algorithm SHA256).Hash.ToLower() }
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
