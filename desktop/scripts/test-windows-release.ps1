param(
  [Parameter(Mandatory)][string]$Tag,
  [Parameter(Mandatory)][string]$InstanceUrl,
  [string]$FixtureDirectory
)
$ErrorActionPreference = 'Stop'
if ($Tag -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') { throw 'Invalid release tag' }
$uri = [Uri]$InstanceUrl
if (!$uri.IsAbsoluteUri -or $uri.Scheme -ne 'https' -or $uri.UserInfo -or $uri.Query -or $uri.Fragment -or $uri.AbsolutePath -ne '/' -or $InstanceUrl.Length -gt 2048) { throw 'An HTTPS instance origin is required' }
$fixture = $null
if ($FixtureDirectory) {
  $temp = [IO.Path]::GetFullPath($env:RUNNER_TEMP).TrimEnd('\') + '\'
  if ($env:GITHUB_ACTIONS -ne 'true' -or ![IO.Path]::GetFullPath($FixtureDirectory).StartsWith($temp, [StringComparison]::OrdinalIgnoreCase)) { throw 'Fixture must belong to this disposable runner' }
  try { $fixture = Get-Content (Join-Path $FixtureDirectory 'READY.json') -Raw | ConvertFrom-Json }
  catch { throw 'Fixture readiness document unavailable or invalid' }
  if ($fixture.nonce -notmatch '^[a-f0-9]{32}$' -or [IO.Path]::GetFileName($FixtureDirectory) -ne "mnema-auth-$($fixture.nonce)" -or $fixture.origin -ne $InstanceUrl -or $uri.Host -ne '127.0.0.1' -or $uri.Port -lt 1024 -or $fixture.normal_router -ne $true -or $fixture.username -ne 'desktop-fixture-user' -or $fixture.password -notmatch '^[a-f0-9]{64}$' -or $fixture.channel -ne 'desktop-fixture-chat' -or $fixture.outbound -ne "Desktop fixture outbound $($fixture.nonce)" -or $fixture.inbound -ne "Desktop fixture inbound $($fixture.nonce)") { throw 'Invalid owned local fixture receipt' }
} else {
if ($uri.Host -notmatch '^[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$') { throw 'A public HTTPS instance origin is required' }
foreach ($address in [Net.Dns]::GetHostAddresses($uri.Host)) {
  if ($address.IsIPv4MappedToIPv6) { $address = $address.MapToIPv4() }
  $bytes = $address.GetAddressBytes()
  if ([Net.IPAddress]::IsLoopback($address) -or $address.IsIPv6LinkLocal -or $address.IsIPv6SiteLocal -or $address.IsIPv6Multicast -or ($bytes.Length -eq 16 -and ($bytes[0] -band 254) -eq 252) -or ($bytes.Length -eq 4 -and ($bytes[0] -in 0,10,127 -or $bytes[0] -ge 224 -or ($bytes[0] -eq 172 -and $bytes[1] -ge 16 -and $bytes[1] -le 31) -or ($bytes[0] -eq 192 -and $bytes[1] -eq 168) -or ($bytes[0] -eq 169 -and $bytes[1] -eq 254) -or ($bytes[0] -eq 100 -and $bytes[1] -ge 64 -and $bytes[1] -le 127)))) { throw 'Private instance addresses are not accepted by this public CI test' }
}
}
$directory = Join-Path $env:RUNNER_TEMP 'windows-release-acceptance'
New-Item -ItemType Directory -Path $directory | Out-Null
$zipName = "Mnema-Desktop-DEV-$Tag-windows-x64.zip"
gh release download $Tag --repo msch128/mnema-talk --dir $directory --pattern $zipName --pattern WINDOWS-DESKTOP-SHA256SUMS
if ($LASTEXITCODE -ne 0) { throw 'Release download failed' }
$checksum = (Get-Content (Join-Path $directory 'WINDOWS-DESKTOP-SHA256SUMS') -Raw).Trim()
if ($checksum -notmatch ('^([a-f0-9]{64})  ' + [regex]::Escape($zipName) + '$')) { throw 'Invalid release checksum file' }
if ((Get-FileHash (Join-Path $directory $zipName) -Algorithm SHA256).Hash.ToLowerInvariant() -ne $Matches[1]) { throw 'Release ZIP checksum mismatch' }
Add-Type -AssemblyName System.IO.Compression.FileSystem
$archive = [IO.Compression.ZipFile]::OpenRead((Join-Path $directory $zipName))
try {
  if ($archive.Entries.Count -gt 4096) { throw 'Release archive exceeds the entry limit' }
  $total = 0L
  $targets = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
  foreach ($entry in $archive.Entries) {
    if ($entry.FullName -match '(^[\\/]|(^|[\\/])\.\.([\\/]|$)|:)') { throw 'Unsafe release archive path' }
    $relative = $entry.FullName.Replace('\', '/').TrimEnd('/')
    foreach ($segment in $relative.Split('/')) {
      if (!$segment -or $segment -match '[<>:"|?*\x00-\x1f]' -or $segment -match '[. ]$' -or $segment -match '^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\.|$)') { throw 'Unsafe Windows archive path segment' }
    }
    if (!$targets.Add($relative)) { throw 'Duplicate Windows archive target' }
    $total += $entry.Length
    if ($total -gt 268435456) { throw 'Release archive exceeds the size limit' }
  }
} finally { $archive.Dispose() }
Expand-Archive (Join-Path $directory $zipName) (Join-Path $directory 'package')
$package = Join-Path $directory 'package'
$receipt = Get-Content (Join-Path $package 'DEV-BUILD.json') -Raw | ConvertFrom-Json
$tagJson = gh api "repos/msch128/mnema-talk/git/ref/tags/$Tag"
if ($LASTEXITCODE -ne 0) { throw 'Release tag lookup failed' }
$env:GH_TOKEN = $null
$tagObject = $tagJson | ConvertFrom-Json
if ($fixture -and $fixture.backend_revision -ne $tagObject.object.sha) { throw 'Fixture backend and published client release revisions differ' }
if ($tagObject.object.type -ne 'commit' -or $receipt.revision -ne $tagObject.object.sha -or $receipt.kind -ne 'unsigned-development-web-desktop-client' -or $receipt.target -ne 'x86_64-pc-windows-msvc' -or $receipt.signed -ne $false -or $receipt.automatic_updater_package -ne $false -or $receipt.windows_startup_checked -ne $true -or $receipt.instance_transport -ne 'same-origin-browser') { throw 'Release receipt mismatch' }
$exe = Join-Path $package 'Mnema Desktop DEV.exe'
if ((Get-FileHash $exe -Algorithm SHA256).Hash.ToLowerInvariant() -ne $receipt.executable_sha256) { throw 'Executable checksum mismatch' }
Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes
$process = Start-Process -FilePath $exe -PassThru
function Wait-Ui([scriptblock]$Probe, [string]$Message) {
  for ($attempt = 0; $attempt -lt 120; $attempt++) {
    $process.Refresh()
    if ($process.HasExited) { throw 'The release executable exited unexpectedly' }
    $found = & $Probe
    if ($null -ne $found) { return $found }
    Start-Sleep -Milliseconds 500
  }
  throw $Message
}
function Find-Window([string]$Title) {
  $condition = [Windows.Automation.AndCondition]::new(
    [Windows.Automation.PropertyCondition]::new([Windows.Automation.AutomationElement]::ProcessIdProperty, $process.Id),
    [Windows.Automation.PropertyCondition]::new([Windows.Automation.AutomationElement]::NameProperty, $Title))
  return [Windows.Automation.AutomationElement]::RootElement.FindFirst([Windows.Automation.TreeScope]::Children, $condition)
}
function Find-Control($Root, $Type, [string]$Name) {
  $condition = [Windows.Automation.AndCondition]::new(
    [Windows.Automation.PropertyCondition]::new([Windows.Automation.AutomationElement]::ControlTypeProperty, $Type),
    [Windows.Automation.PropertyCondition]::new([Windows.Automation.AutomationElement]::NameProperty, $Name, [Windows.Automation.PropertyConditionFlags]::IgnoreCase))
  return $Root.FindFirst([Windows.Automation.TreeScope]::Descendants, $condition)
}
function Activate-Control($Root, [string]$Name, [bool]$Toggle = $false) {
  $requiredPattern = if ($Toggle) { [Windows.Automation.TogglePattern]::Pattern } else { [Windows.Automation.InvokePattern]::Pattern }
  $pattern = Wait-Ui {
    $control = Find-Control $Root ([Windows.Automation.ControlType]::Button) $Name
    if ($null -ne $control -and $control.Current.IsEnabled) {
      $candidate = $null
      if ($control.TryGetCurrentPattern($requiredPattern, [ref]$candidate)) { return $candidate }
    }
  } "Button not ready for invocation: $Name"
  if ($Toggle) { $pattern.Toggle() } else { $pattern.Invoke() }
}
function Focus-Control($Control) {
  $Control.SetFocus()
  $null = Wait-Ui {
    if ([Windows.Automation.Automation]::Compare($Control, [Windows.Automation.AutomationElement]::FocusedElement)) { return $true }
  } 'Owned control did not receive keyboard focus'
}
function Enter-Control($Root, $Type, [string]$Name) {
  $control = Wait-Ui { Find-Control $Root $Type $Name } 'Expected keyboard control unavailable'
  Focus-Control $control
  [Windows.Forms.SendKeys]::SendWait('{ENTER}')
}
try {
  Write-Host 'Phase: published executable selector'
  $selector = Wait-Ui { Find-Window 'Mnema Desktop DEV — Instance' } 'Selector window did not appear'
  $addressInput = Wait-Ui { Find-Control $selector ([Windows.Automation.ControlType]::Edit) 'Server address' } 'Address input unavailable'
  $value = $addressInput.GetCurrentPattern([Windows.Automation.ValuePattern]::Pattern)
  $value.SetValue($uri.GetLeftPart([UriPartial]::Authority))
  $null = Wait-Ui {
    if ($value.Current.Value -eq $uri.GetLeftPart([UriPartial]::Authority)) { return $true }
  } 'Address input was not updated'
  Write-Host 'Phase: validated address readback, invoking Connect'
  Activate-Control $selector 'Connect'
  $instance = Wait-Ui { Find-Window 'Mnema Desktop DEV' } 'Instance window did not appear'
  $signin = Wait-Ui { Find-Control $instance ([Windows.Automation.ControlType]::Button) 'Sign in' } 'Canonical sign-in page did not appear'
  $username = Find-Control $instance ([Windows.Automation.ControlType]::Edit) 'USERNAME'
  if ($null -eq $username) { throw 'Canonical username control unavailable' }
  if ($fixture) {
    Write-Host 'Phase: local fixture authentication'
    Add-Type -AssemblyName System.Windows.Forms
    $usernameValue = $username.GetCurrentPattern([Windows.Automation.ValuePattern]::Pattern)
    $usernameValue.SetValue($fixture.username)
    $null = Wait-Ui { if ($usernameValue.Current.Value -eq $fixture.username) { return $true } } 'Fixture username was not updated'
    $password = Wait-Ui { Find-Control $instance ([Windows.Automation.ControlType]::Edit) 'PASSWORD' } 'Password control unavailable'
    Focus-Control $password
    [Windows.Forms.SendKeys]::SendWait($fixture.password)
    Activate-Control $instance 'Sign in'
    $null = Wait-Ui { Find-Control $instance ([Windows.Automation.ControlType]::Button) $fixture.channel } 'Authenticated channels unavailable'
    Write-Host 'Phase: authenticated canonical chat'
    Enter-Control $instance ([Windows.Automation.ControlType]::Button) $fixture.channel
    $composer = Wait-Ui { Find-Control $instance ([Windows.Automation.ControlType]::Edit) "Message #$($fixture.channel)" } 'Canonical chat composer unavailable'
    Focus-Control $composer
    [Windows.Forms.SendKeys]::SendWait($fixture.outbound)
    [Windows.Forms.SendKeys]::SendWait('{ENTER}')
    $null = Wait-Ui { Find-Control $instance ([Windows.Automation.ControlType]::Text) $fixture.inbound } 'Live peer message did not arrive'
    Write-Host 'Phase: live peer message received, logout'
    Enter-Control $instance ([Windows.Automation.ControlType]::Button) 'Account menu'
    Enter-Control $instance ([Windows.Automation.ControlType]::MenuItem) 'Sign out'
    $null = Wait-Ui { Find-Control $instance ([Windows.Automation.ControlType]::Button) 'Sign in' } 'Logout did not restore canonical sign-in'
  } else {
    Write-Host 'Phase: canonical sign-in page, toggling Deutsch'
    Activate-Control $instance 'Deutsch' $true
    $null = Wait-Ui { Find-Control $instance ([Windows.Automation.ControlType]::Button) 'Anmelden' } 'Canonical language interaction failed'
  }
  [ordered]@{
    kind = $(if ($fixture) { 'windows-published-release-authenticated-chat' } else { 'windows-published-release-instance-entry' })
    tag = $Tag
    revision = $receipt.revision
    executable_sha256 = $receipt.executable_sha256
    selector_to_canonical_signin = $true
    language_interaction_checked = ($null -eq $fixture)
    credentials_used = ($null -ne $fixture)
    production_credentials_used = $false
    authenticated_chat_ui_checked = ($null -ne $fixture)
    authenticated_chat_media_and_games_qualified = $false
  } | ConvertTo-Json | Set-Content (Join-Path $directory 'ACCEPTANCE.json') -Encoding utf8
  if ($fixture) { 'Published Windows release: local fixture login, chat send/receive and logout UI PASS. Generated fixture credentials only; no media, S3, E2EE or game qualification.' | Out-File -Append $env:GITHUB_STEP_SUMMARY }
  else { 'Published Windows release: selector, actual instance sign-in page and language interaction PASS. No credentials or authenticated/media qualification.' | Out-File -Append $env:GITHUB_STEP_SUMMARY }
} finally {
  if (!$process.HasExited) { Stop-Process -Id $process.Id -Force }
}
