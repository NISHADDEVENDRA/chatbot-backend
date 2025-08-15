# Multi-Tenant SaaS Chatbot Platform Backend

A comprehensive, production-ready backend system for a multi-tenant SaaS chatbot platform built with Go, MongoDB, and Gemini AI integration.

## 🏗️ Architecture Overview

This platform provides a complete backend solution for managing multiple client tenants, each with their own chatbot instances, PDF document processing, and AI-powered conversations.

### Key Features

- **Multi-tenant Architecture**: Isolated data and resources per client
- **JWT Authentication**: Secure role-based access control (admin/client/visitor)
- **PDF Processing**: Real-time text extraction and intelligent chunking
- **AI Integration**: Gemini Pro API with context-aware responses
- **Token Management**: Usage tracking and atomic limit enforcement
- **Embeddable Widgets**: Cross-origin chat widgets for client websites
- **Real-time Analytics**: Comprehensive usage statistics and monitoring

## 🚀 Quick Start

### Prerequisites

- Go 1.21 or higher
- MongoDB 4.4 or higher
- Gemini API key from Google AI Studio

### Installation

1. **Clone the repository**
```bash
git clone <your-repo-url>
cd saas-chatbot-platform
```

2. **Install dependencies**
```bash
go mod download
```

3. **Configure environment**
```bash
cp .env.example .env
# Edit .env with your configuration
```

4. **Required environment variables**
```env
MONGO_URI=mongodb://localhost:27017/saas_chatbot
JWT_SECRET=your-super-secure-jwt-secret-256-bits-minimum
GEMINI_API_KEY=your-gemini-api-key-here
```

5. **Run the application**
```bash
go run cmd/main.go
```

The server will start on `http://localhost:8080`

## 📚 API Documentation

### Authentication Endpoints

#### Register User
```http
POST /auth/register
Content-Type: application/json

{
  "username": "johndoe",
  "name": "John Doe",
  "password": "securepassword123",
  "role": "client",
  "client_id": "optional-client-id"
}
```

#### Login
```http
POST /auth/login
Content-Type: application/json

{
  "username": "johndoe",
  "password": "securepassword123"
}
```

### Admin Endpoints

#### Create Client Tenant
```http
POST /admin/client
Authorization: Bearer <admin-token>
Content-Type: application/json

{
  "name": "Acme Corp",
  "token_limit": 50000,
  "branding": {
    "logo_url": "https://example.com/logo.png",
    "theme_color": "#3B82F6",
    "welcome_message": "Hello! How can I help you today?",
    "pre_questions": ["What is your pricing?", "How does it work?"],
    "allow_embedding": true
  }
}
```

#### Get Usage Analytics
```http
GET /admin/usage
Authorization: Bearer <admin-token>
```

### Client Endpoints

#### Upload PDF
```http
POST /client/upload
Authorization: Bearer <client-token>
Content-Type: multipart/form-data

pdf: <file>
```

#### Update Branding
```http
POST /client/branding
Authorization: Bearer <client-token>
Content-Type: application/json

{
  "logo_url": "https://example.com/new-logo.png",
  "theme_color": "#10B981",
  "welcome_message": "Welcome to our support chat!",
  "pre_questions": ["FAQ 1", "FAQ 2", "FAQ 3"]
}
```

#### Check Token Usage
```http
GET /client/tokens
Authorization: Bearer <client-token>
```

### Chat Endpoints

#### Send Message
```http
POST /chat/send
Authorization: Bearer <token>
Content-Type: application/json

{
  "message": "What are your business hours?",
  "conversation_id": "optional-conversation-id"
}
```

#### Get Conversation History
```http
GET /chat/conversations/conversation-id
Authorization: Bearer <token>
```

## 🔧 Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MONGO_URI` | MongoDB connection string | `mongodb://localhost:27017/saas_chatbot` |
| `JWT_SECRET` | JWT signing secret (256-bit minimum) | Required |
| `GEMINI_API_KEY` | Google Gemini API key | Required |
| `PORT` | Server port | `8080` |
| `MAX_FILE_SIZE` | Maximum PDF file size in bytes | `10485760` (10MB) |
| `DEFAULT_TOKEN_LIMIT` | Default token limit for new clients | `10000` |
| `MAX_CHUNK_SIZE` | PDF chunk size in tokens | `1000` |
| `CHUNK_OVERLAP` | Chunk overlap in characters | `200` |

### Database Indexes

The application automatically creates the following indexes:

- **users**: `username` (unique), `client_id`
- **clients**: `name` (unique), `embed_secret`
- **pdfs**: `client_id`, `client_id + filename`
- **messages**: `client_id`, `conversation_id`, `from_user_id`, `timestamp`

## 🏢 Multi-Tenant Architecture

### Data Isolation

Each client tenant has complete data isolation:

- **Users**: Scoped to specific client via `client_id`
- **PDFs**: Client-specific document storage
- **Messages**: Conversation history per client
- **Branding**: Custom theming per client
- **Token Limits**: Individual usage tracking

### Role-Based Access Control

- **Admin**: Full system access, client management
- **Client**: Access to own tenant data only
- **Visitor**: Limited access for embedded chat

## 🤖 AI Integration

### Gemini Pro Integration

- **Context-Aware Responses**: Uses PDF content for relevant answers
- **Token Estimation**: Accurate usage calculation
- **Error Handling**: Retry logic with exponential backoff
- **Rate Limiting**: Prevents API quota exhaustion

### PDF Processing

1. **Text Extraction**: Real PDF parsing with fallback simulation
2. **Intelligent Chunking**: Configurable chunk size with overlap
3. **Context Retrieval**: Semantic search through document chunks
4. **Relevance Scoring**: Keyword-based chunk ranking

## 🔐 Security Features

### Authentication Security

- **Bcrypt Hashing**: Minimum cost factor of 12
- **JWT Tokens**: Secure signing with role-based claims
- **Token Validation**: Comprehensive verification middleware
- **CORS Protection**: Configurable origin policies

### Data Security

- **Input Validation**: Request sanitization and validation
- **SQL Injection Prevention**: MongoDB parameterized queries
- **File Upload Security**: Type and size validation
- **Rate Limiting**: API endpoint protection

## 📊 Monitoring & Analytics

### Usage Tracking

- **Token Consumption**: Real-time usage monitoring
- **API Metrics**: Request/response tracking
- **Client Activity**: User engagement statistics
- **Error Monitoring**: Comprehensive error logging

### Analytics Dashboard Data

- Total clients and active users
- Token usage across all tenants
- Message volume and conversation metrics
- Client-specific performance statistics

## 🧪 Testing

### Running Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific test package
go test ./utils -v
```

### Test Coverage

- **Unit Tests**: JWT utilities, password hashing, PDF processing
- **Integration Tests**: Complete API workflows
- **Security Tests**: Authentication and authorization flows
- **Concurrency Tests**: Token limit enforcement

## 🚀 Deployment

### Docker Deployment

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o main cmd/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/main .
CMD ["./main"]
```

### Environment Setup

1. **Production MongoDB**: Use MongoDB Atlas or dedicated instance
2. **SSL/TLS**: Configure HTTPS with proper certificates
3. **Load Balancing**: Use reverse proxy (nginx, traefik)
4. **Monitoring**: Integrate with logging and monitoring services

## 🔄 API Rate Limits

- **Authentication**: 10 requests/minute per IP
- **File Upload**: 5 files/minute per client
- **Chat Messages**: 60 messages/minute per user
- **Admin Operations**: 100 requests/minute

## 📝 Development Guidelines

### Code Organization

- **cmd/**: Application entry points
- **internal/config/**: Configuration management
- **models/**: Data models and schemas
- **routes/**: HTTP route handlers
- **middleware/**: Request middleware
- **utils/**: Utility functions

### Database Operations

- **Atomic Updates**: Use MongoDB transactions for token updates
- **Optimistic Locking**: Prevent race conditions
- **Connection Pooling**: Efficient database connections
- **Index Optimization**: Query performance optimization

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Write tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## 📄 License

This project is licensed under the MIT License - see the LICENSE file for details.

## 🆘 Support

For support and questions:

- Create an issue in the repository
- Check the documentation for common solutions
- Review the test files for usage examples

## 🔄 Version History

- **v1.0.0**: Initial release with core functionality
- **v1.1.0**: Enhanced PDF processing and chunking
- **v1.2.0**: Improved authentication and security
- **v1.3.0**: Advanced analytics and monitoring

---

## Architecture Decisions

### Why Go + Gin?

- **Performance**: Excellent concurrency handling
- **Type Safety**: Compile-time error detection
- **Ecosystem**: Rich library support
- **Deployment**: Single binary deployment

### Why MongoDB?

- **Flexible Schema**: Easy tenant customization
- **Horizontal Scaling**: Built-in sharding support
- **JSON Storage**: Natural API data mapping
- **Atomic Operations**: Consistent token updates

### Token Management Strategy

The platform uses atomic MongoDB operations to prevent race conditions in token usage:

```go
filter := bson.M{
    "_id": clientID,
    "token_used": bson.M{"$lte": tokenLimit - estimatedTokens},
}
update := bson.M{"$inc": bson.M{"token_used": estimatedTokens}}
```

This ensures tokens are never over-allocated, even under high concurrency.

### PDF Chunking Algorithm

The intelligent chunking system:

1. Splits text into configurable chunk sizes (default: 1000 tokens)
2. Maintains overlap between chunks (default: 200 characters)
3. Preserves document structure and context
4. Enables efficient semantic search

This approach balances context preservation with AI model token limits.