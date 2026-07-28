[CmdletBinding(SupportsShouldProcess=$true)]
param(
    [string]$Destination = (Join-Path $HOME "bin"),
    [switch]$NoBuild
)
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Push-Location $Root
try {
    if (-not $NoBuild) {
        New-Item -ItemType Directory -Force -Path bin | Out-Null
        go build -trimpath -o bin/futurediff.exe ./cmd/futurediff
        go build -trimpath -o bin/futurediffd.exe ./cmd/futurediffd
        go build -trimpath -o bin/fdif.exe ./cmd/fdif
    }
    $Binaries = @("futurediff.exe", "futurediffd.exe", "fdif.exe")
    foreach ($Binary in $Binaries) {
        $Path = Join-Path $Root (Join-Path "bin" $Binary)
        if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
            throw "Missing $Path"
        }
    }
    if ($PSCmdlet.ShouldProcess($Destination, "Install FutureDiff binaries")) {
        New-Item -ItemType Directory -Force -Path $Destination | Out-Null
        foreach ($Binary in $Binaries) {
            Copy-Item -LiteralPath (Join-Path $Root (Join-Path "bin" $Binary)) -Destination (Join-Path $Destination $Binary) -Force
        }
    }
    Write-Host "Installed futurediff, futurediffd, and fdif to $Destination"
} finally {
    Pop-Location
}
