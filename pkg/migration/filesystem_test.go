package migration

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var (
	unstructuredAwsVpc = map[string]interface{}{
		"apiVersion": "ec2.aws.crossplane.io/v1beta1",
		"kind":       "VPC",
		"metadata": map[string]interface{}{
			"name": "sample-vpc",
		},
		"spec": map[string]interface{}{
			"forProvider": map[string]interface{}{
				"region":    "us-west-1",
				"cidrBlock": "172.16.0.0/16",
			},
		},
	}

	unstructuredResourceGroup = map[string]interface{}{
		"apiVersion": "azure.crossplane.io/v1beta1",
		"kind":       "ResourceGroup",
		"metadata": map[string]interface{}{
			"name": "example-resources",
		},
		"spec": map[string]interface{}{
			"forProvider": map[string]interface{}{
				"location": "West Europe",
			},
		},
	}
)

func TestNewFileSystemSource(t *testing.T) {
	type args struct {
		dir string
		a   func() afero.Afero
	}
	type want struct {
		fs  *FileSystemSource
		err error
	}

	cases := map[string]struct {
		args
		want
	}{
		"Successful": {
			args: args{
				dir: "testdata",
				a: func() afero.Afero {
					fss := afero.Afero{Fs: afero.NewMemMapFs()}
					_ = fss.WriteFile("testdata/source/awsvpc.yaml",
						[]byte("apiVersion: ec2.aws.crossplane.io/v1beta1\nkind: VPC\nmetadata:\n  name: sample-vpc\nspec:\n  forProvider:\n    cidrBlock: 172.16.0.0/16\n    region: us-west-1\n"),
						0600)
					_ = fss.WriteFile("testdata/source/resourcegroup.yaml",
						[]byte("apiVersion: azure.crossplane.io/v1beta1\nkind: ResourceGroup\nmetadata:\n  name: example-resources\nspec:\n  forProvider:\n    location: West Europe\n"),
						0600)
					return fss
				},
			},
			want: want{
				fs: &FileSystemSource{
					index: 0,
					items: []UnstructuredWithMetadata{
						{
							Object: unstructured.Unstructured{
								Object: unstructuredAwsVpc,
							},
							Metadata: Metadata{
								Path: "testdata/source/awsvpc.yaml",
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: unstructuredResourceGroup,
							},
							Metadata: Metadata{
								Path: "testdata/source/resourcegroup.yaml",
							},
						},
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			files := tc.args.a()
			fs, err := NewFileSystemSource("testdata/source", FsWithFileSystem(files))
			if err != nil {
				t.Fatalf("Failed to initialize a new FileSystemSource: %v", err)
			}
			if diff := cmp.Diff(tc.want.err, err); diff != "" {
				t.Errorf("\nNext(...): -want, +got:\n%s", diff)
			}
			if diff := cmp.Diff(tc.want.fs.items, fs.items); diff != "" {
				t.Errorf("\nNext(...): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestFileSystemTarget_Put(t *testing.T) {
	type args struct {
		o UnstructuredWithMetadata
		a func() afero.Afero
	}
	type want struct {
		data string
		err  error
	}

	cases := map[string]struct {
		args
		want
	}{
		"Write": {
			args: args{
				o: UnstructuredWithMetadata{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "ec2.aws.upbound.io/v1beta1",
							"kind":       "VPC",
							"metadata": map[string]interface{}{
								"name": "sample-vpc",
							},
							"spec": map[string]interface{}{
								"forProvider": map[string]interface{}{
									"region":    "us-west-1",
									"cidrBlock": "172.16.0.0/16",
								},
							},
						},
					},
					Metadata: Metadata{
						Path: "testdata/source/awsvpc.yaml",
					},
				},
				a: func() afero.Afero {
					return afero.Afero{Fs: afero.NewMemMapFs()}
				},
			},
			want: want{
				data: "apiVersion: ec2.aws.upbound.io/v1beta1\nkind: VPC\nmetadata:\n  name: sample-vpc\nspec:\n  forProvider:\n    cidrBlock: 172.16.0.0/16\n    region: us-west-1\n",
				err:  nil,
			},
		},
		"Append": {
			args: args{
				o: UnstructuredWithMetadata{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "azure.crossplane.io/v1beta1",
							"kind":       "ResourceGroup",
							"metadata": map[string]interface{}{
								"name": "example-resources",
							},
							"spec": map[string]interface{}{
								"forProvider": map[string]interface{}{
									"location": "West Europe",
								},
							},
						},
					},
					Metadata: Metadata{
						Path:    "testdata/source/awsvpc.yaml",
						Parents: "parent metadata",
					},
				},
				a: func() afero.Afero {
					fss := afero.Afero{Fs: afero.NewMemMapFs()}
					_ = fss.WriteFile("testdata/source/awsvpc.yaml",
						[]byte("apiVersion: ec2.aws.upbound.io/v1beta1\nkind: VPC\nmetadata:\n  name: sample-vpc\nspec:\n  forProvider:\n    cidrBlock: 172.16.0.0/16\n    region: us-west-1\n"),
						0600)
					return fss
				},
			},
			want: want{
				data: "apiVersion: ec2.aws.upbound.io/v1beta1\nkind: VPC\nmetadata:\n  name: sample-vpc\nspec:\n  forProvider:\n    cidrBlock: 172.16.0.0/16\n    region: us-west-1\n\n---\n\napiVersion: azure.crossplane.io/v1beta1\nkind: ResourceGroup\nmetadata:\n  name: example-resources\nspec:\n  forProvider:\n    location: West Europe\n",
				err:  nil,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			files := tc.args.a()
			ft := NewFileSystemTarget(FtWithFileSystem(files))
			if err := ft.Put(tc.args.o); err != nil {
				t.Error(err)
			}
			b, err := ft.afero.ReadFile("testdata/source/awsvpc.yaml")
			if diff := cmp.Diff(tc.want.err, err); diff != "" {
				t.Errorf("\nNext(...): -want, +got:\n%s", diff)
			}
			if diff := cmp.Diff(tc.want.data, string(b)); diff != "" {
				t.Errorf("\nNext(...): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestFileSystemTarget_Delete(t *testing.T) {
	type args struct {
		o UnstructuredWithMetadata
		a func() afero.Afero
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"Successful": {
			args: args{
				o: UnstructuredWithMetadata{
					Metadata: Metadata{
						Path: "testdata/source/awsvpc.yaml",
					},
				},
				a: func() afero.Afero {
					fss := afero.Afero{Fs: afero.NewMemMapFs()}
					_ = fss.WriteFile("testdata/source/awsvpc.yaml",
						[]byte("apiVersion: ec2.aws.upbound.io/v1beta1\nkind: VPC\nmetadata:\n  name: sample-vpc\nspec:\n  forProvider:\n    cidrBlock: 172.16.0.0/16\n    region: us-west-1\n"),
						0600)
					return fss
				},
			},
			want: want{
				err: errors.New(fmt.Sprintf("%s: %s", "open testdata/source/awsvpc.yaml", afero.ErrFileNotFound)),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			files := tc.args.a()
			ft := NewFileSystemTarget(FtWithFileSystem(files))
			if err := ft.Delete(tc.args.o); err != nil {
				t.Error(err)
			}
			_, err := ft.afero.ReadFile("testdata/source/awsvpc.yaml")
			if diff := cmp.Diff(tc.want.err.Error(), err.Error()); diff != "" {
				t.Errorf("\nNext(...): -want, +got:\n%s", diff)
			}
		})
	}
}
