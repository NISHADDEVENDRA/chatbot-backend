package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// EmailTemplate represents an email template for quote/proposal emails
type EmailTemplate struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ClientID      primitive.ObjectID `bson:"client_id" json:"client_id"`
	Type          string             `bson:"type" json:"type"` // "quote_visitor", "quote_company", etc.
	Name          string             `bson:"name" json:"name" binding:"required"` // Template name
	Subject       string             `bson:"subject" json:"subject" binding:"required"`
	
	// HTML and Text body templates (supports placeholders)
	HTMLBody      string             `bson:"html_body" json:"html_body" binding:"required"`
	TextBody      string             `bson:"text_body" json:"text_body" binding:"required"`
	
	// Template fields for quote visitor email
	TemplateFields EmailTemplateFields `bson:"template_fields" json:"template_fields"`
	
	IsActive  bool      `bson:"is_active" json:"is_active"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

// EmailTemplateFields contains all dynamic fields for quote email
type EmailTemplateFields struct {
	// Company information
	CompanyName        string `bson:"company_name" json:"company_name"`
	CompanyDescription string `bson:"company_description" json:"company_description"`
	
	// Welcome/Greeting section
	GreetingMessage string `bson:"greeting_message" json:"greeting_message"`
	ServiceIntroduction string `bson:"service_introduction" json:"service_introduction"`
	
	// Services section
	ServiceBenefits string `bson:"service_benefits" json:"service_benefits"` // Why choose us
	FreePanelMessage string `bson:"free_panel_message" json:"free_panel_message"`
	RetailRateMessage string `bson:"retail_rate_message" json:"retail_rate_message"`
	
	// Pricing plans (dynamic array)
	PricingPlans []PricingPlan `bson:"pricing_plans" json:"pricing_plans"`
	
	// How it works section
	HowItWorksTitle string   `bson:"how_it_works_title" json:"how_it_works_title"`
	HowItWorksFeatures []string `bson:"how_it_works_features" json:"how_it_works_features"`
	
	// Demo section
	DemoTitle       string `bson:"demo_title" json:"demo_title"`
	DemoDescription string `bson:"demo_description" json:"demo_description"`
	DemoURL         string `bson:"demo_url" json:"demo_url"`
	DemoUsername    string `bson:"demo_username" json:"demo_username"`
	DemoPassword    string `bson:"demo_password" json:"demo_password"`
	
	// Links section
	CompanyProfileURL string `bson:"company_profile_url" json:"company_profile_url"`
	ClientListURL     string `bson:"client_list_url" json:"client_list_url"`
	FAQsURL           string `bson:"faqs_url" json:"faqs_url"`
	
	// Footer/CTA section
	CTATitle    string `bson:"cta_title" json:"cta_title"`
	CTAMessage  string `bson:"cta_message" json:"cta_message"`
	
	// Footer contact information
	FooterName    string `bson:"footer_name" json:"footer_name"`
	FooterPhone   string `bson:"footer_phone" json:"footer_phone"`
	FooterEmail   string `bson:"footer_email" json:"footer_email"`
	FooterWebsite string `bson:"footer_website" json:"footer_website"`
	
	// Special offers/discounts message
	SpecialDiscountMessage string `bson:"special_discount_message" json:"special_discount_message"`
}

// PricingPlan represents a pricing plan in the email template
type PricingPlan struct {
	Title       string `bson:"title" json:"title"` // e.g., "Buy 2 Lac WhatsApp messages, Get 1 Lac Free"
	Price       string `bson:"price" json:"price"` // e.g., "INR 1,20,000/- Plus GST"
	Rate        string `bson:"rate" json:"rate"`   // e.g., "@40 Paisa Per Message"
	IsActive    bool   `bson:"is_active" json:"is_active"`
	DisplayOrder int   `bson:"display_order" json:"display_order"`
}

