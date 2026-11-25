# Starts The Daily Star InDesign bridge now and at every Windows sign-in,
# without a window (a shortcut in the user's Startup folder).
#   powershell -ExecutionPolicy Bypass -File install-startup.ps1
#   powershell -ExecutionPolicy Bypass -File install-startup.ps1 -Remove
param([switch]$Remove)
$exe = Join-Path $PSScriptRoot 'indd-bridge.exe'
$link = Join-Path ([Environment]::GetFolderPath('Startup')) 'Daily Star InDesign bridge.lnk'
if ($Remove) {
  Remove-Item $link -ErrorAction SilentlyContinue
  Get-Process indd-bridge -ErrorAction SilentlyContinue | Stop-Process
  'Removed: the bridge no longer starts at sign-in, and is stopped.'
  return
}
if (-not (Test-Path $exe)) { "indd-bridge.exe is not in $PSScriptRoot"; exit 1 }
$sh = New-Object -ComObject WScript.Shell
$s = $sh.CreateShortcut($link)
$s.TargetPath = 'powershell.exe'
$s.Arguments = "-NoProfile -WindowStyle Hidden -Command `"Start-Process -WindowStyle Hidden -FilePath '$exe'`""
$s.WorkingDirectory = $PSScriptRoot
$s.Description = 'The Daily Star e-paper: pages as InDesign files'
$s.Save()
if (-not (Get-Process indd-bridge -ErrorAction SilentlyContinue)) {
  Start-Process -WindowStyle Hidden -FilePath $exe -WorkingDirectory $PSScriptRoot
}
"Installed: the bridge runs now and starts at every sign-in ($link)."
