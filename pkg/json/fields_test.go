package json

import (
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
)

var testData = []byte(`
{
  "top_level_secret": "sensitive-data-top-level-secret",
  "top_config_secretmap": {
	"inner_config_secretmap.first": "sensitive-data-inner-first",
	"inner_config_secretmap_second": "sensitive-data-inner-second",
	"inner_config_secretmap_third": "sensitive-data-inner-third"
  },
  "top_object_with_number": { "key1": 1, "key2": 2, "key3": 3},
  "top_config_array": [
    {
      "inner_some_field": "non-sensitive-data-1",
      "inner_config_array": [
        {
          "bottom_some_field": "non-sensitive-data-1",
          "bottom_level_secret": "sensitive-data-bottom-level-1"
        }
      ]
    },
    {
      "inner_some_field": "non-sensitive-data-2"
    },
    {
      "inner_some_field": "non-sensitive-data-3",
      "inner_config_array": [
        {
          "bottom_some_field": "non-sensitive-data-3a",
          "bottom_level_secret": "sensitive-data-bottom-level-3a"
        },
        {
          "bottom_some_field": "non-sensitive-data-3a",
          "bottom_level_secret": "sensitive-data-bottom-level-3b"
        }
      ]
    }
  ]
}
`)

func TestValuesMatchingPaths(t *testing.T) {
	type args struct {
		paths []string
		data  []byte
	}
	type want struct {
		out map[string][]byte
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"Single": {
			args: args{
				paths: []string{"top_level_secret"},
				data:  testData,
			},
			want: want{
				out: map[string][]byte{
					"top_level_secret": []byte("sensitive-data-top-level-secret"),
				},
			},
		},
		"SingleNonExisting": {
			args: args{
				paths: []string{"missing_field"},
				data:  testData,
			},
			want: want{
				err: errors.Wrapf(errors.Wrapf(
					errors.Errorf("%s: no such field", "missing_field"),
					errFmtCannotGetStringsForParts, []string{"missing_field"}),
					errFmtCannotGetStringsForFieldPath, "missing_field"),
			},
		},
		"SingleGettingNumber": {
			args: args{
				paths: []string{"top_object_with_number[key1]"},
				data:  testData,
			},
			want: want{
				err: errors.Wrapf(errors.Wrapf(
					errors.Errorf("%s: not a string", "top_object_with_number.key1"),
					errFmtCannotGetStringsForParts, []interface{}{"top_object_with_number", "key1"}),
					errFmtCannotGetStringsForFieldPath, "top_object_with_number[key1]"),
			},
		},
		"WildcardMultipleFromMap": {
			args: args{
				paths: []string{"top_config_secretmap[*]"},
				data:  testData,
			},
			want: want{
				out: map[string][]byte{
					"top_config_secretmap[inner_config_secretmap.first]": []byte("sensitive-data-inner-first"),
					"top_config_secretmap.inner_config_secretmap_second": []byte("sensitive-data-inner-second"),
					"top_config_secretmap.inner_config_secretmap_third":  []byte("sensitive-data-inner-third"),
				},
			},
		},
		"WildcardMultipleFromArray": {
			args: args{
				paths: []string{"top_config_array[*].inner_some_field"},
				data:  testData,
			},
			want: want{
				out: map[string][]byte{
					"top_config_array[0].inner_some_field": []byte("non-sensitive-data-1"),
					"top_config_array[1].inner_some_field": []byte("non-sensitive-data-2"),
					"top_config_array[2].inner_some_field": []byte("non-sensitive-data-3"),
				},
			},
		},
		"WildcardMultipleFromArrayMultipleLevel": {
			args: args{
				paths: []string{"top_config_array[*].inner_config_array[*].bottom_level_secret"},
				data:  testData,
			},
			want: want{
				out: map[string][]byte{
					"top_config_array[0].inner_config_array[0].bottom_level_secret": []byte("sensitive-data-bottom-level-1"),
					"top_config_array[2].inner_config_array[0].bottom_level_secret": []byte("sensitive-data-bottom-level-3a"),
					"top_config_array[2].inner_config_array[1].bottom_level_secret": []byte("sensitive-data-bottom-level-3b"),
				},
			},
		},
		"WildcardMixedWithNumbers": {
			args: args{
				paths: []string{"top_config_array[2].inner_config_array[*].bottom_level_secret"},
				data:  testData,
			},
			want: want{
				out: map[string][]byte{
					"top_config_array[2].inner_config_array[0].bottom_level_secret": []byte("sensitive-data-bottom-level-3a"),
					"top_config_array[2].inner_config_array[1].bottom_level_secret": []byte("sensitive-data-bottom-level-3b"),
				},
			},
		},
		"MultipleFieldPaths": {
			args: args{
				paths: []string{"top_level_secret", "top_config_secretmap.*", "top_config_array[2].inner_config_array[*].bottom_level_secret"},
				data:  testData,
			},
			want: want{
				out: map[string][]byte{
					"top_level_secret": []byte("sensitive-data-top-level-secret"),
					"top_config_secretmap[inner_config_secretmap.first]":            []byte("sensitive-data-inner-first"),
					"top_config_secretmap.inner_config_secretmap_second":            []byte("sensitive-data-inner-second"),
					"top_config_secretmap.inner_config_secretmap_third":             []byte("sensitive-data-inner-third"),
					"top_config_array[2].inner_config_array[0].bottom_level_secret": []byte("sensitive-data-bottom-level-3a"),
					"top_config_array[2].inner_config_array[1].bottom_level_secret": []byte("sensitive-data-bottom-level-3b"),
				},
			},
		},
		"NotAValue": {
			args: args{
				paths: []string{"top_config_secretmap"},
				data:  testData,
			},
			want: want{
				err: errors.Wrapf(errors.Wrapf(
					errors.Errorf("%s: not a string", "top_config_secretmap"),
					errFmtCannotGetStringsForParts, []interface{}{"top_config_secretmap"}),
					errFmtCannotGetStringsForFieldPath, "top_config_secretmap"),
			},
		},
		"UnexpectedWildcard": {
			args: args{
				paths: []string{"top_level_secret.*"},
				data:  testData,
			},
			want: want{
				err: errors.Wrapf(errors.Wrap(
					errors.Errorf(errFmtUnexpectedWildcardUsage, jsoniter.StringValue),
					errCannotExpandWildcards),
					errFmtCannotGetStringsForFieldPath, "top_level_secret.*"),
			},
		},
		"UnexpectedWildcardInObjectMultipleLevel": {
			args: args{
				paths: []string{"top_config_array.*.inner_some_field.*"},
				data:  testData,
			},
			want: want{
				err: errors.Wrapf(errors.Wrap(errors.Wrapf(
					errors.Errorf(errFmtUnexpectedWildcardUsage, jsoniter.StringValue),
					errFmtCannotExpandForArray, []interface{}{"top_config_array", 0, "inner_some_field", '*'}),
					errCannotExpandWildcards),
					errFmtCannotGetStringsForFieldPath, "top_config_array.*.inner_some_field.*"),
			},
		},
		"NoData": {
			args: args{
				paths: []string{"top_level_secret"},
				data:  nil,
			},
			want: want{
				err: errors.Wrapf(errors.Wrapf(
					errors.Errorf("%s: no such field", "top_level_secret"),
					errFmtCannotGetStringsForParts, []string{"top_level_secret"}),
					errFmtCannotGetStringsForFieldPath, "top_level_secret"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotErr := StringsMatchingFieldPaths(tc.data, tc.paths)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("GetFields(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("GetFields(...) out = %v, want %v", got, tc.want.out)
			}
		})
	}
}
