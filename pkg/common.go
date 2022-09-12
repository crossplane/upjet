package pkg

import "strings"

const (
	descriptionSeparator = "."
	TerraformKeyword     = "terraform"
)

// FilterDescription filters given keyword in description by deleting the whole
// sentence.
func FilterDescription(description, keyword string) string {
	var result []string
	sentences := strings.Split(description, descriptionSeparator)
	for _, s := range sentences {
		if !strings.Contains(strings.ToLower(s), keyword) {
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		return strings.ReplaceAll(strings.ToLower(description), keyword, "Upbound official provider")
	}
	return strings.Join(result, descriptionSeparator)
}
