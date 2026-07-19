@echo off
REM YuriScraper v0.5.0 — Windows build
set VERSION=0.5.0
echo Building YuriScraper v%VERSION%...
go build -ldflags="-s -w -X main.Version=%VERSION%" -o bin\yuri_scraper.exe .\cmd\52scraper\
if %ERRORLEVEL% EQU 0 (
    echo Build successful: bin\yuri_scraper.exe
) else (
    echo Build failed!
    pause
)
