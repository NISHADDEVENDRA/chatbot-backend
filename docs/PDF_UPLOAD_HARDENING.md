# PDF Upload Hardening

This document summarizes production-grade safeguards added to the PDF upload pipeline and recommended operational settings.

## Changes implemented
- Remove sensitive token logging in `middleware/auth.go`.
- Cap audit request body capture to 1MB and skip multipart bodies in `middleware/audit.go`.
- Stream async PDF uploads to disk with secure file permissions (0600) in `routes/async_upload.go`.
- Validate PDF magic header without loading entire file; avoid `io.ReadAll` for uploads.
- Add extractor safety: context deadline checks and 200MB hard cap before in-memory reads in `services/pdf_extractor.go`.
- Provide configurable CORS helper `CORSMiddlewareWithOrigins` without breaking existing callers.
- Avoid duplicating large data by making `models.PDF.ContentChunks` optional via `omitempty`.

## Operational recommendations
- Enforce `FILE_STORAGE_DIR` to a non-world-readable path, owned by the service user.
- Set `MAX_FILE_SIZE` per tenant and enforce rate limits at the edge.
- Add PDF structural validation (header/trailer/xref) and page count caps using `pdfcpu`.
- Configure background job retries, retention, and DLQ for extraction tasks.
- Add metrics: upload latency, extraction method, chunk count, compression ratio; add tracing spans.

## Backward compatibility
- Existing CORS function kept; new function is additive.
- Model change uses `omitempty` only; existing documents remain valid.


