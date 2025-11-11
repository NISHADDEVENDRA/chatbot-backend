# 📧 Email Setup Guide

This guide explains how to configure SMTP email settings to enable email sending functionality for quote/proposal emails and token alerts.

## 🔧 Required Environment Variables

Add the following SMTP configuration variables to your `.env` file in the `backend/` directory:

```env
# SMTP Email Configuration
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
SMTP_PASS=your-app-password
SMTP_FROM=your-email@gmail.com
ADMIN_EMAILS=admin@example.com,admin2@example.com
```

## 📝 Configuration Details

### SMTP_HOST
- **Description**: Your SMTP server hostname
- **Examples**:
  - Gmail: `smtp.gmail.com`
  - Outlook: `smtp-mail.outlook.com`
  - SendGrid: `smtp.sendgrid.net`
  - Custom SMTP: `mail.yourdomain.com`

### SMTP_PORT
- **Description**: SMTP server port
- **Default**: `587` (TLS/STARTTLS)
- **Common Ports**:
  - `587` - TLS/STARTTLS (recommended)
  - `465` - SSL/TLS
  - `25` - Plain SMTP (not recommended for production)

### SMTP_USER
- **Description**: Your SMTP username/email address
- **Example**: `your-email@gmail.com`

### SMTP_PASS
- **Description**: Your SMTP password or app password
- **Important**: 
  - For Gmail, you need to use an **App Password** (not your regular password)
  - For other providers, use your account password or app-specific password

### SMTP_FROM
- **Description**: The "From" email address displayed in sent emails
- **Example**: `your-email@gmail.com` or `noreply@yourdomain.com`
- **Note**: Some providers require this to match `SMTP_USER`

### ADMIN_EMAILS
- **Description**: Comma-separated list of admin email addresses
- **Example**: `admin@example.com,admin2@example.com`
- **Purpose**: These emails receive:
  - Quote/proposal notifications
  - Token usage alerts
  - System notifications

## 🔐 Provider-Specific Setup

### Gmail Setup

1. **Enable 2-Factor Authentication** (required for App Passwords)
   - Go to your Google Account settings
   - Enable 2-Step Verification

2. **Generate App Password**
   - Go to: https://myaccount.google.com/apppasswords
   - Select "Mail" and "Other (Custom name)"
   - Enter "Chatbot Platform" as the name
   - Copy the generated 16-character password

3. **Configure `.env`**:
```env
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
SMTP_PASS=xxxx xxxx xxxx xxxx  # The 16-character app password
SMTP_FROM=your-email@gmail.com
ADMIN_EMAILS=your-email@gmail.com
```

### Outlook/Hotmail Setup

```env
SMTP_HOST=smtp-mail.outlook.com
SMTP_PORT=587
SMTP_USER=your-email@outlook.com
SMTP_PASS=your-password
SMTP_FROM=your-email@outlook.com
ADMIN_EMAILS=your-email@outlook.com
```

### SendGrid Setup

1. Create a SendGrid account at https://sendgrid.com
2. Create an API key with "Mail Send" permissions
3. Configure `.env`:
```env
SMTP_HOST=smtp.sendgrid.net
SMTP_PORT=587
SMTP_USER=apikey
SMTP_PASS=your-sendgrid-api-key
SMTP_FROM=noreply@yourdomain.com
ADMIN_EMAILS=admin@yourdomain.com
```

### Custom SMTP Server Setup

```env
SMTP_HOST=mail.yourdomain.com
SMTP_PORT=587
SMTP_USER=your-username
SMTP_PASS=your-password
SMTP_FROM=noreply@yourdomain.com
ADMIN_EMAILS=admin@yourdomain.com
```

## ✅ Testing Email Configuration

After configuring your `.env` file, restart your backend server:

```bash
cd backend
go run cmd/main.go
```

### Test Email Sending

1. **Via Chatbot Widget**:
   - Open your chatbot widget on a website
   - Click "Get Quote" button
   - Fill in company name, description, and email
   - Submit the form
   - Check the recipient email inbox

2. **Check Server Logs**:
   - If email sending fails, check the server console for error messages
   - Common errors:
     - `authentication failed` - Wrong credentials
     - `connection refused` - Wrong host/port
     - `timeout` - Firewall blocking port

## 🚨 Troubleshooting

### Authentication Failed
- **Gmail**: Make sure you're using an App Password, not your regular password
- **Other providers**: Verify your username and password are correct
- Check if 2FA is enabled and requires app passwords

### Connection Refused
- Verify `SMTP_HOST` and `SMTP_PORT` are correct
- Check firewall settings (port 587 must be open)
- Some networks block SMTP ports - try using a VPN

### Emails Not Delivering
- Check spam/junk folders
- Verify `SMTP_FROM` address is valid
- Some providers require domain verification for custom "From" addresses
- Check SMTP server logs for rejection reasons

### Port 587 Blocked
- Try port `465` (SSL/TLS) instead
- Update `SMTP_PORT=465` in `.env`
- Note: Some providers use different ports

## 📋 Complete `.env` Example

```env
# Database
MONGO_URI=mongodb://localhost:27017/saas_chatbot
DB_NAME=saas_chatbot

# JWT Secrets
JWT_SECRET=your-super-secure-jwt-secret-256-bits-minimum
ACCESS_SECRET=your-access-secret-256-bits-minimum
REFRESH_SECRET=your-refresh-secret-256-bits-minimum

# Gemini AI
GEMINI_API_KEY=your-gemini-api-key-here

# SMTP Email Configuration
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
SMTP_PASS=your-app-password
SMTP_FROM=your-email@gmail.com
ADMIN_EMAILS=admin@example.com

# Server
PORT=8080
CORS_ORIGINS=http://localhost:3000,http://localhost:8080
```

## 🔒 Security Best Practices

1. **Never commit `.env` file** to version control
2. **Use App Passwords** instead of account passwords when possible
3. **Use environment-specific credentials** for production vs development
4. **Rotate passwords regularly** for production environments
5. **Use dedicated SMTP accounts** for production (not personal email)

## 📚 Additional Resources

- [Gmail App Passwords](https://support.google.com/accounts/answer/185833)
- [SendGrid SMTP Setup](https://docs.sendgrid.com/for-developers/sending-email/getting-started-smtp)
- [Outlook SMTP Settings](https://support.microsoft.com/en-us/office/pop-imap-and-smtp-settings-8361e398-8af4-4e97-b147-6c6c4ac95353)

---

**Note**: After updating your `.env` file, restart your backend server for changes to take effect.

