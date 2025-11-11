#!/usr/bin/env python3
"""
Simplified OCR Service for Testing
Uses PyMuPDF and basic image processing for PDF text extraction
"""

import asyncio
import base64
import io
import json
import logging
import os
import tempfile
import time
from typing import Dict, List, Optional, Union
from pathlib import Path

from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse
from pydantic import BaseModel
import uvicorn

# Basic OCR imports
try:
    import fitz  # PyMuPDF for PDF processing
    from PIL import Image
    import numpy as np
    import cv2
except ImportError as e:
    print(f"Missing dependencies: {e}")
    print("Install with: pip install pymupdf pillow opencv-python")
    exit(1)

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Initialize FastAPI app
app = FastAPI(
    title="Simple OCR Service",
    description="Basic OCR service using PyMuPDF and OpenCV",
    version="1.0.0"
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
class OCRRequest(BaseModel):
    file_data: str  # Base64 encoded file
    file_type: str  # "pdf" or "image"
    filename: str
    extract_tables: bool = True
    extract_images: bool = True
    confidence_threshold: float = 0.7

class TextChunk(BaseModel):
    text: str
    confidence: float
    page: int
    bbox: List[float]  # [x1, y1, x2, y2]
    chunk_type: str  # "text", "table", "image_caption"

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
    error: Optional[str] = None

class HealthResponse(BaseModel):
    status: str
    model_loaded: bool
    device: str
    version: str

def extract_text_from_pdf(pdf_data: bytes) -> List[Dict]:
    """Extract text and images from PDF using PyMuPDF"""
    try:
        doc = fitz.open(stream=pdf_data, filetype="pdf")
        pages_data = []
        
        for page_num in range(len(doc)):
            page = doc[page_num]
            
            # Extract text blocks
            text_blocks = page.get_text("dict")
            
            # Extract images
            image_list = page.get_images()
            images = []
            for img_index, img in enumerate(image_list):
                xref = img[0]
                pix = fitz.Pixmap(doc, xref)
                if pix.n - pix.alpha < 4:  # GRAY or RGB
                    img_data = pix.tobytes("png")
                    images.append({
                        "data": base64.b64encode(img_data).decode(),
                        "bbox": img[1:5],
                        "index": img_index
                    })
                pix = None
            
            pages_data.append({
                "page_num": page_num + 1,
                "text_blocks": text_blocks,
                "images": images,
                "width": page.rect.width,
                "height": page.rect.height
            })
        
        doc.close()
        return pages_data
    except Exception as e:
        logger.error(f"PDF processing error: {e}")
        raise

def calculate_quality_score(text: str) -> float:
    """Calculate quality score for extracted text"""
    if not text or len(text.strip()) < 10:
        return 0.0
    
    # Count different character types
    alphanumeric = sum(1 for c in text if c.isalnum())
    printable = sum(1 for c in text if c.isprintable())
    total = len(text)
    
    if total == 0:
        return 0.0
    
    # Base score from printable ratio
    score = (printable / total) * 0.4
    
    # Bonus for alphanumeric content
    alphanumeric_ratio = alphanumeric / total
    if alphanumeric_ratio >= 0.3:
        score += 0.3
    else:
        score += alphanumeric_ratio
    
    # Bonus for reasonable length
    if len(text) > 100:
        score += 0.1
    
    # Check for good patterns
    good_patterns = [
        r'\b[A-Z][a-z]+\b',  # Capitalized words
        r'\b\d+\b',          # Numbers
        r'[.!?]\s+[A-Z]',    # Sentence boundaries
    ]
    
    import re
    pattern_matches = sum(1 for pattern in good_patterns if re.search(pattern, text))
    if pattern_matches >= 2:
        score += 0.2
    
    return min(1.0, max(0.0, score))

def detect_language(text: str) -> str:
    """Simple language detection"""
    text_lower = text.lower()
    english_words = ["the", "and", "or", "of", "to", "in", "for", "with", "on", "at"]
    english_count = sum(text_lower.count(word) for word in english_words)
    
    if english_count > 10:
        return "en"
    return "unknown"

@app.get("/health", response_model=HealthResponse)
async def health_check():
    """Health check endpoint"""
    return HealthResponse(
        status="healthy",
        model_loaded=True,
        device="cpu",
        version="1.0.0"
    )

@app.post("/ocr/extract", response_model=OCRResponse)
async def extract_text(
    file: UploadFile = File(...),
    extract_tables: bool = Form(True),
    extract_images: bool = Form(True),
    confidence_threshold: float = Form(0.7)
):
    """Extract text from uploaded PDF or image file"""
    start_time = time.time()
    
    try:
        # Read file data
        file_data = await file.read()
        file_type = file.content_type
        
        if not file_data:
            raise HTTPException(status_code=400, detail="Empty file")
        
        # Process based on file type
        if "pdf" in file_type.lower():
            result = await process_pdf(file_data, extract_tables, extract_images, confidence_threshold)
        elif "image" in file_type.lower():
            result = await process_image(file_data, extract_tables, extract_images, confidence_threshold)
        else:
            raise HTTPException(status_code=400, detail="Unsupported file type")
        
        processing_time = time.time() - start_time
        
        return OCRResponse(
            success=True,
            text=result["text"],
            chunks=result["chunks"],
            pages=result["pages"],
            processing_time=processing_time,
            method="simple-ocr",
            quality_score=result["quality_score"],
            word_count=result["word_count"],
            character_count=result["character_count"],
            has_tables=result["has_tables"],
            has_images=result["has_images"],
            language=result["language"]
        )
        
    except Exception as e:
        logger.error(f"OCR processing error: {e}")
        return OCRResponse(
            success=False,
            text="",
            chunks=[],
            pages=0,
            processing_time=time.time() - start_time,
            method="simple-ocr",
            quality_score=0.0,
            word_count=0,
            character_count=0,
            has_tables=False,
            has_images=False,
            language="unknown",
            error=str(e)
        )

async def process_pdf(pdf_data: bytes, extract_tables: bool, extract_images: bool, confidence_threshold: float) -> Dict:
    """Process PDF file"""
    try:
        # Extract pages data from PDF
        pages_data = extract_text_from_pdf(pdf_data)
        
        all_text = ""
        all_chunks = []
        has_tables = False
        has_images = False
        
        for page_data in pages_data:
            page_num = page_data["page_num"]
            page_text = ""
            
            # Process text blocks
            for block in page_data["text_blocks"]["blocks"]:
                if "lines" in block:
                    for line in block["lines"]:
                        line_text = ""
                        for span in line["spans"]:
                            line_text += span["text"]
                        if line_text.strip():
                            page_text += line_text + "\n"
                            
                            # Create chunk
                            chunk = TextChunk(
                                text=line_text.strip(),
                                confidence=0.9,  # High confidence for text blocks
                                page=page_num,
                                bbox=line["bbox"],
                                chunk_type="text"
                            )
                            all_chunks.append(chunk)
            
            # Process images if requested
            if extract_images and page_data["images"]:
                has_images = True
                for img_data in page_data["images"]:
                    try:
                        # For now, just add a placeholder for image text
                        page_text += f"\n[Image {img_data['index']} detected]\n"
                        
                        chunk = TextChunk(
                            text=f"[Image {img_data['index']} detected]",
                            confidence=0.8,
                            page=page_num,
                            bbox=img_data["bbox"],
                            chunk_type="image_caption"
                        )
                        all_chunks.append(chunk)
                    except Exception as e:
                        logger.warning(f"Failed to process image on page {page_num}: {e}")
            
            all_text += f"\n--- PAGE {page_num} ---\n{page_text}\n"
        
        # Calculate metrics
        word_count = len(all_text.split())
        character_count = len(all_text)
        quality_score = calculate_quality_score(all_text)
        language = detect_language(all_text)
        
        return {
            "text": all_text,
            "chunks": all_chunks,
            "pages": len(pages_data),
            "quality_score": quality_score,
            "word_count": word_count,
            "character_count": character_count,
            "has_tables": has_tables,
            "has_images": has_images,
            "language": language
        }
        
    except Exception as e:
        logger.error(f"PDF processing error: {e}")
        raise

async def process_image(image_data: bytes, extract_tables: bool, extract_images: bool, confidence_threshold: float) -> Dict:
    """Process image file"""
    try:
        # For now, just return a placeholder
        text = "[Image processing not implemented in simple version]"
        word_count = len(text.split())
        character_count = len(text)
        quality_score = 0.5
        language = "unknown"
        
        chunks = [TextChunk(
            text=text,
            confidence=0.5,
            page=1,
            bbox=[0, 0, 100, 100],
            chunk_type="text"
        )]
        
        return {
            "text": text,
            "chunks": chunks,
            "pages": 1,
            "quality_score": quality_score,
            "word_count": word_count,
            "character_count": character_count,
            "has_tables": False,
            "has_images": False,
            "language": language
        }
        
    except Exception as e:
        logger.error(f"Image processing error: {e}")
        raise

if __name__ == "__main__":
    # Configuration
    host = os.getenv("OCR_HOST", "0.0.0.0")
    port = int(os.getenv("OCR_PORT", "8001"))
    
    logger.info(f"Starting Simple OCR service on {host}:{port}")
    uvicorn.run(app, host=host, port=port)
