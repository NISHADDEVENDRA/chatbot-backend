package services

import (
    "bytes"
    "fmt"
    "html/template"
    "net/smtp"
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
    
    // Send email
    addr := fmt.Sprintf("%s:%s", s.config.SMTPHost, s.config.SMTPPort)
    return smtp.SendMail(addr, auth, s.config.SMTPFrom, recipients, []byte(message))
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
