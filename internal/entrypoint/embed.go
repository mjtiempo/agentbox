// Package entrypoint embeds the container entrypoint script (FR-9). The
// script is the single source of truth: Dockerfiles COPY the same file and Go
// embeds it, so the contract cannot drift between image and binary.
package entrypoint

import _ "embed"

//go:embed entrypoint.sh
var Script string

// Path is where every adapter image installs the script.
const Path = "/entrypoint.sh"
