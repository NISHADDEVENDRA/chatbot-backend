#!/usr/bin/env python3
"""
Tesseract OCR Manual Installation Guide and Configuration
Provides step-by-step instructions for installing Tesseract OCR
"""

import os
import sys
import subprocess
import urllib.request
from pathlib import Path

def print_installation_guide():
    """Print comprehensive installation guide"""
    print("=" * 60)
    print("🔧 TESSERACT OCR INSTALLATION GUIDE")
    print("=" * 60)
    print()
    
    print("📋 STEP 1: Download Tesseract OCR")
    print("-" * 40)
    print("1. Visit: https://github.com/UB-Mannheim/tesseract/wiki")
    print("2. Download the latest Windows installer")
    print("3. Recommended version: tesseract-ocr-w64-setup-5.3.3.20231005.exe")
    print()
    
    print("📋 STEP 2: Install Tesseract OCR")
    print("-" * 40)
    print("1. Run the installer as Administrator")
    print("2. IMPORTANT: Check 'Add to PATH' during installation")
    print("3. Install to default location: C:\\Program Files\\Tesseract-OCR")
    print()
    
    print("📋 STEP 3: Download Language Packs")
    print("-" * 40)
    print("1. Navigate to: C:\\Program Files\\Tesseract-OCR\\tessdata")
    print("2. Download language packs from: https://github.com/tesseract-ocr/tessdata")
    print("3. Required files:")
    print("   - eng.traineddata (English)")
    print("   - hin.traineddata (Hindi)")
    print("   - spa.traineddata (Spanish) - Optional")
    print()
    
    print("📋 STEP 4: Verify Installation")
    print("-" * 40)
    print("1. Open Command Prompt as Administrator")
    print("2. Run: tesseract --version")
    print("3. You should see version information")
    print()
    
    print("📋 STEP 5: Configure Python")
    print("-" * 40)
    print("1. Install pytesseract: pip install pytesseract")
    print("2. Configure path in your Python code:")
    print("   import pytesseract")
    print("   pytesseract.pytesseract.tesseract_cmd = r'C:\\Program Files\\Tesseract-OCR\\tesseract.exe'")
    print()
    
    print("📋 STEP 6: Test Installation")
    print("-" * 40)
    print("1. Restart your terminal/command prompt")
    print("2. Run: python comprehensive_ocr_service.py")
    print("3. Test with: curl http://localhost:8001/health")
    print()
    
    print("=" * 60)

def download_language_packs():
    """Download language packs to project directory"""
    print("📥 Downloading language packs to project directory...")
    
    project_root = Path(__file__).parent
    tessdata_dir = project_root / "tessdata"
    tessdata_dir.mkdir(exist_ok=True)
    
    language_packs = {
        "eng": "https://github.com/tesseract-ocr/tessdata/raw/main/eng.traineddata",
        "hin": "https://github.com/tesseract-ocr/tessdata/raw/main/hin.traineddata",
        "spa": "https://github.com/tesseract-ocr/tessdata/raw/main/spa.traineddata"
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
    
    print(f"📁 Language packs saved to: {tessdata_dir}")

def configure_pytesseract():
    """Configure pytesseract with project-specific settings"""
    print("🔧 Configuring pytesseract...")
    
    try:
        import pytesseract
        
        # Try different possible paths
        possible_paths = [
            r"C:\Program Files\Tesseract-OCR\tesseract.exe",
            r"C:\Program Files (x86)\Tesseract-OCR\tesseract.exe",
            "tesseract"  # If in PATH
        ]
        
        tesseract_path = None
        for path in possible_paths:
            try:
                if path == "tesseract":
                    # Test if tesseract is in PATH
                    result = subprocess.run(['tesseract', '--version'], 
                                          capture_output=True, text=True, timeout=5)
                    if result.returncode == 0:
                        tesseract_path = "tesseract"
                        break
                else:
                    # Test specific path
                    result = subprocess.run([path, '--version'], 
                                          capture_output=True, text=True, timeout=5)
                    if result.returncode == 0:
                        tesseract_path = path
                        break
            except:
                continue
        
        if tesseract_path:
            pytesseract.pytesseract.tesseract_cmd = tesseract_path
            print(f"✅ Configured pytesseract path: {tesseract_path}")
            
            # Test configuration
            try:
                version = pytesseract.get_tesseract_version()
                print(f"✅ Tesseract version: {version}")
                return True
            except Exception as e:
                print(f"❌ Configuration test failed: {e}")
                return False
        else:
            print("❌ Tesseract binary not found")
            print("Please install Tesseract OCR first")
            return False
            
    except ImportError:
        print("❌ pytesseract not installed")
        print("Run: pip install pytesseract")
        return False

def test_ocr_functionality():
    """Test OCR functionality"""
    print("🧪 Testing OCR functionality...")
    
    try:
        import pytesseract
        from PIL import Image
        import numpy as np
        
        # Create a simple test image
        test_img = Image.new('RGB', (300, 100), color='white')
        
        # Test basic OCR
        result = pytesseract.image_to_string(test_img)
        print("✅ OCR functionality test passed")
        
        # Test with language specification
        result_eng = pytesseract.image_to_string(test_img, lang='eng')
        print("✅ English OCR test passed")
        
        return True
        
    except Exception as e:
        print(f"❌ OCR test failed: {e}")
        return False

def create_config_file():
    """Create configuration file for the project"""
    print("📝 Creating configuration file...")
    
    project_root = Path(__file__).parent
    config_file = project_root / "tesseract_config.json"
    
    config = {
        "tesseract_path": r"C:\Program Files\Tesseract-OCR\tesseract.exe",
        "tessdata_dir": str(project_root / "tessdata"),
        "languages": ["eng", "hin", "spa"],
        "installed": False,
        "configured": False
    }
    
    # Try to detect actual installation
    try:
        import pytesseract
        version = pytesseract.get_tesseract_version()
        config["installed"] = True
        config["configured"] = True
        config["version"] = version
    except:
        pass
    
    try:
        import json
        with open(config_file, 'w') as f:
            json.dump(config, f, indent=2)
        print(f"✅ Configuration file created: {config_file}")
    except Exception as e:
        print(f"❌ Failed to create config file: {e}")

def main():
    """Main function"""
    print("🚀 Tesseract OCR Setup and Configuration")
    print()
    
    # Print installation guide
    print_installation_guide()
    
    # Download language packs
    download_language_packs()
    
    # Try to configure pytesseract
    if configure_pytesseract():
        print("✅ Tesseract OCR is properly configured!")
        
        # Test functionality
        if test_ocr_functionality():
            print("🎉 OCR functionality is working correctly!")
        else:
            print("⚠️ OCR functionality test failed")
    else:
        print("❌ Tesseract OCR configuration failed")
        print("Please follow the installation guide above")
    
    # Create config file
    create_config_file()
    
    print("\n🎯 Next Steps:")
    print("1. If Tesseract is not installed, follow the guide above")
    print("2. Restart your terminal/command prompt")
    print("3. Run: python comprehensive_ocr_service.py")
    print("4. Test with: curl http://localhost:8001/health")

if __name__ == "__main__":
    main()
