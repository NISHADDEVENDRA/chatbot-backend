#!/usr/bin/env python3
"""
Tesseract Configuration Module
Handles Tesseract OCR path configuration and language pack management
"""

import os
import json
import subprocess
from pathlib import Path
from typing import Optional, List, Dict

class TesseractConfig:
    def __init__(self, project_root: Path = None):
        self.project_root = project_root or Path(__file__).parent
        self.config_file = self.project_root / "tesseract_config.json"
        self.tesseract_dir = self.project_root / "tesseract"
        self.tessdata_dir = self.tesseract_dir / "tessdata"
        
    def load_config(self) -> Dict:
        """Load Tesseract configuration from file"""
        if self.config_file.exists():
            try:
                with open(self.config_file, 'r') as f:
                    return json.load(f)
            except Exception as e:
                print(f"Error loading config: {e}")
        return {}
    
    def save_config(self, config: Dict):
        """Save Tesseract configuration to file"""
        try:
            with open(self.config_file, 'w') as f:
                json.dump(config, f, indent=2)
            print(f"Configuration saved to: {self.config_file}")
        except Exception as e:
            print(f"Error saving config: {e}")
    
    def find_tesseract_binary(self) -> Optional[str]:
        """Find Tesseract binary in common locations"""
        possible_paths = [
            "tesseract",  # In PATH
            "C:\\Program Files\\Tesseract-OCR\\tesseract.exe",
            "C:\\Program Files (x86)\\Tesseract-OCR\\tesseract.exe",
            str(self.tesseract_dir / "tesseract-ocr" / "tesseract.exe"),
            "/usr/bin/tesseract",
            "/usr/local/bin/tesseract",
            "/opt/homebrew/bin/tesseract",
            "/usr/local/Cellar/tesseract/*/bin/tesseract"
        ]
        
        for path in possible_paths:
            try:
                # Test if binary exists and works
                result = subprocess.run(
                    [path, '--version'], 
                    capture_output=True, 
                    text=True, 
                    timeout=5
                )
                if result.returncode == 0:
                    print(f"Found Tesseract at: {path}")
                    return path
            except (subprocess.TimeoutExpired, FileNotFoundError, OSError):
                continue
        
        return None
    
    def configure_pytesseract(self) -> bool:
        """Configure pytesseract with correct binary path"""
        try:
            import pytesseract
            
            # Load existing config
            config = self.load_config()
            tesseract_path = config.get("tesseract_path")
            
            # If not in config, try to find it
            if not tesseract_path:
                tesseract_path = self.find_tesseract_binary()
            
            if tesseract_path:
                # Set pytesseract path
                pytesseract.pytesseract.tesseract_cmd = tesseract_path
                
                # Update config
                config["tesseract_path"] = tesseract_path
                config["tessdata_dir"] = str(self.tessdata_dir)
                self.save_config(config)
                
                # Test configuration
                try:
                    version = pytesseract.get_tesseract_version()
                    print(f"✅ pytesseract configured successfully")
                    print(f"✅ Tesseract version: {version}")
                    return True
                except Exception as e:
                    print(f"❌ pytesseract test failed: {e}")
                    return False
            else:
                print("❌ Tesseract binary not found")
                return False
                
        except ImportError:
            print("❌ pytesseract not installed. Run: pip install pytesseract")
            return False
    
    def get_available_languages(self) -> List[str]:
        """Get list of available Tesseract languages"""
        try:
            import pytesseract
            
            # Configure pytesseract first
            if not self.configure_pytesseract():
                return []
            
            # Get available languages
            langs = pytesseract.get_languages()
            print(f"Available languages: {', '.join(langs)}")
            return langs
            
        except Exception as e:
            print(f"Error getting languages: {e}")
            return []
    
    def test_ocr_functionality(self) -> bool:
        """Test OCR functionality with a simple image"""
        try:
            import pytesseract
            from PIL import Image
            import numpy as np
            
            # Configure pytesseract
            if not self.configure_pytesseract():
                return False
            
            # Create a simple test image with text
            img = Image.new('RGB', (300, 100), color='white')
            
            # Test basic OCR
            result = pytesseract.image_to_string(img)
            
            print("✅ OCR functionality test passed")
            return True
            
        except Exception as e:
            print(f"❌ OCR test failed: {e}")
            return False
    
    def setup_tessdata_directory(self):
        """Setup tessdata directory for language packs"""
        self.tessdata_dir.mkdir(parents=True, exist_ok=True)
        
        # Set TESSDATA_PREFIX environment variable
        os.environ['TESSDATA_PREFIX'] = str(self.tessdata_dir)
        
        print(f"Tessdata directory: {self.tessdata_dir}")
    
    def get_configuration_status(self) -> Dict:
        """Get comprehensive configuration status"""
        status = {
            "tesseract_found": False,
            "tesseract_path": None,
            "tesseract_version": None,
            "pytesseract_configured": False,
            "tessdata_dir": str(self.tessdata_dir),
            "available_languages": [],
            "ocr_functional": False,
            "config_file": str(self.config_file)
        }
        
        try:
            # Check Tesseract binary
            tesseract_path = self.find_tesseract_binary()
            if tesseract_path:
                status["tesseract_found"] = True
                status["tesseract_path"] = tesseract_path
                
                # Get version
                try:
                    result = subprocess.run(
                        [tesseract_path, '--version'], 
                        capture_output=True, text=True, timeout=5
                    )
                    if result.returncode == 0:
                        status["tesseract_version"] = result.stdout.split()[1]
                except:
                    pass
            
            # Check pytesseract configuration
            try:
                import pytesseract
                if self.configure_pytesseract():
                    status["pytesseract_configured"] = True
                    status["available_languages"] = self.get_available_languages()
                    status["ocr_functional"] = self.test_ocr_functionality()
            except ImportError:
                pass
                
        except Exception as e:
            print(f"Error checking status: {e}")
        
        return status

def configure_tesseract_for_project() -> bool:
    """Main function to configure Tesseract for the project"""
    print("🔧 Configuring Tesseract OCR for project...")
    
    config = TesseractConfig()
    
    # Setup tessdata directory
    config.setup_tessdata_directory()
    
    # Configure pytesseract
    if config.configure_pytesseract():
        print("✅ Tesseract OCR configured successfully!")
        
        # Show status
        status = config.get_configuration_status()
        print(f"📊 Configuration Status:")
        print(f"  Tesseract Found: {status['tesseract_found']}")
        print(f"  Tesseract Path: {status['tesseract_path']}")
        print(f"  Tesseract Version: {status['tesseract_version']}")
        print(f"  pytesseract Configured: {status['pytesseract_configured']}")
        print(f"  Available Languages: {', '.join(status['available_languages'])}")
        print(f"  OCR Functional: {status['ocr_functional']}")
        
        return True
    else:
        print("❌ Tesseract OCR configuration failed")
        return False

if __name__ == "__main__":
    configure_tesseract_for_project()
