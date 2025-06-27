// app/chat/page.js
"use client";

import { useEffect, useState, useRef } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { auth } from "../../lib/firebase"; // Assuming firebase.js is configured with env vars
import { onAuthStateChanged, signOut } from "firebase/auth";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";

// Use environment variable for the API base URL
const API_BASE_URL = process.env.NEXT_PUBLIC_API_BASE_URL;

export default function ChatPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [user, setUser] = useState(null);
  const [ws, setWs] = useState(null);
  const [isConnected, setIsConnected] = useState(false);
  const [messages, setMessages] = useState([]);
  const [inputMessage, setInputMessage] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [documentInfo, setDocumentInfo] = useState(null);
  const [chatHistory, setChatHistory] = useState([]);
  const messagesEndRef = useRef(null);
  const inputRef = useRef(null);

  // Get document ID from URL params
  const documentId = searchParams.get("documentId");

  // Redirect if not logged in
  useEffect(() => {
    const unsubscribe = onAuthStateChanged(auth, (currentUser) => {
      if (currentUser) {
        setUser(currentUser);
      } else {
        router.push("/");
      }
    });

    return () => unsubscribe();
  }, [router]);

  // Initialize WebSocket connection
  useEffect(() => {
    if (!user || !documentId || !API_BASE_URL) return; // Ensure API_BASE_URL is available

    // Construct WebSocket URL using API_BASE_URL
    const API_WS_URL = process.env.NEXT_PUBLIC_API_WS_URL;
    const wsUrl = API_WS_URL + `/ws?documentId=${documentId}&userId=u1`;
    const websocket = new WebSocket(wsUrl);

    websocket.onopen = () => {
      console.log("WebSocket connected");
      setIsConnected(true);
      setMessages((prev) => [
        ...prev,
        {
          type: "system",
          content:
            "Connected to document chat. You can now ask questions about your document.",
          timestamp: new Date().toISOString(),
        },
      ]);
    };

    websocket.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        console.log("Received message:", data);

        // Handle different message types
        if (data.type === "response") {
          setMessages((prev) => [
            ...prev,
            {
              type: "assistant",
              content: data.content,
              timestamp: data.timestamp || new Date().toISOString(),
            },
          ]);
          setIsLoading(false);
        } else if (data.type === "document_info") {
          setDocumentInfo(data.document);
        } else if (data.type === "error") {
          setMessages((prev) => [
            ...prev,
            {
              type: "error",
              content: data.content || "An error occurred",
              timestamp: new Date().toISOString(),
            },
          ]);
          setIsLoading(false);
        }
      } catch (error) {
        console.error("Error parsing WebSocket message:", error);
      }
    };

    websocket.onclose = () => {
      console.log("WebSocket disconnected");
      setIsConnected(false);
      setMessages((prev) => [
        ...prev,
        {
          type: "system",
          content: "Disconnected from chat. Please refresh to reconnect.",
          timestamp: new Date().toISOString(),
        },
      ]);
    };

    websocket.onerror = (error) => {
      console.error("WebSocket error:", error);
      setIsConnected(false);
    };

    setWs(websocket);

    return () => {
      websocket.close();
    };
  }, [user, documentId]);

  // Auto-scroll to bottom when new messages arrive
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  // Load document info and chat history on mount
  useEffect(() => {
    if (documentId && user && API_BASE_URL) {
      // Ensure API_BASE_URL is available
      loadDocumentInfo();
      loadChatHistory();
    }
  }, [documentId, user]);

  const loadDocumentInfo = async () => {
    try {
      const response = await fetch(`${API_BASE_URL}/documents/${documentId}`);
      if (response.ok) {
        const data = await response.json();
        console.log("Document Info:", data);
        setDocumentInfo(data.document);
      }
    } catch (error) {
      console.error("Error loading document info:", error);
    }
  };

  const loadChatHistory = async () => {
    if (!documentId || !user || !API_BASE_URL) return; // Ensure API_BASE_URL is available
    try {
      const response = await fetch(
        `${API_BASE_URL}/documents/${documentId}/chat?userId=u1`
      );
      if (response.ok) {
        const data = await response.json();
        console.log("Chat History:", data);
        const history = data.messages || [];
        setChatHistory(history);

        const formattedMessages = history.map((msg) => ({
          type: msg.message_type === "bot" ? "assistant" : msg.message_type,
          content: msg.message_content,
          timestamp: msg.timestamp,
        }));
        setMessages(formattedMessages);
      }
    } catch (error) {
      console.error("Error loading chat history:", error);
    }
  };

  const sendMessage = async () => {
    if (!inputMessage.trim() || !ws || !isConnected || !API_BASE_URL) return; // Ensure API_BASE_URL is available

    const userMessage = {
      type: "user",
      content: inputMessage.trim(),
      timestamp: new Date().toISOString(),
    };

    // Add user message to chat immediately
    setMessages((prev) => [...prev, userMessage]);

    // Clear input and set loading state
    setInputMessage("");
    setIsLoading(true);

    try {
      // Save the message to the backend
      await fetch(`${API_BASE_URL}/chat`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          document_id: documentId,
          user_id: "u1",
          message_type: "user",
          message_content: userMessage.content,
        }),
      });

      // Send the message via WebSocket to get a response
      ws.send(
        JSON.stringify({
          type: "query",
          content: userMessage.content,
          documentId: documentId,
          userId: "u1",
        })
      );
    } catch (error) {
      console.error("Failed to send message:", error);
      setMessages((prev) => [
        ...prev,
        {
          type: "error",
          content: "Failed to send message. Please try again.",
          timestamp: new Date().toISOString(),
        },
      ]);
      setIsLoading(false);
    }
  };

  const handleKeyPress = (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  };

  const formatTime = (timestamp) => {
    return new Date(timestamp).toLocaleTimeString("en-US", {
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  const formatFileSize = (bytes) => {
    if (!bytes) return "Unknown size";
    if (bytes === 0) return "0 Bytes";
    const k = 1024;
    const sizes = ["Bytes", "KB", "MB", "GB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
  };

  // Custom markdown components for styling
  const markdownComponents = {
    code: ({ node, inline, className, children, ...props }) => {
      const match = /language-(\w+)/.exec(className || "");
      return !inline && match ? (
        <SyntaxHighlighter
          style={oneDark}
          language={match[1]}
          PreTag="div"
          className="rounded-lg !bg-gray-800/50 !mt-2 !mb-2"
          {...props}
        >
          {String(children).replace(/\n$/, "")}
        </SyntaxHighlighter>
      ) : (
        <code
          className="bg-gray-700/50 px-1.5 py-0.5 rounded text-sm font-mono text-purple-300"
          {...props}
        >
          {children}
        </code>
      );
    },
    h1: ({ children }) => (
      <h1 className="text-2xl font-bold mb-4 text-white border-b border-gray-600 pb-2">
        {children}
      </h1>
    ),
    h2: ({ children }) => (
      <h2 className="text-xl font-semibold mb-3 text-white mt-6">{children}</h2>
    ),
    h3: ({ children }) => (
      <h3 className="text-lg font-semibold mb-2 text-white mt-4">{children}</h3>
    ),
    h4: ({ children }) => (
      <h4 className="text-base font-semibold mb-2 text-white mt-3">
        {children}
      </h4>
    ),
    p: ({ children }) => (
      <p className="mb-3 text-gray-100 leading-relaxed">{children}</p>
    ),
    ul: ({ children }) => (
      <ul className="list-disc list-inside mb-3 text-gray-100 space-y-1 ml-4">
        {children}
      </ul>
    ),
    ol: ({ children }) => (
      <ol className="list-decimal list-inside mb-3 text-gray-100 space-y-1 ml-4">
        {children}
      </ol>
    ),
    li: ({ children }) => (
      <li className="text-gray-100 leading-relaxed">{children}</li>
    ),
    blockquote: ({ children }) => (
      <blockquote className="border-l-4 border-purple-500 pl-4 py-2 mb-3 bg-gray-800/30 rounded-r-lg">
        <div className="text-gray-200 italic">{children}</div>
      </blockquote>
    ),
    table: ({ children }) => (
      <div className="overflow-x-auto mb-4">
        <table className="min-w-full border border-gray-600 rounded-lg">
          {children}
        </table>
      </div>
    ),
    thead: ({ children }) => (
      <thead className="bg-gray-700/50">{children}</thead>
    ),
    tbody: ({ children }) => (
      <tbody className="bg-gray-800/20">{children}</tbody>
    ),
    tr: ({ children }) => (
      <tr className="border-b border-gray-600">{children}</tr>
    ),
    th: ({ children }) => (
      <th className="px-4 py-2 text-left text-white font-semibold">
        {children}
      </th>
    ),
    td: ({ children }) => (
      <td className="px-4 py-2 text-gray-100">{children}</td>
    ),
    a: ({ children, href }) => (
      <a
        href={href}
        className="text-purple-400 hover:text-purple-300 underline transition-colors"
        target="_blank"
        rel="noopener noreferrer"
      >
        {children}
      </a>
    ),
    strong: ({ children }) => (
      <strong className="font-semibold text-white">{children}</strong>
    ),
    em: ({ children }) => <em className="italic text-gray-200">{children}</em>,
  };

  if (!documentId) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-gray-900 via-black to-purple-900 flex items-center justify-center">
        <div className="text-center text-white">
          <h2 className="text-2xl font-bold mb-4">No Document Selected</h2>
          <p className="text-gray-400 mb-6">
            Please upload a document first to start chatting.
          </p>
          <button
            onClick={() => router.push("/dashboard")}
            className="px-6 py-3 bg-gradient-to-r from-purple-600 to-blue-600 rounded-lg font-medium hover:from-purple-700 hover:to-blue-700 transition-all"
          >
            Go to Dashboard
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-gray-900 via-black to-purple-900 text-white">
      {/* Background Effects */}
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_top,_var(--tw-gradient-stops))] from-purple-900/20 via-transparent to-transparent"></div>
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_bottom_right,_var(--tw-gradient-stops))] from-blue-900/20 via-transparent to-transparent"></div>

      {/* Header */}
      <div className="relative z-10 flex justify-between items-center p-6 lg:p-8 border-b border-white/10">
        <div className="flex items-center space-x-4">
          <button
            onClick={() => router.push("/dashboard")}
            className="p-2 hover:bg-white/10 rounded-lg transition-colors"
          >
            <svg
              className="w-6 h-6"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M15 19l-7-7 7-7"
              />
            </svg>
          </button>

          <div className="flex items-center space-x-3">
            <div className="w-8 h-8 bg-gradient-to-r from-purple-500 to-blue-500 rounded-lg flex items-center justify-center">
              <svg
                className="w-5 h-5 text-white"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z"
                />
              </svg>
            </div>
            <div>
              <h1 className="text-xl font-bold">Document Chat</h1>
              <div className="flex items-center space-x-2">
                <div
                  className={`w-2 h-2 rounded-full ${
                    isConnected ? "bg-green-500" : "bg-red-500"
                  }`}
                ></div>
                <span className="text-sm text-gray-400">
                  {isConnected ? "Connected" : "Disconnected"}
                </span>
              </div>
            </div>
          </div>
        </div>

        <button
          onClick={() => signOut(auth)}
          className="flex items-center space-x-2 px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors duration-200 hover:bg-white/10 rounded-lg"
        >
          <svg
            className="w-4 h-4"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1"
            />
          </svg>
          <span>Sign Out</span>
        </button>
      </div>

      <div className="relative z-10 flex h-[calc(100vh-120px)]">
        {/* Left Sidebar */}
        <div className="w-80 border-r border-white/10 p-6 overflow-y-auto">
          {/* Document Info */}
          {documentInfo && (
            <div className="bg-white/5 backdrop-blur-xl border border-white/10 rounded-2xl p-6 mb-6">
              <h3 className="text-lg font-semibold mb-4 flex items-center space-x-2">
                <svg
                  className="w-5 h-5 text-purple-400"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
                  />
                </svg>
                <span>Document Details</span>
              </h3>
              <div className="space-y-2 text-sm">
                <p className="text-white font-medium truncate">
                  {documentInfo.file_name}
                </p>
                <p className="text-gray-400">
                  Size: {formatFileSize(documentInfo.size)}
                </p>
                <p className="text-gray-400">
                  Uploaded:{" "}
                  {new Date(documentInfo.uploaded_at).toLocaleDateString()}
                </p>
              </div>
            </div>
          )}

          {/* Chat History */}
          <div className="bg-white/5 backdrop-blur-xl border border-white/10 rounded-2xl p-6">
            <h3 className="text-lg font-semibold mb-4 flex items-center space-x-2">
              <svg
                className="w-5 h-5 text-blue-400"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
                />
              </svg>
              <span>Chat History</span>
            </h3>

            {chatHistory.length > 0 ? (
              <div className="space-y-3">
                {chatHistory.map((chat, index) => (
                  <div
                    key={chat.id}
                    className="p-3 bg-white/5 rounded-lg cursor-pointer hover:bg-white/10 transition-colors border border-white/5"
                  >
                    <p className="text-white text-sm font-medium line-clamp-2 mb-1">
                      {chat.message_content}
                    </p>
                    <span className="text-xs text-gray-400">
                      {new Date(chat.timestamp).toLocaleDateString("en-US", {
                        month: "short",
                        day: "numeric",
                      })}
                    </span>
                  </div>
                ))}
              </div>
            ) : (
              <div className="text-center py-8">
                <div className="w-12 h-12 bg-gradient-to-r from-purple-500/20 to-blue-500/20 rounded-full flex items-center justify-center mx-auto mb-3">
                  <svg
                    className="w-6 h-6 text-purple-400"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z"
                    />
                  </svg>
                </div>
                <p className="text-gray-400 text-sm">No chat history yet</p>
                <p className="text-gray-500 text-xs mt-1">
                  Your conversations will appear here
                </p>
              </div>
            )}
          </div>

          {/* Usage Tips */}
          <div className="mt-6 bg-white/5 backdrop-blur-xl border border-white/10 rounded-2xl p-6">
            <h4 className="text-sm font-semibold mb-3 text-purple-400">
              💡 Tips
            </h4>
            <ul className="text-sm text-gray-400 space-y-2">
              <li>• Ask specific questions about the document content</li>
              <li>• Request summaries of specific sections</li>
              <li>• Ask for explanations of complex concepts</li>
              <li>• Use Can you find... to search for information</li>
            </ul>
          </div>
        </div>

        {/* Chat Area */}
        <div className="flex-1 flex flex-col">
          {/* Messages */}
          <div className="flex-1 overflow-y-auto p-6 space-y-4">
            {messages.length === 0 && (
              <div className="text-center py-12">
                <div className="w-16 h-16 bg-gradient-to-r from-purple-500/20 to-blue-500/20 rounded-full flex items-center justify-center mx-auto mb-4">
                  <svg
                    className="w-8 h-8 text-purple-400"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z"
                    />
                  </svg>
                </div>
                <h3 className="text-xl font-semibold text-white mb-2">
                  Start a conversation
                </h3>
                <p className="text-gray-400">
                  Ask me anything about your document. I am here to help!
                </p>
              </div>
            )}

            {messages.map((message, index) => (
              <div
                key={index}
                className={`flex ${
                  message.type === "user" ? "justify-end" : "justify-start"
                }`}
              >
                <div
                  className={`max-w-4xl ${
                    message.type === "user" ? "ml-12" : "mr-12"
                  }`}
                >
                  <div
                    className={`rounded-2xl p-4 ${
                      message.type === "user"
                        ? "bg-gradient-to-r from-purple-600 to-blue-600 text-white"
                        : message.type === "assistant"
                        ? "bg-white/10 backdrop-blur-xl border border-white/10 text-white"
                        : message.type === "error"
                        ? "bg-red-500/10 border border-red-500/20 text-red-400"
                        : "bg-yellow-500/10 border border-yellow-500/20 text-yellow-400"
                    }`}
                  >
                    <div className="flex items-start space-x-3">
                      <div
                        className={`w-8 h-8 rounded-full flex items-center justify-center flex-shrink-0 ${
                          message.type === "user"
                            ? "bg-white/20"
                            : message.type === "assistant"
                            ? "bg-purple-500/20"
                            : "bg-gray-500/20"
                        }`}
                      >
                        {message.type === "user" ? (
                          <svg
                            className="w-4 h-4"
                            fill="none"
                            stroke="currentColor"
                            viewBox="0 0 24 24"
                          >
                            <path
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              strokeWidth={2}
                              d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z"
                            />
                          </svg>
                        ) : message.type === "assistant" ? (
                          <svg
                            className="w-4 h-4"
                            fill="none"
                            stroke="currentColor"
                            viewBox="0 0 24 24"
                          >
                            <path
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              strokeWidth={2}
                              d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z"
                            />
                          </svg>
                        ) : (
                          <svg
                            className="w-4 h-4"
                            fill="none"
                            stroke="currentColor"
                            viewBox="0 0 24 24"
                          >
                            <path
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              strokeWidth={2}
                              d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                            />
                          </svg>
                        )}
                      </div>
                      <div className="flex-1 min-w-0">
                        {message.type === "assistant" ? (
                          <div className="prose prose-invert max-w-none">
                            <ReactMarkdown
                              remarkPlugins={[remarkGfm]}
                              components={markdownComponents}
                            >
                              {message.content}
                            </ReactMarkdown>
                          </div>
                        ) : (
                          <p className="whitespace-pre-wrap">
                            {message.content}
                          </p>
                        )}
                        <span className="text-xs opacity-60 mt-2 block">
                          {formatTime(message.timestamp)}
                        </span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            ))}

            {isLoading && (
              <div className="flex justify-start">
                <div className="max-w-2xl mr-12">
                  <div className="bg-white/10 backdrop-blur-xl border border-white/10 rounded-2xl p-4">
                    <div className="flex items-center space-x-3">
                      <div className="w-8 h-8 bg-purple-500/20 rounded-full flex items-center justify-center">
                        <div className="w-4 h-4 border-2 border-purple-400/30 border-t-purple-400 rounded-full animate-spin"></div>
                      </div>
                      <span className="text-gray-400">AI is thinking...</span>
                    </div>
                  </div>
                </div>
              </div>
            )}

            <div ref={messagesEndRef} />
          </div>

          {/* Input Area */}
          <div className="border-t border-white/10 p-6">
            <div className="flex space-x-4">
              <div className="flex-1 relative">
                <textarea
                  ref={inputRef}
                  value={inputMessage}
                  onChange={(e) => setInputMessage(e.target.value)}
                  onKeyPress={handleKeyPress}
                  placeholder={
                    isConnected
                      ? "Ask a question about your document..."
                      : "Connecting..."
                  }
                  disabled={!isConnected || isLoading}
                  className="w-full bg-white/10 backdrop-blur-xl border border-white/10 rounded-xl px-4 py-3 pr-12 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent resize-none min-h-[50px] max-h-32 disabled:opacity-50"
                  rows="1"
                />
                <div className="absolute right-3 bottom-3 text-xs text-gray-500">
                  Press Enter to send
                </div>
              </div>
              <button
                onClick={sendMessage}
                disabled={!inputMessage.trim() || !isConnected || isLoading}
                className="px-6 py-3 bg-gradient-to-r from-purple-600 to-blue-600 hover:from-purple-700 hover:to-blue-700 disabled:from-gray-600 disabled:to-gray-600 disabled:cursor-not-allowed text-white rounded-xl font-medium transition-all duration-200 flex items-center space-x-2"
              >
                {isLoading ? (
                  <div className="w-5 h-5 border-2 border-white/30 border-t-white rounded-full animate-spin"></div>
                ) : (
                  <svg
                    className="w-5 h-5"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8"
                    />
                  </svg>
                )}
                <span>Send</span>
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
