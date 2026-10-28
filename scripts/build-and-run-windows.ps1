param(
    [string]$Workspace = (Join-Path ([Environment]::GetFolderPath('MyDocuments')) 'SiYuan-v2'),
    [switch]$BuildOnly,
    [switch]$LaunchOnly,
    [switch]$ReinstallDependencies
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$ProgressPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$appRoot = Join-Path $projectRoot 'app'
$outputRoot = Join-Path $projectRoot '.cache\windows-desktop'
$runtimeRoot = Join-Path $outputRoot 'runtime'
$toolRoot = Join-Path $env:USERPROFILE '.cache\siyuan-toolchain'
$downloadRoot = Join-Path $toolRoot 'downloads'
$logFile = Join-Path $outputRoot 'build.log'
$transcriptStarted = $false
$buildLock = $null

function Invoke-Checked {
    param([string]$Executable, [string[]]$Arguments)
    $previousPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        & $Executable @Arguments 2>&1 | ForEach-Object { Write-Host $_ }
        $commandExitCode = $LASTEXITCODE
    } finally { $ErrorActionPreference = $previousPreference }
    if ($commandExitCode -ne 0) { throw "Command failed ($commandExitCode): $Executable" }
}

function Save-VerifiedDownload {
    param([string]$Url, [string]$Destination, [string]$Hash, [string]$Algorithm = 'SHA256')
    if (Test-Path -LiteralPath $Destination) {
        if ((Get-ContentHash $Destination $Algorithm) -eq $Hash) { return }
    }
    Write-Host "Downloading $Url"
    Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile "$Destination.partial" -TimeoutSec 600
    if ((Get-ContentHash "$Destination.partial" $Algorithm) -ne $Hash) {
        throw "Download checksum mismatch: $Url"
    }
    Move-Item -LiteralPath "$Destination.partial" -Destination $Destination -Force
}

function Get-ContentHash {
    param([string]$Path, [string]$Algorithm = 'SHA256')
    $absolute = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Path)
    $stream = [IO.File]::OpenRead($absolute)
    $hasher = [Security.Cryptography.HashAlgorithm]::Create($Algorithm)
    try { return ([BitConverter]::ToString($hasher.ComputeHash($stream))).Replace('-', '') }
    finally { $hasher.Dispose(); $stream.Dispose() }
}

function Copy-RuntimeDirectory {
    param([string]$Name)
    $source = Join-Path $appRoot $Name
    $destination = Join-Path $runtimeRoot $Name
    if (-not (Test-Path -LiteralPath $source)) { return }
    $arguments = @($source, $destination, '/E', '/R:1', '/W:1', '/NFL', '/NDL', '/NJH', '/NJS', '/NP')
    if ($Name -eq 'stage') { $arguments += @('/XD', (Join-Path $source 'build')) }
    & robocopy.exe @arguments
    if ($LASTEXITCODE -ge 8) { throw "Cannot copy runtime directory: $Name" }
}

function Install-RegistryArchive {
    param([string]$Name, [string]$Version, [string]$Destination)
    $encodedName = [Uri]::EscapeDataString($Name)
    $metadata = Invoke-RestMethod "https://registry.npmjs.org/$encodedName/$Version"
    $integrity = $metadata.dist.integrity -split '-', 2
    if ($integrity[0] -ne 'sha512') { throw 'Unexpected package checksum algorithm' }
    $hash = ([BitConverter]::ToString([Convert]::FromBase64String($integrity[1]))).Replace('-', '')
    $archive = Join-Path $downloadRoot (($Name -replace '[@/]', '_') + "-$Version.tgz")
    Save-VerifiedDownload $metadata.dist.tarball $archive $hash 'SHA512'
    New-Item -ItemType Directory -Path $Destination -Force | Out-Null
    Invoke-Checked 'tar.exe' @('-xzf', $archive, '-C', $Destination)
}

function Get-NodeExecutable {
    $candidates = @()
    $installed = Get-Command node.exe -ErrorAction SilentlyContinue
    if ($installed) { $candidates += $installed.Source }
    $candidates += Join-Path $env:USERPROFILE '.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe'
    if (Test-Path -LiteralPath $toolRoot) {
        $candidates += @(Get-ChildItem -LiteralPath $toolRoot -Directory -Filter 'node-v24.*-win-x64' |
            Sort-Object LastWriteTime -Descending | ForEach-Object { Join-Path $_.FullName 'node.exe' })
    }
    foreach ($candidate in $candidates) {
        if ((Test-Path -LiteralPath $candidate) -and ((& $candidate --version) -match '^v24\.')) { return $candidate }
    }
    $release = @(Invoke-RestMethod 'https://nodejs.org/dist/index.json') | Where-Object { $_.version -match '^v24\.' } | Select-Object -First 1
    if (-not $release) { throw 'Cannot find Node.js 24 release' }
    $version = $release.version
    $name = "node-$version-win-x64"
    $nodePath = Join-Path $toolRoot "$name\node.exe"
    if (-not (Test-Path -LiteralPath $nodePath)) {
        $checksums = (Invoke-WebRequest -UseBasicParsing "https://nodejs.org/dist/$version/SHASUMS256.txt").Content
        $line = $checksums -split "`n" | Where-Object { $_ -match " $([regex]::Escape($name)).zip\s*$" } | Select-Object -First 1
        if (-not $line) { throw 'Node.js archive checksum missing' }
        $archive = Join-Path $downloadRoot "$name.zip"
        Save-VerifiedDownload "https://nodejs.org/dist/$version/$name.zip" $archive (($line -split '\s+')[0])
        Expand-Archive -LiteralPath $archive -DestinationPath $toolRoot -Force
    }
    return $nodePath
}

function Get-GoExecutable {
    $version = [regex]::Match((Get-Content -LiteralPath (Join-Path $projectRoot 'kernel\go.mod') -Raw), '(?m)^go (\S+)').Groups[1].Value
    $candidates = @((Join-Path $toolRoot "go-$version\go\bin\go.exe"), (Join-Path $toolRoot 'go\bin\go.exe'))
    $installed = Get-Command go.exe -ErrorAction SilentlyContinue
    if ($installed) { $candidates += $installed.Source }
    foreach ($candidate in $candidates) {
        if ((Test-Path -LiteralPath $candidate) -and ((& $candidate version) -match "go$([regex]::Escape($version))\s")) { return $candidate }
    }
    $releases = Invoke-RestMethod 'https://go.dev/dl/?mode=json&include=all'
    $release = $releases | Where-Object { $_.version -eq "go$version" } | Select-Object -First 1
    if (-not $release) { throw "Go $version release not found" }
    $file = $release.files | Where-Object { $_.os -eq 'windows' -and $_.arch -eq 'amd64' -and $_.kind -eq 'archive' } | Select-Object -First 1
    if (-not $file) { throw 'Go Windows archive not found' }
    $archive = Join-Path $downloadRoot $file.filename
    Save-VerifiedDownload "https://go.dev/dl/$($file.filename)" $archive $file.sha256
    $destination = Join-Path $toolRoot "go-$version"
    Expand-Archive -LiteralPath $archive -DestinationPath $destination -Force
    return (Join-Path $destination 'go\bin\go.exe')
}

function Get-ZigExecutable {
    $name = 'zig-x86_64-windows-0.15.2'
    $executable = Join-Path $toolRoot "$name\zig.exe"
    if (-not (Test-Path -LiteralPath $executable)) {
        $index = Invoke-RestMethod 'https://ziglang.org/download/index.json'
        $release = $index.'0.15.2'.'x86_64-windows'
        $archive = Join-Path $downloadRoot "$name.zip"
        Save-VerifiedDownload $release.tarball $archive $release.shasum
        Expand-Archive -LiteralPath $archive -DestinationPath $toolRoot -Force
    }
    return $executable
}

try {
    if (-not [Environment]::Is64BitOperatingSystem -or $env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') {
        throw 'This launcher supports Windows x64 only'
    }
    if ($BuildOnly -and $LaunchOnly) { throw 'BuildOnly and LaunchOnly cannot be combined' }
    New-Item -ItemType Directory -Path $outputRoot, $downloadRoot -Force | Out-Null
    # 只锁定本构建目录，不终止任何正在运行的客户端或内核。
    $buildLock = [IO.File]::Open((Join-Path $outputRoot 'build.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
    Start-Transcript -LiteralPath $logFile -Force | Out-Null
    $transcriptStarted = $true
    $Workspace = [IO.Path]::GetFullPath($Workspace)
    Write-Host "Project: $projectRoot"
    Write-Host "Workspace: $Workspace"
    $node = Get-NodeExecutable
    $env:PATH = "$(Split-Path -Parent $node);$env:PATH"

    if (-not $LaunchOnly) {
        $running = @(Get-CimInstance Win32_Process -Filter "Name='SiYuan-Kernel.exe' OR Name='electron.exe'" | Where-Object {
            ($_.ExecutablePath -and $_.ExecutablePath.StartsWith($runtimeRoot, [StringComparison]::OrdinalIgnoreCase)) -or
            ($_.CommandLine -and $_.CommandLine.IndexOf($runtimeRoot, [StringComparison]::OrdinalIgnoreCase) -ge 0)
        })
        if ($running.Count -gt 0) { throw 'Close the client launched by this script before rebuilding. You can use -LaunchOnly to open it again.' }
        $go = Get-GoExecutable
        $zig = Get-ZigExecutable
        $package = Get-Content -LiteralPath (Join-Path $appRoot 'package.json') -Raw | ConvertFrom-Json
        $pnpmVersion = $package.packageManager -replace '^pnpm@', ''
        $pnpmDirectory = Join-Path $toolRoot "pnpm-$pnpmVersion"
        if (-not (Test-Path -LiteralPath (Join-Path $pnpmDirectory 'package\package.json'))) {
            Install-RegistryArchive 'pnpm' $pnpmVersion $pnpmDirectory
        }
        $pnpm = Join-Path $pnpmDirectory 'package\pnpm-native.exe'
        if (-not (Test-Path -LiteralPath $pnpm)) {
            $nativeDirectory = Join-Path $pnpmDirectory 'native'
            Install-RegistryArchive '@pnpm/exe.win32-x64' $pnpmVersion $nativeDirectory
            $binary = Get-ChildItem -LiteralPath (Join-Path $nativeDirectory 'package') -Filter 'pnpm.exe' -Recurse | Select-Object -First 1
            if (-not $binary) { throw 'pnpm Windows executable missing from package' }
            Copy-Item -LiteralPath $binary.FullName -Destination $pnpm -Force
        }
        Write-Host 'Installing frontend dependencies...'
        Push-Location $appRoot
        try {
            if (-not $env:ELECTRON_MIRROR) { $env:ELECTRON_MIRROR = 'https://npmmirror.com/mirrors/electron/' }
            $fingerprint = (Get-ContentHash 'package.json') + (Get-ContentHash 'pnpm-lock.yaml') + $pnpmVersion
            $dependencyStamp = Join-Path $outputRoot 'dependencies.sha256'
            $reuseDependencies = -not $ReinstallDependencies -and (Test-Path -LiteralPath $dependencyStamp) -and
                (Test-Path -LiteralPath 'node_modules\webpack\package.json') -and
                (Test-Path -LiteralPath 'node_modules\electron\package.json') -and
                ((Get-Content -LiteralPath $dependencyStamp -Raw).Trim() -eq $fingerprint)
            if (-not $reuseDependencies) {
                Invoke-Checked $pnpm @('install', '--frozen-lockfile')
                Set-Content -LiteralPath $dependencyStamp -Value $fingerprint -Encoding ASCII
            } else { Write-Host 'Dependency lockfile unchanged; reusing installed packages.' }
            Invoke-Checked $node @('node_modules\electron\install.js')
        } finally { Pop-Location }

        New-Item -ItemType Directory -Path $runtimeRoot -Force | Out-Null
        foreach ($name in @('appearance', 'stage', 'guide', 'pandoc', 'changelogs')) { Copy-RuntimeDirectory $name }
        $runtimeApp = Join-Path $runtimeRoot 'app'
        New-Item -ItemType Directory -Path $runtimeApp -Force | Out-Null
        & robocopy.exe (Join-Path $appRoot 'electron') (Join-Path $runtimeApp 'electron') /E /R:1 /W:1 /NFL /NDL /NJH /NJS /NP
        if ($LASTEXITCODE -ge 8) { throw 'Cannot copy Electron application' }
        # 使用独立应用标识，避免已有官方客户端接管本次启动请求。
        $runtimePackage = Get-Content -LiteralPath (Join-Path $appRoot 'package.json') -Raw | ConvertFrom-Json
        $runtimePackage.name = 'SiYuan-v2'
        $runtimePackage.desktopName = 'org.b3log.siyuan.v2'
        [IO.File]::WriteAllText((Join-Path $runtimeApp 'package.json'), ($runtimePackage | ConvertTo-Json -Depth 20), [Text.UTF8Encoding]::new($false))
        $modules = Join-Path $runtimeApp 'node_modules'
        if (-not (Test-Path -LiteralPath $modules)) {
            New-Item -ItemType Junction -Path $modules -Target (Join-Path $appRoot 'node_modules') | Out-Null
        }
        Invoke-Checked $node @((Join-Path $PSScriptRoot 'build-desktop-ui.cjs'), $runtimeRoot)

        Write-Host 'Building Windows kernel (first build can take several minutes)...'
        $env:CGO_ENABLED = '1'
        $env:GOOS = 'windows'
        $env:GOARCH = 'amd64'
        $env:CC = '"' + $zig + '" cc'
        if (-not $env:GOPROXY) { $env:GOPROXY = 'https://goproxy.cn,direct' }
        $kernelDirectory = Join-Path $runtimeRoot 'kernel'
        New-Item -ItemType Directory -Path $kernelDirectory -Force | Out-Null
        Push-Location (Join-Path $projectRoot 'kernel')
        try {
            Invoke-Checked $go @('build', '-tags', 'fts5 sqlcipher', '-o', (Join-Path $kernelDirectory 'SiYuan-Kernel.exe'), '-ldflags', '-s -w', '.')
        } finally { Pop-Location }
        Copy-Item -LiteralPath (Join-Path $appRoot 'elevator\elevator-amd64.exe') -Destination (Join-Path $kernelDirectory 'elevator.exe') -Force
        Write-Host 'Build complete.'
    }

    if (-not $BuildOnly) {
        $kernel = Join-Path $runtimeRoot 'kernel\SiYuan-Kernel.exe'
        $html = Join-Path $runtimeRoot 'stage\build\app\index.html'
        if (-not (Test-Path -LiteralPath $kernel) -or -not (Test-Path -LiteralPath $html)) { throw 'Build output missing. Run without -LaunchOnly first.' }
        Push-Location $appRoot
        try { $electron = & $node -p "require('electron')" } finally { Pop-Location }
        if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $electron)) { throw 'Electron runtime missing' }
        # 作为正式布局启动，让 Electron 管理独立内核和工作空间的生命周期。
        $env:NODE_ENV = 'production'
        Remove-Item Env:ELECTRON_RUN_AS_NODE -ErrorAction SilentlyContinue
        New-Item -ItemType Directory -Path $Workspace -Force | Out-Null
        $profileDirectory = Join-Path $env:USERPROFILE '.config\siyuan'
        $workspaceList = Join-Path $profileDirectory 'workspace.json'
        if (-not (Test-Path -LiteralPath $workspaceList)) {
            # 仅初始化不存在的工作空间列表，不覆盖已有用户配置。
            New-Item -ItemType Directory -Path $profileDirectory -Force | Out-Null
            $profileStream = $null
            try {
                $profileStream = [IO.File]::Open($workspaceList, 'CreateNew', 'Write', 'None')
                $bytes = [Text.Encoding]::UTF8.GetBytes((ConvertTo-Json -InputObject @($Workspace)))
                $profileStream.Write($bytes, 0, $bytes.Length)
            } catch [IO.IOException] {
                if ($profileStream -or -not (Test-Path -LiteralPath $workspaceList)) { throw }
            } finally { if ($profileStream) { $profileStream.Dispose() } }
        }
        $launchTag = [DateTime]::Now.ToString('yyyyMMdd-HHmmssfff')
        Start-Process -FilePath $electron -ArgumentList @(('"' + (Join-Path $runtimeRoot 'app') + '"'), ('"--workspace=' + $Workspace + '"')) -WorkingDirectory $runtimeRoot -WindowStyle Normal -RedirectStandardOutput (Join-Path $outputRoot "electron-$launchTag.log") -RedirectStandardError (Join-Path $outputRoot "electron-error-$launchTag.log") | Out-Null
        Write-Host 'SiYuan started.'
    }
} catch {
    Write-Host "ERROR: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
} finally {
    if ($transcriptStarted) { Stop-Transcript | Out-Null }
    if ($buildLock) { $buildLock.Dispose() }
}
