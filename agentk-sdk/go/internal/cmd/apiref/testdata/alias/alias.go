// Package alias exports a type implemented in an internal package.
package alias

import "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/internal/cmd/apiref/testdata/internal/target"

// Subject exposes target.Subject without wrapping it.
type Subject = target.Subject
