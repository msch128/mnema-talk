param([Parameter(Mandatory)][string]$Tag)
$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_OS -ne 'Windows' -or $Tag -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') { throw 'This fixture requires a disposable Windows Actions runner and an existing release tag' }
$token = $env:GH_TOKEN
$env:GH_TOKEN = $null
git fetch --no-tags origin "refs/tags/${Tag}:refs/tags/${Tag}"
if ($LASTEXITCODE -ne 0) { throw 'Exact release tag fetch failed' }
$backendRevision = git rev-parse "refs/tags/$Tag"
if ($LASTEXITCODE -ne 0 -or $backendRevision -notmatch '^[a-f0-9]{40}$') { throw 'Exact release revision lookup failed' }
# The backend and embedded app must still be the released product sources.
git diff --exit-code $Tag -- . ':!desktop' ':!.github' ':!internal/server/desktop_client_fixture_integration_test.go' ':!internal/ws/native_failure_integration_test.go' ':!web/dist'
if ($LASTEXITCODE -ne 0) { throw 'Fixture backend differs from the requested released product' }
$nonce = [Guid]::NewGuid().ToString('N')
$directory = Join-Path $env:RUNNER_TEMP "mnema-auth-$nonce"
New-Item -ItemType Directory $directory | Out-Null
$pgBin = Get-ChildItem (Join-Path $env:ProgramFiles 'PostgreSQL') -Directory | Where-Object { $_.Name -in '17','18' } | Sort-Object Name -Descending | Select-Object -First 1
if ($null -eq $pgBin) { throw 'A preinstalled PostgreSQL 17 or 18 fixture toolchain is required' }
$pgBin = Join-Path $pgBin.FullName 'bin'
$data = Join-Path $directory 'pg-data'
$dbName = "mnema_desktop_$nonce"
$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$listener.Start()
$port = $listener.LocalEndpoint.Port
$listener.Stop()
$goExecutable = Join-Path $directory 'server-fixture.test.exe'
$env:CGO_ENABLED = '0'
go test -c -tags=integration -o $goExecutable ./internal/server
if ($LASTEXITCODE -ne 0) { throw 'Normal Go router fixture compilation failed' }
# Emit only exact, source-defined failure labels; redirected logs remain private.
function Get-FixtureFailureLabel([string]$Directory) {
  $labels = @(
    'refusing a database without fixture ownership',
    'fixture cluster ownership marker missing',
    'cannot connect to fixture database',
    'refusing a nonempty or unowned fixture database',
    'fixture configuration failed',
    'normal fixture router failed',
    'fixture output failed',
    'fixture CA output failed',
    'fixture admin creation failed',
    'fixture admin login failed',
    'fixture invite creation failed',
    'fixture member registration failed',
    'desktop fixture timed out',
    'fixture message verification failed',
    'desktop login/message/logout chain incomplete',
    'fixture randomness failed',
    'fixture CA key failed',
    'fixture CA failed',
    'fixture server key failed',
    'fixture server certificate failed'
  )
  foreach ($name in @('server.log', 'server-error.log')) {
    $path = Join-Path $Directory $name
    if (!(Test-Path $path)) { continue }
    try { $contents = [IO.File]::ReadAllText($path) } catch { continue }
    foreach ($label in $labels) {
      $pattern = '(?m)^\s+desktop_client_fixture_integration_test\.go:[0-9]+: ' + [regex]::Escape($label) + '\r?$'
      if ([regex]::IsMatch($contents, $pattern)) { return $label }
    }
    # Helpers may append response bodies or connection details. Return only labels.
    $helperFailures = @(
      @{ Prefix = 'start postgres:'; Label = 'fixture database migration failed' },
      @{ Prefix = 'create channel:'; Label = 'fixture channel creation failed' },
      @{ Prefix = 'decode '; Label = 'fixture response decode failed' },
      @{ Prefix = 'POST /api/'; Label = 'fixture HTTPS request failed' }
    )
    foreach ($failure in $helperFailures) {
      $pattern = '(?m)^\s+(desktop_client_fixture|helpers)_integration_test\.go:[0-9]+: ' + [regex]::Escape($failure.Prefix)
      if ([regex]::IsMatch($contents, $pattern)) { return $failure.Label }
    }
  }
  return 'unclassified fixture process failure'
}
$server = $null
$rootThumbprint = $null
$clusterInitialized = $false
try {
  & (Join-Path $pgBin 'initdb.exe') -D $data -U desktop_fixture -A trust -E UTF8 --locale=C
  if ($LASTEXITCODE -ne 0) { throw 'Owned PostgreSQL cluster initialization failed' }
  $clusterInitialized = $true
  [IO.File]::WriteAllText((Join-Path $data '.mnema-owner'), $nonce)
  & (Join-Path $pgBin 'pg_ctl.exe') -D $data -l (Join-Path $directory 'postgres.log') -o "-h 127.0.0.1 -p $port" -w -t 30 start
  if ($LASTEXITCODE -ne 0) { throw 'Owned PostgreSQL cluster startup failed' }
  & (Join-Path $pgBin 'createdb.exe') -h 127.0.0.1 -p $port -U desktop_fixture $dbName
  if ($LASTEXITCODE -ne 0) { throw 'Fresh fixture database creation failed' }
  $env:TEST_DATABASE_URL = "postgres://desktop_fixture@127.0.0.1:$port/${dbName}?sslmode=disable"
  $env:MNEMA_DESKTOP_FIXTURE_DIR = $directory
  $env:MNEMA_DESKTOP_FIXTURE_NONCE = $nonce
  $env:MNEMA_DESKTOP_FIXTURE_VERSION = $Tag.Substring(1)
  $env:MNEMA_DESKTOP_FIXTURE_REVISION = $backendRevision
  $server = Start-Process $goExecutable -ArgumentList '-test.run=^TestDesktopClientGUIFixture$','-test.timeout=12m' -PassThru -RedirectStandardOutput (Join-Path $directory 'server.log') -RedirectStandardError (Join-Path $directory 'server-error.log')
  $readyFile = Join-Path $directory 'READY.json'
  for ($attempt = 0; $attempt -lt 120 -and !(Test-Path $readyFile); $attempt++) {
    $server.Refresh()
    if ($server.HasExited) {
      $failureLabel = Get-FixtureFailureLabel $directory
      throw ('Normal router fixture exited before readiness: ' + $failureLabel + ' (exit code ' + $server.ExitCode + ')')
    }
    Start-Sleep -Milliseconds 500
  }
  if (!(Test-Path $readyFile)) { throw 'Normal router fixture readiness timed out' }
  try { $ready = Get-Content $readyFile -Raw | ConvertFrom-Json }
  catch { throw 'Fixture readiness document unavailable or invalid' }
  if ($ready.nonce -ne $nonce -or $ready.normal_router -ne $true) { throw 'Fixture receipt mismatch' }
  $root = Import-Certificate -FilePath (Join-Path $directory 'root.crt') -CertStoreLocation Cert:\CurrentUser\Root
  $rootThumbprint = $root.Thumbprint
  $env:GH_TOKEN = $token
  & desktop/scripts/test-windows-release.ps1 -Tag $Tag -InstanceUrl $ready.origin -FixtureDirectory $directory
  if (!$?) { throw 'Authenticated desktop UI probe failed' }
  [IO.File]::WriteAllText((Join-Path $directory 'STOP'), 'done')
  if (!$server.WaitForExit(30000) -or $server.ExitCode -ne 0) { throw 'Normal router fixture verification failed' }
  $result = Get-Content (Join-Path $directory 'SERVER-RESULT.json') -Raw | ConvertFrom-Json
  if ($result.login_200 -ne $true -or $result.logout_204 -ne $true -or $result.outbound_message_persisted -ne $true -or $result.peer_message_sent -ne $true) { throw 'Backend evidence incomplete' }
  Copy-Item (Join-Path $directory 'SERVER-RESULT.json') (Join-Path $env:RUNNER_TEMP 'windows-release-acceptance/SERVER-RESULT.json')
} finally {
  $env:GH_TOKEN = $null
  $cleanupErrors = @()
  if ($rootThumbprint) {
    try { Remove-Item "Cert:\CurrentUser\Root\$rootThumbprint" } catch { $cleanupErrors += 'owned certificate cleanup failed' }
  }
  if ($server) {
    try {
      $server.Refresh()
      if (!$server.HasExited) { Stop-Process -Id $server.Id -Force }
    } catch { $cleanupErrors += 'owned Go fixture cleanup failed' }
  }
  if ($clusterInitialized -and (Test-Path (Join-Path $data 'postmaster.pid'))) {
    try {
      & (Join-Path $pgBin 'pg_ctl.exe') -D $data -m immediate -w -t 30 stop
      if ($LASTEXITCODE -ne 0) { $cleanupErrors += 'owned PostgreSQL cleanup failed' }
    } catch { $cleanupErrors += 'owned PostgreSQL cleanup failed' }
  }
  $env:TEST_DATABASE_URL = $null
  $env:MNEMA_DESKTOP_FIXTURE_DIR = $null
  $env:MNEMA_DESKTOP_FIXTURE_NONCE = $null
  $env:MNEMA_DESKTOP_FIXTURE_VERSION = $null
  $env:MNEMA_DESKTOP_FIXTURE_REVISION = $null
  if ($cleanupErrors.Count) { throw ($cleanupErrors -join '; ') }
}
