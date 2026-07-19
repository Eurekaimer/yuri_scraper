@echo off
setlocal
cd /d "%~dp0"

if not exist "release\VERSION" (
    echo Missing release\VERSION
    exit /b 1
)

set /p VERSION=<"release\VERSION"
set "VERSIONED_EXE=yuri_scraper-v%VERSION%.exe"
set "LDFLAGS=-s -w -X main.Version=%VERSION%"

echo Building YuriScraper v%VERSION%...
go build -trimpath -buildvcs=false -ldflags="%LDFLAGS%" -o "%VERSIONED_EXE%" .\cmd\yuri_scraper\
if errorlevel 1 (
    echo Build failed.
    exit /b 1
)

copy /Y "%VERSIONED_EXE%" "yuri_scraper.exe" >nul
if errorlevel 1 (
    echo Failed to update yuri_scraper.exe.
    exit /b 1
)

echo Built %VERSIONED_EXE%
echo Updated yuri_scraper.exe
".\%VERSIONED_EXE%" --version
