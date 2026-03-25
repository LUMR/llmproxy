@echo off
echo Setting up Claude Code proxy...

:: 检查 ZHIPU_API_KEY
if "%ZHIPU_API_KEY%"=="" (
    echo Error: ZHIPU_API_KEY environment variable is not set!
    echo Please set it with: set ZHIPU_API_KEY=your_api_key
    pause
    exit /b 1
)

:: 启动代理
echo Starting proxy server on http://localhost:8080
proxy.exe
