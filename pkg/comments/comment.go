package comments

import (
	"strings"

	"github.com/crossplane-contrib/terrajet/pkg/markers"
)

// Option is a comment option
type Option func(*Comment)

// WithReferenceTo returns a comment options with reference to input type
func WithReferenceTo(t string) Option {
	return func(c *Comment) {
		c.ReferenceToType = t
	}
}

// WithTFTag returns a comment options with input tf tag
func WithTFTag(t string) Option {
	return func(c *Comment) {
		c.FieldTFTag = &t
	}
}

// New returns a Comment by parsing Terrajet markers as Options
func New(text string, opts ...Option) (*Comment, error) {
	to := markers.TerrajetOptions{}

	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			lines = append(lines, line)
			continue
		}
		// Only add raw marker line if not processed as an option (i.e. if it is
		// not a Terrejet marker.) Terrajet markers will still be printed as
		// comments while building from options.
		parsed, err := markers.ParseAsTerrajetOption(&to, line)
		if err != nil {
			return nil, err
		}
		if !parsed {
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

	return c, nil
}

// Comment represents a comment with text and supported marker options.
type Comment struct {
	Text string
	markers.Options
}

// String returns a string representation of this Comment (no "// " prefix)
func (c *Comment) String() string {
	return c.Text + "\n" + c.Options.String()
}

// Build builds comments by adding comment prefix ("// ") to each line of
// the string representation of this Comment.
func (c *Comment) Build() string {
	all := strings.ReplaceAll("// "+c.String(), "\n", "\n// ")
	return strings.TrimSuffix(all, "// ")
}
