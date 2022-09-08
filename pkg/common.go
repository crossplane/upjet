package pkg

import "strings"

const (
	DescriptionSeparator = "."
	TerraformKeyword     = "terraform"
)

// FilterDescription filters given keyword in description by deleting the whole
// sentence.
func FilterDescription(description, keyword string) string {
	var result []string
	sentences := strings.Split(description, DescriptionSeparator)
	for _, s := range sentences {
		if !strings.Contains(strings.ToLower(s), keyword) {
			result = append(result, s)
		}
	}
	return strings.Join(result, DescriptionSeparator)
}
