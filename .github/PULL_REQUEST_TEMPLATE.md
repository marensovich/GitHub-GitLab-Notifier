## 📝 Description

Brief description of the changes.

## 🔗 Related Issue

Closes #(issue number)

## 🛠️ Changes Made

- [ ] Added new feature
- [ ] Fixed bug
- [ ] Updated documentation
- [ ] Refactored code
- [ ] Added tests

## 🧪 Testing

Describe how you tested your changes:

```bash
# Commands to test
go fmt ./...
go vet ./...
make build
./GitHub-GitLab-Notifier --lang=en
```

## 📸 Screenshots

If applicable, add screenshots.

## ✅ Checklist

- [ ] Code formatted (`go fmt ./...`)
- [ ] No lint errors (`go vet ./...`)
- [ ] Build succeeds (`make build`)
- [ ] Tested with `--lang=en` and `--lang=ru`
- [ ] Database migrations work (delete `data/` and test fresh start)
- [ ] New i18n keys added to both `locales/en.json` and `locales/ru.json`
- [ ] Documentation updated (if needed)
- [ ] CHANGELOG updated (if significant change)

## 🚀 How to Test

```bash
# Clone the branch
git clone -b feature/your-branch https://github.com/marensovich/github-gitlab-notifier.git

# Setup
cp .env.example .env
# Edit .env

# Run
make run
```

## 💬 Additional Notes

Any other information reviewers should know.

