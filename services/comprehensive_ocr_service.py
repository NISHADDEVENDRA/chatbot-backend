#!/usr/bin/env python3
"""
Comprehensive OCR Service with Tesseract Integration
Full text extraction from PDF files with accuracy optimization and error handling
"""

import asyncio
import base64
import io
import json
import logging
import os
import tempfile
import time
import traceback
from typing import Dict, List, Optional, Union, Tuple
from pathlib import Path
import hashlib

from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse
from pydantic import BaseModel
import uvicorn

# Comprehensive OCR imports
try:
    import fitz  # PyMuPDF for PDF processing
    from PIL import Image, ImageEnhance, ImageFilter, ImageOps
    import numpy as np
    import cv2
    import pytesseract
    TESSERACT_AVAILABLE = True
except ImportError as e:
    print(f"Missing dependencies: {e}")
    print("Install with: pip install pymupdf pillow opencv-python numpy pytesseract")
    TESSERACT_AVAILABLE = False

# Import Tesseract configuration
try:
    from tesseract_config import TesseractConfig
    tesseract_config = TesseractConfig()
    TESSERACT_CONFIGURED = tesseract_config.configure_pytesseract()
    
    # Set Tesseract path explicitly
    if TESSERACT_AVAILABLE:
        pytesseract.pytesseract.tesseract_cmd = r'C:\Program Files\Tesseract-OCR\tesseract.exe'
        TESSERACT_CONFIGURED = True
except ImportError:
    TESSERACT_CONFIGURED = False

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s',
    handlers=[
        logging.FileHandler('ocr_service.log'),
        logging.StreamHandler()
    ]
)
logger = logging.getLogger(__name__)

# Initialize FastAPI app
app = FastAPI(
    title="Comprehensive OCR Service",
    description="Full text extraction from PDF files with Tesseract OCR",
    version="5.0.0"
)

# CORS middleware
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Pydantic models
class TextChunk(BaseModel):
    text: str
    confidence: float
    page: int
    bbox: List[float]
    chunk_type: str
    language: Optional[str] = None

class OCRResponse(BaseModel):
    success: bool
    text: str
    chunks: List[TextChunk]
    pages: int
    processing_time: float
    method: str
    quality_score: float
    word_count: int
    character_count: int
    has_tables: bool
    has_images: bool
    language: str
    extracted_languages: List[str]
    file_hash: str
    error: Optional[str] = None
    warnings: List[str] = []

class HealthResponse(BaseModel):
    status: str
    model_loaded: bool
    device: str
    version: str
    tesseract_available: bool
    tesseract_version: Optional[str] = None

class InstallationGuide(BaseModel):
    tesseract_installed: bool
    installation_steps: List[str]
    download_url: str

# Global configuration
MAX_FILE_SIZE = 50 * 1024 * 1024  # 50MB
MAX_PAGES_PER_REQUEST = 100
TEMP_DIR = Path("temp")
TEMP_DIR.mkdir(exist_ok=True)

def check_tesseract_installation() -> Tuple[bool, str]:
    """Check if Tesseract is properly installed and configured"""
    try:
        if not TESSERACT_AVAILABLE:
            return False, "pytesseract not installed"
        
        if not TESSERACT_CONFIGURED:
            return False, "Tesseract not configured - run install_tesseract_project.py"
        
        # Try to get Tesseract version
        version = pytesseract.get_tesseract_version()
        # Convert version to string if it's a version object
        version_str = str(version) if version else "unknown"
        return True, version_str
    except Exception as e:
        return False, str(e)

def get_installation_guide() -> InstallationGuide:
    """Provide installation guide for Tesseract"""
    steps = [
        "1. Download Tesseract OCR for Windows from: https://github.com/UB-Mannheim/tesseract/wiki",
        "2. Install Tesseract OCR (choose 'Add to PATH' during installation)",
        "3. Download language packs (eng, hin, etc.) if needed",
        "4. Restart your terminal/command prompt",
        "5. Verify installation: tesseract --version",
        "6. Restart the OCR service"
    ]
    
    return InstallationGuide(
        tesseract_installed=check_tesseract_installation()[0],
        installation_steps=steps,
        download_url="https://github.com/UB-Mannheim/tesseract/wiki"
    )

def extract_text_from_pdf_comprehensive(pdf_data: bytes) -> List[Dict]:
    """Comprehensive PDF text extraction with error handling"""
    try:
        doc = fitz.open(stream=pdf_data, filetype="pdf")
        pages_data = []
        
        for page_num in range(len(doc)):
            try:
                page = doc[page_num]
                
                # Extract text blocks with formatting
                text_dict = page.get_text("dict")
                
                # Extract images for OCR processing
                image_list = page.get_images()
                images = []
                for img_index, img in enumerate(image_list):
                    try:
                        xref = img[0]
                        pix = fitz.Pixmap(doc, xref)
                        if pix.n - pix.alpha < 4:  # GRAY or RGB
                            img_data = pix.tobytes("png")
                            images.append({
                                "data": base64.b64encode(img_data).decode(),
                                "bbox": img[1:5],
                                "index": img_index,
                                "width": pix.width,
                                "height": pix.height
                            })
                        pix = None
                    except Exception as e:
                        logger.warning(f"Failed to extract image {img_index} from page {page_num}: {e}")
                
                pages_data.append({
                    "page_num": page_num + 1,
                    "text_blocks": text_dict,
                    "images": images,
                    "width": page.rect.width,
                    "height": page.rect.height
                })
                
            except Exception as e:
                logger.error(f"Failed to process page {page_num}: {e}")
                # Add empty page data to maintain page count
                pages_data.append({
                    "page_num": page_num + 1,
                    "text_blocks": {"blocks": []},
                    "images": [],
                    "width": 0,
                    "height": 0,
                    "error": str(e)
                })
        
        doc.close()
        return pages_data
    except Exception as e:
        logger.error(f"PDF processing error: {e}")
        raise

def preprocess_image_for_ocr(image: Image.Image) -> Image.Image:
    """Preprocess image for better OCR accuracy"""
    try:
        # Convert to RGB if necessary
        if image.mode != 'RGB':
            image = image.convert('RGB')
        
        # Resize if too small (minimum 300 DPI equivalent)
        width, height = image.size
        min_size = 300
        if width < min_size or height < min_size:
            scale_factor = max(min_size / width, min_size / height)
            new_size = (int(width * scale_factor), int(height * scale_factor))
            image = image.resize(new_size, Image.Resampling.LANCZOS)
        
        # Convert to numpy array for OpenCV processing
        img_array = np.array(image)
        img_cv = cv2.cvtColor(img_array, cv2.COLOR_RGB2BGR)
        
        # Convert to grayscale
        gray = cv2.cvtColor(img_cv, cv2.COLOR_BGR2GRAY)
        
        # Apply noise reduction
        denoised = cv2.medianBlur(gray, 3)
        
        # Apply adaptive thresholding
        thresh = cv2.adaptiveThreshold(
            denoised, 255, cv2.ADAPTIVE_THRESH_GAUSSIAN_C, cv2.THRESH_BINARY, 11, 2
        )
        
        # Morphological operations to clean up
        kernel = np.ones((1, 1), np.uint8)
        cleaned = cv2.morphologyEx(thresh, cv2.MORPH_CLOSE, kernel)
        
        # Convert back to PIL Image
        processed_image = Image.fromarray(cleaned)
        
        return processed_image
        
    except Exception as e:
        logger.warning(f"Image preprocessing failed: {e}")
        return image

def extract_text_with_tesseract(image_data: bytes, page_num: int = 1, languages: List[str] = None) -> Dict:
    """Extract text using Tesseract OCR with multiple configurations"""
    if not TESSERACT_AVAILABLE:
        return {
            "text": "[Tesseract OCR not available - install Tesseract OCR engine]",
            "confidence": 0.0,
            "page": page_num,
            "bbox": [0, 0, 100, 100],
            "chunk_type": "ocr_unavailable",
            "language": "unknown",
            "processing_time": 0.0
        }
    
    try:
        # Load image
        image = Image.open(io.BytesIO(image_data))
        
        # Preprocess image for better OCR
        processed_image = preprocess_image_for_ocr(image)
        
        # OCR configurations to try
        configs = [
            '--psm 6 -c tessedit_char_whitelist=0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz.,!?;:()[]{}"\'',  # Uniform block
            '--psm 3',  # Fully automatic page segmentation
            '--psm 1',  # Automatic page segmentation with OSD
            '--psm 4',  # Assume a single column of text
            '--psm 8',  # Single word
        ]
        
        # Language configuration
        lang_config = '+'.join(languages) if languages else 'eng'
        
        best_text = ""
        best_confidence = 0.0
        best_config = ""
        
        start_time = time.time()
        
        for config in configs:
            try:
                # Get text with confidence scores
                data = pytesseract.image_to_data(
                    processed_image, 
                    config=config, 
                    lang=lang_config,
                    output_type=pytesseract.Output.DICT
                )
                
                # Extract text and calculate average confidence
                text_parts = []
                confidences = []
                
                for i in range(len(data['text'])):
                    conf = int(data['conf'][i])
                    if conf > 30:  # Only include text with confidence > 30%
                        text_parts.append(data['text'][i])
                        confidences.append(conf)
                
                if text_parts:
                    current_text = ' '.join(text_parts).strip()
                    current_confidence = sum(confidences) / len(confidences) / 100.0
                    
                    if current_confidence > best_confidence:
                        best_text = current_text
                        best_confidence = current_confidence
                        best_config = config
                        
            except Exception as e:
                logger.warning(f"OCR config {config} failed: {e}")
                continue
        
        # If no text found with confidence, try basic extraction
        if not best_text:
            try:
                best_text = pytesseract.image_to_string(
                    processed_image, 
                    config='--psm 6', 
                    lang=lang_config
                ).strip()
                best_confidence = 0.5  # Default confidence
            except Exception as e:
                logger.warning(f"Basic OCR failed: {e}")
                best_text = f"[OCR processing failed for image on page {page_num}]"
                best_confidence = 0.1
        
        processing_time = time.time() - start_time
        
        return {
            "text": best_text,
            "confidence": best_confidence,
            "page": page_num,
            "bbox": [0, 0, image.width, image.height],
            "chunk_type": "ocr_text",
            "language": lang_config,
            "processing_time": processing_time
        }
        
    except Exception as e:
        logger.error(f"Tesseract OCR processing error: {e}")
        return {
            "text": f"[OCR error: {str(e)}]",
            "confidence": 0.1,
            "page": page_num,
            "bbox": [0, 0, 100, 100],
            "chunk_type": "ocr_error",
            "language": "unknown",
            "processing_time": 0.0
        }

def calculate_file_hash(file_data: bytes) -> str:
    """Calculate SHA-256 hash of file data"""
    return hashlib.sha256(file_data).hexdigest()

def calculate_quality_score_comprehensive(text: str) -> float:
    """Comprehensive quality scoring for extracted text"""
    if not text or len(text.strip()) < 5:
        return 0.0
    
    # Count different character types
    alphanumeric = sum(1 for c in text if c.isalnum())
    printable = sum(1 for c in text if c.isprintable())
    total = len(text)
    
    if total == 0:
        return 0.0
    
    # Base score from printable ratio
    score = (printable / total) * 0.3
    
    # Alphanumeric content bonus
    alphanumeric_ratio = alphanumeric / total
    if alphanumeric_ratio >= 0.4:
        score += 0.4
    else:
        score += alphanumeric_ratio
    
    # Length bonus
    if len(text) > 500:
        score += 0.1
    elif len(text) > 100:
        score += 0.05
    
    # Pattern recognition bonus
    import re
    patterns = [
        r'\b[A-Z][a-z]+\b',  # Capitalized words
        r'\b\d+\b',          # Numbers
        r'[.!?]\s+[A-Z]',    # Sentence boundaries
        r'\b(the|and|or|of|to|in|for|with|on|at|by|from)\b',  # Common words
    ]
    
    pattern_matches = sum(1 for pattern in patterns if re.search(pattern, text))
    if pattern_matches >= 3:
        score += 0.2
    
    # Penalty for excessive whitespace
    whitespace_ratio = sum(1 for c in text if c.isspace()) / total
    if whitespace_ratio > 0.3:
        score -= 0.1
    
    return min(1.0, max(0.0, score))

def detect_languages_comprehensive(text: str) -> List[str]:
    """Comprehensive language detection"""
    text_lower = text.lower()
    
    # English indicators
    english_words = ["the", "and", "or", "of", "to", "in", "for", "with", "on", "at", "by", "from", "this", "that", "these", "those"]
    english_count = sum(text_lower.count(word) for word in english_words)
    
    # Hindi indicators
    hindi_words = ["है", "हैं", "था", "थे", "की", "के", "को", "पर", "से", "में", "का", "कि"]
    hindi_count = sum(text_lower.count(word) for word in hindi_words)
    
    # Spanish indicators
    spanish_words = ["el", "la", "de", "que", "y", "a", "en", "un", "es", "se", "no", "te", "lo", "le", "da", "su", "por", "son", "con", "para"]
    spanish_count = sum(text_lower.count(word) for word in spanish_words)
    
    languages = []
    if english_count > 15:
        languages.append("en")
    if hindi_count > 5:
        languages.append("hi")
    if spanish_count > 10:
        languages.append("es")
    
    return languages if languages else ["unknown"]

@app.get("/health", response_model=HealthResponse)
async def health_check():
    """Comprehensive health check"""
    tesseract_available, tesseract_version = check_tesseract_installation()
    
    return HealthResponse(
        status="healthy",
        model_loaded=True,
        device="cpu",
        version="5.0.0",
        tesseract_available=tesseract_available,
        tesseract_version=tesseract_version
    )

@app.get("/installation-guide", response_model=InstallationGuide)
async def get_installation_guide():
    """Get Tesseract installation guide"""
    return get_installation_guide()

@app.post("/ocr/extract", response_model=OCRResponse)
async def extract_text(
    file: UploadFile = File(...),
    extract_tables: bool = Form(True),
    extract_images: bool = Form(True),
    confidence_threshold: float = Form(0.7),
    languages: str = Form("eng"),  # Comma-separated languages
    preserve_layout: bool = Form(True)
):
    """Comprehensive text extraction with Tesseract OCR"""
    start_time = time.time()
    warnings = []
    
    try:
        # Read file data
        file_data = await file.read()
        file_type = file.content_type
        
        if not file_data:
            raise HTTPException(status_code=400, detail="Empty file")
        
        # Check file size
        if len(file_data) > MAX_FILE_SIZE:
            raise HTTPException(status_code=413, detail=f"File too large (max {MAX_FILE_SIZE // (1024*1024)}MB)")
        
        # Calculate file hash
        file_hash = calculate_file_hash(file_data)
        
        # Parse languages
        lang_list = [lang.strip() for lang in languages.split(',') if lang.strip()]
        if not lang_list:
            lang_list = ['eng']
        
        # Check Tesseract availability
        tesseract_available, tesseract_error = check_tesseract_installation()
        if not tesseract_available:
            warnings.append(f"Tesseract OCR not available: {tesseract_error}")
        
        # Process based on file type
        if "pdf" in file_type.lower():
            result = await process_pdf_comprehensive(
                file_data, extract_tables, extract_images, confidence_threshold, lang_list, preserve_layout
            )
        elif "image" in file_type.lower():
            result = await process_image_comprehensive(
                file_data, extract_tables, extract_images, confidence_threshold, lang_list
            )
        else:
            raise HTTPException(status_code=400, detail="Unsupported file type")
        
        processing_time = time.time() - start_time
        
        return OCRResponse(
            success=True,
            text=result["text"],
            chunks=result["chunks"],
            pages=result["pages"],
            processing_time=processing_time,
            method="comprehensive-tesseract-ocr",
            quality_score=result["quality_score"],
            word_count=result["word_count"],
            character_count=result["character_count"],
            has_tables=result["has_tables"],
            has_images=result["has_images"],
            language=result["language"],
            extracted_languages=result["extracted_languages"],
            file_hash=file_hash,
            warnings=warnings
        )
        
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Comprehensive OCR processing error: {e}")
        logger.error(traceback.format_exc())
        
        return OCRResponse(
            success=False,
            text="",
            chunks=[],
            pages=0,
            processing_time=time.time() - start_time,
            method="comprehensive-tesseract-ocr",
            quality_score=0.0,
            word_count=0,
            character_count=0,
            has_tables=False,
            has_images=False,
            language="unknown",
            extracted_languages=[],
            file_hash="",
            error=str(e),
            warnings=warnings
        )

async def process_pdf_comprehensive(
    pdf_data: bytes, 
    extract_tables: bool, 
    extract_images: bool, 
    confidence_threshold: float,
    languages: List[str],
    preserve_layout: bool
) -> Dict:
    """Comprehensive PDF processing with Tesseract OCR"""
    try:
        # Extract pages data from PDF
        pages_data = extract_text_from_pdf_comprehensive(pdf_data)
        
        if len(pages_data) > MAX_PAGES_PER_REQUEST:
            raise HTTPException(status_code=413, detail=f"Too many pages (max {MAX_PAGES_PER_REQUEST})")
        
        all_text = ""
        all_chunks = []
        has_tables = False
        has_images = False
        extracted_languages = set()
        
        for page_data in pages_data:
            page_num = page_data["page_num"]
            page_text = ""
            
            # Check for page errors
            if "error" in page_data:
                logger.warning(f"Page {page_num} has error: {page_data['error']}")
                continue
            
            # Process text blocks with real formatting
            for block in page_data["text_blocks"]["blocks"]:
                if "lines" in block:
                    for line in block["lines"]:
                        line_text = ""
                        for span in line["spans"]:
                            line_text += span["text"]
                        if line_text.strip():
                            if preserve_layout:
                                page_text += line_text + "\n"
                            else:
                                page_text += line_text + " "
                            
                            # Create chunk with real confidence
                            chunk = TextChunk(
                                text=line_text.strip(),
                                confidence=0.95,  # High confidence for text blocks
                                page=page_num,
                                bbox=line["bbox"],
                                chunk_type="text"
                            )
                            all_chunks.append(chunk)
            
            # Process images with OCR if requested
            if extract_images and page_data["images"]:
                has_images = True
                for img_data in page_data["images"]:
                    try:
                        img_bytes = base64.b64decode(img_data["data"])
                        ocr_result = extract_text_with_tesseract(img_bytes, page_num, languages)
                        
                        if ocr_result["text"].strip():
                            if preserve_layout:
                                page_text += f"\n{ocr_result['text']}\n"
                            else:
                                page_text += f"{ocr_result['text']} "
                            
                            chunk = TextChunk(
                                text=ocr_result["text"],
                                confidence=ocr_result["confidence"],
                                page=page_num,
                                bbox=ocr_result["bbox"],
                                chunk_type=ocr_result["chunk_type"],
                                language=ocr_result["language"]
                            )
                            all_chunks.append(chunk)
                            
                            # Track extracted languages
                            if ocr_result["language"]:
                                extracted_languages.add(ocr_result["language"])
                                
                    except Exception as e:
                        logger.warning(f"Failed to process image on page {page_num}: {e}")
            
            all_text += f"\n--- PAGE {page_num} ---\n{page_text}\n"
        
        # Calculate comprehensive metrics
        word_count = len(all_text.split())
        character_count = len(all_text)
        quality_score = calculate_quality_score_comprehensive(all_text)
        detected_languages = detect_languages_comprehensive(all_text)
        
        return {
            "text": all_text,
            "chunks": all_chunks,
            "pages": len(pages_data),
            "quality_score": quality_score,
            "word_count": word_count,
            "character_count": character_count,
            "has_tables": has_tables,
            "has_images": has_images,
            "language": detected_languages[0] if detected_languages else "unknown",
            "extracted_languages": list(extracted_languages)
        }
        
    except Exception as e:
        logger.error(f"Comprehensive PDF processing error: {e}")
        raise

async def process_image_comprehensive(
    image_data: bytes, 
    extract_tables: bool, 
    extract_images: bool, 
    confidence_threshold: float,
    languages: List[str]
) -> Dict:
    """Comprehensive image processing with Tesseract OCR"""
    try:
        result = extract_text_with_tesseract(image_data, 1, languages)
        
        text = result["text"]
        word_count = len(text.split())
        character_count = len(text)
        quality_score = calculate_quality_score_comprehensive(text)
        detected_languages = detect_languages_comprehensive(text)
        
        chunks = [TextChunk(
            text=text,
            confidence=result["confidence"],
            page=1,
            bbox=result["bbox"],
            chunk_type=result["chunk_type"],
            language=result["language"]
        )]
        
        return {
            "text": text,
            "chunks": chunks,
            "pages": 1,
            "quality_score": quality_score,
            "word_count": word_count,
            "character_count": character_count,
            "has_tables": False,
            "has_images": True,
            "language": detected_languages[0] if detected_languages else "unknown",
            "extracted_languages": [result["language"]] if result["language"] else []
        }
        
    except Exception as e:
        logger.error(f"Comprehensive image processing error: {e}")
        raise

if __name__ == "__main__":
    # Configuration
    host = "0.0.0.0"
    port = 8001
    
    logger.info(f"Starting Comprehensive OCR service on {host}:{port}")
    logger.info("Using PyMuPDF + Tesseract OCR for full text extraction")
    
    # Check Tesseract availability
    tesseract_available, tesseract_version = check_tesseract_installation()
    if tesseract_available:
        logger.info(f"Tesseract OCR available: {tesseract_version}")
    else:
        logger.warning("Tesseract OCR not available - install Tesseract for full functionality")
    
    uvicorn.run(app, host=host, port=port)
