## Summary

<!-- Brief description of changes and motivation -->

## Related Issues

<!-- Link related issues, e.g. Fixes #12, Relates to #34 -->

## Changes Checklist

- [ ] Code follows project standards in [CONTRIBUTING.md](CONTRIBUTING.md) and [CLAUDE.md](CLAUDE.md)
- [ ] Tests pass cleanly (`go test ./...`)
- [ ] Benchmark allocations verified if modifying collector or UI render paths (`go test -run='^$' -bench='^Benchmark[^L]' -benchmem ./...`)
- [ ] Cross-platform compilation stubs remain clean (`$env:GOOS="linux"; go vet ./...`)
- [ ] Commit messages follow [Conventional Commits v1.0.0](https://www.conventionalcommits.org/)
- [ ] Documentation updated if adding/modifying shortcuts, panels, or workflows
- [ ] No sensitive credentials, private keys, or personal telemetry introduced per [PRIVACY.md](PRIVACY.md)
