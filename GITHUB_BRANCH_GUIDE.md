# GitHub Branch Guide - How to Change Branches

## 🌿 Understanding Branches

A **branch** is like a separate timeline for your code. You can have multiple branches for different features or versions.

- **main/master**: Usually your production/stable code
- **feature branches**: For new features (e.g., `feature/login`, `feature/payment`)
- **bugfix branches**: For fixing bugs (e.g., `bugfix/crash-fix`)

---

## 📋 Common Branch Operations

### 1. Check Current Branch

**See which branch you're on:**
```powershell
git branch
```
The branch with `*` is your current branch.

**Or see more details:**
```powershell
git status
```
Shows current branch and any uncommitted changes.

---

### 2. Switch to an Existing Branch

**If the branch exists locally:**
```powershell
git checkout branch-name
```

**Or using newer syntax:**
```powershell
git switch branch-name
```

**Example:**
```powershell
git switch main
git switch develop
git switch feature/login
```

---

### 3. Switch to a Branch from GitHub (Remote)

**If the branch exists on GitHub but not locally:**

```powershell
# Fetch all branches from GitHub
git fetch origin

# Switch to the remote branch
git checkout branch-name
# or
git switch branch-name
```

**Or in one command:**
```powershell
git checkout -b branch-name origin/branch-name
```

**Example:**
```powershell
# Someone created a branch called "develop" on GitHub
git fetch origin
git switch develop
```

---

### 4. Create a New Branch

**Create and switch to a new branch:**
```powershell
git checkout -b new-branch-name
```

**Or using newer syntax:**
```powershell
git switch -c new-branch-name
```

**Example:**
```powershell
# Create a branch for a new feature
git checkout -b feature/user-authentication

# Create a branch for bug fixes
git checkout -b bugfix/login-error
```

---

### 5. Create Branch from Current Branch

**Create new branch based on current branch:**
```powershell
# Make sure you're on the branch you want to base from
git checkout main
git checkout -b feature/new-feature
```

---

### 6. Create Branch from Specific Branch

**Create new branch from another branch:**
```powershell
git checkout -b new-branch existing-branch
```

**Example:**
```powershell
# Create feature branch from develop branch
git checkout -b feature/payment develop
```

---

### 7. List All Branches

**Local branches:**
```powershell
git branch
```

**Remote branches (on GitHub):**
```powershell
git branch -r
```

**All branches (local + remote):**
```powershell
git branch -a
```

---

### 8. Push New Branch to GitHub

**After creating a branch locally, push it to GitHub:**
```powershell
# Push and set up tracking
git push -u origin branch-name
```

**Example:**
```powershell
git checkout -b feature/login
# ... make some changes ...
git add .
git commit -m "Add login feature"
git push -u origin feature/login
```

**After first push, you can just use:**
```powershell
git push
```

---

### 9. Delete a Branch

**Delete local branch:**
```powershell
# Make sure you're NOT on the branch you want to delete
git branch -d branch-name
```

**Force delete (if branch has unmerged changes):**
```powershell
git branch -D branch-name
```

**Delete remote branch (on GitHub):**
```powershell
git push origin --delete branch-name
```

**Example:**
```powershell
# Switch to main first
git checkout main

# Delete local branch
git branch -d feature/old-feature

# Delete remote branch
git push origin --delete feature/old-feature
```

---

## 🔄 Complete Workflow Examples

### Example 1: Create Feature Branch and Push

```powershell
# 1. Make sure you're on main and it's up to date
cd D:\Devendra\Chatbot-Vectors\backend
git checkout main
git pull origin main

# 2. Create new feature branch
git checkout -b feature/add-search

# 3. Make your changes, then commit
git add .
git commit -m "Add search functionality"

# 4. Push to GitHub
git push -u origin feature/add-search
```

---

### Example 2: Switch Between Branches

```powershell
# You're working on feature/login
git checkout feature/login
# ... make changes ...

# Need to quickly fix something on main
git checkout main
# ... fix bug ...
git add .
git commit -m "Fix critical bug"
git push

# Go back to your feature
git checkout feature/login
# Continue working...
```

---

### Example 3: Merge Branch into Main

```powershell
# 1. Switch to main
git checkout main

# 2. Pull latest changes
git pull origin main

# 3. Merge your feature branch
git merge feature/add-search

# 4. Push to GitHub
git push origin main

# 5. (Optional) Delete the feature branch
git branch -d feature/add-search
git push origin --delete feature/add-search
```

---

## 🌐 Changing Branches on GitHub Website

**You can't "switch" branches on GitHub's website**, but you can:

### View Different Branches:
1. Go to your repository on GitHub
2. Click the branch dropdown (usually says "main" or shows branch count)
3. Select any branch to view its code

### Create Branch on GitHub:
1. Go to your repository
2. Click the branch dropdown
3. Type a new branch name
4. Click "Create branch: new-name from 'main'"

### Delete Branch on GitHub:
1. Go to your repository
2. Click "branches" link (or the branch dropdown)
3. Find the branch you want to delete
4. Click the trash icon

---

## ⚠️ Important Notes

### Before Switching Branches:

**Check for uncommitted changes:**
```powershell
git status
```

**If you have uncommitted changes:**
- **Option 1**: Commit them first
  ```powershell
  git add .
  git commit -m "Save work in progress"
  git checkout other-branch
  ```

- **Option 2**: Stash them (temporarily save)
  ```powershell
  git stash
  git checkout other-branch
  # ... do work ...
  git checkout original-branch
  git stash pop  # Restore your changes
  ```

- **Option 3**: Discard changes (⚠️ WARNING: You'll lose changes!)
  ```powershell
  git checkout -- .
  git checkout other-branch
  ```

---

## 🎯 Quick Reference Commands

| Task | Command |
|------|---------|
| See current branch | `git branch` or `git status` |
| Switch to branch | `git checkout branch-name` or `git switch branch-name` |
| Create new branch | `git checkout -b new-branch` |
| List all branches | `git branch -a` |
| Push branch to GitHub | `git push -u origin branch-name` |
| Delete local branch | `git branch -d branch-name` |
| Delete remote branch | `git push origin --delete branch-name` |
| Fetch remote branches | `git fetch origin` |

---

## 🐛 Troubleshooting

### Problem: "Please commit your changes or stash them"
**Solution:** You have uncommitted changes. Either:
- Commit them: `git add .` then `git commit -m "message"`
- Stash them: `git stash`
- Discard them: `git checkout -- .` (⚠️ loses changes)

### Problem: "branch 'xyz' does not exist"
**Solution:** 
- Check branch name: `git branch -a`
- If it's on GitHub: `git fetch origin` then `git checkout branch-name`

### Problem: "Your branch is behind 'origin/main'"
**Solution:** Pull latest changes:
```powershell
git pull origin main
```

### Problem: Can't delete branch
**Solution:** Make sure you're not on that branch:
```powershell
git checkout main
git branch -d branch-name
```

---

## 💡 Best Practices

1. **Always pull before switching:** `git pull origin main` before creating new branches
2. **Use descriptive names:** `feature/user-login` not `branch1`
3. **Delete merged branches:** Clean up after merging
4. **One feature per branch:** Keep branches focused
5. **Regular commits:** Commit often, push regularly

---

## 📝 Common Branch Naming Conventions

- `feature/description` - New features
- `bugfix/description` - Bug fixes
- `hotfix/description` - Urgent production fixes
- `develop` - Development branch
- `main` or `master` - Production branch
- `release/v1.0.0` - Release branches

**Examples:**
- `feature/add-payment-gateway`
- `bugfix/fix-login-crash`
- `hotfix/security-patch`
- `feature/user-profile-page`

