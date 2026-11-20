// Package langs lists the language frontends compiled into myoo.
package langs

import (
	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/code/clike"
	"github.com/myoo-ai/cli/internal/code/cpp"
	"github.com/myoo-ai/cli/internal/code/golang"
	"github.com/myoo-ai/cli/internal/code/javascript"
	"github.com/myoo-ai/cli/internal/code/php"
	"github.com/myoo-ai/cli/internal/code/python"
	"github.com/myoo-ai/cli/internal/code/rust"
)

// All returns an instance of each frontend.
func All() []code.Frontend {
	return []code.Frontend{golang.New(), python.New(), javascript.New(), clike.New(), cpp.New(), rust.New(), php.New()}
}
