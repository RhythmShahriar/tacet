# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT

# Installs tacet on Windows from GitHub Releases:
#   irm https://rythm.sh/tacet/install.ps1 | iex
#
# Environment:
#   TACET_INSTALL_DIR     where to install (default %LOCALAPPDATA%\Programs\tacet)
#   TACET_VERSION         a specific version, e.g. 0.1.0 (default: latest)
#   TACET_SKIP_SIGNATURE  1 skips the cosign check, for a mirror you trust
#   TACET_GITHUB_API      GitHub API base, for mirrors (default https://api.github.com).
#                         It must be https; plain http is accepted only for
#                         127.0.0.1, ::1 and localhost.
#   GITHUB_TOKEN          sent to api.github.com only, to lift the anonymous rate limit
#   TACET_INSTALL_TIMEOUT seconds to wait for a server to answer (default 30, at most 3600)
#
# Every download is checked against the release's checksums.txt. When
# cosign is installed, checksums.txt itself must carry a Sigstore
# signature from tacet's release workflow for that tag.
#
# Everything is in one script block, closed on the last line, so a
# download cut off partway is a parse error instead of a partial install.
# The block also keeps its variables out of the session that ran iex.
& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest is far slower with the progress bar
    # Windows PowerShell 5.1 on an older .NET Framework may offer only TLS
    # 1.0 and 1.1, which GitHub refuses.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $Repo = 'RhythmShahriar/tacet'
    # The shared workflow that signs every release. Its identity alone would
    # accept any repository that calls it, so the repository and tag are
    # pinned as well (see scripts/verify-release.sh).
    $Workflow = 'RhythmShahriar/.github/.github/workflows/release-go.yml'
    $Issuer = 'https://token.actions.githubusercontent.com'
    $SkipSignature = $env:TACET_SKIP_SIGNATURE -and $env:TACET_SKIP_SIGNATURE -ne '0'
    $Api = if ($env:TACET_GITHUB_API) { $env:TACET_GITHUB_API.TrimEnd('/') } else { 'https://api.github.com' }
    $Dir = if ($env:TACET_INSTALL_DIR) { $env:TACET_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\tacet' }

    function Test-Loopback([Uri]$Uri) {
        @('127.0.0.1', '::1', 'localhost') -contains $Uri.Host.Trim('[', ']').ToLowerInvariant()
    }

    # Assert-Https refuses a download URL that isn't https. Plain http
    # passes only when both the API base and the URL are on loopback.
    function Assert-Https([string]$Url) {
        $Uri = $null
        if ([Uri]::TryCreate($Url, [UriKind]::Absolute, [ref]$Uri)) {
            if ($Uri.Scheme -eq 'https') { return }
            if ($Uri.Scheme -eq 'http' -and $ApiIsLoopback -and (Test-Loopback $Uri)) { return }
        }
        throw "tacet install: refusing to download over http: $Url"
    }

    if ($Api -match '[@?#\\]') {
        throw "tacet install: TACET_GITHUB_API must be a plain base URL, without user info, query or fragment: $Api"
    }
    $ApiUri = $null
    if (-not [Uri]::TryCreate($Api, [UriKind]::Absolute, [ref]$ApiUri) -or ($ApiUri.Scheme -ne 'https' -and $ApiUri.Scheme -ne 'http')) {
        throw "tacet install: TACET_GITHUB_API must be an https URL: $Api"
    }
    $ApiIsLoopback = Test-Loopback $ApiUri
    if ($ApiUri.Scheme -eq 'http' -and -not $ApiIsLoopback) {
        throw "tacet install: TACET_GITHUB_API must be https (http only for 127.0.0.1, ::1 and localhost): $Api"
    }

    $Headers = @{ Accept = 'application/vnd.github+json'; 'User-Agent' = 'tacet-install' }
    # GITHUB_TOKEN belongs to github.com, so it never goes to a mirror.
    if ($env:GITHUB_TOKEN -and $ApiUri.Scheme -eq 'https' -and $ApiUri.Host -eq 'api.github.com') {
        $Headers.Authorization = "Bearer $env:GITHUB_TOKEN"
    }

    $Timeout = if ($env:TACET_INSTALL_TIMEOUT) { $env:TACET_INSTALL_TIMEOUT } else { '30' }
    $Secs = 0
    if ($Timeout -notmatch '^[0-9]+$' -or ($Timeout -match '^0+$')) {
        throw "tacet install: TACET_INSTALL_TIMEOUT must be a whole number of seconds: '$Timeout'"
    }
    if (-not [int]::TryParse($Timeout, [ref]$Secs) -or $Secs -gt 3600) {
        throw "tacet install: TACET_INSTALL_TIMEOUT must be at most 3600 seconds: '$Timeout'"
    }
    $Timeout = $Secs

    # 32-bit PowerShell on 64-bit Windows runs under WOW64, where
    # PROCESSOR_ARCHITECTURE says x86 and PROCESSOR_ARCHITEW6432 names the
    # real CPU.
    $Cpu = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    $Arch = switch ($Cpu) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { throw "tacet install: unsupported CPU '$Cpu'" }
    }

    # The tag the signature must name: v, a digit, then only [0-9A-Za-z.+-].
    $TagPattern = '^v[0-9][0-9A-Za-z.+-]*$'
    if ($env:TACET_VERSION) {
        $Tag = "v$($env:TACET_VERSION -creplace '^v', '')"
        if ($Tag -cnotmatch $TagPattern) { throw "tacet install: TACET_VERSION isn't a version: '$env:TACET_VERSION'" }
        $ReleaseUrl = "$Api/repos/$Repo/releases/tags/$Tag"
    }
    else {
        $ReleaseUrl = "$Api/repos/$Repo/releases/latest"
    }
    # -UseBasicParsing keeps Windows PowerShell 5.1 off the Internet
    # Explorer engine. pwsh ignores it.
    try {
        $Resp = Invoke-WebRequest -Uri $ReleaseUrl -Headers $Headers -UseBasicParsing -TimeoutSec $Timeout
        $Body = $Resp.Content
        if ($Body -is [byte[]]) { $Body = [Text.Encoding]::UTF8.GetString($Body) }
        $Release = $Body | ConvertFrom-Json
    }
    catch {
        $Status = 0
        if ($_.Exception.Response) { $Status = [int]$_.Exception.Response.StatusCode }
        if ($Status -eq 403 -or $Status -eq 429) {
            if ($Headers.Authorization -or $ApiUri.Host -ne 'api.github.com') { throw "tacet install: rate limit reached at $($ApiUri.Host)" }
            throw 'tacet install: GitHub rate limit reached; set GITHUB_TOKEN'
        }
        if ($Status -eq 404) { throw "tacet install: no such release: $ReleaseUrl" }
        if ($Status) { throw "tacet install: couldn't fetch the release ($ReleaseUrl): HTTP $Status" }
        throw "tacet install: couldn't reach $($ApiUri.Host) ($ReleaseUrl): $($_.Exception.Message)"
    }
    if (-not $env:TACET_VERSION) {
        $Tag = "$($Release.tag_name)"
        if ($Tag -cnotmatch $TagPattern) { throw "tacet install: the release has an odd tag: '$Tag'" }
    }

    $Name = "tacet_windows_$Arch.zip"
    $Archive = $Release.assets | Where-Object { $_.name -eq $Name }
    $Sums = $Release.assets | Where-Object { $_.name -eq 'checksums.txt' }
    $Bundle = $Release.assets | Where-Object { $_.name -eq 'checksums.txt.sigstore.json' }
    if (-not $Archive) { throw "tacet install: the release has no build for windows/$Arch" }
    if (-not $Sums) { throw 'tacet install: the release has no checksums.txt; refusing to install an unverified binary' }
    Assert-Https $Archive.browser_download_url
    Assert-Https $Sums.browser_download_url
    if ($Bundle) { Assert-Https $Bundle.browser_download_url }

    $Tmp = Join-Path ([IO.Path]::GetTempPath()) "tacet-install-$([Guid]::NewGuid().ToString('N').Substring(0, 8))"
    New-Item -ItemType Directory -Path $Tmp | Out-Null
    try {
        $ZipFile = Join-Path $Tmp $Name
        $SumsFile = Join-Path $Tmp 'checksums.txt'
        $BundleFile = ''
        Invoke-WebRequest -Uri $Archive.browser_download_url -OutFile $ZipFile -UseBasicParsing -TimeoutSec $Timeout
        Invoke-WebRequest -Uri $Sums.browser_download_url -OutFile $SumsFile -UseBasicParsing -TimeoutSec $Timeout
        if ($Bundle) {
            $BundleFile = Join-Path $Tmp 'checksums.txt.sigstore.json'
            Invoke-WebRequest -Uri $Bundle.browser_download_url -OutFile $BundleFile -UseBasicParsing -TimeoutSec $Timeout
        }

        if ($SkipSignature) {
            Write-Host "warning: TACET_SKIP_SIGNATURE is set: nothing checks that $Repo signed this release. Use it only with a mirror you trust."
        }
        elseif (-not (Get-Command cosign -CommandType Application -ErrorAction SilentlyContinue)) {
            Write-Host 'Signature not checked (no cosign); the checksum still is. Install cosign to check who signed the release.'
        }
        elseif (-not $BundleFile) {
            Write-Host 'warning: the release has no checksums.txt.sigstore.json, so its signature was not checked'
        }
        else {
            # Windows PowerShell 5.1 turns redirected stderr into errors,
            # which 'Stop' would throw, so the preference is lowered here.
            $Prev = $ErrorActionPreference
            $ErrorActionPreference = 'Continue'
            $Out = & cosign verify-blob --bundle $BundleFile `
                --certificate-oidc-issuer $Issuer `
                --certificate-identity "https://github.com/$Workflow@refs/heads/master" `
                --certificate-github-workflow-repository $Repo `
                --certificate-github-workflow-ref "refs/tags/$Tag" `
                $SumsFile 2>&1 | ForEach-Object { "$_" }
            $Code = $LASTEXITCODE
            $ErrorActionPreference = $Prev
            if ($Code -ne 0) {
                if ($Out) { Write-Host ($Out -join "`n") }
                throw "tacet install: cosign says checksums.txt wasn't signed for $Repo $Tag; nothing was installed"
            }
            Write-Host "Signature ok  $Repo @ refs/tags/$Tag (cosign)"
        }

        # A sum line is "<hex>  <name>", or "<hex> *<name>" in binary mode.
        # A missing line counts as a mismatch.
        $Want = ''
        foreach ($Line in Get-Content -LiteralPath $SumsFile) {
            $Parts = $Line -split '\s+', 2
            if ($Parts.Count -eq 2 -and $Parts[1].TrimStart('*') -eq $Name) { $Want = $Parts[0].ToLowerInvariant() }
        }
        $Got = (Get-FileHash -Algorithm SHA256 -LiteralPath $ZipFile).Hash.ToLowerInvariant()
        if (-not $Want -or $Want -ne $Got) { throw "tacet install: checksum mismatch for $Name; nothing was installed" }
        Write-Host "Checksum ok   $Name"

        # Only tacet.exe is taken out, so nothing else in the archive lands
        # anywhere.
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        $Zip = [IO.Compression.ZipFile]::OpenRead($ZipFile)
        try {
            $Entry = $Zip.Entries | Where-Object { $_.FullName -eq 'tacet.exe' }
            if (-not $Entry) { throw 'tacet install: the archive has no tacet.exe' }
            $Exe = Join-Path $Tmp 'tacet.exe'
            [IO.Compression.ZipFileExtensions]::ExtractToFile($Entry, $Exe, $true)
        }
        finally {
            $Zip.Dispose()
        }

        try {
            New-Item -ItemType Directory -Force -Path $Dir | Out-Null
        }
        catch {
            throw "tacet install: can't create $Dir; set TACET_INSTALL_DIR to a folder you own"
        }
        $Dest = Join-Path $Dir 'tacet.exe'
        # A running tacet.exe can't be overwritten but can be renamed, so
        # the old one moves aside first.
        if (Test-Path -LiteralPath $Dest) {
            $Old = "$Dest.old"
            Remove-Item -LiteralPath $Old -Force -ErrorAction SilentlyContinue
            Move-Item -LiteralPath $Dest -Destination $Old -Force
        }
        Copy-Item -LiteralPath $Exe -Destination $Dest -Force
        Write-Host "Installed tacet $($Tag.TrimStart('v')) to $Dest"
    }
    finally {
        Remove-Item -LiteralPath $Tmp -Recurse -Force -ErrorAction SilentlyContinue
    }

    # Adds the folder to the user's PATH (not the machine's), once.
    $UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not (($UserPath -split ';') -contains $Dir)) {
        $NewPath = if ($UserPath) { "$UserPath;$Dir" } else { $Dir }
        [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
        Write-Host "Added $Dir to your PATH; open a new terminal to use it."
    }
    Write-Host ''
    Write-Host 'Next, in your project folder:'
    Write-Host '  tacet init'
}
