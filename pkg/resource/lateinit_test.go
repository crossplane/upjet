/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resource

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestLateInitialize(t *testing.T) {
	type args struct {
		desiredObject  interface{}
		observedObject interface{}
		opts           []GenericLateInitializerOption
	}

	testKeyDesiredField := "test-key-desiredField"
	testStringDesiredField := "test-string-desiredField"
	testKeyObservedField := "test-key-observedField"
	testStringObservedField := "test-string-observedField"
	testInt64ObservedField := 1
	testStringEmpty := ""

	type nestedStruct1 struct {
		F1 *string
		F2 []*string
	}

	type nestedStruct2 struct {
		F1 *int
		F2 []*int
	}

	type nestedStruct3 struct {
		F1 *string
		F2 *string
	}

	type nestedStruct4 struct {
		F1 *string `json:"f_1,omitempty"`
	}

	type nestedStruct5 struct {
		F1 [][]string
	}

	type nestedStruct6 struct {
		F1 []string `json:"f_1,omitempty"`
	}

	type nestedStruct7 struct {
		F1 map[string]string
	}

	type nestedStruct8 struct {
		F1 map[string]*string
	}

	type nestedStruct9 struct {
		F1 map[string][]string
	}

	type nestedStruct10 struct {
		F1 *string
		F2 *nestedStruct1
	}

	tests := map[string]struct {
		args         args
		wantModified bool
		wantErr      bool
		wantCRObject interface{}
	}{
		"TypeMismatch": {
			args: args{
				desiredObject: &nestedStruct1{
					F1: &testStringDesiredField,
					F2: []*string{
						&testStringDesiredField,
					},
				},
				observedObject: &nestedStruct2{
					F1: &testInt64ObservedField,
				},
			},
			wantErr: true,
		},
		"NilCRObject": {
			args: args{
				observedObject: &struct{}{},
			},
		},
		"NilResponseObject": {
			args: args{
				desiredObject: &struct{}{},
			},
			wantCRObject: &struct{}{},
		},
		"TestNonStructCRObject": {
			args: args{
				desiredObject:  &testStringDesiredField,
				observedObject: &struct{}{},
			},
			wantErr: true,
		},
		"TestNonStructResponseObject": {
			args: args{
				desiredObject:  &struct{}{},
				observedObject: &testStringObservedField,
			},
			wantErr: true,
		},
		"TestEmptyStructCRAndResponseObjects": {
			args: args{
				desiredObject:  &struct{}{},
				observedObject: &struct{}{},
			},
			wantCRObject: &struct{}{},
		},
		"TestInitializedCRStringField": {
			args: args{
				desiredObject: &struct {
					F1 *string
				}{
					F1: &testStringDesiredField,
				},
				observedObject: &struct {
					F1 *string
				}{
					F1: &testStringObservedField,
				},
			},
			wantModified: false,
			wantCRObject: &struct {
				F1 *string
			}{
				F1: &testStringDesiredField,
			},
		},
		"TestUninitializedCRStringField": {
			args: args{
				desiredObject: &struct {
					F1 *string
				}{
					F1: nil,
				},
				observedObject: &struct {
					F1 *string
				}{
					F1: &testStringObservedField,
				},
			},
			wantModified: true,
			wantCRObject: &struct {
				F1 *string
			}{
				F1: &testStringObservedField,
			},
		},
		"TestInitializedCRNestedFields": {
			args: args{
				desiredObject: &struct {
					C1 *nestedStruct1
				}{
					C1: &nestedStruct1{
						F1: &testStringDesiredField,
						F2: []*string{
							&testStringDesiredField,
						},
					},
				},
				observedObject: &struct {
					C1 *nestedStruct1
				}{
					C1: &nestedStruct1{
						F1: &testStringObservedField,
						F2: []*string{
							&testStringObservedField,
						},
					},
				},
			},
			wantModified: false,
			wantCRObject: &struct {
				C1 *nestedStruct1
			}{
				C1: &nestedStruct1{
					F1: &testStringDesiredField,
					F2: []*string{
						&testStringDesiredField,
					},
				},
			},
		},
		"TestUninitializedCRNestedFields": {
			args: args{
				desiredObject: &struct {
					C1 *nestedStruct1
				}{},
				observedObject: &struct {
					C1 *nestedStruct1
				}{
					C1: &nestedStruct1{
						F1: &testStringObservedField,
						F2: []*string{
							&testStringObservedField,
						},
					},
				},
			},
			wantModified: true,
			wantCRObject: &struct {
				C1 *nestedStruct1
			}{
				C1: &nestedStruct1{
					F1: &testStringObservedField,
					F2: []*string{
						&testStringObservedField,
					},
				},
			},
		},
		"TestNilObservedNestedFields": {
			args: args{
				desiredObject: &struct {
					C1 *nestedStruct10
				}{},
				observedObject: &struct {
					C1 *nestedStruct10
				}{
					C1: &nestedStruct10{
						F1: &testStringDesiredField,
					},
				},
			},
			wantModified: true,
			wantCRObject: &struct {
				C1 *nestedStruct10
			}{
				C1: &nestedStruct10{
					F1: &testStringDesiredField,
				},
			},
		},
		"TestFieldKindMismatch": {
			args: args{
				desiredObject: &nestedStruct1{
					F1: nil,
				},
				observedObject: &nestedStruct2{
					F1: &testInt64ObservedField,
				},
			},
			wantErr: true,
		},
		"TestNestedFieldKindMismatch": {
			args: args{
				desiredObject: &struct {
					C1 *nestedStruct1
				}{
					C1: &nestedStruct1{
						F1: nil,
					},
				},
				observedObject: &struct {
					C1 *nestedStruct2
				}{
					C1: &nestedStruct2{
						F1: &testInt64ObservedField,
					},
				},
			},
			wantErr: true,
		},
		"TestSliceItemKindMismatch": {
			args: args{
				desiredObject: &nestedStruct1{},
				observedObject: &nestedStruct3{
					F1: &testStringObservedField,
					F2: &testStringObservedField,
				},
			},
			wantErr: true,
		},
		"TestInitializedSliceOfStringField": {
			args: args{
				desiredObject: &nestedStruct6{
					F1: []string{
						testStringDesiredField,
					},
				},
				observedObject: &nestedStruct6{
					F1: []string{
						testStringObservedField,
					},
				},
			},
			wantModified: false,
			wantCRObject: &nestedStruct6{
				F1: []string{
					testStringDesiredField,
				},
			},
		},
		"TestUninitializedSliceOfStringField": {
			args: args{
				desiredObject: &nestedStruct6{},
				observedObject: &nestedStruct6{
					F1: []string{
						testStringObservedField,
					},
				},
			},
			wantModified: true,
			wantCRObject: &nestedStruct6{
				F1: []string{
					testStringObservedField,
				},
			},
		},
		"TestInitializedSliceOfSliceField": {
			args: args{
				desiredObject: &nestedStruct5{
					F1: [][]string{
						{
							testStringDesiredField,
						},
					},
				},
				observedObject: &nestedStruct5{
					F1: [][]string{
						{
							testStringObservedField,
						},
					},
				},
			},
			wantModified: false,
			wantCRObject: &nestedStruct5{
				F1: [][]string{
					{
						testStringDesiredField,
					},
				},
			},
		},
		"TestInitializedMapOfStringField": {
			args: args{
				desiredObject: &nestedStruct7{
					F1: map[string]string{
						testKeyDesiredField: testStringDesiredField,
					},
				},
				observedObject: &nestedStruct7{
					F1: map[string]string{
						testKeyObservedField: testStringObservedField,
					},
				},
			},
			wantModified: false,
			wantCRObject: &nestedStruct7{
				F1: map[string]string{
					testKeyDesiredField: testStringDesiredField,
				},
			},
		},
		"TestUninitializedMapOfStringField": {
			args: args{
				desiredObject: &nestedStruct7{},
				observedObject: &nestedStruct7{
					F1: map[string]string{
						testKeyObservedField: testStringObservedField,
					},
				},
			},
			wantModified: true,
			wantCRObject: &nestedStruct7{
				F1: map[string]string{
					testKeyObservedField: testStringObservedField,
				},
			},
		},
		"TestInitializedMapOfPointerStringField": {
			args: args{
				desiredObject: &nestedStruct8{
					F1: map[string]*string{
						testKeyDesiredField: &testStringDesiredField,
					},
				},
				observedObject: &nestedStruct8{
					F1: map[string]*string{
						testKeyObservedField: &testStringObservedField,
					},
				},
			},
			wantModified: false,
			wantCRObject: &nestedStruct8{
				F1: map[string]*string{
					testKeyDesiredField: &testStringDesiredField,
				},
			},
		},
		"TestUninitializedMapOfPointerStringField": {
			args: args{
				desiredObject: &nestedStruct8{},
				observedObject: &nestedStruct8{
					F1: map[string]*string{
						testKeyObservedField: &testStringObservedField,
					},
				},
			},
			wantModified: true,
			wantCRObject: &nestedStruct8{
				F1: map[string]*string{
					testKeyObservedField: &testStringObservedField,
				},
			},
		},
		"TestInitializedMapOfStringSliceField": {
			args: args{
				desiredObject: &nestedStruct9{
					F1: map[string][]string{
						testKeyDesiredField: {testStringDesiredField},
					},
				},
				observedObject: &nestedStruct9{
					F1: map[string][]string{
						testKeyObservedField: {testStringObservedField},
					},
				},
			},
			wantModified: false,
			wantCRObject: &nestedStruct9{
				F1: map[string][]string{
					testKeyDesiredField: {testStringDesiredField},
				},
			},
		},
		"TestUninitializedMapOfStringSliceField": {
			args: args{
				desiredObject: &nestedStruct9{},
				observedObject: &nestedStruct9{
					F1: map[string][]string{
						testKeyObservedField: {testStringObservedField},
					},
				},
			},
			wantModified: true,
			wantCRObject: &nestedStruct9{
				F1: map[string][]string{
					testKeyObservedField: {testStringObservedField},
				},
			},
		},
		"TestInitializeWithZeroValues": {
			args: args{
				desiredObject: &nestedStruct4{},
				observedObject: &nestedStruct4{
					F1: &testStringEmpty,
				},
			},
			wantModified: true,
			wantCRObject: &nestedStruct4{
				F1: &testStringEmpty,
			},
		},
		"TestSkipZeroElem": {
			args: args{
				desiredObject: &nestedStruct4{},
				observedObject: &nestedStruct4{
					F1: &testStringEmpty,
				},
				opts: []GenericLateInitializerOption{WithZeroElemPtrFilter("F1")},
			},
			wantModified: false,
			wantCRObject: &nestedStruct4{},
		},
		"TestSkipOmitemptyTaggedPtrElem": {
			args: args{
				desiredObject: &nestedStruct4{},
				observedObject: &nestedStruct4{
					F1: &testStringEmpty,
				},
				opts: []GenericLateInitializerOption{WithZeroValueJSONOmitEmptyFilter(CNameWildcard)},
			},
			wantModified: false,
			wantCRObject: &nestedStruct4{},
		},
		"TestSkipOmitemptyTaggedSliceElem": {
			args: args{
				desiredObject: &nestedStruct6{},
				observedObject: &nestedStruct6{
					F1: []string{},
				},
				opts: []GenericLateInitializerOption{WithZeroValueJSONOmitEmptyFilter("F1")},
			},
			wantModified: false,
			wantCRObject: &nestedStruct6{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			li := NewGenericLateInitializer(tt.args.opts...)
			got, err := li.LateInitialize(tt.args.desiredObject, tt.args.observedObject)

			if (err != nil) != tt.wantErr {
				t.Errorf("lateInitializeFromResponse() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if tt.wantErr {
				return
			}

			if got != tt.wantModified {
				t.Errorf("lateInitializeFromResponse() got = %v, want %v", got, tt.wantModified)
			}

			if diff := cmp.Diff(tt.wantCRObject, tt.args.desiredObject); diff != "" {
				t.Errorf("lateInitializeFromResponse(...): -want, +got:\n%s", diff)
			}
		})
	}
}
