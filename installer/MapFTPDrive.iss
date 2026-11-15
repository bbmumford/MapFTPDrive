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
Source: "{#WinFspMsi}"; DestDir: "{tmp}"; DestName: "winfsp.msi"; Flags: deleteafterinstall
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
Filename: "{sys}\msiexec.exe"; Parameters: "/i ""{tmp}\winfsp.msi"" /qn /norestart"; StatusMsg: "Installing the WinFsp filesystem runtime..."; Flags: waituntilterminated
Filename: "{app}\MapFTPDrive.exe"; Parameters: "install"; StatusMsg: "Securing MapFTPDrive data and installing its service..."; Flags: runhidden waituntilterminated
Filename: "{app}\MapFTPDrive.exe"; Description: "Launch MapFTPDrive"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{app}\MapFTPDrive.exe"; Parameters: "uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveExplorerIntegration"
