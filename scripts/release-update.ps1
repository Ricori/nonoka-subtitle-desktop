param(
    [Parameter(Mandatory = $true)][ValidatePattern('^\d+\.\d+\.\d+$')][string]$Version,
    [ValidateSet("windows", "macos", "all")][string]$Platform = "windows",
    [ValidateSet("amd64", "arm64")][string]$Arch = "amd64",
    [ValidatePattern('^$|^\d+\.\d+\.\d+$')][string]$MinVersion = "",
    [string]$NotesFile = "docs/RELEASE_NOTES.md",
    [switch]$SkipBuild,
    [switch]$Publish
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$utf8 = [Text.UTF8Encoding]::new($false)

function Set-VersionInFile {
    param(
        [string]$RelativePath,
        [string]$Pattern,
        [string]$Replacement,
        [int]$ExpectedMatches = 1
    )
    $normalisedPath = $RelativePath -replace '[\\/]', [IO.Path]::DirectorySeparatorChar
    $path = Join-Path $projectRoot $normalisedPath
    $text = [IO.File]::ReadAllText($path)
    $matches = [regex]::Matches($text, $Pattern)
    if ($matches.Count -ne $ExpectedMatches) {
        throw "Unexpected version field count in ${RelativePath}: $($matches.Count)"
    }
    [IO.File]::WriteAllText($path, [regex]::Replace($text, $Pattern, $Replacement), $utf8)
}

Set-VersionInFile "internal\app\desktop_service.go" '(?m)(desktopVersion\s*=\s*")[^"]+("\s*$)' "`${1}$Version`${2}"
Set-VersionInFile "frontend\package.json" '(?m)^(  "version": ")[^"]+("\s*,?\s*$)' "`${1}$Version`${2}"
Set-VersionInFile "frontend\package-lock.json" '(?m)^(  "version": ")[^"]+("\s*,?\s*$)' "`${1}$Version`${2}"
Set-VersionInFile "frontend\package-lock.json" '("":\s*\{\s*"name":\s*"[^"]+",\s*"version":\s*")[^"]+(")' "`${1}$Version`${2}"
Set-VersionInFile "build\config.yml" '(?m)^  version: "[^"]+"$' "  version: `"$Version`""
Set-VersionInFile "build\windows\info.json" '("(?:file_version|product_version|ProductVersion)"\s*:\s*")[^"]+(")' "`${1}$Version`${2}" 5
Set-VersionInFile "build\windows\wails.exe.manifest" '(<assemblyIdentity\s+type="win32"\s+name="online\.nonoka\.subtitle"\s+version=")[^"]+(")' "`${1}$Version`${2}"
Set-VersionInFile "build\windows\msix\app_manifest.xml" '(?m)(^\s*Version=")[^"]+"[ \t]*(\r?$)' "`${1}$Version.0`"`${2}"
Set-VersionInFile "build\windows\msix\template.xml" '(?m)(^\s*Version=")[^"]+(")' "`${1}$Version.0`${2}"
Set-VersionInFile "build\windows\nsis\wails_tools.nsh" '(?m)(^\s*!define INFO_PRODUCTVERSION ")[^"]+("\s*$)' "`${1}$Version`${2}"
Set-VersionInFile "build\darwin\Info.plist" '(<key>CFBundle(?:ShortVersionString|Version)</key>\s*<string>)[^<]+(</string>)' "`${1}$Version`${2}" 2
Set-VersionInFile "build\darwin\Info.dev.plist" '(<key>CFBundle(?:ShortVersionString|Version)</key>\s*<string>)[^<]+(</string>)' "`${1}$Version`${2}" 2

if (-not $SkipBuild) {
    $wails = "D:\Envir\go\bin\wails3.exe"
    if (-not (Test-Path -LiteralPath $wails)) {
        $wails = (Get-Command wails3 -ErrorAction Stop).Source
    }
    $targets = if ($Platform -eq "all") { @("windows", "macos") } else { @($Platform) }
    $originalPath = $env:Path
    $toolDirs = @((Split-Path -Parent $wails))
    $bundledNode = "D:\Envir\nodejs"
    if (Test-Path -LiteralPath $bundledNode -PathType Container) { $toolDirs += $bundledNode }
    $env:Path = ($toolDirs + $originalPath) -join [IO.Path]::PathSeparator
    Push-Location $projectRoot
    try {
        foreach ($target in $targets) {
            if ($target -eq "windows") {
                & $wails build "GOOS=windows" "ARCH=$Arch"
            } else {
                & $wails task darwin:package:universal
            }
            if ($LASTEXITCODE -ne 0) { throw "$target build failed" }
        }
    } finally {
        Pop-Location
        $env:Path = $originalPath
    }
}

$prepare = Join-Path $PSScriptRoot "prepare-update.ps1"
& $prepare -Version $Version -Platform $Platform -Arch $Arch -MinVersion $MinVersion -NotesFile $NotesFile
if ($LASTEXITCODE -ne 0) { throw "Failed to prepare update artifacts" }

if ($Publish) {
    $node = "D:\Envir\nodejs\node.exe"
    if (-not (Test-Path -LiteralPath $node)) {
        $node = (Get-Command node -ErrorAction Stop).Source
    }
    $publisher = Join-Path $PSScriptRoot "publish-update.mjs"
    $outputDir = Join-Path (Join-Path $projectRoot "bin\update") $Version
    & $node $publisher --dir $outputDir
    if ($LASTEXITCODE -ne 0) { throw "Failed to publish update artifacts to R2" }
}
