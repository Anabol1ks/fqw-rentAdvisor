@echo off
chcp 65001 > nul
echo Starting Resilient Yandex Scraper...
echo.

REM Загружаем настройки из .env.resilient если файл существует
if exist .env.resilient (
    echo Loading configuration from .env.resilient
    for /f "eol=# tokens=1,2 delims==" %%a in (.env.resilient) do (
        set %%a=%%b
    )
)

REM Устанавливаем значения по умолчанию если переменные не заданы
if not defined CITY set CITY=moskva
if not defined MAX_ITEMS set MAX_ITEMS=1000
if not defined PAGES set PAGES=100
if not defined PARALLELISM set PARALLELISM=2
if not defined MAX_RETRIES set MAX_RETRIES=5
if not defined CAPTCHA_COOLDOWN set CAPTCHA_COOLDOWN=10m
if not defined ERROR_THRESHOLD set ERROR_THRESHOLD=10
if not defined DEAL_TYPE set DEAL_TYPE=sale

echo Configuration:
echo   City: %CITY%
echo   Max Items: %MAX_ITEMS%
echo   Pages: %PAGES%
echo   Parallelism: %PARALLELISM%
echo   Max Retries: %MAX_RETRIES%
echo   Captcha Cooldown: %CAPTCHA_COOLDOWN%
echo   Error Threshold: %ERROR_THRESHOLD%
echo   Deal Type: %DEAL_TYPE%
echo.

REM Сборка приложения
echo Building scraper...
go build -o scrape_yandex.exe ./cmd/scrape_yandex
if %ERRORLEVEL% neq 0 (
    echo Build failed!
    pause
    exit /b 1
)

REM Запуск скрапера с оптимальными настройками
echo Starting scraping session...
echo Press Ctrl+C to stop gracefully
echo.

scrape_yandex.exe ^
    -city=%CITY% ^
    -max-items=%MAX_ITEMS% ^
    -pages=%PAGES% ^
    -parallel=%PARALLELISM% ^
    -max-retries=%MAX_RETRIES% ^
    -captcha-cooldown=%CAPTCHA_COOLDOWN% ^
    -error-threshold=%ERROR_THRESHOLD% ^
    -deal=%DEAL_TYPE% ^
    -delay-min=1500ms ^
    -delay-max=3000ms ^
    -base-retry-delay=3s ^
    -max-retry-delay=10m ^
    -protective-delay=45s ^
    -recovery-delay=3m ^
    -max-empty-pages=8 ^
    -user-agent-rotation=true ^
    -use-referer=true

echo.
echo Scraping session completed.
pause