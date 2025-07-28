# Docsy

Docsy is a full-stack application that allows users to upload documents (PDF and TXT) and interact with them through a chat interface. The application extracts text from the documents, chunks it, and uses a Large Language Model (LLM) to answer user queries based on the document's content.

## Tech Stack

### Frontend

- **Framework:** [Next.js](https://nextjs.org/)
- **Language:** JavaScript
- **Styling:** [Tailwind CSS](https://tailwindcss.com/)
- **UI Components:** [Shadcn UI](https://ui.shadcn.com/)
- **Authentication:** [Firebase Authentication](https://firebase.google.com/docs/auth)
- **Real-time Communication:** WebSockets

### Backend

- **Language:** [Go](https://golang.org/)
- **Framework:** [Gin](https://gin-gonic.com/)
- **Database:** [PostgreSQL](https://www.postgresql.org/)
- **Real-time Communication:** [Gorilla WebSocket](https://github.com/gorilla/websocket)
- **LLM:** [Google Gemini API](https://ai.google.dev/)

### Deployment

- **Platform:** [Render](https://render.com/)
- **Database:** Render PostgreSQL

## Features

- **User Authentication:** Secure user sign-up and sign-in using Firebase.
- **Document Upload:** Supports PDF and TXT file uploads.
- **Text Extraction:** Extracts text content from uploaded documents.
- **Document Chat:** Real-time chat interface to ask questions about the document's content.
- **LLM Integration:** Uses the Gemini API to generate answers based on the document.
- **Chat History:** Stores and displays the chat history for each document.

## Project Structure

```
.
├── backend/
│   ├── main.go         # Main application logic, API endpoints
│   ├── schema.go       # Database schema definition
│   ├── go.mod          # Go module dependencies
│   └── ...
├── frontend/
│   ├── src/
│   │   ├── app/        # Next.js app router
│   │   └── ...
│   ├── public/         # Static assets
│   ├── package.json    # Frontend dependencies
│   └── ...
└── render.yaml         # Deployment configuration for Render
```

## How It Works

1.  **User Authentication:** The frontend uses Firebase to authenticate users.
2.  **Document Upload:** Authenticated users can upload PDF or TXT files.
3.  **Backend Processing:**
    - The Go backend receives the uploaded file.
    - It extracts the text from the document.
    - The extracted text is split into smaller chunks.
    - The document metadata and chunks are stored in the PostgreSQL database.
4.  **Chat Interface:**
    - The frontend establishes a WebSocket connection with the backend for real-time communication.
    - When a user sends a message, it is sent to the backend via the WebSocket.
5.  **LLM-Powered Responses:**
    - The backend retrieves the relevant document chunks from the database.
    - It constructs a prompt with the user's query and the document content.
    - The prompt is sent to the Google Gemini API.
    - The LLM's response is sent back to the frontend and displayed in the chat.

## Usability and Scalability

-   **Usability:** The application provides a simple and intuitive interface for users to upload documents and chat with them. The real-time chat functionality enhances the user experience.
-   **Scalability:** The backend is built with Go, which is known for its performance and concurrency, making it suitable for handling multiple concurrent users. The use of a PostgreSQL database allows for efficient data storage and retrieval. The application is deployed on Render, which provides a scalable infrastructure that can be easily upgraded to handle increased traffic.

## Local Development

### Prerequisites

-   [Node.js](https://nodejs.org/) (v18 or later)
-   [Go](https://golang.org/doc/install) (v1.18 or later)
-   [PostgreSQL](https://www.postgresql.org/download/)
-   [Firebase Project](https://console.firebase.google.com/)
-   [Google Gemini API Key](https://ai.google.dev/)

### Backend Setup

1.  **Clone the repository:**
    ```bash
    git clone https://github.com/Himanshukumar56/Docsy.git
    cd Docsy/backend
    ```

2.  **Create a `.env` file:**
    ```bash
    cp .env.example .env
    ```

3.  **Update the `.env` file with your credentials:**
    ```
    DATABASE_URL="your_postgresql_connection_string"
    API_KEY="your_gemini_api_key"
    FRONTEND_URL="http://localhost:3000"
    ```

4.  **Install dependencies and run the server:**
    ```bash
    go mod tidy
    go run main.go
    ```

### Frontend Setup

1.  **Navigate to the frontend directory:**
    ```bash
    cd ../frontend
    ```

2.  **Create a `.env.local` file:**
    ```bash
    cp .env.local.example .env.local
    ```

3.  **Update the `.env.local` file with your Firebase configuration:**
    ```
    NEXT_PUBLIC_FIREBASE_API_KEY="your_firebase_api_key"
    NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN="your_firebase_auth_domain"
    NEXT_PUBLIC_FIREBASE_PROJECT_ID="your_firebase_project_id"
    NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET="your_firebase_storage_bucket"
    NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID="your_firebase_messaging_sender_id"
    NEXT_PUBLIC_FIREBASE_APP_ID="your_firebase_app_id"
    ```

4.  **Install dependencies and run the development server:**
    ```bash
    npm install
    npm run dev
    ```

The application will be available at `http://localhost:3000`.
