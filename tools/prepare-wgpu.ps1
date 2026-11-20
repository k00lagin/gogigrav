# Run once from a fresh checkout before building Gogigrav.
$ErrorActionPreference = 'Stop'
Push-Location (Split-Path $PSScriptRoot)
try {
    $version = 'v0.34.14'
    $destination = ".deps/wgpu-$version"
    $patch = 'tools/wgpu-vulkan-memory.patch'
    if (Test-Path "$destination/go.mod") {
        git apply --reverse --check --ignore-space-change "--directory=$destination" $patch
        if ($LASTEXITCODE) { throw "Unexpected wgpu sources in $destination" }
        return
    }

    $module = go mod download -json "github.com/gogpu/wgpu@$version" | ConvertFrom-Json
    if ($LASTEXITCODE -or !$module.Dir) { throw "Cannot download wgpu $version" }
    $stage = ".deps/staging-$([guid]::NewGuid())"
    New-Item -ItemType Directory -Force .deps | Out-Null
    Copy-Item -LiteralPath $module.Dir -Destination $stage -Recurse
    (Get-Item "$stage/hal/vulkan/device.go").IsReadOnly = $false
    git apply --ignore-space-change "--directory=$stage" $patch
    if ($LASTEXITCODE) { throw "Cannot apply Vulkan allocator patch in $stage" }
    Move-Item -LiteralPath $stage -Destination $destination
} finally {
    Pop-Location
}
