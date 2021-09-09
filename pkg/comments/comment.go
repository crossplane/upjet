package comments

import (
	"strings"

	"github.com/crossplane-contrib/terrajet/pkg/markers"
)

type Option func(*Comment)

func WithReferenceTo(t string) Option {
	return func(c *Comment) {
		c.ReferenceToType = t
	}
}

func WithTFTag(t string) Option {
	return func(c *Comment) {
		c.FieldTFTag = &t
	}
}

func New(text string, opts ...Option) *Comment {
	to := markers.TerrajetOptions{}

	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !markers.ParseAsTerrajetOption(&to, line) {
			lines = append(lines, line)
		}
	}

	c := &Comment{
		Text: strings.Join(lines, "\n"),
		Options: markers.Options{
			TerrajetOptions: to,
		},
	}

	for _, o := range opts {
		o(c)
	}

	return c
}

type Comment struct {
	Text string
	markers.Options
}

func (c *Comment) String() string {
	return c.Text + "\n" + c.Options.String()
}

// Build builds comments using added comments by first printing non-marker
// ones and then marker ones.
func (c *Comment) Build() string {
	all := strings.ReplaceAll("// "+c.String(), "\n", "\n// ")
	return strings.TrimSuffix(all, "// ")
}
