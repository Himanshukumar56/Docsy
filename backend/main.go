package main

import (
	"context"
	"database/sql"

	// "encoding/json"
	"fmt"
	"io"
	"log"

	// "mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	// "strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/ledongthuc/pdf"
)

// Database models
type User struct {
	ID        string    `json:"id" db:"id"`
	Email     string    `json:"email" db:"email"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type Document struct {
	ID          string    `json:"id" db:"id"`
	UserID      string    `json:"user_id" db:"user_id"`
	FileName    string    `json:"file_name" db:"file_name"`
	StoragePath string    `json:"storage_path" db:"storage_path"`
	UploadedAt  time.Time `json:"uploaded_at" db:"uploaded_at"`
}

type DocumentChunk struct {
	ID         string    `json:"id" db:"id"`
	DocumentID string    `json:"document_id" db:"document_id"`
	ChunkIndex int       `json:"chunk_index" db:"chunk_index"`
	Content    string    `json:"content" db:"content"`
	Embedding  *string   `json:"embedding,omitempty" db:"embedding"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

type ChatMessage struct {
	ID             string    `json:"id" db:"id"`
	DocumentID     string    `json:"document_id" db:"document_id"`
	UserID         string    `json:"user_id" db:"user_id"`
	MessageType    string    `json:"message_type" db:"message_type"`
	MessageContent string    `json:"message_content" db:"message_content"`
	Timestamp      time.Time `json:"timestamp" db:"timestamp"`
}

// Request/Response structures
type UploadRequest struct {
	UserID string `form:"user_id" binding:"required"`
	Email  string `form:"email" binding:"required"`
}

type UploadResponse struct {
	Success    bool     `json:"success"`
	Message    string   `json:"message"`
	DocumentID string   `json:"document_id,omitempty"`
	Document   Document `json:"document,omitempty"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

// Database connection
var db *sql.DB

// Initialize database connection
func initDB() error {
	var err error
	// Update with your database connection string
	dbURL := "root:root@tcp(localhost:3306)/document_processor?parseTime=true"
	if dbURL == "" {
		dbURL = "username:password@tcp(localhost:3306)/dbname?parseTime=true"
	}
	
	db, err = sql.Open("mysql", dbURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %v", err)
	}
	
	// Test connection
	if err = db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %v", err)
	}
	
	log.Println("Database connected successfully")
	return nil
}

// Create or get user
func createOrGetUser(ctx context.Context, userID, email string) (*User, error) {
	user := &User{}
	
	// First try to get existing user
	err := db.QueryRowContext(ctx, "SELECT id, email, created_at FROM users WHERE id = $1", userID).
		Scan(&user.ID, &user.Email, &user.CreatedAt)
	
	if err == sql.ErrNoRows {
		// User doesn't exist, create new one
		now := time.Now()
		_, err = db.ExecContext(ctx, 
			"INSERT INTO users (id, email, created_at) VALUES ($1, $2, $3)",
			userID, email, now)
		if err != nil {
			return nil, fmt.Errorf("failed to create user: %v", err)
		}
		
		user.ID = userID
		user.Email = email
		user.CreatedAt = now
		return user, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to query user: %v", err)
	}
	
	return user, nil
}

// Extract text from PDF
// Extract text from PDF
func extractTextFromPDF(filePath string) (string, error) {
	file, reader, err := pdf.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %v", err)
	}
	defer file.Close()
	
	var text strings.Builder
	totalPages := reader.NumPage()
	
	for pageIndex := 1; pageIndex <= totalPages; pageIndex++ {
		page := reader.Page(pageIndex)
		if page.V.IsNull() {
			continue
		}
		
		// Create an empty font map - this is what was missing
		fontMap := make(map[string]*pdf.Font)
		
		pageText, err := page.GetPlainText(fontMap)
		if err != nil {
			log.Printf("Warning: failed to extract text from page %d: %v", pageIndex, err)
			continue
		}
		
		text.WriteString(pageText)
		text.WriteString("\n\n")
	}
	
	return text.String(), nil
}

// Extract text from text file
func extractTextFromFile(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %v", err)
	}
	return string(content), nil
}

// Split text into chunks
func splitTextIntoChunks(text string, maxChunkSize int) []string {
	if maxChunkSize <= 0 {
		maxChunkSize = 1000 // Default chunk size
	}
	
	var chunks []string
	words := strings.Fields(text)
	
	if len(words) == 0 {
		return chunks
	}
	
	var currentChunk strings.Builder
	currentSize := 0
	
	for _, word := range words {
		wordSize := len(word) + 1 // +1 for space
		
		if currentSize+wordSize > maxChunkSize && currentChunk.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(currentChunk.String()))
			currentChunk.Reset()
			currentSize = 0
		}
		
		if currentChunk.Len() > 0 {
			currentChunk.WriteString(" ")
		}
		currentChunk.WriteString(word)
		currentSize += wordSize
	}
	
	if currentChunk.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(currentChunk.String()))
	}
	
	return chunks
}

// Save document to database
func saveDocument(ctx context.Context, userID, fileName, storagePath string) (*Document, error) {
	documentID := uuid.New().String()
	now := time.Now()
	
	_, err := db.ExecContext(ctx,
		"INSERT INTO documents (id, user_id, file_name, storage_path, uploaded_at) VALUES ($1, $2, $3, $4, $5)",
		documentID, userID, fileName, storagePath, now)
	
	if err != nil {
		return nil, fmt.Errorf("failed to save document: %v", err)
	}
	
	return &Document{
		ID:          documentID,
		UserID:      userID,
		FileName:    fileName,
		StoragePath: storagePath,
		UploadedAt:  now,
	}, nil
}

// Save document chunks to database
func saveDocumentChunks(ctx context.Context, documentID string, chunks []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback()
	
	for i, chunk := range chunks {
		chunkID := uuid.New().String()
		_, err := tx.ExecContext(ctx,
			"INSERT INTO document_chunks (id, document_id, chunk_index, content, created_at) VALUES ($1, $2, $3, $4, $5)",
			chunkID, documentID, i, chunk, time.Now())
		
		if err != nil {
			return fmt.Errorf("failed to save chunk %d: %v", i, err)
		}
	}
	
	return tx.Commit()
}

// Upload handler
func uploadHandler(c *gin.Context) {
	var req UploadRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "Invalid request parameters: " + err.Error(),
		})
		return
	}
	
	// Get uploaded file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "No file uploaded: " + err.Error(),
		})
		return
	}
	defer file.Close()
	
	// Validate file type
	fileName := header.Filename
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext != ".pdf" && ext != ".txt" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "Only PDF and TXT files are supported",
		})
		return
	}
	
	// Create uploads directory if it doesn't exist
	uploadsDir := "uploads"
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Error:   "Failed to create uploads directory: " + err.Error(),
		})
		return
	}
	
	// Save file to disk
	filePath := filepath.Join(uploadsDir, uuid.New().String()+ext)
	out, err := os.Create(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Error:   "Failed to create file: " + err.Error(),
		})
		return
	}
	defer out.Close()
	
	_, err = io.Copy(out, file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Error:   "Failed to save file: " + err.Error(),
		})
		return
	}
	
	ctx := context.Background()
	
	// Create or get user
	_, err = createOrGetUser(ctx, req.UserID, req.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Error:   "Failed to create/get user: " + err.Error(),
		})
		return
	}
	
	// Extract text from file
	var extractedText string
	if ext == ".pdf" {
		extractedText, err = extractTextFromPDF(filePath)
	} else {
		extractedText, err = extractTextFromFile(filePath)
	}
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Error:   "Failed to extract text: " + err.Error(),
		})
		return
	}
	
	if strings.TrimSpace(extractedText) == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "No text content found in the file",
		})
		return
	}
	
	// Save document to database
	document, err := saveDocument(ctx, req.UserID, fileName, filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Error:   "Failed to save document: " + err.Error(),
		})
		return
	}
	
	// Split text into chunks
	chunks := splitTextIntoChunks(extractedText, 1000)
	
	// Save chunks to database
	err = saveDocumentChunks(ctx, document.ID, chunks)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Error:   "Failed to save document chunks: " + err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, UploadResponse{
		Success:    true,
		Message:    fmt.Sprintf("Document uploaded successfully. Extracted %d chunks of text.", len(chunks)),
		DocumentID: document.ID,
		Document:   *document,
	})
}

// Get documents for a user
func getUserDocuments(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "User ID is required",
		})
		return
	}
	
	rows, err := db.Query("SELECT id, user_id, file_name, storage_path, uploaded_at FROM documents WHERE user_id = $1 ORDER BY uploaded_at DESC", userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Error:   "Failed to fetch documents: " + err.Error(),
		})
		return
	}
	defer rows.Close()
	
	var documents []Document
	for rows.Next() {
		var doc Document
		err := rows.Scan(&doc.ID, &doc.UserID, &doc.FileName, &doc.StoragePath, &doc.UploadedAt)
		if err != nil {
			log.Printf("Error scanning document: %v", err)
			continue
		}
		documents = append(documents, doc)
	}
	
	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"documents": documents,
	})
}

// Get document chunks
func getDocumentChunks(c *gin.Context) {
	documentID := c.Param("documentId")
	if documentID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "Document ID is required",
		})
		return
	}
	
	rows, err := db.Query("SELECT id, document_id, chunk_index, content, created_at FROM document_chunks WHERE document_id = $1 ORDER BY chunk_index", documentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Error:   "Failed to fetch chunks: " + err.Error(),
		})
		return
	}
	defer rows.Close()
	
	var chunks []DocumentChunk
	for rows.Next() {
		var chunk DocumentChunk
		err := rows.Scan(&chunk.ID, &chunk.DocumentID, &chunk.ChunkIndex, &chunk.Content, &chunk.CreatedAt)
		if err != nil {
			log.Printf("Error scanning chunk: %v", err)
			continue
		}
		chunks = append(chunks, chunk)
	}
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"chunks":  chunks,
	})
}

// Health check
func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"message": "Document processing API is running",
	})
}

func main() {
	// Initialize database
	if err := initDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()
	
	// Initialize Gin router
	r := gin.Default()
	
	// Configure CORS
	config := cors.DefaultConfig()
	config.AllowOrigins = []string{"http://localhost:3000", "http://localhost:3001"} // Add your frontend URLs
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	r.Use(cors.New(config))
	
	// Routes
	r.GET("/health", healthCheck)
	r.POST("/upload", uploadHandler)
	r.GET("/users/:userId/documents", getUserDocuments)
	r.GET("/documents/:documentId/chunks", getDocumentChunks)
	
	// Start server
	port := "8080"
	if port == "" {
		port = "8080"
	}
	
	log.Printf("Server starting on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}