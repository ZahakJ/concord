; Concord one-click Windows installer: no wizard, no admin prompt. Installs
; per-user into %LOCALAPPDATA%\Concord (which keeps the exe
; user-writable, so the app's built-in self-update can swap it in place),
; creates Start Menu + Desktop shortcuts and an Add/Remove Programs entry,
; then launches the app.
;
; Compiled by scripts/publish-release.sh via makensis (runs fine under wine):
;   makensis /DVERSION=v0.11.0 /DEXE=<desktop exe> /DICON=<icon.ico> \
;            /DOUT=<Concord-Setup-v0.11.0.exe> installer.nsi
;
; The output is named Concord-Setup-* on purpose: it carries no OS keyword,
; so the in-app updater can never mistake the INSTALLER for the app binary.

!define APPNAME "Concord"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPNAME}"

Name "${APPNAME}"
OutFile "${OUT}"
Unicode true
RequestExecutionLevel user
InstallDir "$LOCALAPPDATA\${APPNAME}"
Icon "${ICON}"
SetCompressor /SOLID lzma
ShowInstDetails nevershow
AutoCloseWindow true
BrandingText "${APPNAME} ${VERSION}"

Page instfiles

Section "Install"
  SetDetailsPrint none
  ; A running instance (reinstall/repair) would hold the exe lock — stop it.
  ; nsExec runs the console tool HIDDEN (ExecWait would flash a black box).
  nsExec::Exec 'taskkill /F /IM Concord.exe'
  Pop $0

  SetOutPath "$INSTDIR"
  File "/oname=Concord.exe" "${EXE}"
  WriteUninstaller "$INSTDIR\Uninstall.exe"

  CreateShortcut "$SMPROGRAMS\${APPNAME}.lnk" "$INSTDIR\Concord.exe"
  CreateShortcut "$DESKTOP\${APPNAME}.lnk" "$INSTDIR\Concord.exe"

  ; Add/Remove Programs (per-user hive; no admin needed).
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "${APPNAME}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\Concord.exe"
  WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "Concord contributors"
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
SectionEnd

Function .onInstSuccess
  Exec '"$INSTDIR\Concord.exe"'
FunctionEnd

Section "Uninstall"
  nsExec::Exec 'taskkill /F /IM Concord.exe'
  Pop $0
  Delete "$INSTDIR\Concord.exe"
  Delete "$INSTDIR\Concord.exe.old" ; parked binary from a self-update
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\${APPNAME}.lnk"
  Delete "$DESKTOP\${APPNAME}.lnk"
  DeleteRegKey HKCU "${UNINSTKEY}"
  ; The encrypted chat database (under %AppData%) is deliberately LEFT — an
  ; uninstall must never destroy someone's message history and identity.
SectionEnd
