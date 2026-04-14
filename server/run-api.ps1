$ErrorActionPreference = 'Stop'

Set-Location $PSScriptRoot

go run ./cmd/api

if ($LASTEXITCODE -ne 0) {
  exit $LASTEXITCODE
}
