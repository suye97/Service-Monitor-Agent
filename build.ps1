$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    Push-Location frontend
    try {
        npm install
        if ($LASTEXITCODE -ne 0) { throw 'npm install failed' }
        npm run build
        if ($LASTEXITCODE -ne 0) { throw 'frontend build failed' }
    } finally { Pop-Location }
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
    New-Item -ItemType Directory -Force -Path 'build/bin' | Out-Null
    go run ./tools/icongen -out build
    if ($LASTEXITCODE -ne 0) { throw 'icon generation failed' }
    go run github.com/tc-hib/go-winres@v0.3.3 simply --icon build/windows/icon.ico --out rsrc --arch amd64 --manifest gui --product-name 'Service Monitor Agent' --file-description 'Service Monitor Agent' --original-filename 'ServiceMonitorAgent.exe'
    if ($LASTEXITCODE -ne 0) { throw 'Windows resource generation failed' }
    go build -tags production -ldflags '-H windowsgui' -o build/bin/ServiceMonitorAgent.exe .
    if ($LASTEXITCODE -ne 0) { throw 'desktop build failed' }
    Write-Host 'Built build/bin/ServiceMonitorAgent.exe'
} finally { Pop-Location }
