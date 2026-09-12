Option Explicit

Dim shell
Set shell = CreateObject("WScript.Shell")

If WScript.Arguments.Named.Exists("check") Then
  WScript.Quit 0
End If

shell.Run "taskkill /F /IM screenshot-client.exe", 0, True
shell.Run "taskkill /F /IM screenshot-server.exe", 0, True

MsgBox "Background services stopped.", vbInformation, "screenshot-plat"
