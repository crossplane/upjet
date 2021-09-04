package comments

import (
	"strings"

	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/markers"
)

const (
	errCannotParseAsFieldMarker = "cannot parse line as marker for field"
)

// Builder builds comments for fields by also building terrajet options
// supported by comment markers
type Builder struct {
	lines       []string
	markerLines []string
}

// AddComment adds comment to the comments builder
func (b *Builder) AddComment(comment string) {
	for _, line := range strings.Split(comment, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, markers.Prefix) {
			b.markerLines = append(b.markerLines, line)
			continue
		}
		// TODO(hasan): we would likely want to wrap long lines with a certain
		//  character limit.
		b.lines = append(b.lines, line)
	}
}

// Build builds comments using added comments by first printing non-marker
// ones and then marker ones.
func (b *Builder) Build() string {
	c := ""
	if len(b.lines) > 0 {
		c = "// " + strings.Join(b.lines, "\n// ")
	}
	if len(b.markerLines) > 0 {
		c = c + "\n// " + strings.Join(b.markerLines, "\n// ")
	}
	return c
}

// BuildOptions returns terrajet options built with the comments
func (b *Builder) BuildOptions() (*markers.Options, error) {
	opts := &markers.Options{}
	for _, rawMarker := range b.markerLines {
		if err := markers.ParseIfMarkerForField(opts, rawMarker); err != nil {
			return nil, errors.Wrap(err, errCannotParseAsFieldMarker)
		}
	}
	return opts, nil
}
