Option Explicit

Dim shell, fso, root, serverPath, clientPath
Set shell = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")

root = fso.GetParentFolderName(WScript.ScriptFullName)
shell.CurrentDirectory = root
serverPath = fso.BuildPath(root, "bin\screenshot-server.exe")
clientPath = fso.BuildPath(root, "bin\screenshot-client.exe")

If WScript.Arguments.Named.Exists("check") Then
  WScript.Quit 0
End If

If Not fso.FileExists(serverPath) Or Not fso.FileExists(clientPath) Then
  MsgBox "Background programs are missing. Please rebuild first.", vbCritical, "screenshot-plat"
  WScript.Quit 1
End If

If Not IsRunning("screenshot-server.exe") Then
  shell.Run Quote(serverPath), 0, False
  WScript.Sleep 800
End If

If Not IsRunning("screenshot-client.exe") Then
  shell.Run Quote(clientPath), 0, False
End If

MsgBox "Background services started. Press F8 or use the mobile button to analyze.", vbInformation, "screenshot-plat"

Function IsRunning(imageName)
  Dim command, process, output
  command = "cmd /c tasklist /FI ""IMAGENAME eq " & imageName & """ /NH"
  Set process = shell.Exec(command)
  output = process.StdOut.ReadAll
  IsRunning = (InStr(1, output, imageName, vbTextCompare) > 0)
End Function

Function Quote(value)
  Quote = Chr(34) & value & Chr(34)
End Function
