@echo off
setlocal enabledelayedexpansion

:input
set /p "dir=请输入保存路径的txt文件位置: "

stage1stpost2md.exe -l !dir!

endlocal
pause