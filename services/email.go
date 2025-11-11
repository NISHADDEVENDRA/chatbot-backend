package services

import (
    "bytes"
    "context"
    "crypto/tls"
    "fmt"
    "html/template"
    "log"
    "net"
    "net/smtp"
    "strconv"
    "strings"
    "time"
    
    "saas-chatbot-platform/internal/config"
    "saas-chatbot-platform/models"
)

type EmailSender interface {
    SendTokenAlert(client models.Client, alertLevel string, tokenData TokenAlertData) error
}

type SMTPEmailSender struct {
    config config.Config
}

type TokenAlertData struct {
    TenantName         string
    ClientEmail        string
    AdminEmails        []string
    UsedTokens         int
    TotalTokens        int
    RemainingTokens    int
    PercentUsed        float64
    ProjectedRunoutDate *time.Time
}

func NewSMTPEmailSender(cfg config.Config) *SMTPEmailSender {
    return &SMTPEmailSender{config: cfg}
}

func (s *SMTPEmailSender) SendTokenAlert(client models.Client, alertLevel string, tokenData TokenAlertData) error {
    // Prepare recipient list
    recipients := []string{}
    if client.ContactEmail != "" {
        recipients = append(recipients, client.ContactEmail)
    }
    for _, adminEmail := range s.config.AdminEmails {
        if strings.TrimSpace(adminEmail) != "" {
            recipients = append(recipients, strings.TrimSpace(adminEmail))
        }
    }
    
    if len(recipients) == 0 {
        return fmt.Errorf("no recipients configured for client %s", client.Name)
    }
    
    // Generate email content
    subject, htmlBody, textBody, err := s.generateEmailContent(alertLevel, tokenData)
    if err != nil {
        return fmt.Errorf("failed to generate email content: %w", err)
    }
    
    // Send email
    return s.sendEmail(recipients, subject, htmlBody, textBody)
}

func (s *SMTPEmailSender) generateEmailContent(alertLevel string, data TokenAlertData) (subject, htmlBody, textBody string, err error) {
    var subjectTpl, htmlTpl, textTpl string
    
    switch alertLevel {
    case "warn":
        subjectTpl = "Token Usage Warning - {{.TenantName}} ({{.PercentUsed}}% used)"
        htmlTpl = getWarnHTMLTemplate()
        textTpl = getWarnTextTemplate()
    case "critical":
        subjectTpl = "CRITICAL: Token Usage Alert - {{.TenantName}} ({{.PercentUsed}}% used)"
        htmlTpl = getCriticalHTMLTemplate()
        textTpl = getCriticalTextTemplate()
    case "exhausted":
        subjectTpl = "URGENT: Tokens Exhausted - {{.TenantName}}"
        htmlTpl = getExhaustedHTMLTemplate()
        textTpl = getExhaustedTextTemplate()
    default:
        return "", "", "", fmt.Errorf("unknown alert level: %s", alertLevel)
    }
    
    // Parse and execute templates
    subjectT, _ := template.New("subject").Parse(subjectTpl)
    htmlT, _ := template.New("html").Parse(htmlTpl)
    textT, _ := template.New("text").Parse(textTpl)
    
    var subjectBuf, htmlBuf, textBuf bytes.Buffer
    
    if err := subjectT.Execute(&subjectBuf, data); err != nil {
        return "", "", "", err
    }
    if err := htmlT.Execute(&htmlBuf, data); err != nil {
        return "", "", "", err
    }
    if err := textT.Execute(&textBuf, data); err != nil {
        return "", "", "", err
    }
    
    return subjectBuf.String(), htmlBuf.String(), textBuf.String(), nil
}

func (s *SMTPEmailSender) sendEmail(recipients []string, subject, htmlBody, textBody string) error {
    // Validate SMTP configuration
    if s.config.SMTPHost == "" || s.config.SMTPUser == "" || s.config.SMTPPass == "" || s.config.SMTPFrom == "" {
        return fmt.Errorf("SMTP configuration incomplete - missing required fields (Host: %v, User: %v, Pass: %v, From: %v)", 
            s.config.SMTPHost != "", s.config.SMTPUser != "", s.config.SMTPPass != "", s.config.SMTPFrom != "")
    }
    
    // Create context with timeout for SMTP connection (10 seconds)
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    // SMTP authentication
    auth := smtp.PlainAuth("", s.config.SMTPUser, s.config.SMTPPass, s.config.SMTPHost)
    
    // Compose message
    message := fmt.Sprintf(`From: %s
To: %s
Subject: %s
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary="boundary123"

--boundary123
Content-Type: text/plain; charset=UTF-8

%s

--boundary123
Content-Type: text/html; charset=UTF-8

%s

--boundary123--`,
        s.config.SMTPFrom,
        strings.Join(recipients, ", "),
        subject,
        textBody,
        htmlBody)
    
    // Send email with timeout
    addr := fmt.Sprintf("%s:%s", s.config.SMTPHost, s.config.SMTPPort)
    port, _ := strconv.Atoi(s.config.SMTPPort)
    
    // Use dialer with timeout
    log.Printf("📧 Connecting to SMTP server: %s (port %d)", addr, port)
    d := &net.Dialer{Timeout: 5 * time.Second}
    conn, err := d.DialContext(ctx, "tcp", addr)
    if err != nil {
        log.Printf("❌ SMTP connection failed: %v", err)
        return fmt.Errorf("failed to connect to SMTP server %s: %w", addr, err)
    }
    defer conn.Close()
    log.Printf("✅ Connected to SMTP server")
    
    // Set deadline on the connection for all SMTP operations
    conn.SetDeadline(time.Now().Add(10 * time.Second))
    
    var client *smtp.Client
    
    // Port 465 uses SSL/TLS from the start (no STARTTLS)
    if port == 465 {
        log.Printf("📧 Using SSL/TLS for port 465...")
        tlsConfig := &tls.Config{
            ServerName: s.config.SMTPHost,
            InsecureSkipVerify: false,
        }
        tlsConn := tls.Client(conn, tlsConfig)
        if err := tlsConn.HandshakeContext(ctx); err != nil {
            log.Printf("❌ TLS handshake failed: %v", err)
            return fmt.Errorf("TLS handshake failed: %w", err)
        }
        log.Printf("✅ TLS handshake successful")
        
        // Create SMTP client over TLS connection
        client, err = smtp.NewClient(tlsConn, s.config.SMTPHost)
        if err != nil {
            log.Printf("❌ Failed to create SMTP client: %v", err)
            return fmt.Errorf("failed to create SMTP client: %w", err)
        }
    } else {
        // Port 587 uses STARTTLS (upgrade after connection)
        log.Printf("📧 Using STARTTLS for port %d...", port)
        client, err = smtp.NewClient(conn, s.config.SMTPHost)
        if err != nil {
            log.Printf("❌ Failed to create SMTP client: %v", err)
            return fmt.Errorf("failed to create SMTP client: %w", err)
        }
        
        // Check if server supports STARTTLS
        if ok, _ := client.Extension("STARTTLS"); ok {
            log.Printf("📧 Starting TLS...")
            tlsConfig := &tls.Config{
                ServerName: s.config.SMTPHost,
                InsecureSkipVerify: false,
            }
            if err := client.StartTLS(tlsConfig); err != nil {
                log.Printf("❌ STARTTLS failed: %v", err)
                return fmt.Errorf("STARTTLS failed: %w", err)
            }
            log.Printf("✅ STARTTLS successful")
        }
    }
    
    defer client.Close()
    log.Printf("✅ SMTP client created")
    
    // Authenticate
    log.Printf("📧 Authenticating with SMTP server...")
    if err := client.Auth(auth); err != nil {
        log.Printf("❌ SMTP authentication failed: %v", err)
        return fmt.Errorf("SMTP authentication failed: %w", err)
    }
    log.Printf("✅ SMTP authentication successful")
    
    // Set sender
    log.Printf("📧 Setting sender: %s", s.config.SMTPFrom)
    if err := client.Mail(s.config.SMTPFrom); err != nil {
        log.Printf("❌ Failed to set sender: %v", err)
        return fmt.Errorf("failed to set sender: %w", err)
    }
    
    // Set recipients
    log.Printf("📧 Setting recipients: %v", recipients)
    for _, recipient := range recipients {
        if err := client.Rcpt(recipient); err != nil {
            log.Printf("❌ Failed to set recipient %s: %v", recipient, err)
            return fmt.Errorf("failed to set recipient %s: %w", recipient, err)
        }
    }
    log.Printf("✅ All recipients set")
    
    // Send email data
    log.Printf("📧 Sending email data...")
    w, err := client.Data()
    if err != nil {
        log.Printf("❌ Failed to open data connection: %v", err)
        return fmt.Errorf("failed to open data connection: %w", err)
    }
    _, err = w.Write([]byte(message))
    if err != nil {
        w.Close()
        log.Printf("❌ Failed to write email data: %v", err)
        return fmt.Errorf("failed to write email data: %w", err)
    }
    err = w.Close()
    if err != nil {
        log.Printf("❌ Failed to close data connection: %v", err)
        return fmt.Errorf("failed to close data connection: %w", err)
    }
    log.Printf("✅ Email sent successfully to %v", recipients)
    
    return nil
}

// SendEmail sends a generic email with HTML and text bodies
func (s *SMTPEmailSender) SendEmail(recipients []string, subject, htmlBody, textBody string) error {
    return s.sendEmail(recipients, subject, htmlBody, textBody)
}

// Email templates
func getWarnHTMLTemplate() string {
    return `<html><body>
<h2>Token Usage Warning</h2>
<p>Hello,</p>
<p>Your chatbot service <strong>{{.TenantName}}</strong> has used <strong>{{.PercentUsed}}%</strong> of allocated tokens.</p>
<ul>
<li>Used: {{.UsedTokens}} tokens</li>
<li>Total: {{.TotalTokens}} tokens</li>
<li>Remaining: {{.RemainingTokens}} tokens</li>
</ul>
<p>Consider upgrading your plan or monitoring usage closely.</p>
</body></html>`
}

func getWarnTextTemplate() string {
    return `Token Usage Warning

Hello,

Your chatbot service {{.TenantName}} has used {{.PercentUsed}}% of allocated tokens.

Used: {{.UsedTokens}} tokens
Total: {{.TotalTokens}} tokens  
Remaining: {{.RemainingTokens}} tokens

Consider upgrading your plan or monitoring usage closely.`
}

func getCriticalHTMLTemplate() string {
    return `<html><body>
<h2 style="color: red;">CRITICAL: Token Usage Alert</h2>
<p>Hello,</p>
<p><strong style="color: red;">URGENT:</strong> Your chatbot service <strong>{{.TenantName}}</strong> has used <strong>{{.PercentUsed}}%</strong> of allocated tokens.</p>
<ul>
<li>Used: {{.UsedTokens}} tokens</li>
<li>Total: {{.TotalTokens}} tokens</li>
<li>Remaining: {{.RemainingTokens}} tokens</li>
</ul>
<p><strong>Action required immediately</strong> to avoid service interruption.</p>
</body></html>`
}

func getCriticalTextTemplate() string {
    return `CRITICAL: Token Usage Alert

Hello,

URGENT: Your chatbot service {{.TenantName}} has used {{.PercentUsed}}% of allocated tokens.

Used: {{.UsedTokens}} tokens
Total: {{.TotalTokens}} tokens
Remaining: {{.RemainingTokens}} tokens

Action required immediately to avoid service interruption.`
}

func getExhaustedHTMLTemplate() string {
    return `<html><body>
<h2 style="color: red;">URGENT: Tokens Exhausted</h2>
<p>Hello,</p>
<p><strong style="color: red;">SERVICE IMPACT:</strong> Your chatbot service <strong>{{.TenantName}}</strong> has exhausted all allocated tokens.</p>
<ul>
<li>Used: {{.UsedTokens}} tokens</li>
<li>Total: {{.TotalTokens}} tokens</li>
<li>Remaining: 0 tokens</li>
</ul>
<p><strong>Immediate action required</strong> - service may be limited until tokens are replenished.</p>
</body></html>`
}

func getExhaustedTextTemplate() string {
    return `URGENT: Tokens Exhausted

Hello,

SERVICE IMPACT: Your chatbot service {{.TenantName}} has exhausted all allocated tokens.

Used: {{.UsedTokens}} tokens
Total: {{.TotalTokens}} tokens
Remaining: 0 tokens

Immediate action required - service may be limited until tokens are replenished.`
}
