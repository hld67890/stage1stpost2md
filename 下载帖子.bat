@echo off
setlocal enabledelayedexpansion

:input
set /p "num=请输入帖子id: "
:input
set /p "dir=请输入保存位置（以'\'结尾）: "

stage1stpost2md.exe !num! -d !dir!

endlocal
pause