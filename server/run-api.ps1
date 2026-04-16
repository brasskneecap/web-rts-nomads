$ErrorActionPreference = 'Stop'

Set-Location $PSScriptRoot

go run ./cmd/api

exit $LASTEXITCODE
