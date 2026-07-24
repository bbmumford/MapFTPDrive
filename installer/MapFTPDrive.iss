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
Root: HKLM; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "MapFTPDrive"; ValueData: """{app}\MapFTPDrive.exe"" daemon --scope user"; Flags: uninsdeletevalue

[Run]
Filename: "{sys}\msiexec.exe"; Parameters: "/i ""{tmp}\winfsp.msi"" /qn /norestart"; StatusMsg: "Installing the WinFsp filesystem runtime..."; Flags: waituntilterminated
Filename: "{app}\MapFTPDrive.exe"; Parameters: "install"; StatusMsg: "Registering Explorer commands..."; Flags: runhidden waituntilterminated
Filename: "{app}\MapFTPDrive.exe"; Description: "Launch MapFTPDrive"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{app}\MapFTPDrive.exe"; Parameters: "uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveExplorerIntegration"
