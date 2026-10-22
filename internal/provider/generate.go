package provider

// The registry docs are rendered from the provider's schema, so they are generated after
// the field tables under internal/generated. `go generate ./...` runs packages in
// path order, which puts this directive after theirs.

//go:generate go run ../../tools/docgen -root ../.. -out ../../docs
