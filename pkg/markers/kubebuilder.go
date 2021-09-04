package markers

import "fmt"

const markerPrefixValidation = "kubebuilder:validation"

// ValidationRequired represents a kubebuilder validation required marker.
type ValidationRequired struct{}

func (v ValidationRequired) getMarkerPrefix() string {
	return fmt.Sprintf("%s:Required", markerPrefixValidation)
}

// ValidationOptional represents a kubebuilder validation optional marker.
type ValidationOptional struct{}

func (v ValidationOptional) getMarkerPrefix() string {
	return fmt.Sprintf("%s:Optional", markerPrefixValidation)
}

// ValidationMinimum represents a kubebuilder validation minimum marker.
type ValidationMinimum struct {
	Minimum int
}

func (v ValidationMinimum) getMarkerPrefix() string {
	return markerPrefixValidation
}

// ValidationMaximum represents a kubebuilder validation maximum marker.
type ValidationMaximum struct {
	Maximum int
}

func (v ValidationMaximum) getMarkerPrefix() string {
	return markerPrefixValidation
}
