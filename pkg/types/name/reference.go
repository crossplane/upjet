/*
Copyright 2022 Upbound Inc.
*/

package name

// NOTE(muvaf): We try to rely on snake for name calculations because it is more
// accurate in cases where two words are all acronyms and full capital, i.e.
// APIID would be converted to apiid when you convert it to lower camel computed
// but if you start with api_id, then it becomes apiId as lower camel computed
// and APIID as camel, which is what we want.

// ReferenceFieldName returns the field name for a reference field whose
// value field name is given.
func ReferenceFieldName(n Name, plural bool, camelOverride string) Name {
	if camelOverride != "" {
		return NewFromCamel(camelOverride)
	}
	temp := n.Snake + "_ref"
	if plural {
		temp += "s"
	}
	return NewFromSnake(temp)
}

// SelectorFieldName returns the field name for a selector field whose
// value field name is given.
func SelectorFieldName(n Name, camelOverride string) Name {
	if camelOverride != "" {
		return NewFromCamel(camelOverride)
	}
	return NewFromSnake(n.Snake + "_selector")
}
