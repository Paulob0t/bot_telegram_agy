Set WshShell = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")
winDir = fso.GetParentFolderName(WScript.ScriptFullName)
psScript = winDir & "\tray_controller.ps1"
WshShell.Run "powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File """ & psScript & """", 0, False
