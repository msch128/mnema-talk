param([Parameter(Mandatory)][string]$Tag, [switch]$VoiceAcceptance)
$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_OS -ne 'Windows' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted' -or $Tag -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') { throw 'This fixture requires a disposable Windows Actions runner and an existing release tag' }
# This opt-in test owns a disposable Actions VM; never install trust on a user's PC.
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
try {
  $principal = [Security.Principal.WindowsPrincipal]::new($identity)
  if (!$principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Disposable fixture CA setup requires an elevated runner' }
} finally { $identity.Dispose() }
$token = $env:GH_TOKEN
$env:GH_TOKEN = $null
git fetch --no-tags origin "refs/tags/${Tag}:refs/tags/${Tag}"
if ($LASTEXITCODE -ne 0) { throw 'Exact release tag fetch failed' }
$backendRevision = git rev-parse "refs/tags/$Tag"
if ($LASTEXITCODE -ne 0 -or $backendRevision -notmatch '^[a-f0-9]{40}$') { throw 'Exact release revision lookup failed' }
# The runtime Go sources, migrations, dependencies and embedded web app must
# match the release. Documentation/build metadata may differ on the draft.
# Only these exact integration/regression test changes are excluded.
git diff --exit-code $Tag -- '*.go' go.mod go.sum go.work go.work.sum cmd internal api web ':!desktop' ':!internal/server/desktop_client_fixture_integration_test.go' ':!internal/ws/native_failure_integration_test.go' ':!internal/sfu/signaling_test.go' ':!web/dist'
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
$rootCertificate = $null
$rootStorePath = 'Cert:\LocalMachine\Root'
$clusterInitialized = $false
try {
  & (Join-Path $pgBin 'initdb.exe') -D $data -U desktop_fixture -A trust -E UTF8 --locale=C
  if ($LASTEXITCODE -ne 0) { throw 'Owned PostgreSQL cluster initialization failed' }
  $clusterInitialized = $true
  [IO.File]::WriteAllText((Join-Path $data '.mnema-owner'), $nonce)
  & (Join-Path $pgBin 'pg_ctl.exe') -D $data -l (Join-Path $directory 'postgres.log') -o "-h 127.0.0.1 -p $port" -w -t 30 start
  if ($LASTEXITCODE -ne 0) { throw 'Owned PostgreSQL cluster startup failed' }
  Write-Host 'Fixture phase: creating the owned database'
  & (Join-Path $pgBin 'createdb.exe') -h 127.0.0.1 -p $port -U desktop_fixture $dbName
  if ($LASTEXITCODE -ne 0) { throw 'Fresh fixture database creation failed' }
  $env:TEST_DATABASE_URL = "postgres://desktop_fixture@127.0.0.1:$port/${dbName}?sslmode=disable"
  $env:MNEMA_DESKTOP_FIXTURE_DIR = $directory
  $env:MNEMA_DESKTOP_FIXTURE_NONCE = $nonce
  $env:MNEMA_DESKTOP_FIXTURE_VERSION = $Tag.Substring(1)
  $env:MNEMA_DESKTOP_FIXTURE_REVISION = $backendRevision
  $env:MNEMA_DESKTOP_FIXTURE_VOICE = $(if ($VoiceAcceptance) { "1" } else { "0" })
  Write-Host 'Fixture phase: starting the normal backend'
  $server = Start-Process $goExecutable -ArgumentList '-test.run=^TestDesktopClientGUIFixture$','-test.timeout=12m' -PassThru -RedirectStandardOutput (Join-Path $directory 'server.log') -RedirectStandardError (Join-Path $directory 'server-error.log')
  # PowerShell 5.1 redirected Start-Process needs a retained handle for ExitCode.
  # https://github.com/PowerShell/PowerShell/issues/5421
  $null = $server.Handle
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
  Write-Host 'Fixture phase: backend ready, importing the owned test CA'
  $rootBytes = [IO.File]::ReadAllBytes((Join-Path $directory 'root.crt'))
  $rootCertificate = [Security.Cryptography.X509Certificates.X509Certificate2]::new($rootBytes)
  $constraints = $rootCertificate.Extensions['2.5.29.19']
  if ($rootCertificate.Subject -ne 'CN=Mnema disposable GUI fixture' -or $rootCertificate.Issuer -ne $rootCertificate.Subject -or $rootCertificate.HasPrivateKey -or $null -eq $constraints -or !$constraints.CertificateAuthority -or $rootCertificate.NotAfter -le [DateTime]::Now) { throw 'Invalid owned fixture CA' }
  $candidateThumbprint = $rootCertificate.Thumbprint
  if ($candidateThumbprint -notmatch '^[A-F0-9]{40}$' -or (Test-Path "$rootStorePath\$candidateThumbprint")) { throw 'Refusing a pre-existing fixture CA' }
  # Record cleanup ownership before any import, including partially failed imports.
  $rootThumbprint = $candidateThumbprint
  # Import only this public certificate into this disposable VM's Root store.
  # The CurrentUser CA setup blocked the hosted fixture. The explicit
  # elevated-runner guard and exact cleanup bound this temporary machine trust.
  # Real Schannel/WebView TLS verification remains enabled.
  # https://learn.microsoft.com/windows/win32/api/cryptuiapi/nf-cryptuiapi-cryptuiwizimport
  Write-Host 'Fixture phase: compiling the owned CA import helper'
  Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
using System.Security.Cryptography.X509Certificates;
public static class MnemaFixtureCertificateImport {
    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct Source {
        public uint Size;
        public uint Choice;
        public IntPtr Certificate;
        public uint Flags;
        [MarshalAs(UnmanagedType.LPWStr)] public string Password;
    }
    [DllImport("cryptui.dll", CharSet = CharSet.Unicode, ExactSpelling = true, SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool CryptUIWizImport(uint flags, IntPtr parent, string title, ref Source source, IntPtr store);
    public static bool Import(X509Certificate2 certificate, X509Store store) {
        var source = new Source { Size = (uint)Marshal.SizeOf(typeof(Source)), Choice = 2,
            Certificate = certificate.Handle, Flags = 0, Password = String.Empty };
        // NO_UI | NO_CHANGE_DEST_STORE | ALLOW_CERT; explicit certificate context.
        bool result = CryptUIWizImport(0x00030001, IntPtr.Zero, null, ref source, store.StoreHandle);
        GC.KeepAlive(certificate);
        GC.KeepAlive(store);
        return result;
    }
}
'@
  $rootStore = [Security.Cryptography.X509Certificates.X509Store]::new('Root', [Security.Cryptography.X509Certificates.StoreLocation]::LocalMachine)
  try {
    Write-Host 'Fixture phase: opening the owned machine CA store'
    $rootStore.Open([Security.Cryptography.X509Certificates.OpenFlags]::ReadWrite)
    Write-Host 'Fixture phase: importing the owned machine CA without a dialog'
    if (![MnemaFixtureCertificateImport]::Import($rootCertificate, $rootStore)) { throw 'Noninteractive owned fixture CA import failed' }
  } finally { $rootStore.Close() }
  Write-Host 'Fixture phase: verifying the owned machine CA readback'
  $installed = Get-Item "$rootStorePath\$rootThumbprint"
  if ([Convert]::ToBase64String($installed.RawData) -ne [Convert]::ToBase64String($rootBytes)) { throw 'Owned fixture CA readback mismatch' }
  Write-Host 'Fixture phase: test CA verified, starting the published client probe'
  $env:GH_TOKEN = $token
  & desktop/scripts/test-windows-release.ps1 -Tag $Tag -InstanceUrl $ready.origin -FixtureDirectory $directory -VoiceAcceptance:$VoiceAcceptance
  if (!$?) { throw 'Authenticated desktop UI probe failed' }
  [IO.File]::WriteAllText((Join-Path $directory 'STOP'), 'done')
  if (!$server.WaitForExit(30000)) { throw 'Normal router fixture exit timed out' }
  $server.Refresh()
  $exitCode = $server.ExitCode
  if ($null -eq $exitCode) { throw 'Normal router fixture exit code unavailable' }
  if ($exitCode -ne 0) {
    $failureLabel = Get-FixtureFailureLabel $directory
    throw ('Normal router fixture verification failed: ' + $failureLabel + ' (exit code ' + $exitCode + ')')
  }
  $result = Get-Content (Join-Path $directory 'SERVER-RESULT.json') -Raw | ConvertFrom-Json
  if ($result.login_200 -ne $true -or $result.logout_204 -ne $true -or $result.outbound_message_persisted -ne $true -or $result.peer_message_sent -ne $true) { throw 'Backend evidence incomplete' }
  Copy-Item (Join-Path $directory 'SERVER-RESULT.json') (Join-Path $env:RUNNER_TEMP 'windows-release-acceptance/SERVER-RESULT.json')
} finally {
  $env:GH_TOKEN = $null
  $cleanupErrors = @()
  if ($rootThumbprint) {
    try {
      $ownedCertificatePath = "$rootStorePath\$rootThumbprint"
      if (Test-Path $ownedCertificatePath) { Remove-Item $ownedCertificatePath }
      if (Test-Path $ownedCertificatePath) { throw 'Owned certificate remains installed' }
    } catch { $cleanupErrors += 'owned certificate cleanup failed' }
  }
  if ($rootCertificate) {
    try { $rootCertificate.Dispose() } catch { $cleanupErrors += 'owned certificate disposal failed' }
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
  $env:MNEMA_DESKTOP_FIXTURE_VOICE = $null
  if ($cleanupErrors.Count) { throw ($cleanupErrors -join '; ') }
}
