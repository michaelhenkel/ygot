// Copyright 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ytypes

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kylelemons/godebug/pretty"
	"github.com/michaelhenkel/ygot/ygot"
	"github.com/openconfig/goyang/pkg/yang"
)

// errToString returns the string representation of err and the empty string if
// err is nil.
func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type ContainerStruct struct {
	Leaf1Name        *string                     `path:"config/leaf1|leaf1"`
	Leaf2Name        *string                     `path:"leaf2"`
	BadLeafName      *string                     `path:"bad-leaf"`
	BadEmptyLeafName YANGEmpty                   `path:"bad-leaf-empty"`
	BadEnumLeafName  EnumType                    `path:"bad-leaf-enum"`
	Annotation       *string                     `path:"@annotation" ygotAnnotation:"true"`
	ChildList        map[string]*ContainerStruct `path:"child-list"`
}

func (c *ContainerStruct) IsYANGGoStruct() {}

func TestValidateContainerSchema(t *testing.T) {
	validContainerSchema := &yang.Entry{
		Name: "valid-container-schema",
		Kind: yang.DirectoryEntry,
		Dir: map[string]*yang.Entry{
			"config": {
				Dir: map[string]*yang.Entry{
					"leaf1": {
						Kind: yang.LeafEntry,
						Name: "leaf1",
						Type: &yang.YangType{Kind: yang.Ystring},
					},
				},
			},
			"leaf2": {
				Kind: yang.LeafEntry,
				Name: "leaf2",
				Type: &yang.YangType{Kind: yang.Ystring},
			},
		},
	}
	tests := []struct {
		desc    string
		schema  *yang.Entry
		wantErr bool
	}{
		{
			desc:   "success",
			schema: validContainerSchema,
		},
		{
			desc:   "empty container",
			schema: &yang.Entry{Kind: yang.DirectoryEntry},
		},
		{
			desc:    "nil schema",
			schema:  nil,
			wantErr: true,
		},
		{
			desc:    "nil schema type",
			schema:  &yang.Entry{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := validateContainerSchema(tt.schema)
			if got, want := (err != nil), tt.wantErr; got != want {
				t.Errorf("%s: validateContainerSchema(%v) got error: %v, want error? %v", tt.desc, tt.schema, err, tt.wantErr)
			}
			testErrLog(t, tt.desc, err)
		})
	}
}

type BadStruct struct {
	UnknownName *string `path:"unknown"`
}

func (*BadStruct) IsYANGGoStruct() {}

func TestValidateContainer(t *testing.T) {
	containerSchema := &yang.Entry{
		Name: "container-schema",
		Kind: yang.DirectoryEntry,
		Dir: map[string]*yang.Entry{
			"config": {
				Dir: map[string]*yang.Entry{
					"leaf1": {
						Kind: yang.LeafEntry,
						Name: "leaf1",
						Type: &yang.YangType{Kind: yang.Ystring},
					},
				},
			},
			"leaf2": {
				Kind: yang.LeafEntry,
				Name: "leaf2",
				Type: &yang.YangType{Kind: yang.Ystring},
			},
			"child-list": {
				Name:     "child-list",
				Kind:     yang.DirectoryEntry,
				Key:      "leaf2",
				ListAttr: &yang.ListAttr{},
				Dir: map[string]*yang.Entry{
					"config": {
						Dir: map[string]*yang.Entry{
							"leaf1": {
								Kind: yang.LeafEntry,
								Name: "leaf1",
								Type: &yang.YangType{Kind: yang.Ystring},
							},
						},
					},
					"leaf2": {
						Kind: yang.LeafEntry,
						Name: "leaf2",
						Type: &yang.YangType{Kind: yang.Ystring},
					},
					"bad-leaf": {
						Kind: yang.LeafEntry,
						Name: "bad-leaf",
						Type: &yang.YangType{Kind: yang.Ystring, Pattern: []string{"^a.*"}},
					},
					"bad-leaf-empty": {
						Kind: yang.LeafEntry,
						Name: "bad-leaf-empty",
						Type: &yang.YangType{Kind: yang.Yempty},
					},
					"bad-leaf-enum": {
						Kind: yang.LeafEntry,
						Name: "bad-leaf-enum",
						Type: &yang.YangType{Kind: yang.Yenum},
					},
					// Placeholder due to re-using the ContainerStruct type.
					"child-list": {Name: "child-list"},
				},
			},
		},
	}

	tests := []struct {
		desc    string
		schema  *yang.Entry
		val     interface{}
		wantErr string
	}{
		{
			desc: "success with nil",
			val:  nil,
		},
		{
			desc:   "success",
			schema: containerSchema,
			val: &ContainerStruct{
				Leaf1Name: ygot.String("Leaf1Value"),
				Leaf2Name: ygot.String("Leaf2Value"),
			},
		},
		{
			desc:   "bad field",
			schema: containerSchema,
			val: &ContainerStruct{
				BadLeafName: ygot.String("value"),
			},
			wantErr: `fields [BadLeafName] are not found in the container schema container-schema`,
		},
		{
			desc:   "bad field - #189 - empty",
			schema: containerSchema,
			val: &ContainerStruct{
				BadEmptyLeafName: YANGEmpty(true),
			},
			wantErr: `fields [BadEmptyLeafName] are not found in the container schema container-schema`,
		},
		{
			desc:   "bad field - #189 - enum",
			schema: containerSchema,
			val: &ContainerStruct{
				BadEnumLeafName: EnumType(42),
			},
			wantErr: `fields [BadEnumLeafName] are not found in the container schema container-schema`,
		},
		{
			desc:    "bad value type",
			schema:  containerSchema,
			val:     int(1),
			wantErr: `type int is not a GoStruct for schema container-schema`,
		},
		{
			desc:    "bad schema",
			schema:  nil,
			val:     int(1),
			wantErr: `nil schema for type int, value 1`,
		},
		{
			desc:    "missing key",
			schema:  containerSchema,
			val:     &BadStruct{UnknownName: ygot.String("Unknown")},
			wantErr: `fields [UnknownName] are not found in the container schema container-schema`,
		},
		{
			desc:   "child list",
			schema: containerSchema,
			val: &ContainerStruct{
				ChildList: map[string]*ContainerStruct{
					"Child1": {
						Leaf2Name:   ygot.String("Child1"),
						BadLeafName: ygot.String("fish"),
					},
					"Child2": {
						Leaf2Name:   ygot.String("Child2"),
						BadLeafName: ygot.String("fish"),
					},
				},
			},
			// Should just get one error back with the error, not two.
			wantErr: `/child-list: "fish" does not match regular expression pattern "^a.*$" for schema bad-leaf`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			errs := Validate(tt.schema, tt.val)
			if got, want := errs.String(), tt.wantErr; got != want {
				t.Errorf("%s: got error: %v, want error: %v", tt.desc, got, want)
			}
			testErrLog(t, tt.desc, errs)
		})
	}

	// Additional tests through private API.
	if err := validateContainer(nil, nil); err != nil {
		t.Errorf("nil value: got error: %v, want error: nil", err)
	}
	if err := validateContainer(nil, &ContainerStruct{}); err == nil {
		t.Errorf("nil schema: got error: nil, want nil schema error")
	}
}

func TestUnmarshalContainer(t *testing.T) {
	innerContainerSchema := &yang.Entry{
		Name: "container-field",
		Kind: yang.DirectoryEntry,
		Dir: map[string]*yang.Entry{
			"config": {
				Name: "config",
				Kind: yang.DirectoryEntry,
				Dir: map[string]*yang.Entry{
					"leaf1-field": {
						Kind: yang.LeafEntry,
						Name: "leaf1-field",
						Type: &yang.YangType{Kind: yang.Yint32},
					},
				},
			},
			"state": {
				Name: "state",
				Kind: yang.DirectoryEntry,
				Dir: map[string]*yang.Entry{
					"leaf1-field": {
						Kind: yang.LeafEntry,
						Name: "leaf1-field",
						Type: &yang.YangType{Kind: yang.Yint32},
					},
				},
			},
			"leaf2-field": {
				Kind: yang.LeafEntry,
				Name: "leaf2-field",
				Type: &yang.YangType{Kind: yang.Yint32},
			},
		},
	}
	containerSchema := &yang.Entry{
		Name: "parent-field",
		Kind: yang.DirectoryEntry,
		Dir: map[string]*yang.Entry{
			"container-field": innerContainerSchema,
		},
	}

	populateParentField(nil, containerSchema)

	type ContainerStruct struct {
		ConfigLeaf1Field *int32 `path:"config/leaf1-field"`
		StateLeaf1Field  *int32 `path:"state/leaf1-field"`
		Leaf2Field       *int32 `path:"leaf2-field"`
	}

	type ParentContainerStruct struct {
		ContainerField *ContainerStruct `path:"container-field"`
	}

	tests := []struct {
		desc    string
		schema  *yang.Entry
		json    string
		opts    []UnmarshalOpt
		want    interface{}
		wantErr string
	}{
		{
			desc:   "success nil value",
			schema: containerSchema,
			json:   ``,
			want:   &ParentContainerStruct{},
		},
		{
			desc:   "success",
			schema: containerSchema,
			json:   `{ "container-field": { "leaf2-field": 43, "config": { "leaf1-field": 41 } , "state": { "leaf1-field": 42 } } }`,
			want:   &ParentContainerStruct{ContainerField: &ContainerStruct{ConfigLeaf1Field: ygot.Int32(41), StateLeaf1Field: ygot.Int32(42), Leaf2Field: ygot.Int32(43)}},
		},
		{
			desc:    "nil schema",
			schema:  nil,
			json:    `{}`,
			wantErr: `nil schema for parent type *ytypes.ParentContainerStruct, value map[] (map[string]interface {})`,
		},
		{
			desc:    "bad field name",
			schema:  containerSchema,
			json:    `{ "container-field": { "bad-field": 42 } }`,
			wantErr: `parent container container-field (type *ytypes.ContainerStruct): JSON contains unexpected field bad-field`,
		},
		{
			desc:    "bad field type",
			schema:  containerSchema,
			json:    `{ "container-field": { "leaf2-field":  "forty-two"} }`,
			wantErr: `got string type for field leaf2-field, expect float64`,
		},
		{
			desc:   "unknown field name with ignore",
			schema: containerSchema,
			json:   `{"container-field": {"aug-field": 43 } }`,
			opts:   []UnmarshalOpt{&IgnoreExtraFields{}},
			want:   &ParentContainerStruct{ContainerField: &ContainerStruct{}},
		},
	}

	var jsonTree interface{}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			var parent ParentContainerStruct

			if tt.json != "" {
				if err := json.Unmarshal([]byte(tt.json), &jsonTree); err != nil {
					t.Fatal(fmt.Sprintf("json unmarshal (%s) : %s", tt.desc, err))
				}
			}

			err := Unmarshal(tt.schema, &parent, jsonTree, tt.opts...)
			if got, want := errToString(err), tt.wantErr; got != want {
				t.Errorf("%s: got error: %v, want error: %v", tt.desc, got, want)
			}
			testErrLog(t, tt.desc, err)
			if err == nil {
				if got, want := &parent, tt.want; !areEqual(got, want) {
					t.Errorf("%s: got:\n%v\nwant:\n%v\n", tt.desc, pretty.Sprint(got), pretty.Sprint(want))
				}
			}
		})
	}

	var parent ParentContainerStruct
	badJSONTree := []interface{}{}

	wantErrStr := `unmarshalContainer for schema parent-field: jsonTree [  ]: got type []interface {} inside container, expect map[string]interface{}`
	if got, want := errToString(Unmarshal(containerSchema, &parent, badJSONTree)), wantErrStr; got != want {
		t.Errorf("Unmarshal container with bad json : got error: %s, want error: %s", got, want)
	}

	// Additional tests through private API.
	if err := unmarshalContainer(nil, nil, nil, JSONEncoding); err != nil {
		t.Errorf("nil value: got error: %v, want error: nil", err)
	}
	if err := unmarshalContainer(nil, nil, map[string]interface{}{}, JSONEncoding); err == nil {
		t.Errorf("nil schema: got error: nil, want nil schema error")
	}
}
