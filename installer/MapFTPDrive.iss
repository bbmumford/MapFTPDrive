#ifndef Version
  #define Version "0.0.0-dev"
#endif
#ifndef Arch
  #define Arch "x64"
#endif
#ifndef Binary
  #define Binary "..\build\MapFTPDrive.exe"
#endif
#ifndef WinFspMsi
  #define WinFspMsi "..\build\winfsp.msi"
#endif
#ifndef WinFspVersion
  #define WinFspVersion "2.1.25156"
#endif
#ifndef SigningSuffix
  #define SigningSuffix "-unsigned"
#endif

#if Arch == "arm64"
  #define ArchitecturesAllowedValue "arm64"
  #define ArchitecturesInstallValue "arm64"
#else
  #define ArchitecturesAllowedValue "x64compatible"
  #define ArchitecturesInstallValue "x64compatible"
#endif

[Setup]
AppId={{52D7D81E-72E4-42F7-B3C7-92CE08779B47}
AppName=MapFTPDrive
AppVersion={#Version}
AppPublisher=MapFTPDrive contributors
AppPublisherURL=https://github.com/bbmumford/MapFTPDrive
AppSupportURL=https://github.com/bbmumford/MapFTPDrive/issues
DefaultDirName={autopf}\MapFTPDrive
DefaultGroupName=MapFTPDrive
DisableProgramGroupPage=yes
PrivilegesRequired=admin
ArchitecturesAllowed={#ArchitecturesAllowedValue}
ArchitecturesInstallIn64BitMode={#ArchitecturesInstallValue}
OutputDir=output
OutputBaseFilename=MapFTPDrive-{#Version}-windows-{#Arch}-setup{#SigningSuffix}
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
UninstallDisplayIcon={app}\MapFTPDrive.exe
VersionInfoCompany=MapFTPDrive contributors
VersionInfoDescription=MapFTPDrive installer
VersionInfoProductName=MapFTPDrive
CloseApplications=yes
RestartApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "{#Binary}"; DestDir: "{app}"; DestName: "MapFTPDrive.exe"; Flags: ignoreversion
; Extracted and run by InstallWinFsp only when WinFsp is missing or older.
Source: "{#WinFspMsi}"; DestDir: "{tmp}"; DestName: "winfsp.msi"; Flags: dontcopy
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\MapFTPDrive"; Filename: "{app}\MapFTPDrive.exe"
Name: "{autodesktop}\MapFTPDrive"; Filename: "{app}\MapFTPDrive.exe"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Additional shortcuts:"

[Registry]
; One machine-wide startup entry starts each signed-in user's host.
Root: HKLM; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "MapFTPDrive"; ValueData: """{app}\MapFTPDrive.exe"" daemon --scope user"; Flags: uninsdeletevalue
; Explorer verbs, registered machine-wide exactly as the MSI does.
; internal/explorer/verbs.go holds the same table; verbs_test.go fails when
; these lines diverge from it.
Root: HKLM; Subkey: "Software\Classes\CLSID\{{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell\MapFTPDrive"; ValueType: string; ValueName: "MUIVerb"; ValueData: "Map FTP Drive"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\Classes\CLSID\{{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell\MapFTPDrive"; ValueType: string; ValueName: "Icon"; ValueData: "{app}\MapFTPDrive.exe,0"
Root: HKLM; Subkey: "Software\Classes\CLSID\{{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell\MapFTPDrive\command"; ValueType: string; ValueName: ""; ValueData: """{app}\MapFTPDrive.exe"" map --scope user"
Root: HKLM; Subkey: "Software\Classes\CLSID\{{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell\MapFTPDriveSystem"; ValueType: string; ValueName: "MUIVerb"; ValueData: "Map FTP Drive (system-wide)"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\Classes\CLSID\{{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell\MapFTPDriveSystem"; ValueType: string; ValueName: "Icon"; ValueData: "{app}\MapFTPDrive.exe,0"
Root: HKLM; Subkey: "Software\Classes\CLSID\{{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell\MapFTPDriveSystem"; ValueType: string; ValueName: "Extended"; ValueData: ""
Root: HKLM; Subkey: "Software\Classes\CLSID\{{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell\MapFTPDriveSystem"; ValueType: string; ValueName: "HasLUAShield"; ValueData: ""
Root: HKLM; Subkey: "Software\Classes\CLSID\{{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell\MapFTPDriveSystem\command"; ValueType: string; ValueName: ""; ValueData: """{app}\MapFTPDrive.exe"" map --scope system"
Root: HKLM; Subkey: "Software\Classes\CLSID\{{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell\MapFTPDriveMount"; ValueType: string; ValueName: "MUIVerb"; ValueData: "Mount FTP Drive"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\Classes\CLSID\{{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell\MapFTPDriveMount"; ValueType: string; ValueName: "Icon"; ValueData: "{app}\MapFTPDrive.exe,0"
Root: HKLM; Subkey: "Software\Classes\CLSID\{{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell\MapFTPDriveMount\command"; ValueType: string; ValueName: ""; ValueData: """{app}\MapFTPDrive.exe"" mount"
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveUnmount"; ValueType: string; ValueName: "MUIVerb"; ValueData: "Unmount FTP Drive"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveUnmount"; ValueType: string; ValueName: "Icon"; ValueData: "{app}\MapFTPDrive.exe,0"
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveUnmount"; ValueType: string; ValueName: "AppliesTo"; ValueData: "System.Volume.FileSystem:=""FUSE-MapFTP"""
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveUnmount\command"; ValueType: string; ValueName: ""; ValueData: """{app}\MapFTPDrive.exe"" unmount --drive ""%V."""
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveRename"; ValueType: string; ValueName: "MUIVerb"; ValueData: "Rename FTP Drive"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveRename"; ValueType: string; ValueName: "Icon"; ValueData: "{app}\MapFTPDrive.exe,0"
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveRename"; ValueType: string; ValueName: "AppliesTo"; ValueData: "System.Volume.FileSystem:=""FUSE-MapFTP"""
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveRename\command"; ValueType: string; ValueName: ""; ValueData: """{app}\MapFTPDrive.exe"" rename --drive ""%V."""
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveRemove"; ValueType: string; ValueName: "MUIVerb"; ValueData: "Remove FTP Drive"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveRemove"; ValueType: string; ValueName: "Icon"; ValueData: "{app}\MapFTPDrive.exe,0"
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveRemove"; ValueType: string; ValueName: "Extended"; ValueData: ""
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveRemove"; ValueType: string; ValueName: "AppliesTo"; ValueData: "System.Volume.FileSystem:=""FUSE-MapFTP"""
Root: HKLM; Subkey: "Software\Classes\Drive\shell\MapFTPDriveRemove\command"; ValueType: string; ValueName: ""; ValueData: """{app}\MapFTPDrive.exe"" remove --drive ""%V."""

[Run]
Filename: "{app}\MapFTPDrive.exe"; Description: "Launch MapFTPDrive"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{app}\MapFTPDrive.exe"; Parameters: "uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveExplorerIntegration"

[Code]
const
  BundledWinFspVersion = '{#WinFspVersion}';

var
  RestartRequired: Boolean;

{ NextVersionPart removes and returns the leading dotted component of S. }
function NextVersionPart(var S: String): Integer;
var
  Dot: Integer;
begin
  Dot := Pos('.', S);
  if Dot = 0 then
  begin
    Result := StrToIntDef(S, 0);
    S := '';
  end
  else
  begin
    Result := StrToIntDef(Copy(S, 1, Dot - 1), 0);
    Delete(S, 1, Dot);
  end;
end;

{ CompareVersions compares dotted numeric versions: -1, 0 or 1. }
function CompareVersions(A, B: String): Integer;
var
  PartA, PartB: Integer;
begin
  Result := 0;
  while (Result = 0) and ((A <> '') or (B <> '')) do
  begin
    PartA := NextVersionPart(A);
    PartB := NextVersionPart(B);
    if PartA < PartB then
      Result := -1
    else if PartA > PartB then
      Result := 1;
  end;
end;

{ InstalledWinFspVersion reads the ProductVersion the WinFsp MSI records in
  its 32-bit registry key, or '' when WinFsp is absent. }
function InstalledWinFspVersion: String;
begin
  if not RegQueryStringValue(HKLM32, 'SOFTWARE\WinFsp', 'Version', Result) then
    Result := '';
end;

{ InstallWinFsp installs the bundled WinFsp MSI unless the same or a newer
  WinFsp is present: WinFsp is a shared runtime, and its MSI refuses a
  downgrade. It returns '' to continue Setup, or the message Setup shows
  before stopping. A 3010 result continues and asks for a restart at the
  end, because the runtime is in place once Windows restarts. }
function InstallWinFsp: String;
var
  Installed, LogFile, Parameters: String;
  ResultCode: Integer;
begin
  Result := '';
  Installed := InstalledWinFspVersion;
  if (Installed <> '') and (CompareVersions(Installed, BundledWinFspVersion) >= 0) then
  begin
    Log('WinFsp ' + Installed + ' is installed; the bundled ' + BundledWinFspVersion + ' is not needed.');
    Exit;
  end;
  ExtractTemporaryFile('winfsp.msi');
  LogFile := ExpandConstant('{%TEMP}\MapFTPDrive-WinFsp-install.log');
  Parameters := '/i "' + ExpandConstant('{tmp}\winfsp.msi') + '" /qn /norestart /l*v "' + LogFile + '"';
  if not Exec(ExpandConstant('{sys}\msiexec.exe'), Parameters, '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    Result := 'Windows Installer could not be started to install WinFsp: ' + SysErrorMessage(ResultCode);
    Exit;
  end;
  Log('WinFsp installer exit code: ' + IntToStr(ResultCode));
  case ResultCode of
    0: Log('WinFsp ' + BundledWinFspVersion + ' installed.');
    3010: RestartRequired := True;
    1638: Log('Another WinFsp version is installed and is kept.');
    1618: Result := 'Another installation is in progress. Wait for it to finish, then run MapFTPDrive Setup again.';
    1602: Result := 'The WinFsp installation was cancelled.';
    1603: Result := 'WinFsp could not be installed (Windows Installer error 1603). '
      + 'If an older WinFsp release is installed, uninstall it from Settings, Apps and run Setup again. '
      + 'Details are in ' + LogFile + '.';
  else
    Result := 'WinFsp could not be installed (Windows Installer error ' + IntToStr(ResultCode) + '). '
      + 'Details are in ' + LogFile + '.';
  end;
end;

{ StopService stops an installed MapFTPDrive service and waits for it, so
  Setup can replace its executable; the install step then reconfigures and
  restarts it. "net stop" exits with 2 when the service is absent or
  already stopped, which needs no action. }
procedure StopService;
var
  ResultCode: Integer;
begin
  if Exec(ExpandConstant('{sys}\net.exe'), 'stop MapFTPDrive', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
    Log('net stop MapFTPDrive exit code: ' + IntToStr(ResultCode));
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := InstallWinFsp;
  if Result = '' then
    StopService;
end;

function NeedRestart: Boolean;
begin
  Result := RestartRequired;
end;

{ RunInstallStep runs "MapFTPDrive.exe install", which secures the system
  data directory, removes per-user registration and installs, updates and
  restarts the service. The program writes its error to stderr, which is
  captured so a failure is reported instead of passing silently. }
procedure RunInstallStep;
var
  ResultCode: Integer;
  ErrorFile: String;
  Details: AnsiString;
begin
  ErrorFile := ExpandConstant('{tmp}\install-step-error.txt');
  if Exec(ExpandConstant('{cmd}'),
      '/C ""' + ExpandConstant('{app}\MapFTPDrive.exe') + '" install 2>"' + ErrorFile + '""',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode) and (ResultCode = 0) then
    Exit;
  if not LoadStringFromFile(ErrorFile, Details) then
    Details := '';
  Log('MapFTPDrive install step failed with exit code ' + IntToStr(ResultCode) + ': ' + String(Details));
  SuppressibleMsgBox('MapFTPDrive was copied, but its data directory or system service could not be set up:'
    + #13#10#13#10 + Trim(String(Details)) + #13#10#13#10
    + 'Run Setup again to repair the installation.', mbError, MB_OK, IDOK);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    WizardForm.StatusLabel.Caption := 'Securing MapFTPDrive data and installing its service...';
    RunInstallStep;
  end;
end;
