package version

// Version is injected at link time via -ldflags "-X github.com/michaelsanford/wtop/internal/version.Version=<tag>".
var Version = "dev"
