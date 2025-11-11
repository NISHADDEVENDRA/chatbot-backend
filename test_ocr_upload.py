#!/usr/bin/env python3
"""
Test OCR service by uploading a PDF directly
"""

import requests
import json
import os

def create_test_pdf():
    """Create a simple test PDF content"""
    # This is a minimal PDF content for testing
    pdf_content = b"""%PDF-1.4
1 0 obj
<<
/Type /Catalog
/Pages 2 0 R
>>
endobj

2 0 obj
<<
/Type /Pages
/Kids [3 0 R]
/Count 1
>>
endobj

3 0 obj
<<
/Type /Page
/Parent 2 0 R
/MediaBox [0 0 612 792]
/Contents 4 0 R
>>
endobj

4 0 obj
<<
/Length 44
>>
stream
BT
/F1 12 Tf
100 700 Td
(Test PDF for OCR) Tj
ET
endstream
endobj

xref
0 5
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000204 00000 n 
trailer
<<
/Size 5
/Root 1 0 R
>>
startxref
297
%%EOF"""
    
    with open("test_ocr.pdf", "wb") as f:
        f.write(pdf_content)
    return "test_ocr.pdf"

def test_ocr_upload():
    """Test OCR service with PDF upload"""
    print("Testing OCR Service with PDF Upload...")
    print("=" * 50)
    
    # Create test PDF
    test_file = create_test_pdf()
    print(f"Created test PDF: {test_file}")
    
    try:
        with open(test_file, 'rb') as f:
            files = {'file': (test_file, f, 'application/pdf')}
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
            
            if response.status_code == 200:
                result = response.json()
                
                if result['success']:
                    print("SUCCESS: OCR service processed the PDF!")
                    print(f"  Method: {result['method']}")
                    print(f"  Quality Score: {result['quality_score']:.2f}")
                    print(f"  Text Length: {len(result['text'])} characters")
                    print(f"  Pages: {result['pages']}")
                    print(f"  Processing Time: {result['processing_time']:.2f}s")
                    print(f"  Word Count: {result['word_count']}")
                    print(f"  Character Count: {result['character_count']}")
                    
                    # Show extracted text
                    if result['text']:
                        preview = result['text'][:200] + "..." if len(result['text']) > 200 else result['text']
                        print(f"  Extracted Text: {preview}")
                    
                    return True
                else:
                    print(f"ERROR: Processing failed: {result.get('error', 'Unknown error')}")
                    return False
            else:
                print(f"ERROR: Upload failed: HTTP {response.status_code}")
                print(f"Response: {response.text}")
                return False
                
    except Exception as e:
        print(f"ERROR: Test failed: {e}")
        return False
    finally:
        # Clean up test file
        if os.path.exists(test_file):
            os.remove(test_file)

def main():
    """Main test function"""
    print("OCR Service Upload Test")
    print("=" * 40)
    
    success = test_ocr_upload()
    
    if success:
        print("\nSUCCESS: OCR service is working correctly!")
        print("The issue is that your Go backend is not calling the OCR service.")
        print("\nTo fix this:")
        print("1. Make sure your Go backend is restarted")
        print("2. Check the Go backend logs for debug messages")
        print("3. Upload a PDF through your frontend and watch the logs")
    else:
        print("\nERROR: OCR service is not working properly.")
        print("Please check the OCR service logs.")

if __name__ == "__main__":
    main()
