#!/usr/bin/env python3
"""
Tesseract OCR Installation and Configuration Script
Automatically installs Tesseract OCR and language packs for the project
"""

import os
import sys
import subprocess
import platform
import urllib.request
import zipfile
import shutil
from pathlib import Path
import json

class TesseractInstaller:
    def __init__(self):
        self.project_root = Path(__file__).parent
        self.tesseract_dir = self.project_root / "tesseract"
        self.tessdata_dir = self.tesseract_dir / "tessdata"
        self.system = platform.system().lower()
        
    def check_tesseract_installed(self):
        """Check if Tesseract is already installed"""
        try:
            result = subprocess.run(['tesseract', '--version'], 
                                  capture_output=True, text=True, timeout=10)
            if result.returncode == 0:
                print(f"✅ Tesseract already installed: {result.stdout.split()[1]}")
                return True
        except (subprocess.TimeoutExpired, FileNotFoundError):
            pass
        
        # Check if pytesseract can find tesseract
        try:
            import pytesseract
            version = pytesseract.get_tesseract_version()
            print(f"✅ Tesseract found via pytesseract: {version}")
            return True
        except Exception as e:
            print(f"❌ Tesseract not found: {e}")
            return False
    
    def install_tesseract_windows(self):
        """Install Tesseract OCR for Windows"""
        print("🔧 Installing Tesseract OCR for Windows...")
        
        # Create tesseract directory
        self.tesseract_dir.mkdir(exist_ok=True)
        self.tessdata_dir.mkdir(exist_ok=True)
        
        # Download URLs for Windows Tesseract
        tesseract_urls = {
            "5.3.3": "https://github.com/UB-Mannheim/tesseract/releases/download/v5.3.3.20231005/tesseract-ocr-w64-setup-5.3.3.20231005.exe",
            "5.3.2": "https://github.com/UB-Mannheim/tesseract/releases/download/v5.3.2.20230714/tesseract-ocr-w64-setup-5.3.2.20230714.exe",
            "5.3.1": "https://github.com/UB-Mannheim/tesseract/releases/download/v5.3.1.20230401/tesseract-ocr-w64-setup-5.3.1.20230401.exe"
        }
        
        # Try to download latest version
        installer_path = None
        for version, url in tesseract_urls.items():
            try:
                print(f"📥 Downloading Tesseract {version}...")
                installer_path = self.tesseract_dir / f"tesseract-{version}.exe"
                
                urllib.request.urlretrieve(url, installer_path)
                print(f"✅ Downloaded: {installer_path}")
                break
            except Exception as e:
                print(f"❌ Failed to download {version}: {e}")
                continue
        
        if not installer_path or not installer_path.exists():
            print("❌ Failed to download Tesseract installer")
            return False
        
        # Install Tesseract silently
        try:
            print("🚀 Installing Tesseract OCR...")
            install_cmd = [
                str(installer_path),
                "/S",  # Silent install
                "/D=" + str(self.tesseract_dir / "tesseract-ocr")  # Install directory
            ]
            
            result = subprocess.run(installer_cmd, capture_output=True, text=True)
            if result.returncode == 0:
                print("✅ Tesseract OCR installed successfully")
                
                # Add to PATH
                tesseract_exe = self.tesseract_dir / "tesseract-ocr" / "tesseract.exe"
                if tesseract_exe.exists():
                    self.configure_pytesseract_path(str(tesseract_exe))
                    return True
            else:
                print(f"❌ Installation failed: {result.stderr}")
                
        except Exception as e:
            print(f"❌ Installation error: {e}")
        
        return False
    
    def install_tesseract_linux(self):
        """Install Tesseract OCR for Linux"""
        print("🔧 Installing Tesseract OCR for Linux...")
        
        try:
            # Update package list
            subprocess.run(['sudo', 'apt', 'update'], check=True)
            
            # Install Tesseract OCR
            subprocess.run(['sudo', 'apt', 'install', '-y', 'tesseract-ocr'], check=True)
            
            # Install additional language packs
            subprocess.run(['sudo', 'apt', 'install', '-y', 'tesseract-ocr-eng', 'tesseract-ocr-hin'], check=True)
            
            print("✅ Tesseract OCR installed successfully")
            return True
            
        except subprocess.CalledProcessError as e:
            print(f"❌ Installation failed: {e}")
            return False
    
    def install_tesseract_macos(self):
        """Install Tesseract OCR for macOS"""
        print("🔧 Installing Tesseract OCR for macOS...")
        
        try:
            # Install via Homebrew
            subprocess.run(['brew', 'install', 'tesseract'], check=True)
            subprocess.run(['brew', 'install', 'tesseract-lang'], check=True)
            
            print("✅ Tesseract OCR installed successfully")
            return True
            
        except subprocess.CalledProcessError as e:
            print(f"❌ Installation failed: {e}")
            return False
    
    def download_language_packs(self):
        """Download required language packs"""
        print("📥 Downloading language packs...")
        
        language_packs = {
            "eng": "https://github.com/tesseract-ocr/tessdata/raw/main/eng.traineddata",
            "hin": "https://github.com/tesseract-ocr/tessdata/raw/main/hin.traineddata",
            "spa": "https://github.com/tesseract-ocr/tessdata/raw/main/spa.traineddata",
            "fra": "https://github.com/tesseract-ocr/tessdata/raw/main/fra.traineddata"
        }
        
        for lang, url in language_packs.items():
            try:
                pack_path = self.tessdata_dir / f"{lang}.traineddata"
                if not pack_path.exists():
                    print(f"📥 Downloading {lang} language pack...")
                    urllib.request.urlretrieve(url, pack_path)
                    print(f"✅ Downloaded: {pack_path}")
                else:
                    print(f"✅ {lang} language pack already exists")
            except Exception as e:
                print(f"❌ Failed to download {lang}: {e}")
    
    def configure_pytesseract_path(self, tesseract_path=None):
        """Configure pytesseract to use correct binary path"""
        print("🔧 Configuring pytesseract...")
        
        if not tesseract_path:
            # Try to find tesseract in common locations
            possible_paths = [
                "tesseract",
                "C:\\Program Files\\Tesseract-OCR\\tesseract.exe",
                "C:\\Program Files (x86)\\Tesseract-OCR\\tesseract.exe",
                str(self.tesseract_dir / "tesseract-ocr" / "tesseract.exe"),
                "/usr/bin/tesseract",
                "/usr/local/bin/tesseract",
                "/opt/homebrew/bin/tesseract"
            ]
            
            for path in possible_paths:
                try:
                    result = subprocess.run([path, '--version'], 
                                          capture_output=True, text=True, timeout=5)
                    if result.returncode == 0:
                        tesseract_path = path
                        break
                except:
                    continue
        
        if tesseract_path:
            # Create configuration file
            config = {
                "tesseract_path": tesseract_path,
                "tessdata_dir": str(self.tessdata_dir),
                "languages": ["eng", "hin", "spa", "fra"]
            }
            
            config_path = self.project_root / "tesseract_config.json"
            with open(config_path, 'w') as f:
                json.dump(config, f, indent=2)
            
            print(f"✅ Configured pytesseract path: {tesseract_path}")
            print(f"✅ Configuration saved: {config_path}")
            return True
        else:
            print("❌ Could not find Tesseract binary")
            return False
    
    def test_installation(self):
        """Test Tesseract installation"""
        print("🧪 Testing Tesseract installation...")
        
        try:
            # Test basic functionality
            result = subprocess.run(['tesseract', '--version'], 
                                  capture_output=True, text=True, timeout=10)
            if result.returncode == 0:
                print(f"✅ Tesseract version: {result.stdout.split()[1]}")
                
                # Test pytesseract
                import pytesseract
                pytesseract.pytesseract.tesseract_cmd = self.get_tesseract_path()
                
                # Test with a simple image
                from PIL import Image
                import numpy as np
                
                # Create a simple test image
                test_img = Image.new('RGB', (200, 50), color='white')
                test_text = pytesseract.image_to_string(test_img)
                
                print("✅ pytesseract configuration successful")
                return True
            else:
                print(f"❌ Tesseract test failed: {result.stderr}")
                
        except Exception as e:
            print(f"❌ Test error: {e}")
        
        return False
    
    def get_tesseract_path(self):
        """Get configured Tesseract path"""
        config_path = self.project_root / "tesseract_config.json"
        if config_path.exists():
            with open(config_path, 'r') as f:
                config = json.load(f)
                return config.get("tesseract_path", "tesseract")
        return "tesseract"
    
    def install(self):
        """Main installation process"""
        print("🚀 Starting Tesseract OCR Installation...")
        print(f"📁 Project directory: {self.project_root}")
        print(f"🖥️  Operating system: {self.system}")
        
        # Check if already installed
        if self.check_tesseract_installed():
            print("✅ Tesseract is already installed and working!")
            return True
        
        # Install based on operating system
        success = False
        if self.system == "windows":
            success = self.install_tesseract_windows()
        elif self.system == "linux":
            success = self.install_tesseract_linux()
        elif self.system == "darwin":  # macOS
            success = self.install_tesseract_macos()
        else:
            print(f"❌ Unsupported operating system: {self.system}")
            return False
        
        if success:
            # Download language packs
            self.download_language_packs()
            
            # Configure pytesseract
            self.configure_pytesseract_path()
            
            # Test installation
            if self.test_installation():
                print("🎉 Tesseract OCR installation completed successfully!")
                return True
        
        print("❌ Tesseract OCR installation failed")
        return False

def main():
    """Main function"""
    installer = TesseractInstaller()
    success = installer.install()
    
    if success:
        print("\n🎯 Next steps:")
        print("1. Restart your terminal/command prompt")
        print("2. Run: python comprehensive_ocr_service.py")
        print("3. Test with: curl http://localhost:8001/health")
    else:
        print("\n❌ Installation failed. Please install Tesseract manually:")
        print("Windows: https://github.com/UB-Mannheim/tesseract/wiki")
        print("Linux: sudo apt install tesseract-ocr")
        print("macOS: brew install tesseract")

if __name__ == "__main__":
    main()
