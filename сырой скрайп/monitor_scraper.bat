@echo off
chcp 65001 > nul
setlocal enabledelayedexpansion

echo ===============================================
echo     Resilient Scraper Monitor v1.0
echo ===============================================
echo.

REM Настройки
set MAX_RESTART_COUNT=10
set RESTART_DELAY=300
set MIN_SUCCESS_ITEMS=50
set MONITOR_INTERVAL=60

REM Счетчики
set RESTART_COUNT=0
set TOTAL_ITEMS=0
set SESSION_START=%TIME%

echo Starting monitoring session...
echo Max restarts: %MAX_RESTART_COUNT%
echo Restart delay: %RESTART_DELAY% seconds
echo Monitor interval: %MONITOR_INTERVAL% seconds
echo.

:MAIN_LOOP
    echo [%TIME%] Starting scraper (attempt %RESTART_COUNT%/%MAX_RESTART_COUNT%)
    
    REM Запуск scraper с timeout и мониторингом
    timeout /t 2 /nobreak > nul
    call run_resilient_scraper.bat
    
    set SCRAPER_EXIT_CODE=%ERRORLEVEL%
    echo [%TIME%] Scraper finished with exit code: %SCRAPER_EXIT_CODE%
    
    REM Анализ результатов
    call :ANALYZE_RESULTS
    
    REM Проверка условий перезапуска
    if %RESTART_COUNT% geq %MAX_RESTART_COUNT% (
        echo Maximum restart count reached. Stopping monitor.
        goto :END
    )
    
    if %SCRAPER_EXIT_CODE% equ 0 (
        echo Scraper completed successfully.
        goto :END
    )
    
    echo Preparing for restart...
    set /a RESTART_COUNT+=1
    
    REM Прогрессивная задержка
    set /a CURRENT_DELAY=%RESTART_DELAY% * %RESTART_COUNT%
    if %CURRENT_DELAY% gtr 1800 set CURRENT_DELAY=1800
    
    echo Waiting %CURRENT_DELAY% seconds before restart...
    timeout /t %CURRENT_DELAY% /nobreak > nul
    
    REM Ротация прокси/User-Agent (можно добавить логику)
    call :ROTATE_CONFIG
    
    echo.
    goto :MAIN_LOOP

:ANALYZE_RESULTS
    echo Analyzing scraping results...
    
    REM Проверка логов на наличие капчи
    if exist scraper.log (
        findstr /i "captcha" scraper.log > nul
        if !ERRORLEVEL! equ 0 (
            echo Captcha detected in logs. Increasing delay.
            set /a RESTART_DELAY+=120
        )
        
        REM Подсчет успешных записей
        for /f "tokens=*" %%i in ('findstr /i "inserted new" scraper.log ^| find /c /v ""') do (
            set SESSION_ITEMS=%%i
            set /a TOTAL_ITEMS+=%%i
        )
        
        echo Session items: !SESSION_ITEMS!, Total items: !TOTAL_ITEMS!
    )
    
    REM Проверка файлов прогресса
    if exist progress (
        echo Progress files found - session can be resumed
        for %%f in (progress\*.json) do (
            echo - %%~nxf
        )
    )
    
    return

:ROTATE_CONFIG
    echo Rotating configuration...
    
    REM Здесь можно добавить логику ротации прокси, User-Agent и т.д.
    REM Например, изменение переменных окружения
    
    if defined PROXY_LIST (
        echo Rotating proxy configuration...
        REM Добавить логику ротации прокси
    )
    
    REM Можно добавить изменение других параметров
    echo Configuration rotated.
    return

:END
    echo.
    echo ===============================================
    echo      Monitoring Session Summary
    echo ===============================================
    echo Start time: %SESSION_START%
    echo End time: %TIME%
    echo Total restarts: %RESTART_COUNT%
    echo Total items collected: %TOTAL_ITEMS%
    echo Final exit code: %SCRAPER_EXIT_CODE%
    echo ===============================================
    
    if %TOTAL_ITEMS% geq %MIN_SUCCESS_ITEMS% (
        echo SUCCESS: Minimum target achieved!
    ) else (
        echo WARNING: Target not reached. Consider adjusting parameters.
    )
    
    echo.
    pause
    exit /b %SCRAPER_EXIT_CODE%