#!/usr/bin/env python3
"""
Simple Tesseract OCR Installation Script
Downloads and installs Tesseract OCR for Windows
"""

import os
import sys
import subprocess
import urllib.request
import tempfile
from pathlib import Path

def download_tesseract_installer():
    """Download the latest Tesseract installer"""
    print("🔍 Finding latest Tesseract installer...")
    
    # Try different download approaches
    download_urls = [
        "https://github.com/UB-Mannheim/tesseract/releases/latest/download/tesseract-ocr-w64-setup-5.3.3.20231005.exe",
        "https://github.com/UB-Mannheim/tesseract/releases/download/v5.3.3.20231005/tesseract-ocr-w64-setup-5.3.3.20231005.exe",
        "https://digi.bib.uni-mannheim.de/tesseract/tesseract-ocr-w64-setup-5.3.3.20231005.exe"
    ]
    
    for url in download_urls:
        try:
            print(f"📥 Trying to download from: {url}")
            installer_path = Path(tempfile.gettempdir()) / "tesseract-installer.exe"
            
            urllib.request.urlretrieve(url, installer_path)
            print(f"✅ Downloaded successfully: {installer_path}")
            return installer_path
        except Exception as e:
            print(f"❌ Failed: {e}")
            continue
    
    return None

def install_tesseract(installer_path):
    """Install Tesseract OCR"""
    print("🚀 Installing Tesseract OCR...")
    
    try:
        # Run installer with silent mode
        cmd = [
            str(installer_path),
            "/S",  # Silent install
            "/D=C:\\Program Files\\Tesseract-OCR"  # Install directory
        ]
        
        result = subprocess.run(cmd, capture_output=True, text=True)
        if result.returncode == 0:
            print("✅ Tesseract OCR installed successfully!")
            return True
        else:
            print(f"❌ Installation failed: {result.stderr}")
            return False
            
    except Exception as e:
        print(f"❌ Installation error: {e}")
        return False

def configure_pytesseract():
    """Configure pytesseract"""
    print("🔧 Configuring pytesseract...")
    
    try:
        import pytesseract
        
        # Set tesseract path
        tesseract_path = r"C:\Program Files\Tesseract-OCR\tesseract.exe"
        pytesseract.pytesseract.tesseract_cmd = tesseract_path
        
        # Test configuration
        version = pytesseract.get_tesseract_version()
        print(f"✅ pytesseract configured successfully!")
        print(f"✅ Tesseract version: {version}")
        return True
        
    except ImportError:
        print("❌ pytesseract not installed. Run: pip install pytesseract")
        return False
    except Exception as e:
        print(f"❌ Configuration failed: {e}")
        return False

def download_language_packs():
    """Download language packs"""
    print("📥 Downloading language packs...")
    
    tessdata_dir = Path(r"C:\Program Files\Tesseract-OCR\tessdata")
    tessdata_dir.mkdir(exist_ok=True)
    
    language_packs = {
        "eng": "https://github.com/tesseract-ocr/tessdata/raw/main/eng.traineddata",
        "hin": "https://github.com/tesseract-ocr/tessdata/raw/main/hin.traineddata"
    }
    
    for lang, url in language_packs.items():
        try:
            pack_path = tessdata_dir / f"{lang}.traineddata"
            if not pack_path.exists():
                print(f"📥 Downloading {lang} language pack...")
                urllib.request.urlretrieve(url, pack_path)
                print(f"✅ Downloaded: {pack_path}")
            else:
                print(f"✅ {lang} language pack already exists")
        except Exception as e:
            print(f"❌ Failed to download {lang}: {e}")

def test_installation():
    """Test Tesseract installation"""
    print("🧪 Testing installation...")
    
    try:
        # Test command line
        result = subprocess.run(['tesseract', '--version'], 
                              capture_output=True, text=True, timeout=10)
        if result.returncode == 0:
            print(f"✅ Command line test passed: {result.stdout.split()[1]}")
        else:
            print("❌ Command line test failed")
            return False
        
        # Test pytesseract
        import pytesseract
        from PIL import Image
        import numpy as np
        
        # Create test image
        test_img = Image.new('RGB', (200, 50), color='white')
        test_text = pytesseract.image_to_string(test_img)
        
        print("✅ pytesseract test passed")
        return True
        
    except Exception as e:
        print(f"❌ Test failed: {e}")
        return False

def main():
    """Main installation process"""
    print("🚀 Starting Tesseract OCR Installation...")
    
    # Check if already installed
    try:
        result = subprocess.run(['tesseract', '--version'], 
                              capture_output=True, text=True, timeout=5)
        if result.returncode == 0:
            print("✅ Tesseract is already installed!")
            configure_pytesseract()
            return True
    except:
        pass
    
    # Download installer
    installer_path = download_tesseract_installer()
    if not installer_path:
        print("❌ Failed to download installer")
        print("📥 Please download manually from: https://github.com/UB-Mannheim/tesseract/wiki")
        return False
    
    # Install Tesseract
    if install_tesseract(installer_path):
        # Download language packs
        download_language_packs()
        
        # Configure pytesseract
        configure_pytesseract()
        
        # Test installation
        if test_installation():
            print("🎉 Tesseract OCR installation completed successfully!")
            print("\n🎯 Next steps:")
            print("1. Restart your terminal/command prompt")
            print("2. Run: python comprehensive_ocr_service.py")
            print("3. Test with: curl http://localhost:8001/health")
            return True
    
    print("❌ Installation failed")
    return False

if __name__ == "__main__":
    main()
