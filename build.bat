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

if exist "yuri_scraper.exe" del /Q "yuri_scraper.exe"

echo Built %VERSIONED_EXE%
".\%VERSIONED_EXE%" --version
