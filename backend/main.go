package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/gorilla/websocket" // New import for WebSockets
	"github.com/joho/godotenv"
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
	Size        int64     `json:"size" db:"size"`
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
	Message    string  ` json:"message"`
	DocumentID string   `json:"document_id,omitempty"`
	Document   Document `json:"document,omitempty"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

type LLMRequest struct {
	DocumentID string `json:"document_id" binding:"required"`
	Query      string `json:"query" binding:"required"`
	UserID     string `json:"user_id,omitempty"`
}

type LLMResponse struct {
	Success bool   `json:"success"`
	Answer  string `json:"answer,omitempty"`
	Error   string `json:"error,omitempty"`
}

// WebSocket message types
type WSMessage struct {
	Type       string `json:"type"`
	Content    string `json:"content"`
	DocumentID string `json:"documentId"`
	UserID     string `json:"userId"`
	Timestamp  string `json:"timestamp"`
	ID         string `json:"id,omitempty"`
}

type WSResponse struct {
	Type      string `json:"type"`
	Content   string `json:"content"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
}

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow connections from any origin (configure appropriately for production)
		return true
	},
}

// WebSocket client structure
type Client struct {
	conn       *websocket.Conn
	send       chan WSResponse
	documentID string
	userID     string
}

// Hub maintains the set of active clients
type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
}

// Global hub instance
var hub = &Hub{
	clients:    make(map[*Client]bool),
	register:   make(chan *Client),
	unregister: make(chan *Client),
}

// Database connection
var db *sql.DB

// Initialize database connection
func initDB() error {
	var err error
	// Update with your database connection string
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("DB_URL")
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
	err := db.QueryRowContext(ctx, "SELECT id, email, created_at FROM users WHERE id = ?", userID).
		Scan(&user.ID, &user.Email, &user.CreatedAt)

	if err == sql.ErrNoRows {
		// User doesn't exist, create new one
		now := time.Now()
		_, err = db.ExecContext(ctx,
			"INSERT INTO users (id, email, created_at) VALUES (?, ?, ?)",
			userID, email, now)
		if err != nil {
			return nil, fmt.Errorf("failed to create user: %v", err)
		}

		user.ID = "u1"
		user.Email = "himanshu.khojpur@gmail.com"
		user.CreatedAt = now
		return user, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to query user: %v", err)
	}

	return user, nil
}

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

		// Create an empty font map
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
func saveDocument(ctx context.Context, userID, fileName, storagePath string, size int64) (*Document, error) {
	documentID := uuid.New().String()
	now := time.Now()

	_, err := db.ExecContext(ctx,
		"INSERT INTO documents (id, user_id, file_name, storage_path, uploaded_at, size) VALUES (?, ?, ?, ?, ?, ?)",
		documentID, userID, fileName, storagePath, now, size)

	if err != nil {
		return nil, fmt.Errorf("failed to save document: %v", err)
	}

	return &Document{
		ID:          documentID,
		UserID:      userID,
		FileName:    fileName,
		StoragePath: storagePath,
		UploadedAt:  now,
		Size:        size,
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
			"INSERT INTO document_chunks (id, document_id, chunk_index, content, created_at) VALUES (?, ?, ?, ?, ?)",
			chunkID, documentID, i, chunk, time.Now())

		if err != nil {
			return fmt.Errorf("failed to save chunk %d: %v", i, err)
		}
	}

	return tx.Commit()
}

// Run the hub
func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("Client registered. Total clients: %d", len(h.clients))

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				log.Printf("Client unregistered. Total clients: %d", len(h.clients))
			}
		}
	}
}

// Handle WebSocket connections
func handleWebSocket(c *gin.Context) {
	// Get query parameters
	documentID := c.Query("documentId")
	userID := c.Query("userId")

	if documentID == "" || userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "documentId and userId query parameters are required",
		})
		return
	}

	// Verify document exists and user has access
	var docExists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM documents WHERE id = ? AND user_id = ?)",
		documentID, userID).Scan(&docExists)
	if err != nil {
		log.Printf("Error verifying document access: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to verify document access",
		})
		return
	}

	if !docExists {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Document not found or access denied",
		})
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	// Create client
	client := &Client{
		conn:       conn,
		send:       make(chan WSResponse, 256),
		documentID: documentID,
		userID:     userID,
	}

	// Register client
	hub.register <- client

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()
}

// Read messages from WebSocket
func (c *Client) readPump() {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()

	// Set read deadline and pong handler
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		var msg WSMessage
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		log.Printf("Received WebSocket message: %+v", msg)

		// Handle different message types
		switch msg.Type {
		case "query":
			go c.handleQuery(msg)
		default:
			log.Printf("Unknown message type: %s", msg.Type)
		}
	}
}

// Write messages to WebSocket
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteJSON(message); err != nil {
				log.Printf("Error writing message: %v", err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Handle query messages
func (c *Client) handleQuery(msg WSMessage) {
	// Fetch document content
	rows, err := db.Query("SELECT content FROM document_chunks WHERE document_id = ? ORDER BY chunk_index", c.documentID)
	if err != nil {
		c.sendError("Failed to fetch document content")
		return
	}
	defer rows.Close()

	var contentBuilder strings.Builder
	for rows.Next() {
		var chunk string
		if err := rows.Scan(&chunk); err != nil {
			continue
		}
		contentBuilder.WriteString(chunk + "\n\n")
	}

	content := contentBuilder.String()
	if content == "" {
		c.sendError("No content found for this document")
		return
	}

	// Limit content size
	if len(content) > 24000 {
		content = content[:24000]
	}

	// Create prompt for AI
	prompt := fmt.Sprintf(`Based on the following document content, please answer the user's question accurately and concisely.

Document Content:
%s

User Question: %s

Please provide a helpful and accurate answer based on the document content above.`, content, msg.Content)

	// Call Gemini API
	answer, err := callGeminiAPI(prompt)
	if err != nil {
		log.Printf("Error calling Gemini API: %v", err)
		c.sendError("Failed to get response from AI: " + err.Error())
		return
	}

	// Save bot response to database
	responseID := uuid.New().String()
	_, err = db.Exec(`
		INSERT INTO chat_messages (id, document_id, user_id, message_type, message_content, timestamp)
		VALUES (?, ?, ?, ?, ?, ?)`,
		responseID, c.documentID, c.userID, "bot", answer, time.Now())
	if err != nil {
		log.Printf("Error saving bot message: %v", err)
	}

	// Send response
	response := WSResponse{
		Type:      "response",
		Content:   answer,
		ID:        responseID,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	select {
	case c.send <- response:
	default:
		close(c.send)
		delete(hub.clients, c)
	}
}

// Send error message to client
func (c *Client) sendError(errorMsg string) {
	response := WSResponse{
		Type:      "error",
		Content:   errorMsg,
		ID:        uuid.New().String(),
		Timestamp: time.Now().Format(time.RFC3339),
	}

	select {
	case c.send <- response:
	default:
		close(c.send)
		delete(hub.clients, c)
	}
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
	document, err := saveDocument(ctx, req.UserID, fileName, filePath, header.Size)
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

	rows, err := db.Query("SELECT id, user_id, file_name, storage_path, uploaded_at, size FROM documents WHERE user_id = ? ORDER BY uploaded_at DESC", userID)
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
		err := rows.Scan(&doc.ID, &doc.UserID, &doc.FileName, &doc.StoragePath, &doc.UploadedAt, &doc.Size)
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

	rows, err := db.Query("SELECT id, document_id, chunk_index, content, created_at FROM document_chunks WHERE document_id = ? ORDER BY chunk_index", documentID)
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

// Get document info endpoint
func getDocumentInfo(c *gin.Context) {
	documentID := c.Param("documentId")
	if documentID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "Document ID is required",
		})
		return
	}

	var doc Document
	err := db.QueryRow(`
		SELECT id, user_id, file_name, storage_path, uploaded_at, size
		FROM documents
		WHERE id = ?`, documentID).
		Scan(&doc.ID, &doc.UserID, &doc.FileName, &doc.StoragePath, &doc.UploadedAt, &doc.Size)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Success: false,
			Error:   "Document not found",
		})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Error:   "Failed to fetch document: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"document": doc,
	})
}

// Get chat history endpoint
func getChatHistory(c *gin.Context) {
	documentID := c.Param("documentId")
	userID := c.Query("userId")

	if documentID == "" || userID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "Document ID and User ID are required",
		})
		return
	}

	rows, err := db.Query(`
		SELECT id, message_type, message_content, timestamp
		FROM chat_messages
		WHERE document_id = ? AND user_id = ?
		ORDER BY timestamp ASC`, documentID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Error:   "Failed to fetch chat history: " + err.Error(),
		})
		return
	}
	defer rows.Close()

	var messages []ChatMessage
	for rows.Next() {
		var msg ChatMessage
		err := rows.Scan(&msg.ID, &msg.MessageType, &msg.MessageContent, &msg.Timestamp)
		if err != nil {
			log.Printf("Error scanning message: %v", err)
			continue
		}
		msg.DocumentID = documentID
		msg.UserID = userID
		messages = append(messages, msg)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"messages": messages,
	})
}

// Query LLM handler - Fixed version
func queryLLMHandler(c *gin.Context) {
	log.Printf("Received request to /ask endpoint")

	var req LLMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Error binding JSON: %v", err)
		c.JSON(http.StatusBadRequest, LLMResponse{
			Success: false,
			Error:   "Invalid request body: " + err.Error(),
		})
		return
	}

	log.Printf("Request parsed: DocumentID=%s, Query=%s", req.DocumentID, req.Query)

	if req.DocumentID == "" || req.Query == "" {
		c.JSON(http.StatusBadRequest, LLMResponse{
			Success: false,
			Error:   "Document ID and query are required",
		})
		return
	}

	// Verify document exists
	var documentExists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM documents WHERE id = ?)", req.DocumentID).Scan(&documentExists)
	if err != nil {
		log.Printf("Error checking document existence: %v", err)
		c.JSON(http.StatusInternalServerError, LLMResponse{
			Success: false,
			Error:   "Failed to verify document: " + err.Error(),
		})
		return
	}

	if !documentExists {
		c.JSON(http.StatusNotFound, LLMResponse{
			Success: false,
			Error:   "Document not found",
		})
		return
	}

	// Fetch content chunks from DB
	rows, err := db.Query("SELECT content FROM document_chunks WHERE document_id = ? ORDER BY chunk_index", req.DocumentID)
	if err != nil {
		log.Printf("Error fetching chunks: %v", err)
		c.JSON(http.StatusInternalServerError, LLMResponse{
			Success: false,
			Error:   "Failed to fetch chunks: " + err.Error(),
		})
		return
	}
	defer rows.Close()

	var contentBuilder strings.Builder
	chunkCount := 0
	for rows.Next() {
		var chunk string
		if err := rows.Scan(&chunk); err != nil {
			log.Printf("Error scanning chunk: %v", err)
			continue
		}
		contentBuilder.WriteString(chunk + "\n\n")
		chunkCount++
	}

	log.Printf("Found %d chunks for document %s", chunkCount, req.DocumentID)

	if chunkCount == 0 {
		c.JSON(http.StatusNotFound, LLMResponse{
			Success: false,
			Error:   "No content found for this document",
		})
		return
	}

	// Limit content size to avoid API limits
	content := contentBuilder.String()
	if len(content) > 24000 {
		content = content[:24000]
		log.Printf("Content truncated to 24000 characters")
	}

	prompt := fmt.Sprintf(`Based on the following document content, please answer the user's question accurately and concisely.

Document Content:
%s

User Question: %s

Please provide a helpful and accurate answer based on the document content above.`, content, req.Query)

	// Call Gemini API
	answer, err := callGeminiAPI(prompt)
	if err != nil {
		log.Printf("Error calling Gemini API: %v", err)
		c.JSON(http.StatusInternalServerError, LLMResponse{
			Success: false,
			Error:   "Failed to get response from AI: " + err.Error(),
		})
		return
	}

	log.Printf("Successfully got response from Gemini API")
	c.JSON(http.StatusOK, LLMResponse{
		Success: true,
		Answer:  answer,
	})
}

// Separate function to call Gemini API
func callGeminiAPI(prompt string) (string, error) {
	// Prepare request body
	requestBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": prompt},
				},
				"role": "user",
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":     0.7,
			"maxOutputTokens": 2048,
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %v", err)
	}

	// Make API call
	apiKey := os.Getenv("API_KEY")
	url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=" + apiKey

	resp, err := http.Post(url, "application/json", strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", fmt.Errorf("failed to call Gemini API: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Gemini API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	type GeminiResponse struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %v", err)
	}

	if geminiResp.Error.Code != 0 {
		return "", fmt.Errorf("Gemini API error: %s", geminiResp.Error.Message)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no response generated")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

// Save chat message handler
func saveChatHandler(c *gin.Context) {
	var msg ChatMessage
	if err := c.ShouldBindJSON(&msg); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "Invalid request body: " + err.Error(),
		})
		return
	}

	// Save user message to database
	_, err := db.Exec(`
		INSERT INTO chat_messages (id, document_id, user_id, message_type, message_content, timestamp)
		VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.New().String(), msg.DocumentID, msg.UserID, msg.MessageType, msg.MessageContent, time.Now())
	if err != nil {
		log.Printf("Error saving user message: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Success: false,
			Error:   "Failed to save chat message",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Health check
func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"message": "Document processing API is running",
		"routes": []string{
			"GET /health",
			"POST /upload",
			"GET /users/:userId/documents",
			"GET /documents/:documentId/chunks",
			"POST /ask",
			"GET /documents/:documentId/info",
			"GET /documents/:documentId/chat",
			"GET /ws",
		},
	})
}

func main() {
	// Initialize database
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, will use environment variables from the system")
	}
	if err := initDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	// Start the hub
	go hub.run()

	// Initialize Gin router
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Add logging middleware
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// Configure CORS
	config := cors.DefaultConfig()
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000" // Default for local dev
	}
	config.AllowOrigins = []string{frontendURL, "http://localhost:3001", "http://localhost:8080"}
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With"}
	config.AllowCredentials = true
	r.Use(cors.New(config))

	// Routes
	r.GET("/health", healthCheck)
	r.POST("/upload", uploadHandler)
	r.GET("/users/:userId/documents", getUserDocuments)
	r.GET("/documents/:documentId/chunks", getDocumentChunks)
	r.GET("/documents/:documentId", getDocumentInfo)
	r.GET("/documents/:documentId/chat", getChatHistory)
	r.POST("/ask", queryLLMHandler)
	r.POST("/chat", saveChatHandler)
	r.GET("/ws", handleWebSocket) // NEW WEBSOCKET ROUTE

	// Add a catch-all route for debugging
	r.NoRoute(func(c *gin.Context) {
		log.Printf("Route not found: %s %s", c.Request.Method, c.Request.URL.Path)
		c.JSON(http.StatusNotFound, gin.H{
			"error":  "Route not found",
			"path":   c.Request.URL.Path,
			"method": c.Request.Method,
		})
	})

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	log.Printf("Available routes:")
	log.Printf("  GET  /health")
	log.Printf("  POST /upload")
	log.Printf("  GET  /users/:userId/documents")
	log.Printf("  GET  /documents/:documentId/chunks")
	log.Printf("  GET  /documents/:documentId/info")
	log.Printf("  GET  /documents/:documentId/chat")
	log.Printf("  POST /ask")
	log.Printf("  POST /chat")
	log.Printf("  GET  /ws (WebSocket)")

	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
