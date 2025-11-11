# GitHub Setup Guide for Backend

## Step 1: Create a New Repository on GitHub

1. Go to https://github.com and sign in (or create an account)
2. Click the "+" icon in the top right → "New repository"
3. Name it (e.g., `chatbot-backend` or `backend-go`)
4. Choose **Private** or **Public**
5. **DO NOT** initialize with README, .gitignore, or license (we already have code)
6. Click "Create repository"

## Step 2: Set Up GitHub Authentication

You have two options:

### Option A: Personal Access Token (Recommended for HTTPS)

1. Go to GitHub → Settings → Developer settings → Personal access tokens → Tokens (classic)
2. Click "Generate new token (classic)"
3. Give it a name (e.g., "Cursor Git Access")
4. Select scopes: **repo** (full control of private repositories)
5. Click "Generate token"
6. **COPY THE TOKEN** (you won't see it again!)

### Option B: SSH Key (Recommended for long-term use)

1. Generate SSH key:
   ```powershell
   ssh-keygen -t ed25519 -C "devendranishad981@gmail.com"
   ```
   (Press Enter to accept default location, optionally set a passphrase)

2. Start SSH agent:
   ```powershell
   Start-Service ssh-agent
   ssh-add ~/.ssh/id_ed25519
   ```

3. Copy your public key:
   ```powershell
   cat ~/.ssh/id_ed25519.pub
   ```

4. Go to GitHub → Settings → SSH and GPG keys → New SSH key
5. Paste your public key and save

## Step 3: Update Git Remote and Push

After creating your repository, run these commands (replace YOUR_USERNAME and YOUR_REPO_NAME):

### If using HTTPS (with Personal Access Token):
```powershell
cd D:\Devendra\Chatbot-Vectors\backend
git remote remove origin
git remote add origin https://github.com/YOUR_USERNAME/YOUR_REPO_NAME.git
git branch -M main
git add .
git commit -m "Initial commit: Backend codebase"
git push -u origin main
```
When prompted for password, use your **Personal Access Token** (not your GitHub password)

### If using SSH:
```powershell
cd D:\Devendra\Chatbot-Vectors\backend
git remote remove origin
git remote add origin git@github.com:YOUR_USERNAME/YOUR_REPO_NAME.git
git branch -M main
git add .
git commit -m "Initial commit: Backend codebase"
git push -u origin main
```

## Quick Commands Reference

```powershell
# Check current remote
git remote -v

# Remove old remote
git remote remove origin

# Add new remote (HTTPS)
git remote add origin https://github.com/YOUR_USERNAME/YOUR_REPO_NAME.git

# Add new remote (SSH)
git remote add origin git@github.com:YOUR_USERNAME/YOUR_REPO_NAME.git

# Stage all changes
git add .

# Commit
git commit -m "Your commit message"

# Push to GitHub
git push -u origin main
```

