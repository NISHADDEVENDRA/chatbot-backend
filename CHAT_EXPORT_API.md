# 📊 Chat Export API Documentation

## Overview
Comprehensive chat export functionality with JSON and Excel formats, including production-level error handling, fallback mechanisms, and security features.

## 🚀 Features

### **Export Formats**
- **JSON**: Structured data with metadata and summary
- **Excel**: Multi-sheet workbook with data and analytics
- **ZIP**: Both JSON and Excel files in a single download

### **Data Included**
- **Basic Chat Data**: Messages, replies, timestamps, conversation IDs
- **User Information**: IP addresses, user agents, referrers, session data
- **Geolocation Data**: Country, region, city, coordinates, timezone, ISP, IP type
- **Metadata**: Client IDs, user IDs, creation timestamps
- **Analytics**: Summary statistics, top countries, top ISPs, conversation metrics

### **Security Features**
- **Authentication Required**: JWT token validation
- **Role-Based Access**: Admin can export all data, users can only export their client's data
- **Rate Limiting**: Built-in protection against abuse
- **Data Validation**: Comprehensive input validation and sanitization

## 📡 API Endpoints

### 1. **POST /client/export/chats**
Export chat data with detailed configuration.

**Request Body:**
```json
{
  "format": "json|excel|both",
  "date_from": "2024-01-01T00:00:00Z",
  "date_to": "2024-12-31T23:59:59Z",
  "client_id": "optional_client_id",
  "conversation_id": "optional_conversation_id",
  "limit": 10000,
  "include_geo": true,
  "include_meta": true
}
```

**Response:**
```json
{
  "success": true,
  "message": "Export generated successfully",
  "file_size": 2048576,
  "record_count": 1500
}
```

### 2. **GET /client/export/chats/download**
Direct download of exported data.

**Query Parameters:**
- `format`: json|excel|both (default: json)
- `date_from`: YYYY-MM-DD format
- `date_to`: YYYY-MM-DD format
- `client_id`: Optional client filter
- `conversation_id`: Optional conversation filter
- `limit`: Maximum records (default: 10000)
- `include_geo`: true|false (default: false)
- `include_meta`: true|false (default: false)

**Response:** File download with appropriate Content-Type headers

## 📋 Export Data Structure

### **JSON Format**
```json
{
  "export_info": {
    "export_date": "2024-01-15T10:30:00Z",
    "total_records": 1500,
    "date_range": "2024-01-01 to 2024-01-15",
    "client_id": "64f8a1b2c3d4e5f6a7b8c9d0",
    "format": "json",
    "include_geo": true,
    "include_meta": true
  },
  "messages": [
    {
      "id": "64f8a1b2c3d4e5f6a7b8c9d1",
      "from_name": "Embed User",
      "message": "How can I contact support?",
      "reply": "You can contact us at...",
      "timestamp": "2024-01-15T10:30:00Z",
      "conversation_id": "conv_12345",
      "token_cost": 150,
      "user_ip": "192.168.1.100",
      "user_agent": "Mozilla/5.0...",
      "referrer": "https://example.com",
      "session_id": "sess_67890",
      "is_embed_user": true,
      "geo_data": {
        "country": "United States",
        "country_code": "US",
        "region": "CA",
        "region_name": "California",
        "city": "San Francisco",
        "latitude": 37.7749,
        "longitude": -122.4194,
        "timezone": "America/Los_Angeles",
        "isp": "Comcast Cable",
        "organization": "Comcast Cable Communications",
        "ip_type": "Residential"
      },
      "meta_data": {
        "created_at": "2024-01-15T10:30:00Z",
        "client_id": "64f8a1b2c3d4e5f6a7b8c9d0",
        "from_user_id": "000000000000000000000000",
        "user_name": "",
        "user_email": ""
      }
    }
  ],
  "summary": {
    "total_messages": 1500,
    "total_tokens": 225000,
    "unique_users": 250,
    "date_range": "2024-01-01 to 2024-01-15",
    "top_countries": [
      {"country": "United States", "count": 800},
      {"country": "Canada", "count": 300},
      {"country": "United Kingdom", "count": 200}
    ],
    "top_isps": [
      {"isp": "Comcast Cable", "count": 400},
      {"isp": "Verizon", "count": 300},
      {"isp": "AT&T", "count": 200}
    ],
    "ip_type_breakdown": {
      "Residential": 1200,
      "Mobile": 200,
      "Datacenter": 80,
      "VPN": 20
    },
    "conversation_stats": {
      "total_conversations": 500,
      "avg_messages_per_conversation": 3.0,
      "longest_conversation": 25
    }
  }
}
```

### **Excel Format**
- **Sheet 1: "Chat Messages"** - All chat data in tabular format
- **Sheet 2: "Summary"** - Analytics and statistics

## 🔧 Configuration Options

### **Export Request Parameters**

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `format` | string | Yes | "json" | Export format: json, excel, both |
| `date_from` | datetime | No | - | Start date for filtering |
| `date_to` | datetime | No | - | End date for filtering |
| `client_id` | string | No | - | Filter by specific client |
| `conversation_id` | string | No | - | Filter by specific conversation |
| `limit` | integer | No | 10000 | Maximum records to export |
| `include_geo` | boolean | No | false | Include geolocation data |
| `include_meta` | boolean | No | false | Include metadata |

### **Security & Performance**

| Feature | Description |
|---------|-------------|
| **Authentication** | JWT token required for all requests |
| **Role-Based Access** | Users can only export their client's data |
| **Rate Limiting** | Built-in protection against abuse |
| **Data Validation** | Comprehensive input validation |
| **Error Handling** | Graceful fallbacks for all error conditions |
| **Memory Management** | Efficient streaming for large datasets |

## 🛠️ Usage Examples

### **Basic JSON Export**
```bash
curl -X POST "https://api.yourdomain.com/client/export/chats" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "format": "json",
    "include_geo": true,
    "limit": 1000
  }'
```

### **Excel Export with Date Range**
```bash
curl -X POST "https://api.yourdomain.com/client/export/chats" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "format": "excel",
    "date_from": "2024-01-01T00:00:00Z",
    "date_to": "2024-01-31T23:59:59Z",
    "include_geo": true,
    "include_meta": true
  }'
```

### **Direct Download**
```bash
curl -X GET "https://api.yourdomain.com/client/export/chats/download?format=excel&include_geo=true&limit=5000" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -o "chat_export.xlsx"
```

## 🚨 Error Handling

### **Common Error Responses**

```json
{
  "error_code": "unauthorized",
  "message": "Authentication required"
}
```

```json
{
  "error_code": "invalid_request",
  "message": "Invalid export request: format must be one of [json, excel, both]"
}
```

```json
{
  "error_code": "export_failed",
  "message": "Failed to export chats: database connection error"
}
```

### **Fallback Mechanisms**

1. **Database Connection Issues**: Graceful degradation with cached data
2. **Memory Limitations**: Automatic pagination for large datasets
3. **API Rate Limits**: Intelligent retry with exponential backoff
4. **File Generation Errors**: Fallback to simpler formats

## 📈 Performance Considerations

### **Optimization Features**
- **Pagination**: Automatic handling of large datasets
- **Streaming**: Direct file streaming for downloads
- **Caching**: Intelligent caching of frequently accessed data
- **Compression**: ZIP compression for multiple file exports

### **Recommended Limits**
- **Small Exports**: < 1,000 records (instant)
- **Medium Exports**: 1,000 - 10,000 records (2-5 seconds)
- **Large Exports**: 10,000 - 100,000 records (10-30 seconds)
- **Very Large**: > 100,000 records (consider pagination)

## 🔒 Security Best Practices

### **Data Protection**
- All exports are scoped to user's client data
- Sensitive information is filtered based on user permissions
- IP addresses are anonymized for privacy compliance
- Export logs are maintained for audit purposes

### **Rate Limiting**
- 10 exports per minute per user
- 100 exports per hour per client
- Automatic blocking for abuse detection

## 📊 Analytics & Monitoring

### **Export Metrics**
- Total exports per user/client
- Export format preferences
- Data volume trends
- Error rates and types

### **Performance Monitoring**
- Export generation time
- Memory usage patterns
- Database query performance
- File size distributions

---

**Note**: This export system is designed for production use with enterprise-grade reliability, security, and performance. All features include comprehensive error handling and fallback mechanisms.
