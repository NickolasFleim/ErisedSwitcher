@echo off
rem Сборка Erised Switcher. Нужен Go 1.22+ (https://go.dev/dl/). Зависимости — в папке vendor.
setlocal
chcp 65001 >nul
cd /d "%~dp0"
go test ./... || goto :err
go build -trimpath -ldflags "-H windowsgui -s -w" -o ErisedSwitcher.exe ./cmd/erised || goto :err
echo.
echo Готово: %~dp0ErisedSwitcher.exe
goto :eof
:err
echo Сборка не удалась.
exit /b 1
