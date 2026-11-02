//go:build windows

package app

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

// installUpdateAsset dispatches installation of a downloaded update asset
// based on its file type (Windows-specific formats).
func (a *App) installUpdateAsset(localPath string) error {
	lower := strings.ToLower(localPath)
	switch {
	case strings.HasSuffix(lower, ".exe"):
		return a.launchInstaller(localPath)
	case strings.HasSuffix(lower, ".7z"):
		return a.installSevenZ(localPath)
	default:
		return fmt.Errorf("unsupported update file type: %s", localPath)
	}
}

func (a *App) launchInstaller(localPath string) error {
	a.appendLog("[update] Launching installer: " + localPath)
	cmd := exec.Command(localPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start installer: %v", err)
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

// createNewConsole is CREATE_NEW_CONSOLE from winbase.h; the stdlib syscall
// package does not export it.
const createNewConsole = 0x00000010

func (a *App) installSevenZ(localPath string) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate application directory: %v", err)
	}
	execDir := filepath.Dir(execPath)
	if !isDirWritable(execDir) {
		return fmt.Errorf("dir_not_writable")
	}
	a.pendingUpdateMu.Lock()
	expectedSHA := a.pendingUpdateSHA
	a.pendingUpdateMu.Unlock()
	if expectedSHA == "" {
		return fmt.Errorf("no verified update digest, refusing to install")
	}
	base := filepath.Join(os.TempDir(), "flowweaver-update")
	stage := filepath.Join(base, "stage")
	os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0755); err != nil {
		return err
	}
	script := buildUpdateScript(localPath, stage, execDir, base, expectedSHA)
	// 通过 -EncodedCommand 从命令行传入脚本：不再往 %TEMP% 落盘可被同用户
	// 替换的 .ps1（脚本落盘 + 提权执行 = 换脚本的 TOCTOU 提权窗口）。
	// CREATE_NEW_CONSOLE 给 powershell 独立控制台窗口：GUI 父进程的 std
	// 句柄无效，直接继承会丢失脚本输出。
	cmd := exec.Command("powershell", "-NoProfile", "-EncodedCommand", psEncodeCommand(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch updater: %v", err)
	}
	go func() {
		_ = cmd.Wait()
	}()
	a.appendLog("[update] 7z update script launched (powershell, visible window)")
	return nil
}

// psQuoteSingle 将字符串安全嵌入 PowerShell 单引号字符串（' 转义为 ''）
func psQuoteSingle(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// psEncodeCommand 编码 powershell -EncodedCommand 参数：UTF-16LE 字节做标准 base64。
func psEncodeCommand(script string) string {
	u16 := utf16.Encode([]rune(script))
	b := make([]byte, len(u16)*2)
	for i, v := range u16 {
		b[2*i] = byte(v)
		b[2*i+1] = byte(v >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func buildUpdateScript(archive, stage, execDir, base, expectedSHA string) string {
	archive = psQuoteSingle(archive)
	stage = psQuoteSingle(stage)
	execDir = psQuoteSingle(execDir)
	base = psQuoteSingle(base)
	expectedSHA = psQuoteSingle(expectedSHA)
	return `$ErrorActionPreference = 'Continue'
$Host.UI.RawUI.WindowTitle = 'FlowWeaver 便携版更新'
Write-Host ''
Write-Host '========================================' -ForegroundColor Cyan
Write-Host '          FlowWeaver 便携版更新' -ForegroundColor Cyan
Write-Host '========================================' -ForegroundColor Cyan
Write-Host ''

$archive = '` + archive + `'
$stage = '` + stage + `'
$execDir = '` + execDir + `'
$base = '` + base + `'
$exe = Join-Path $execDir 'flowweaver.exe'
$expected = '` + expectedSHA + `'

Write-Host '[1/6] 正在校验更新包哈希...' -ForegroundColor Yellow
$actual = (Get-FileHash -Path $archive -Algorithm SHA256).Hash
if ($actual -ne $expected) {
    Write-Host "[错误] 更新包哈希不匹配（应为 $expected，实际为 $actual）。" -ForegroundColor Red
    Read-Host '按回车键退出'
    exit 1
}
Write-Host '      哈希校验通过。' -ForegroundColor Green

Write-Host '[2/6] 正在验证更新包内容...' -ForegroundColor Yellow
& tar -xf $archive -C $stage
if ($LASTEXITCODE -ne 0 -or -not (Test-Path (Join-Path $stage 'flowweaver.exe'))) {
    Write-Host '[错误] 更新包解压失败或内容不完整。' -ForegroundColor Red
    Read-Host '按回车键退出'
    exit 2
}
Write-Host '      更新包内容完整。' -ForegroundColor Green

Write-Host '[3/6] 正在停止旧进程...' -ForegroundColor Yellow
Stop-Process -Name 'flowweaver' -Force -ErrorAction SilentlyContinue
for ($i = 0; $i -lt 30; $i++) {
    if (-not (Get-Process -Name 'flowweaver' -ErrorAction SilentlyContinue)) { break }
    Start-Sleep -Milliseconds 500
}
Write-Host '      旧进程已退出。' -ForegroundColor Green

Write-Host '[4/6] 正在覆盖程序文件...' -ForegroundColor Yellow
if ((Get-FileHash -Path $archive -Algorithm SHA256).Hash -ne $expected) {
    Write-Host '[错误] 更新包在安装过程中被修改，已中止。' -ForegroundColor Red
    Read-Host '按回车键退出'
    exit 3
}
$ok = $false
for ($i = 0; $i -lt 10; $i++) {
    & tar -xf $archive -C $execDir flowweaver.exe rules
    if ($LASTEXITCODE -eq 0) { $ok = $true; break }
    Start-Sleep -Seconds 1
}
if (-not $ok) {
    Write-Host '[错误] 覆盖程序文件失败，文件可能仍被占用。' -ForegroundColor Red
    Read-Host '按回车键退出'
    exit 4
}
Write-Host '      文件已更新（保留个人设置）。' -ForegroundColor Green

Write-Host '[5/6] 正在启动新版本...' -ForegroundColor Yellow
for ($i = 0; $i -lt 30; $i++) {
    if (Get-Process -Name 'flowweaver' -ErrorAction SilentlyContinue) { break }
    Start-Process -FilePath $exe
    Start-Sleep -Seconds 3
}

Write-Host '[6/6] 清理临时文件...' -ForegroundColor Yellow
Start-Sleep -Seconds 1
Remove-Item -Path $base -Recurse -Force -ErrorAction SilentlyContinue

if (Get-Process -Name 'flowweaver' -ErrorAction SilentlyContinue) {
    Write-Host ''
    Write-Host '更新完成，FlowWeaver 已重新启动。' -ForegroundColor Green
    Start-Sleep -Seconds 2
} else {
    Write-Host ''
    Write-Host '[警告] 未检测到程序进程，请手动打开 FlowWeaver。' -ForegroundColor Yellow
    Read-Host '按回车键退出'
    exit 5
}
`
}
