package services

import (
	"context"
	"fmt"
	"time"

	"saas-chatbot-platform/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AsyncProcessor handles background PDF processing
type AsyncProcessor struct {
	pdfService     *PDFService
	pdfsCollection *mongo.Collection
	workerCount    int
	stopChan       chan bool
}

// NewAsyncProcessor creates a new async processor
func NewAsyncProcessor(pdfService *PDFService, pdfsCollection *mongo.Collection, workerCount int) *AsyncProcessor {
	if workerCount <= 0 {
		workerCount = 2 // Default to 2 workers
	}

	return &AsyncProcessor{
		pdfService:     pdfService,
		pdfsCollection: pdfsCollection,
		workerCount:    workerCount,
		stopChan:       make(chan bool),
	}
}

// Start begins processing pending PDFs
func (ap *AsyncProcessor) Start() {
	fmt.Printf("Starting async PDF processor with %d workers\n", ap.workerCount)

	for i := 0; i < ap.workerCount; i++ {
		go ap.worker(i)
	}
}

// Stop gracefully stops all workers
func (ap *AsyncProcessor) Stop() {
	fmt.Println("Stopping async PDF processor...")
	close(ap.stopChan)
}

// worker processes PDFs in the background
func (ap *AsyncProcessor) worker(workerID int) {
	fmt.Printf("Worker %d started\n", workerID)

	ticker := time.NewTicker(10 * time.Second) // Check every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ap.stopChan:
			fmt.Printf("Worker %d stopped\n", workerID)
			return
		case <-ticker.C:
			ap.processPendingPDFs(workerID)
		}
	}
}

// processPendingPDFs finds and processes pending PDFs
func (ap *AsyncProcessor) processPendingPDFs(workerID int) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Find pending PDFs
	cursor, err := ap.pdfsCollection.Find(ctx, bson.M{
		"status": models.StatusPending,
	}, &options.FindOptions{
		Sort:  bson.M{"uploaded_at": 1}, // Process oldest first
		Limit: int64Ptr(1),              // Process one at a time
	})
	if err != nil {
		fmt.Printf("Worker %d: Error finding pending PDFs: %v\n", workerID, err)
		return
	}
	defer cursor.Close(ctx)

	var pdfs []models.PDF
	if err := cursor.All(ctx, &pdfs); err != nil {
		fmt.Printf("Worker %d: Error decoding PDFs: %v\n", workerID, err)
		return
	}

	if len(pdfs) == 0 {
		return // No pending PDFs
	}

	// Process each PDF
	for _, pdf := range pdfs {
		fmt.Printf("Worker %d: Processing PDF %s (%s)\n", workerID, pdf.ID.Hex(), pdf.OriginalName)

		// Update status to processing
		if err := ap.pdfService.updateStatus(ctx, pdf.ID, models.StatusProcessing, ""); err != nil {
			fmt.Printf("Worker %d: Failed to update status for %s: %v\n", workerID, pdf.ID.Hex(), err)
			continue
		}

		// Process the PDF
		if err := ap.pdfService.ProcessPDFSync(ctx, &pdf); err != nil {
			fmt.Printf("Worker %d: Failed to process PDF %s: %v\n", workerID, pdf.ID.Hex(), err)

			// Update status to failed
			ap.pdfService.updateStatus(context.Background(), pdf.ID, models.StatusFailed, err.Error())
		} else {
			fmt.Printf("Worker %d: Successfully processed PDF %s\n", workerID, pdf.ID.Hex())
		}
	}
}

// int64Ptr returns a pointer to an int64
func int64Ptr(i int64) *int64 {
	return &i
}

// ProcessPDFByID processes a specific PDF by ID
func (ap *AsyncProcessor) ProcessPDFByID(ctx context.Context, pdfID primitive.ObjectID) error {
	var pdf models.PDF
	err := ap.pdfsCollection.FindOne(ctx, bson.M{"_id": pdfID}).Decode(&pdf)
	if err != nil {
		return fmt.Errorf("PDF not found: %w", err)
	}

	if pdf.Status != models.StatusPending {
		return fmt.Errorf("PDF is not in pending status (current: %s)", pdf.Status)
	}

	// Update status to processing
	if err := ap.pdfService.updateStatus(ctx, pdf.ID, models.StatusProcessing, ""); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	// Process the PDF
	if err := ap.pdfService.ProcessPDFSync(ctx, &pdf); err != nil {
		// Update status to failed
		ap.pdfService.updateStatus(context.Background(), pdf.ID, models.StatusFailed, err.Error())
		return fmt.Errorf("processing failed: %w", err)
	}

	return nil
}
