#!/usr/bin/env python3
"""
Test OCR service to see what error it's giving
"""

import requests
import json
import os

def test_ocr_with_real_pdf():
    """Test OCR service with a real PDF to see the error"""
    print("Testing OCR Service Error...")
    print("=" * 40)
    
    # Check if there's a real PDF in the storage directory
    storage_dir = "storage/pdfs"
    pdf_files = []
    
    if os.path.exists(storage_dir):
        for root, dirs, files in os.walk(storage_dir):
            for file in files:
                if file.endswith('.pdf'):
                    pdf_files.append(os.path.join(root, file))
    
    if not pdf_files:
        print("No PDF files found in storage directory")
        return False
    
    # Use the first PDF found
    test_pdf = pdf_files[0]
    print(f"Testing with PDF: {test_pdf}")
    
    try:
        with open(test_pdf, 'rb') as f:
            files = {'file': (os.path.basename(test_pdf), f, 'application/pdf')}
            data = {
                'extract_tables': 'true',
                'extract_images': 'true',
                'confidence_threshold': '0.7'
            }
            
            print("Uploading PDF to OCR service...")
            response = requests.post(
                "http://localhost:8001/ocr/extract",
                files=files,
                data=data,
                timeout=30
            )
            
            print(f"Response Status: {response.status_code}")
            print(f"Response Headers: {dict(response.headers)}")
            
            if response.status_code == 200:
                result = response.json()
                print("SUCCESS: OCR service processed the PDF!")
                print(f"  Method: {result['method']}")
                print(f"  Success: {result['success']}")
                print(f"  Quality Score: {result['quality_score']:.2f}")
                print(f"  Text Length: {len(result['text'])} characters")
                return True
            else:
                print(f"ERROR: Upload failed: HTTP {response.status_code}")
                print(f"Response Body: {response.text}")
                return False
                
    except Exception as e:
        print(f"ERROR: Test failed: {e}")
        return False

def main():
    """Main test function"""
    print("OCR Error Debug Test")
    print("=" * 30)
    
    success = test_ocr_with_real_pdf()
    
    if success:
        print("\nSUCCESS: OCR service is working with real PDFs!")
    else:
        print("\nERROR: OCR service has issues with real PDFs.")
        print("This explains why the Go backend is falling back to Gemini.")

if __name__ == "__main__":
    main()
