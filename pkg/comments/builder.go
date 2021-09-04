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

	// Options represents terrajet options built with the comments
	Options markers.Options
}

// AddComment adds comment to the comments builder
func (b *Builder) AddComment(comment string) error {
	for _, line := range strings.Split(comment, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, markers.Prefix) {
			if err := markers.ParseIfMarkerForField(&b.Options, line); err != nil {
				return errors.Wrap(err, errCannotParseAsFieldMarker)
			}
			b.markerLines = append(b.markerLines, line)
			continue
		}
		// TODO(hasan): we would likely want to wrap long lines with a certain
		//  character limit.
		b.lines = append(b.lines, line)
	}
	return nil
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
