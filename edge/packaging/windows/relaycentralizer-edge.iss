#define MyAppName "RelayCentralizer Edge"
#define MyAppVersion GetEnv("EDGE_VERSION")
#define MyAppPublisher "RelayCentralizer"
#define MyAppExeName "relaycentralizer-edge.exe"
#define EdgeSourceDir GetEnv("EDGE_SOURCE_DIR")
#define EdgeOutputDir GetEnv("EDGE_OUTPUT_DIR")
#define EdgeRepoRoot GetEnv("EDGE_REPO_ROOT")

[Setup]
AppId={{2D981A8F-93C5-4EAC-B429-1B11D93960D4}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={autopf}\RelayCentralizer Edge
DefaultGroupName={#MyAppName}
OutputDir={#EdgeOutputDir}
OutputBaseFilename=relaycentralizer-edge-windows-installer
ArchitecturesInstallIn64BitMode=x64compatible
Compression=lzma
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=admin
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\{#MyAppExeName}

[Tasks]
Name: "startupservice"; Description: "Register RelayCentralizer Edge to start automatically with Windows"; Flags: checkedonce
Name: "desktopicon"; Description: "Create a desktop icon"; Flags: unchecked

[Dirs]
Name: "{commonappdata}\RelayCentralizerEdge"

[Files]
Source: "{#EdgeSourceDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#EdgeRepoRoot}\edge\packaging\windows\Start-RelayCentralizerEdge.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#EdgeRepoRoot}\edge\packaging\windows\Install-RelayCentralizerEdgeTask.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#EdgeRepoRoot}\edge\packaging\windows\Uninstall-RelayCentralizerEdgeTask.ps1"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\Install-RelayCentralizerEdgeTask.ps1"""; Flags: runhidden; Tasks: startupservice

[UninstallRun]
Filename: "powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\Uninstall-RelayCentralizerEdgeTask.ps1"""; Flags: runhidden
