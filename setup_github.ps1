# GitHub Setup Script for Backend
# Run this AFTER creating your repository on GitHub

Write-Host "=== GitHub Backend Setup Script ===" -ForegroundColor Cyan
Write-Host ""

# Get repository details
$username = Read-Host "Enter your GitHub username"
$repoName = Read-Host "Enter your repository name (e.g., chatbot-backend)"

# Ask for authentication method
Write-Host ""
Write-Host "Choose authentication method:" -ForegroundColor Yellow
Write-Host "1. HTTPS (Personal Access Token)"
Write-Host "2. SSH"
$authChoice = Read-Host "Enter choice (1 or 2)"

# Change to backend directory
Set-Location "D:\Devendra\Chatbot-Vectors\backend"

# Remove existing remote if it exists
Write-Host ""
Write-Host "Removing old remote..." -ForegroundColor Yellow
git remote remove origin 2>$null

# Add new remote based on choice
if ($authChoice -eq "1") {
    $remoteUrl = "https://github.com/$username/$repoName.git"
    Write-Host "Adding HTTPS remote: $remoteUrl" -ForegroundColor Green
    git remote add origin $remoteUrl
} else {
    $remoteUrl = "git@github.com:$username/$repoName.git"
    Write-Host "Adding SSH remote: $remoteUrl" -ForegroundColor Green
    git remote add origin $remoteUrl
}

# Ensure we're on main branch
Write-Host ""
Write-Host "Setting branch to main..." -ForegroundColor Yellow
git branch -M main

# Stage all changes
Write-Host ""
Write-Host "Staging all changes..." -ForegroundColor Yellow
git add .

# Check if there are changes to commit
$status = git status --porcelain
if ($status) {
    Write-Host ""
    $commitMessage = Read-Host "Enter commit message (or press Enter for default)"
    if ([string]::IsNullOrWhiteSpace($commitMessage)) {
        $commitMessage = "Initial commit: Backend codebase"
    }
    
    Write-Host "Committing changes..." -ForegroundColor Yellow
    git commit -m $commitMessage
} else {
    Write-Host "No changes to commit." -ForegroundColor Green
}

# Push to GitHub
Write-Host ""
Write-Host "Pushing to GitHub..." -ForegroundColor Yellow
Write-Host "If using HTTPS, you'll be prompted for credentials." -ForegroundColor Cyan
Write-Host "Use your Personal Access Token as the password." -ForegroundColor Cyan
Write-Host ""

git push -u origin main

if ($LASTEXITCODE -eq 0) {
    Write-Host ""
    Write-Host "✓ Successfully pushed to GitHub!" -ForegroundColor Green
    Write-Host "Repository URL: https://github.com/$username/$repoName" -ForegroundColor Cyan
} else {
    Write-Host ""
    Write-Host "✗ Push failed. Please check:" -ForegroundColor Red
    Write-Host "  1. Repository exists on GitHub" -ForegroundColor Yellow
    Write-Host "  2. Authentication is set up correctly" -ForegroundColor Yellow
    Write-Host "  3. You have push permissions" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "Done!" -ForegroundColor Green

