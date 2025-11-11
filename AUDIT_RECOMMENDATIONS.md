# PDF Pipeline Audit - Recommendations

## Critical Fix: Add File Deletion to Delete Handlers

### Current Issue
When a PDF is deleted via API, only the MongoDB document is removed. The physical file remains in `backend/storage/pdfs/`.

### Recommended Fix

Add file cleanup to `handleDeletePDF` in `backend/routes/client.go`:

```go
// After successful MongoDB deletion (line 286)
if deleteResult.DeletedCount > 0 {
    // Delete physical file from storage
    if err := os.Remove(pdfDoc.FilePath); err != nil && !os.IsNotExist(err) {
        fmt.Printf("Warning: Failed to delete file %s: %v\n", pdfDoc.FilePath, err)
    }
}
```

Apply same fix to `handleBulkDeletePDFs`:

```go
// After retrieving PDFs to delete
for _, pdf := range pdfs {
    if err := os.Remove(pdf.FilePath); err != nil && !os.IsNotExist(err) {
        fmt.Printf("Warning: Failed to delete file %s: %v\n", pdf.FilePath, err)
    }
}
```

## Storage Optimization Recommendation

### Add Retention Policy
Implement a cron job that:
1. Scans `backend/storage/pdfs/` for orphaned files (not in MongoDB)
2. Deletes files older than X days
3. Reports storage usage

```go
// Add to services/pdf_service.go
func (s *PDFService) CleanupOrphanedFiles(ctx context.Context, maxAgeDays int) error {
    cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
    
    // Get all MongoDB PDF records
    cursor, err := s.pdfsCollection.Find(ctx, bson.M{})
    if err != nil {
        return err
    }
    defer cursor.Close(ctx)
    
    // Build map of valid file paths
    validPaths := make(map[string]bool)
    for cursor.Next(ctx) {
        var pdf models.PDF
        if err := cursor.Decode(&pdf); err != nil {
            continue
        }
        validPaths[pdf.FilePath] = true
    }
    
    // Scan storage directory
    return filepath.Walk(s.storage.uploadDir, func(path string, info os.FileInfo, err error) error {
        if err != nil || info.IsDir() {
            return nil
        }
        
        // Check if file is orphaned
        if !validPaths[path] && info.ModTime().Before(cutoff) {
            os.Remove(path)
        }
        return nil
    })
}
```

## Remove Duplicate Storage Path

The `backend/routes/async_upload.go` handler saves files to `/tmp/pdfs/` in addition to the main storage. This is redundant and confusing.

**Recommendation**: Remove `async_upload.go` entirely or migrate it to use the centralized `FileStorageManager`.

## Summary

- **Success**: Upload → Extraction → MongoDB storage works perfectly
- **Critical**: Delete handlers don't clean local files (will cause disk bloat)
- **Recommend**: Add retention policy for long-term storage management
- **Recommend**: Consolidate storage paths to avoid duplication
