## 📝 Summary
Briefly describe the changes introduced in this Pull Request and link any related issue (e.g., `Closes #123`).

## 🔍 Type of Change
- [ ] `fix:` Bug fix (bumps **Patch** version `0.1.X`)
- [ ] `feat:` New feature (bumps **Minor** version `0.X.0`)
- [ ] `feat!:` Breaking change (bumps **Major** version `X.0.0`)
- [ ] `docs:` Documentation update
- [ ] `chore:` CI/CD, build, or dependency update

## ✅ Checklist
- [ ] Code compiles cleanly (`go build -v ./...`)
- [ ] MyGo Native UI static check passes (`go tool mygo vet .`)
- [ ] All unit & single-port integration tests pass (`go test -v ./...`)
- [ ] Desktop-only UI features respect `cfg.ShowDesktopFeatures()` so `--mode=server` keeps them hidden
- [ ] Single-port architecture (`:8443`) is preserved without opening extra ports
- [ ] Documentation (`README.md` / `CHANGELOG.md`) updated if applicable

