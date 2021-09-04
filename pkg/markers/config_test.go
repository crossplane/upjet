package markers

import (
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
)

const (
	markerPrefixForNoOption = "terrajet:markerwith:Nooption"
	markerPrefixForWithArgs = "terrajet:markerwith"
)

type markerWithNoArg struct{}

func (m markerWithNoArg) getMarkerPrefix() string {
	return markerPrefixForNoOption
}

type markerWithOneArg struct {
	Key1 *string `marker:"key1,optional,omitempty"`
}

func (m markerWithOneArg) getMarkerPrefix() string {
	return markerPrefixForWithArgs
}

type markerWithMultipleArgs struct {
	Key1 *string `marker:"key1,optional,omitempty"`
	Key2 string  `marker:"key2"`
	Key3 int     `marker:"key3"`
}

func (m markerWithMultipleArgs) getMarkerPrefix() string {
	return markerPrefixForWithArgs
}

func TestMarkerForConfig(t *testing.T) {
	val1 := "val1"

	type args struct {
		marker config
	}
	type want struct {
		out string
		err error
	}

	cases := map[string]struct {
		args
		want
	}{
		"MarkerWithNoArg": {
			args: args{
				marker: markerWithNoArg{},
			},
			want: want{
				out: Prefix + markerPrefixForNoOption,
			},
		},
		"MarkerWithOneOption": {
			args: args{
				marker: markerWithOneArg{
					Key1: &val1,
				},
			},
			want: want{
				out: Prefix + markerPrefixForWithArgs + ":key1=val1",
			},
		},
		"MarkerWithMultipleOptions": {
			args: args{
				marker: markerWithMultipleArgs{
					Key1: &val1,
					Key2: "val2",
					Key3: 3,
				},
			},
			want: want{
				out: Prefix + markerPrefixForWithArgs + ":key1=val1,key2=val2,key3=3",
			},
		},
		"KubebuilderValidationRequired": {
			args: args{
				marker: ValidationRequired{},
			},
			want: want{
				out: "+kubebuilder:validation:Required",
			},
		},
		"KubebuilderValidationOptional": {
			args: args{
				marker: ValidationOptional{},
			},
			want: want{
				out: "+kubebuilder:validation:Optional",
			},
		},
		"KubebuilderValidationMinimum": {
			args: args{
				marker: ValidationMinimum{Minimum: 1},
			},
			want: want{
				out: "+kubebuilder:validation:Minimum=1",
			},
		},
		"KubebuilderValidationMaximum": {
			args: args{
				marker: ValidationMaximum{Maximum: 3},
			},
			want: want{
				out: "+kubebuilder:validation:Maximum=3",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotErr := MarkerForConfig(tc.args.marker)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Errorf("ForConfig() error = %v, wantErr %v", gotErr, tc.want.err)
			}
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("ForConfig() out = %v, wantOut %v", got, tc.want.out)
			}
		})
	}
}
