package services

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"saas-chatbot-platform/internal/auth"
	"saas-chatbot-platform/models"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ExportRequest represents the request parameters for chat export
type ExportRequest struct {
	Format         string    `json:"format" binding:"required,oneof=json excel both"` // json, excel, both
	DateFrom       time.Time `json:"date_from,omitempty"`
	DateTo         time.Time `json:"date_to,omitempty"`
	ClientID       string    `json:"client_id,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Limit          int       `json:"limit,omitempty"`        // Max records to export (0 = no limit)
	IncludeGeo     bool      `json:"include_geo,omitempty"`  // Include geolocation data
	IncludeMeta    bool      `json:"include_meta,omitempty"` // Include metadata
}

// ExportResponse represents the response for export operations
type ExportResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	DownloadURL string `json:"download_url,omitempty"`
	FileSize    int64  `json:"file_size,omitempty"`
	RecordCount int    `json:"record_count,omitempty"`
}

// ChatExportData represents the structured data for export
type ChatExportData struct {
	ExportInfo ExportInfo      `json:"export_info"`
	Messages   []MessageExport `json:"messages"`
	Summary    ExportSummary   `json:"summary"`
}

type ExportInfo struct {
	ExportDate     time.Time `json:"export_date"`
	TotalRecords   int       `json:"total_records"`
	DateRange      string    `json:"date_range,omitempty"`
	ClientID       string    `json:"client_id,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Format         string    `json:"format"`
	IncludeGeo     bool      `json:"include_geo"`
	IncludeMeta    bool      `json:"include_meta"`
}

type MessageExport struct {
	ID             string    `json:"id"`
	FromName       string    `json:"from_name"`
	Message        string    `json:"message"`
	Reply          string    `json:"reply"`
	Timestamp      time.Time `json:"timestamp"`
	ConversationID string    `json:"conversation_id"`
	TokenCost      int       `json:"token_cost"`
	UserIP         string    `json:"user_ip,omitempty"`
	UserAgent      string    `json:"user_agent,omitempty"`
	Referrer       string    `json:"referrer,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	IsEmbedUser    bool      `json:"is_embed_user"`

	// Geolocation data (optional)
	GeoData *GeoDataExport `json:"geo_data,omitempty"`

	// Metadata (optional)
	MetaData *MetaDataExport `json:"meta_data,omitempty"`
}

type GeoDataExport struct {
	Country      string  `json:"country,omitempty"`
	CountryCode  string  `json:"country_code,omitempty"`
	Region       string  `json:"region,omitempty"`
	RegionName   string  `json:"region_name,omitempty"`
	City         string  `json:"city,omitempty"`
	Latitude     float64 `json:"latitude,omitempty"`
	Longitude    float64 `json:"longitude,omitempty"`
	Timezone     string  `json:"timezone,omitempty"`
	ISP          string  `json:"isp,omitempty"`
	Organization string  `json:"organization,omitempty"`
	IPType       string  `json:"ip_type,omitempty"`
}

type MetaDataExport struct {
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	ClientID   string    `json:"client_id"`
	FromUserID string    `json:"from_user_id,omitempty"`
	UserName   string    `json:"user_name,omitempty"`
	UserEmail  string    `json:"user_email,omitempty"`
}

type ExportSummary struct {
	TotalMessages     int               `json:"total_messages"`
	TotalTokens       int               `json:"total_tokens"`
	UniqueUsers       int               `json:"unique_users"`
	DateRange         string            `json:"date_range"`
	TopCountries      []CountryCount    `json:"top_countries,omitempty"`
	TopISPs           []ISPCount        `json:"top_isps,omitempty"`
	IPTypeBreakdown   map[string]int    `json:"ip_type_breakdown,omitempty"`
	ConversationStats ConversationStats `json:"conversation_stats"`
}

type CountryCount struct {
	Country string `json:"country"`
	Count   int    `json:"count"`
}

type ISPCount struct {
	ISP   string `json:"isp"`
	Count int    `json:"count"`
}

type ConversationStats struct {
	TotalConversations  int     `json:"total_conversations"`
	AvgMessagesPerConv  float64 `json:"avg_messages_per_conversation"`
	LongestConversation int     `json:"longest_conversation"`
}

// ExportService handles chat export operations
type ExportService struct {
	messagesCollection *mongo.Collection
	clientsCollection  *mongo.Collection
}

// NewExportService creates a new export service
func NewExportService(messagesCollection, clientsCollection *mongo.Collection) *ExportService {
	return &ExportService{
		messagesCollection: messagesCollection,
		clientsCollection:  clientsCollection,
	}
}

// ExportChats exports chat data in the requested format
func (es *ExportService) ExportChats(ctx context.Context, req *ExportRequest, userClaims *auth.Claims) (*ExportResponse, error) {
	// Build query filter
	filter := es.BuildQueryFilter(req, userClaims)

	// Set up pagination
	opts := options.Find()
	if req.Limit > 0 {
		opts.SetLimit(int64(req.Limit))
	}
	opts.SetSort(bson.D{primitive.E{Key: "timestamp", Value: -1}}) // Most recent first

	// Fetch messages
	cursor, err := es.messagesCollection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}
	defer cursor.Close(ctx)

	var messages []models.Message
	if err := cursor.All(ctx, &messages); err != nil {
		return nil, fmt.Errorf("failed to decode messages: %w", err)
	}

	if len(messages) == 0 {
		return &ExportResponse{
			Success:     true,
			Message:     "No messages found for the specified criteria",
			RecordCount: 0,
		}, nil
	}

	// Generate summary statistics
	summary, err := es.GenerateSummary(ctx, messages, req)
	if err != nil {
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	// Convert to export format
	exportData := es.ConvertToExportFormat(messages, req, summary)

	// Generate files based on format
	switch req.Format {
	case "json":
		return es.exportJSON(exportData)
	case "excel":
		return es.exportExcel(exportData)
	case "both":
		return es.exportBoth(exportData)
	default:
		return nil, fmt.Errorf("unsupported format: %s", req.Format)
	}
}

// BuildQueryFilter builds MongoDB query filter based on request parameters
func (es *ExportService) BuildQueryFilter(req *ExportRequest, userClaims *auth.Claims) bson.M {
	filter := bson.M{}

	// Add client ID filter (required for non-admin users)
	if userClaims.Role != "admin" {
		clientID, err := primitive.ObjectIDFromHex(userClaims.ClientID)
		if err == nil {
			filter["client_id"] = clientID
		}
	} else if req.ClientID != "" {
		clientID, err := primitive.ObjectIDFromHex(req.ClientID)
		if err == nil {
			filter["client_id"] = clientID
		}
	}

	// Add conversation ID filter
	if req.ConversationID != "" {
		filter["conversation_id"] = req.ConversationID
	}

	// Add date range filter
	if !req.DateFrom.IsZero() || !req.DateTo.IsZero() {
		dateFilter := bson.M{}
		if !req.DateFrom.IsZero() {
			dateFilter["$gte"] = req.DateFrom
		}
		if !req.DateTo.IsZero() {
			dateFilter["$lte"] = req.DateTo
		}
		filter["timestamp"] = dateFilter
	}

	return filter
}

// ConvertToExportFormat converts MongoDB messages to export format
func (es *ExportService) ConvertToExportFormat(messages []models.Message, req *ExportRequest, summary *ExportSummary) *ChatExportData {
	exportMessages := make([]MessageExport, len(messages))

	for i, msg := range messages {
		exportMsg := MessageExport{
			ID:             msg.ID.Hex(),
			FromName:       msg.FromName,
			Message:        msg.Message,
			Reply:          msg.Reply,
			Timestamp:      msg.Timestamp,
			ConversationID: msg.ConversationID,
			TokenCost:      msg.TokenCost,
			UserIP:         msg.UserIP,
			UserAgent:      msg.UserAgent,
			Referrer:       msg.Referrer,
			SessionID:      msg.SessionID,
			IsEmbedUser:    msg.IsEmbedUser,
		}

		// Add geolocation data if requested
		if req.IncludeGeo {
			exportMsg.GeoData = &GeoDataExport{
				Country:      msg.Country,
				CountryCode:  msg.CountryCode,
				Region:       msg.Region,
				RegionName:   msg.RegionName,
				City:         msg.City,
				Latitude:     msg.Latitude,
				Longitude:    msg.Longitude,
				Timezone:     msg.Timezone,
				ISP:          msg.ISP,
				Organization: msg.Organization,
				IPType:       msg.IPType,
			}
		}

		// Add metadata if requested
		if req.IncludeMeta {
			exportMsg.MetaData = &MetaDataExport{
				CreatedAt:  msg.Timestamp, // Using timestamp as created_at
				ClientID:   msg.ClientID.Hex(),
				FromUserID: msg.FromUserID.Hex(),
				UserName:   msg.UserName,
				UserEmail:  msg.UserEmail,
			}
		}

		exportMessages[i] = exportMsg
	}

	// Build date range string
	var dateRange string
	if !req.DateFrom.IsZero() || !req.DateTo.IsZero() {
		if !req.DateFrom.IsZero() && !req.DateTo.IsZero() {
			dateRange = fmt.Sprintf("%s to %s",
				req.DateFrom.Format("2006-01-02"),
				req.DateTo.Format("2006-01-02"))
		} else if !req.DateFrom.IsZero() {
			dateRange = fmt.Sprintf("From %s", req.DateFrom.Format("2006-01-02"))
		} else {
			dateRange = fmt.Sprintf("Until %s", req.DateTo.Format("2006-01-02"))
		}
	}

	return &ChatExportData{
		ExportInfo: ExportInfo{
			ExportDate:     time.Now(),
			TotalRecords:   len(messages),
			DateRange:      dateRange,
			ClientID:       req.ClientID,
			ConversationID: req.ConversationID,
			Format:         req.Format,
			IncludeGeo:     req.IncludeGeo,
			IncludeMeta:    req.IncludeMeta,
		},
		Messages: exportMessages,
		Summary:  *summary,
	}
}

// GenerateSummary generates summary statistics for the export
func (es *ExportService) GenerateSummary(ctx context.Context, messages []models.Message, req *ExportRequest) (*ExportSummary, error) {
	summary := &ExportSummary{
		TotalMessages:   len(messages),
		IPTypeBreakdown: make(map[string]int),
	}

	// Calculate total tokens
	totalTokens := 0
	uniqueUsers := make(map[string]bool)
	conversationCounts := make(map[string]int)
	countryCounts := make(map[string]int)
	ispCounts := make(map[string]int)

	for _, msg := range messages {
		totalTokens += msg.TokenCost
		uniqueUsers[msg.SessionID] = true
		conversationCounts[msg.ConversationID]++

		// Count countries
		if msg.Country != "" {
			countryCounts[msg.Country]++
		}

		// Count ISPs
		if msg.ISP != "" {
			ispCounts[msg.ISP]++
		}

		// Count IP types
		if msg.IPType != "" {
			summary.IPTypeBreakdown[msg.IPType]++
		}
	}

	summary.TotalTokens = totalTokens
	summary.UniqueUsers = len(uniqueUsers)

	// Calculate conversation stats
	conversationCount := len(conversationCounts)
	summary.ConversationStats.TotalConversations = conversationCount
	if conversationCount > 0 {
		summary.ConversationStats.AvgMessagesPerConv = float64(len(messages)) / float64(conversationCount)
	}

	// Find longest conversation
	longestConv := 0
	for _, count := range conversationCounts {
		if count > longestConv {
			longestConv = count
		}
	}
	summary.ConversationStats.LongestConversation = longestConv

	// Build date range string
	if !req.DateFrom.IsZero() || !req.DateTo.IsZero() {
		if !req.DateFrom.IsZero() && !req.DateTo.IsZero() {
			summary.DateRange = fmt.Sprintf("%s to %s",
				req.DateFrom.Format("2006-01-02"),
				req.DateTo.Format("2006-01-02"))
		} else if !req.DateFrom.IsZero() {
			summary.DateRange = fmt.Sprintf("From %s", req.DateFrom.Format("2006-01-02"))
		} else {
			summary.DateRange = fmt.Sprintf("Until %s", req.DateTo.Format("2006-01-02"))
		}
	}

	// Get top countries (limit to 10)
	summary.TopCountries = es.getTopItems(countryCounts, 10)

	// Get top ISPs (limit to 10)
	summary.TopISPs = es.getTopISPs(ispCounts, 10)

	return summary, nil
}

// getTopItems returns top items by count
func (es *ExportService) getTopItems(counts map[string]int, limit int) []CountryCount {
	var items []CountryCount
	for country, count := range counts {
		items = append(items, CountryCount{Country: country, Count: count})
	}

	// Sort by count (descending) and limit
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Count < items[j].Count {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	if len(items) > limit {
		items = items[:limit]
	}

	return items
}

// getTopISPs returns top ISPs by count
func (es *ExportService) getTopISPs(counts map[string]int, limit int) []ISPCount {
	var items []ISPCount
	for isp, count := range counts {
		items = append(items, ISPCount{ISP: isp, Count: count})
	}

	// Sort by count (descending) and limit
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Count < items[j].Count {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	if len(items) > limit {
		items = items[:limit]
	}

	return items
}

// exportJSON exports data as JSON
func (es *ExportService) exportJSON(data *ChatExportData) (*ExportResponse, error) {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return &ExportResponse{
		Success:     true,
		Message:     "JSON export generated successfully",
		FileSize:    int64(len(jsonData)),
		RecordCount: data.ExportInfo.TotalRecords,
	}, nil
}

// exportExcel exports data as Excel file
func (es *ExportService) exportExcel(data *ChatExportData) (*ExportResponse, error) {
	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Printf("Error closing Excel file: %v\n", err)
		}
	}()

	// Create main data sheet
	sheetName := "Chat Messages"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to create sheet: %w", err)
	}
	f.SetActiveSheet(index)

	// Set headers
	headers := []string{
		"ID", "From Name", "Message", "Reply", "Timestamp", "Conversation ID",
		"Token Cost", "User IP", "User Agent", "Referrer", "Session ID", "Is Embed User",
	}

	// Add geolocation headers if included
	if data.ExportInfo.IncludeGeo {
		geoHeaders := []string{
			"Country", "Country Code", "Region", "Region Name", "City",
			"Latitude", "Longitude", "Timezone", "ISP", "Organization", "IP Type",
		}
		headers = append(headers, geoHeaders...)
	}

	// Add metadata headers if included
	if data.ExportInfo.IncludeMeta {
		metaHeaders := []string{
			"Created At", "Client ID", "From User ID", "User Name", "User Email",
		}
		headers = append(headers, metaHeaders...)
	}

	// Write headers
	for i, header := range headers {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue(sheetName, cell, header)
	}

	// Write data rows
	for rowIdx, msg := range data.Messages {
		row := rowIdx + 2 // Start from row 2 (after headers)

		// Basic data
		f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), msg.ID)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), msg.FromName)
		f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), msg.Message)
		f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), msg.Reply)
		f.SetCellValue(sheetName, fmt.Sprintf("E%d", row), msg.Timestamp.Format("2006-01-02 15:04:05"))
		f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), msg.ConversationID)
		f.SetCellValue(sheetName, fmt.Sprintf("G%d", row), msg.TokenCost)
		f.SetCellValue(sheetName, fmt.Sprintf("H%d", row), msg.UserIP)
		f.SetCellValue(sheetName, fmt.Sprintf("I%d", row), msg.UserAgent)
		f.SetCellValue(sheetName, fmt.Sprintf("J%d", row), msg.Referrer)
		f.SetCellValue(sheetName, fmt.Sprintf("K%d", row), msg.SessionID)
		f.SetCellValue(sheetName, fmt.Sprintf("L%d", row), msg.IsEmbedUser)

		colOffset := 12 // After basic data columns

		// Add geolocation data if included
		if data.ExportInfo.IncludeGeo && msg.GeoData != nil {
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset, row), msg.GeoData.Country)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+1, row), msg.GeoData.CountryCode)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+2, row), msg.GeoData.Region)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+3, row), msg.GeoData.RegionName)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+4, row), msg.GeoData.City)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+5, row), msg.GeoData.Latitude)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+6, row), msg.GeoData.Longitude)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+7, row), msg.GeoData.Timezone)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+8, row), msg.GeoData.ISP)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+9, row), msg.GeoData.Organization)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+10, row), msg.GeoData.IPType)
			colOffset += 11
		}

		// Add metadata if included
		if data.ExportInfo.IncludeMeta && msg.MetaData != nil {
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset, row), msg.MetaData.CreatedAt.Format("2006-01-02 15:04:05"))
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+1, row), msg.MetaData.ClientID)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+2, row), msg.MetaData.FromUserID)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+3, row), msg.MetaData.UserName)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+4, row), msg.MetaData.UserEmail)
		}
	}

	// Auto-fit columns
	for i := 0; i < len(headers); i++ {
		col := fmt.Sprintf("%c:%c", 'A'+i, 'A'+i)
		f.SetColWidth(sheetName, col, col, 15)
	}

	// Create summary sheet
	summarySheetName := "Summary"
	_, err = f.NewSheet(summarySheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to create summary sheet: %w", err)
	}

	// Write summary data
	summaryData := [][]interface{}{
		{"Export Information", ""},
		{"Export Date", data.ExportInfo.ExportDate.Format("2006-01-02 15:04:05")},
		{"Total Records", data.ExportInfo.TotalRecords},
		{"Date Range", data.ExportInfo.DateRange},
		{"Format", data.ExportInfo.Format},
		{"Include Geolocation", data.ExportInfo.IncludeGeo},
		{"Include Metadata", data.ExportInfo.IncludeMeta},
		{"", ""},
		{"Summary Statistics", ""},
		{"Total Messages", data.Summary.TotalMessages},
		{"Total Tokens", data.Summary.TotalTokens},
		{"Unique Users", data.Summary.UniqueUsers},
		{"Total Conversations", data.Summary.ConversationStats.TotalConversations},
		{"Avg Messages per Conversation", fmt.Sprintf("%.2f", data.Summary.ConversationStats.AvgMessagesPerConv)},
		{"Longest Conversation", data.Summary.ConversationStats.LongestConversation},
	}

	for i, row := range summaryData {
		for j, cell := range row {
			cellRef := fmt.Sprintf("%c%d", 'A'+j, i+1)
			f.SetCellValue(summarySheetName, cellRef, cell)
		}
	}

	// Write top countries if available
	if len(data.Summary.TopCountries) > 0 {
		row := len(summaryData) + 3
		f.SetCellValue(summarySheetName, fmt.Sprintf("A%d", row), "Top Countries")
		f.SetCellValue(summarySheetName, fmt.Sprintf("B%d", row), "Count")
		row++

		for _, country := range data.Summary.TopCountries {
			f.SetCellValue(summarySheetName, fmt.Sprintf("A%d", row), country.Country)
			f.SetCellValue(summarySheetName, fmt.Sprintf("B%d", row), country.Count)
			row++
		}
	}

	// Write top ISPs if available
	if len(data.Summary.TopISPs) > 0 {
		row := len(summaryData) + 3 + len(data.Summary.TopCountries) + 2
		f.SetCellValue(summarySheetName, fmt.Sprintf("A%d", row), "Top ISPs")
		f.SetCellValue(summarySheetName, fmt.Sprintf("B%d", row), "Count")
		row++

		for _, isp := range data.Summary.TopISPs {
			f.SetCellValue(summarySheetName, fmt.Sprintf("A%d", row), isp.ISP)
			f.SetCellValue(summarySheetName, fmt.Sprintf("B%d", row), isp.Count)
			row++
		}
	}

	// Get Excel file as bytes
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("failed to write Excel file: %w", err)
	}

	return &ExportResponse{
		Success:     true,
		Message:     "Excel export generated successfully",
		FileSize:    int64(buf.Len()),
		RecordCount: data.ExportInfo.TotalRecords,
	}, nil
}

// exportBoth exports data as both JSON and Excel in a ZIP file
func (es *ExportService) exportBoth(data *ChatExportData) (*ExportResponse, error) {
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// Add JSON file
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	jsonFile, err := zipWriter.Create("chat_export.json")
	if err != nil {
		return nil, fmt.Errorf("failed to create JSON file in ZIP: %w", err)
	}
	if _, err := jsonFile.Write(jsonData); err != nil {
		return nil, fmt.Errorf("failed to write JSON data: %w", err)
	}

	// Add Excel file (we'll create it directly in the ZIP)

	excelFile, err := zipWriter.Create("chat_export.xlsx")
	if err != nil {
		return nil, fmt.Errorf("failed to create Excel file in ZIP: %w", err)
	}

	// We need to recreate the Excel file for the ZIP
	f := excelize.NewFile()
	defer f.Close()

	// Create main data sheet
	sheetName := "Chat Messages"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to create sheet: %w", err)
	}
	f.SetActiveSheet(index)

	// Set headers and data (same logic as exportExcel)
	headers := []string{
		"ID", "From Name", "Message", "Reply", "Timestamp", "Conversation ID",
		"Token Cost", "User IP", "User Agent", "Referrer", "Session ID", "Is Embed User",
	}

	if data.ExportInfo.IncludeGeo {
		geoHeaders := []string{
			"Country", "Country Code", "Region", "Region Name", "City",
			"Latitude", "Longitude", "Timezone", "ISP", "Organization", "IP Type",
		}
		headers = append(headers, geoHeaders...)
	}

	if data.ExportInfo.IncludeMeta {
		metaHeaders := []string{
			"Created At", "Client ID", "From User ID", "User Name", "User Email",
		}
		headers = append(headers, metaHeaders...)
	}

	// Write headers
	for i, header := range headers {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue(sheetName, cell, header)
	}

	// Write data rows
	for rowIdx, msg := range data.Messages {
		row := rowIdx + 2

		// Basic data
		f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), msg.ID)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), msg.FromName)
		f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), msg.Message)
		f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), msg.Reply)
		f.SetCellValue(sheetName, fmt.Sprintf("E%d", row), msg.Timestamp.Format("2006-01-02 15:04:05"))
		f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), msg.ConversationID)
		f.SetCellValue(sheetName, fmt.Sprintf("G%d", row), msg.TokenCost)
		f.SetCellValue(sheetName, fmt.Sprintf("H%d", row), msg.UserIP)
		f.SetCellValue(sheetName, fmt.Sprintf("I%d", row), msg.UserAgent)
		f.SetCellValue(sheetName, fmt.Sprintf("J%d", row), msg.Referrer)
		f.SetCellValue(sheetName, fmt.Sprintf("K%d", row), msg.SessionID)
		f.SetCellValue(sheetName, fmt.Sprintf("L%d", row), msg.IsEmbedUser)

		colOffset := 12

		if data.ExportInfo.IncludeGeo && msg.GeoData != nil {
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset, row), msg.GeoData.Country)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+1, row), msg.GeoData.CountryCode)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+2, row), msg.GeoData.Region)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+3, row), msg.GeoData.RegionName)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+4, row), msg.GeoData.City)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+5, row), msg.GeoData.Latitude)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+6, row), msg.GeoData.Longitude)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+7, row), msg.GeoData.Timezone)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+8, row), msg.GeoData.ISP)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+9, row), msg.GeoData.Organization)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+10, row), msg.GeoData.IPType)
			colOffset += 11
		}

		if data.ExportInfo.IncludeMeta && msg.MetaData != nil {
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset, row), msg.MetaData.CreatedAt.Format("2006-01-02 15:04:05"))
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+1, row), msg.MetaData.ClientID)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+2, row), msg.MetaData.FromUserID)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+3, row), msg.MetaData.UserName)
			f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+4, row), msg.MetaData.UserEmail)
		}
	}

	// Write Excel file to ZIP
	var excelBuf bytes.Buffer
	if err := f.Write(&excelBuf); err != nil {
		return nil, fmt.Errorf("failed to write Excel file: %w", err)
	}
	if _, err := excelFile.Write(excelBuf.Bytes()); err != nil {
		return nil, fmt.Errorf("failed to write Excel data to ZIP: %w", err)
	}

	// Close ZIP writer
	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close ZIP writer: %w", err)
	}

	return &ExportResponse{
		Success:     true,
		Message:     "ZIP export with JSON and Excel files generated successfully",
		FileSize:    int64(buf.Len()),
		RecordCount: data.ExportInfo.TotalRecords,
	}, nil
}

// StreamExport streams export data directly to HTTP response
func (es *ExportService) StreamExport(ctx *gin.Context, data *ChatExportData, format string) error {
	switch format {
	case "json":
		ctx.Header("Content-Type", "application/json")
		ctx.Header("Content-Disposition", "attachment; filename=chat_export.json")

		jsonData, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}

		ctx.Header("Content-Length", strconv.Itoa(len(jsonData)))
		ctx.Data(http.StatusOK, "application/json", jsonData)

	case "excel":
		ctx.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		ctx.Header("Content-Disposition", "attachment; filename=chat_export.xlsx")

		// Create Excel file in memory
		f := excelize.NewFile()
		defer f.Close()

		// Create main data sheet
		sheetName := "Chat Messages"
		index, err := f.NewSheet(sheetName)
		if err != nil {
			return fmt.Errorf("failed to create sheet: %w", err)
		}
		f.SetActiveSheet(index)

		// Set headers
		headers := []string{
			"ID", "From Name", "Message", "Reply", "Timestamp", "Conversation ID",
			"Token Cost", "User IP", "User Agent", "Referrer", "Session ID", "Is Embed User",
		}

		if data.ExportInfo.IncludeGeo {
			geoHeaders := []string{
				"Country", "Country Code", "Region", "Region Name", "City",
				"Latitude", "Longitude", "Timezone", "ISP", "Organization", "IP Type",
			}
			headers = append(headers, geoHeaders...)
		}

		if data.ExportInfo.IncludeMeta {
			metaHeaders := []string{
				"Created At", "Client ID", "From User ID", "User Name", "User Email",
			}
			headers = append(headers, metaHeaders...)
		}

		// Write headers
		for i, header := range headers {
			cell := fmt.Sprintf("%c1", 'A'+i)
			f.SetCellValue(sheetName, cell, header)
		}

		// Write data rows
		for rowIdx, msg := range data.Messages {
			row := rowIdx + 2

			// Basic data
			f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), msg.ID)
			f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), msg.FromName)
			f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), msg.Message)
			f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), msg.Reply)
			f.SetCellValue(sheetName, fmt.Sprintf("E%d", row), msg.Timestamp.Format("2006-01-02 15:04:05"))
			f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), msg.ConversationID)
			f.SetCellValue(sheetName, fmt.Sprintf("G%d", row), msg.TokenCost)
			f.SetCellValue(sheetName, fmt.Sprintf("H%d", row), msg.UserIP)
			f.SetCellValue(sheetName, fmt.Sprintf("I%d", row), msg.UserAgent)
			f.SetCellValue(sheetName, fmt.Sprintf("J%d", row), msg.Referrer)
			f.SetCellValue(sheetName, fmt.Sprintf("K%d", row), msg.SessionID)
			f.SetCellValue(sheetName, fmt.Sprintf("L%d", row), msg.IsEmbedUser)

			colOffset := 12

			if data.ExportInfo.IncludeGeo && msg.GeoData != nil {
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset, row), msg.GeoData.Country)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+1, row), msg.GeoData.CountryCode)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+2, row), msg.GeoData.Region)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+3, row), msg.GeoData.RegionName)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+4, row), msg.GeoData.City)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+5, row), msg.GeoData.Latitude)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+6, row), msg.GeoData.Longitude)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+7, row), msg.GeoData.Timezone)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+8, row), msg.GeoData.ISP)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+9, row), msg.GeoData.Organization)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+10, row), msg.GeoData.IPType)
				colOffset += 11
			}

			if data.ExportInfo.IncludeMeta && msg.MetaData != nil {
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset, row), msg.MetaData.CreatedAt.Format("2006-01-02 15:04:05"))
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+1, row), msg.MetaData.ClientID)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+2, row), msg.MetaData.FromUserID)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+3, row), msg.MetaData.UserName)
				f.SetCellValue(sheetName, fmt.Sprintf("%c%d", 'A'+colOffset+4, row), msg.MetaData.UserEmail)
			}
		}

		// Stream Excel file
		var buf bytes.Buffer
		if err := f.Write(&buf); err != nil {
			return fmt.Errorf("failed to write Excel file: %w", err)
		}

		ctx.Header("Content-Length", strconv.Itoa(buf.Len()))
		ctx.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())

	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	return nil
}
