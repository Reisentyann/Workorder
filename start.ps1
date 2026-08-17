$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$Backend = Join-Path $Root "backend"
$Exe = Join-Path $Backend "workbench.exe"
$Frontend = Join-Path $Root "frontend"
$Addr = "http://localhost:8080"

Write-Host ""
Write-Host "=== AI 工单处理工作台 一键启动 ===" -ForegroundColor Cyan
Write-Host ""

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "[错误] 未检测到 Go 环境，请先安装 https://go.dev/dl/" -ForegroundColor Red
    Read-Host "按回车退出"
    exit 1
}

Write-Host "[1/3] 编译后端..." -ForegroundColor Yellow
Push-Location $Backend
go build -o workbench.exe ./cmd/workbench
if ($LASTEXITCODE -ne 0) {
    Pop-Location
    Write-Host "[错误] 编译失败" -ForegroundColor Red
    Read-Host "按回车退出"
    exit 1
}
Pop-Location

Write-Host "[2/3] 启动服务（演示模式）..." -ForegroundColor Yellow
$proc = Start-Process -FilePath $Exe -ArgumentList @("-demo", "-frontend", $Frontend) -WorkingDirectory $Backend -PassThru

$ready = $false
for ($i = 0; $i -lt 10; $i++) {
    Start-Sleep -Milliseconds 500
    try {
        $null = Invoke-RestMethod "$Addr/api/v1/tickets" -Method GET -TimeoutSec 2
        $ready = $true
        break
    } catch {}
}
if (-not $ready) {
    Write-Host "[错误] 服务启动失败（端口可能被占用）" -ForegroundColor Red
    Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
    Read-Host "按回车退出"
    exit 1
}

Write-Host "[3/3] 打开浏览器 $Addr" -ForegroundColor Yellow
Start-Process $Addr

Write-Host ""
Write-Host "服务运行中 (PID=$($proc.Id))" -ForegroundColor Green
Write-Host "  前端页面: $Addr"
Write-Host "  按 Ctrl+C 停止服务"
Write-Host ""

try {
    Wait-Process -Id $proc.Id -ErrorAction SilentlyContinue
} finally {
    if (-not $proc.HasExited) {
        Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
    }
    Write-Host "服务已停止" -ForegroundColor Cyan
}
