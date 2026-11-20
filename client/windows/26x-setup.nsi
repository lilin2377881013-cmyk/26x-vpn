Unicode true
!include "MUI2.nsh"
!include "x64.nsh"

Name "26x VPN"
OutFile "26x-VPN-0.1.8-Setup-x64.exe"
InstallDir "$PROGRAMFILES64\26x VPN"
RequestExecutionLevel admin
SetCompressor /SOLID lzma
VIProductVersion "0.1.8.24"
VIAddVersionKey /LANG=2052 "ProductName" "26x VPN"
VIAddVersionKey /LANG=2052 "CompanyName" "26x"
VIAddVersionKey /LANG=2052 "FileDescription" "26x VPN 安装程序"
VIAddVersionKey /LANG=2052 "FileVersion" "0.1.8.24"
VIAddVersionKey /LANG=2052 "LegalCopyright" "Copyright 2026 26x"

!define MUI_ICON "runner\resources\app_icon.ico"
!define MUI_UNICON "runner\resources\app_icon.ico"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\twentysixx_client.exe"
!define MUI_FINISHPAGE_RUN_TEXT "打开 26x VPN"
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "此安装包需要 64 位 Windows。"
    Abort
  ${EndIf}
  StrCpy $INSTDIR "$PROGRAMFILES64\26x VPN"
FunctionEnd

Function un.onInit
  # NSIS runs a temporary copy of the uninstaller. Keep deletion confined
  # to the application's fixed install directory instead of that temp path.
  StrCpy $INSTDIR "$PROGRAMFILES64\26x VPN"
FunctionEnd

Section "26x VPN"
  SetShellVarContext all
  SetRegView 64
  SetOutPath "$INSTDIR"
  File /r /x "*.pdb" "..\build\windows\x64\runner\Release\*"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateShortcut "$SMPROGRAMS\26x VPN.lnk" "$INSTDIR\twentysixx_client.exe"
  CreateShortcut "$DESKTOP\26x VPN.lnk" "$INSTDIR\twentysixx_client.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "DisplayName" "26x VPN"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "DisplayVersion" "0.1.8"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "Publisher" "26x"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "URLInfoAbout" "https://26xvpn.com"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "DisplayIcon" "$INSTDIR\twentysixx_client.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN" "NoRepair" 1
SectionEnd

Section "Uninstall"
  SetShellVarContext all
  SetRegView 64
  Delete "$SMPROGRAMS\26x VPN.lnk"
  Delete "$DESKTOP\26x VPN.lnk"
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\26xVPN"
  RMDir /r "$INSTDIR"
SectionEnd
