@echo off
REM Tesseract OCR Installation Script for Windows
REM This script helps install and configure Tesseract OCR

echo 🚀 Tesseract OCR Installation Guide
echo =====================================

echo.
echo 📋 Prerequisites:
echo - Windows 10/11
echo - Administrator privileges
echo - Internet connection

echo.
echo 🔧 Installation Steps:
echo.
echo 1. Download Tesseract OCR:
echo    Visit: https://github.com/UB-Mannheim/tesseract/wiki
echo    Download the latest Windows installer
echo.
echo 2. Install Tesseract:
echo    - Run the installer as Administrator
echo    - Choose "Add to PATH" during installation
echo    - Install language packs (eng, hin, etc.)
echo.
echo 3. Verify Installation:
echo    - Open Command Prompt as Administrator
echo    - Run: tesseract --version
echo    - You should see version information
echo.
echo 4. Restart Services:
echo    - Close all terminals
echo    - Restart the OCR service
echo.

echo 🔍 Checking current Tesseract status...
python -c "import pytesseract; print('Tesseract version:', pytesseract.get_tesseract_version())" 2>nul
if errorlevel 1 (
    echo ❌ Tesseract not found or not properly installed
    echo.
    echo 📥 Quick Download Links:
    echo - Windows 64-bit: https://github.com/UB-Mannheim/tesseract/releases/latest
    echo - Language Packs: https://github.com/tesseract-ocr/tessdata
    echo.
    echo ⚠️  After installation, restart this script to verify
) else (
    echo ✅ Tesseract is properly installed and configured
)

echo.
echo 📚 Language Support:
echo - English (eng): Default
echo - Hindi (hin): Download hin.traineddata
echo - Spanish (spa): Download spa.traineddata
echo - French (fra): Download fra.traineddata
echo.
echo 🌐 Language Pack Download:
echo https://github.com/tesseract-ocr/tessdata

echo.
echo 🔄 After installation:
echo 1. Restart Command Prompt
echo 2. Run: tesseract --version
echo 3. Restart OCR service
echo.

pause
