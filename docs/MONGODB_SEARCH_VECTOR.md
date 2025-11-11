## MongoDB Atlas Search and Vector Search Setup

### Collections
- `pdfs`: existing, stores PDFs and `content_chunks`.
- `pdf_chunks`: new denormalized collection for search.

### Atlas Search Index (text)
Index name: `pdf_chunks_text`
Definition:
```json
{
  "mappings": {
    "dynamic": false,
    "fields": {
      "client_id": { "type": "objectId" },
      "pdf_id": { "type": "objectId" },
      "text": { "type": "string" },
      "keywords": { "type": "string" }
    }
  }
}
```

### Atlas Vector Search Index
Index name: `pdf_chunks_vector`
Definition:
```json
{
  "fields": [
    {
      "type": "vector",
      "path": "vector",
      "numDimensions": 768,
      "similarity": "cosine"
    }
  ]
}
```
Adjust `numDimensions` to match `VECTOR_DIM` in your env.

### Env config
```
MONGODB_SEARCH_ENABLED=true
MONGODB_VECTOR_ENABLED=true
MONGODB_SEARCH_INDEX=pdf_chunks_text
MONGODB_VECTOR_INDEX=pdf_chunks_vector
VECTOR_DIM=768
EMBEDDINGS_PROVIDER=google
GOOGLE_EMBEDDINGS_MODEL=text-embedding-004
```

### Notes
- Vector/Search indexes are created in Atlas (UI or Admin API). Driver creates only BTree indexes; we add those for filter fields automatically.
- If Vector Search is disabled, the system falls back to Atlas Text Search; if both are disabled, it falls back to keyword scoring.


