$ErrorActionPreference = 'Stop'

Set-Location $PSScriptRoot

go build -o api.exe ./cmd/api

if ($LASTEXITCODE -ne 0) {
  exit $LASTEXITCODE
}

& "$PSScriptRoot\api.exe"
