package main

import "github.com/sergey-pr/accentctl/cmd"

// version is set by goreleaser via -X main.version.
var version = "dev"

func main() {
	cmd.Execute(version)
}
