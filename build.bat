@echo off
setlocal enabledelayedexpansion

set SCRIPT_DIR=%~dp0
set FRONTEND_DIR=%SCRIPT_DIR%frontend
set BACKEND_DIR=%SCRIPT_DIR%backend
set STATIC_DIR=%BACKEND_DIR%\static

echo ==^> Building frontend...
cd /d "%FRONTEND_DIR%"

if not exist "node_modules" (
    echo ==^> Installing frontend dependencies...
    call npm install
)

echo ==^> Running vite build...
call npm run build

echo ==^> Cleaning static directory...
if exist "%STATIC_DIR%" rd /s /q "%STATIC_DIR%"
mkdir "%STATIC_DIR%"

echo ==^> Copying built files to backend/static...
if exist "%FRONTEND_DIR%\dist" (
    xcopy "%FRONTEND_DIR%\dist\*" "%STATIC_DIR%\" /s /e /y
    echo ==^> Frontend build completed successfully!
    echo ==^> Static files are in: %STATIC_DIR%
) else (
    echo Error: Frontend build output directory not found: %FRONTEND_DIR%\dist
    exit /b 1
)

echo.
echo ==^> Build summary:
echo     Frontend source: %FRONTEND_DIR%
echo     Build output:    %FRONTEND_DIR%\dist
echo     Copied to:       %STATIC_DIR%
echo.
echo ==^> You can now run the backend server to serve both API and static files.

endlocal
