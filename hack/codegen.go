// This package imports things required by build scripts, to force `go mod` to see them as dependencies
// Keep a reference to code-generator so it's not removed by go mod tidy
package hack

import _ "k8s.io/code-generator"
