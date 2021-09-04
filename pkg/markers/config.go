package markers

import (
	"fmt"
	"strings"

	jsoniter "github.com/json-iterator/go"
)

type config interface {
	getMarkerPrefix() string
}

// MarkerForConfig returns raw marker string for a given config
func MarkerForConfig(m config) (string, error) {
	jc := jsoniter.Config{TagKey: "marker", SortMapKeys: true}.Froze()
	b, err := jc.Marshal(m)
	if err != nil {
		return "", err
	}

	args := string(b)
	args = strings.TrimLeft(args, "{")
	args = strings.TrimRight(args, "}")
	args = strings.ReplaceAll(args, ":", "=")
	args = strings.ReplaceAll(args, "\"", "")

	marker := fmt.Sprintf("%s%s", Prefix, m.getMarkerPrefix())
	if args != "" {
		marker += ":" + args
	}

	return marker, nil
}

// Must returns marker string if no error and panics otherwise
func Must(s string, e error) string {
	if e != nil {
		panic(e)
	}
	return s
}
