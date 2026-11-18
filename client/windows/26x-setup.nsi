Unicode true
!include "MUI2.nsh"
!include "x64.nsh"

Name "26x VPN"
OutFile "26X-Setup-x64.exe"
InstallDir "$PROGRAMFILES64\26x VPN"
RequestExecutionLevel admin
SetCompressor /SOLID lzma

!define MUI_ICON "runner\resources\app_icon.ico"
!define MUI_UNICON "runner\resources\app_icon.ico"
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "此安装包需要 64 位 Windows。"
    Abort
  ${EndIf}
FunctionEnd

Section "26x VPN"
  SetShellVarContext all
  SetRegView 64
  SetOutPath "$INSTDIR"
  File /r "..\build\windows\x64\runner\Release\*"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateShortcut "$SMPROGRAMS\26x VPN.lnk" "$INSTDIR\twentysixx_client.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "DisplayName" "26x VPN"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "DisplayIcon" "$INSTDIR\twentysixx_client.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "NoRepair" 1
SectionEnd

Section "Uninstall"
  SetShellVarContext all
  SetRegView 64
  Delete "$SMPROGRAMS\26x VPN.lnk"
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN"
  RMDir /r "$INSTDIR"
SectionEnd
