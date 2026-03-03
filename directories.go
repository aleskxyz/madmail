//go:build !docker
// +build !docker

package maddy

var (
	// ConfigDirectory specifies the configuration directory.
	// It can be overridden at build time via -X linker flag:
	//   -X github.com/themadorg/madmail.ConfigDirectory=/etc/myapp
	//
	// If empty (the default), the directory is derived at runtime from
	// the running binary name: /etc/<binaryname>.
	//
	// It should not be changed at runtime and is a variable only
	// for linker flag modification.
	ConfigDirectory = ""

	// DefaultStateDirectory specifies the default state directory.
	// If empty, derived at runtime as /var/lib/<binaryname>.
	DefaultStateDirectory = ""

	// DefaultRuntimeDirectory specifies the default runtime directory.
	// If empty, derived at runtime as /run/<binaryname>.
	DefaultRuntimeDirectory = ""

	// DefaultLibexecDirectory specifies the default libexec directory.
	// If empty, derived at runtime as /usr/lib/<binaryname>.
	DefaultLibexecDirectory = ""
)
