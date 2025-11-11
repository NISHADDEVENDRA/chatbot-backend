# PowerShell script to install Tesseract OCR
# Run as Administrator

Write-Host "Installing Tesseract OCR for Windows..." -ForegroundColor Green

# Check if running as Administrator
if (-NOT ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole] "Administrator")) {
    Write-Host "This script requires Administrator privileges" -ForegroundColor Red
    Write-Host "Please run PowerShell as Administrator and try again" -ForegroundColor Yellow
    exit 1
}

# Download Tesseract installer
$installerUrl = "https://github.com/UB-Mannheim/tesseract/releases/latest/download/tesseract-ocr-w64-setup-5.3.3.20231005.exe"
$installerPath = "$env:TEMP\tesseract-installer.exe"

Write-Host "Downloading Tesseract installer..." -ForegroundColor Yellow
try {
    Invoke-WebRequest -Uri $installerUrl -OutFile $installerPath
    Write-Host "Download completed" -ForegroundColor Green
} catch {
    Write-Host "Download failed: $($_.Exception.Message)" -ForegroundColor Red
    Write-Host "Please download manually from: https://github.com/UB-Mannheim/tesseract/wiki" -ForegroundColor Yellow
    exit 1
}

# Install Tesseract
Write-Host "Installing Tesseract OCR..." -ForegroundColor Yellow
try {
    $installArgs = @(
        "/S"
        "/D=C:\Program Files\Tesseract-OCR"
    )
    
    Start-Process -FilePath $installerPath -ArgumentList $installArgs -Wait
    Write-Host "Installation completed" -ForegroundColor Green
} catch {
    Write-Host "Installation failed: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}

# Add to PATH
Write-Host "Adding Tesseract to PATH..." -ForegroundColor Yellow
$tesseractPath = "C:\Program Files\Tesseract-OCR"
$currentPath = [Environment]::GetEnvironmentVariable("PATH", "Machine")

if ($currentPath -notlike "*$tesseractPath*") {
    [Environment]::SetEnvironmentVariable("PATH", "$currentPath;$tesseractPath", "Machine")
    Write-Host "Added to system PATH" -ForegroundColor Green
} else {
    Write-Host "Already in PATH" -ForegroundColor Green
}

# Download language packs
Write-Host "Downloading language packs..." -ForegroundColor Yellow
$tessdataDir = "C:\Program Files\Tesseract-OCR\tessdata"

$languagePacks = @{
    "eng" = "https://github.com/tesseract-ocr/tessdata/raw/main/eng.traineddata"
    "hin" = "https://github.com/tesseract-ocr/tessdata/raw/main/hin.traineddata"
}

foreach ($lang in $languagePacks.Keys) {
    $packPath = "$tessdataDir\$lang.traineddata"
    if (-not (Test-Path $packPath)) {
        try {
            Write-Host "Downloading $lang language pack..." -ForegroundColor Cyan
            Invoke-WebRequest -Uri $languagePacks[$lang] -OutFile $packPath
            Write-Host "Downloaded $lang" -ForegroundColor Green
        } catch {
            Write-Host "Failed to download $lang" -ForegroundColor Red
        }
    } else {
        Write-Host "$lang language pack already exists" -ForegroundColor Green
    }
}

# Test installation
Write-Host "Testing installation..." -ForegroundColor Yellow
try {
    $version = & "C:\Program Files\Tesseract-OCR\tesseract.exe" --version
    Write-Host "Tesseract version: $($version[0])" -ForegroundColor Green
} catch {
    Write-Host "Test failed" -ForegroundColor Red
}

Write-Host "Installation completed!" -ForegroundColor Green
Write-Host "Next steps:" -ForegroundColor Yellow
Write-Host "1. Restart your terminal/command prompt" -ForegroundColor White
Write-Host "2. Run: python comprehensive_ocr_service.py" -ForegroundColor White
Write-Host "3. Test with: curl http://localhost:8001/health" -ForegroundColor White

# Cleanup
Remove-Item $installerPath -Force -ErrorAction SilentlyContinue
