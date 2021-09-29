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
	"inner_config_secretmap_first": "sensitive-data-inner-first",
	"inner_config_secretmap_second": "sensitive-data-inner-second",
	"inner_config_secretmap_third": "sensitive-data-inner-third"
  },
  "top_object_with_number": { "key1": 1, "key2": 2, "key3": 3},
  "top_object_with_bool": { "key1": true, "key2": false, "key3": true},
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

func TestValuesMatchingPath(t *testing.T) {
	type args struct {
		fieldPath string
		data      []byte
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
				fieldPath: "top_level_secret",
				data:      testData,
			},
			want: want{
				out: map[string][]byte{
					"top_level_secret": []byte("sensitive-data-top-level-secret"),
				},
			},
		},
		"SingleNonExisting": {
			args: args{
				fieldPath: "missing_field",
				data:      testData,
			},
			want: want{
				out: map[string][]byte{},
			},
		},
		"SingleGettingNumber": {
			args: args{
				fieldPath: "top_object_with_number.key1",
				data:      testData,
			},
			want: want{
				out: map[string][]byte{
					"top_object_with_number.key1": []byte("1"),
				},
			},
		},
		"WildcardGettingBool": {
			args: args{
				fieldPath: "top_object_with_bool.*",
				data:      testData,
			},
			want: want{
				out: map[string][]byte{
					"top_object_with_bool.key1": []byte("true"),
					"top_object_with_bool.key2": []byte("false"),
					"top_object_with_bool.key3": []byte("true"),
				},
			},
		},
		"WildcardMultipleFromMap": {
			args: args{
				fieldPath: "top_config_secretmap.*",
				data:      testData,
			},
			want: want{
				out: map[string][]byte{
					"top_config_secretmap.inner_config_secretmap_first":  []byte("sensitive-data-inner-first"),
					"top_config_secretmap.inner_config_secretmap_second": []byte("sensitive-data-inner-second"),
					"top_config_secretmap.inner_config_secretmap_third":  []byte("sensitive-data-inner-third"),
				},
			},
		},
		"WildcardMultipleFromArray": {
			args: args{
				fieldPath: "top_config_array.*.inner_some_field",
				data:      testData,
			},
			want: want{
				out: map[string][]byte{
					"top_config_array.0.inner_some_field": []byte("non-sensitive-data-1"),
					"top_config_array.1.inner_some_field": []byte("non-sensitive-data-2"),
					"top_config_array.2.inner_some_field": []byte("non-sensitive-data-3"),
				},
			},
		},
		"WildcardMultipleFromArrayMultipleLevel": {
			args: args{
				fieldPath: "top_config_array.*.inner_config_array.*.bottom_level_secret",
				data:      testData,
			},
			want: want{
				out: map[string][]byte{
					"top_config_array.0.inner_config_array.0.bottom_level_secret": []byte("sensitive-data-bottom-level-1"),
					"top_config_array.2.inner_config_array.0.bottom_level_secret": []byte("sensitive-data-bottom-level-3a"),
					"top_config_array.2.inner_config_array.1.bottom_level_secret": []byte("sensitive-data-bottom-level-3b"),
				},
			},
		},
		"WildcardMixedWithNumbers": {
			args: args{
				fieldPath: "top_config_array.2.inner_config_array.*.bottom_level_secret",
				data:      testData,
			},
			want: want{
				out: map[string][]byte{
					"top_config_array.2.inner_config_array.0.bottom_level_secret": []byte("sensitive-data-bottom-level-3a"),
					"top_config_array.2.inner_config_array.1.bottom_level_secret": []byte("sensitive-data-bottom-level-3b"),
				},
			},
		},
		"EndsWithWildcard": {
			args: args{
				fieldPath: "top_config_secretmap.*",
				data:      testData,
			},
			want: want{
				out: map[string][]byte{
					"top_config_secretmap.inner_config_secretmap_first":  []byte("sensitive-data-inner-first"),
					"top_config_secretmap.inner_config_secretmap_second": []byte("sensitive-data-inner-second"),
					"top_config_secretmap.inner_config_secretmap_third":  []byte("sensitive-data-inner-third"),
				},
			},
		},
		"NotAValue": {
			args: args{
				fieldPath: "top_config_secretmap",
				data:      testData,
			},
			want: want{
				err: errors.Wrapf(errors.Errorf(errFmtUnexpectedTypeForValue, jsoniter.ObjectValue), errFmtCannotGetValueForPath, []interface{}{"top_config_secretmap"}),
			},
		},
		"UnexpectedWildcard": {
			args: args{
				fieldPath: "top_level_secret.*",
				data:      testData,
			},
			want: want{
				err: errors.Wrap(errors.Errorf(errFmtUnexpectedWildcardUsage, jsoniter.StringValue), errCannotExpandWildcards),
			},
		},
		"UnexpectedWildcardInArrayMultipleLevel": {
			args: args{
				fieldPath: "top_config_array.*.inner_some_field.*",
				data:      testData,
			},
			want: want{
				err: errors.Wrap(errors.Wrapf(errors.Errorf(errFmtUnexpectedWildcardUsage, jsoniter.StringValue), errFmtCannotExpandForArray, []interface{}{"top_config_array", 0, "inner_some_field", "*"}), errCannotExpandWildcards),
			},
		},
		"UnexpectedWildcardInObjectMultipleLevel": {
			args: args{
				fieldPath: "top_config_array.*.inner_some_field.*",
				data:      testData,
			},
			want: want{
				err: errors.Wrap(errors.Wrapf(errors.Errorf(errFmtUnexpectedWildcardUsage, jsoniter.StringValue), errFmtCannotExpandForArray, []interface{}{"top_config_array", 0, "inner_some_field", "*"}), errCannotExpandWildcards),
			},
		},
		"NoData": {
			args: args{
				fieldPath: "top_level_secret",
				data:      nil,
			},
			want: want{
				out: map[string][]byte{},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotErr := valuesMatchingPath(tc.data, tc.fieldPath)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("GetFields(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("GetFields(...) out = %v, want %v", got, tc.want.out)
			}
		})
	}
}
