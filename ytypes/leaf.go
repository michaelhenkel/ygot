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
	"encoding/base64"
	"fmt"
	"math/big"
	"reflect"
	"strconv"

	log "github.com/golang/glog"
	"github.com/michaelhenkel/ygot/util"
	"github.com/michaelhenkel/ygot/ygot"
	"github.com/openconfig/goyang/pkg/yang"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
)

// Refer to: https://tools.ietf.org/html/rfc6020#section-7.6.

// validateLeaf validates the value of a leaf struct against the given schema.
// This value is expected to be a Go basic type corresponding to the leaf
// schema type.
func validateLeaf(inSchema *yang.Entry, value interface{}) util.Errors {
	// TODO(mostrowski): "mandatory" not implemented.
	if util.IsValueNil(value) {
		return nil
	}

	// Check that the schema itself is valid.
	if err := validateLeafSchema(inSchema); err != nil {
		return util.NewErrs(err)
	}

	util.DbgPrint("validateLeaf with value %s (%T), schema name %s (%s)", util.ValueStrDebug(value), value, inSchema.Name, inSchema.Type.Kind)

	schema, err := util.ResolveIfLeafRef(inSchema)
	if err != nil {
		return util.NewErrs(err)
	}

	var rv interface{}
	ykind := schema.Type.Kind
	rkind := reflect.ValueOf(value).Kind()
	switch rkind {
	case reflect.Ptr:
		rv = reflect.ValueOf(value).Elem().Interface()
	case reflect.Slice:
		if ykind != yang.Ybinary {
			return util.NewErrs(fmt.Errorf("bad leaf type: expect []byte for binary value %v for schema %s, have type %v", value, schema.Name, ykind))
		}
	case reflect.Int64:
		if ykind != yang.Yenum && ykind != yang.Yidentityref && ykind != yang.Yunion {
			return util.NewErrs(fmt.Errorf("bad leaf type: expect Int64 for enum type for schema %s, have type %v", schema.Name, ykind))
		}
	case reflect.Bool:
		if ykind != yang.Yempty {
			return util.NewErrs(fmt.Errorf("bad leaf type: expect Bool for empty type for schema %s, have type %v", schema.Name, ykind))
		}
		rv = value
	default:
		return util.NewErrs(fmt.Errorf("bad leaf value type %v, expect Ptr or Int64 for schema %s", rkind, schema.Name))
	}

	switch ykind {
	case yang.Ybinary:
		return util.NewErrs(validateBinary(schema, value))
	case yang.Ybits:
		return nil
		// TODO(mostrowski): restore when representation is decided.
		//return util.NewErrs(validateBitset(schema, rv))
	case yang.Ybool:
		return util.NewErrs(validateBool(schema, rv))
	case yang.Yempty:
		return util.NewErrs(validateEmpty(schema, rv))
	case yang.Ystring:
		return util.NewErrs(validateString(schema, rv))
	case yang.Ydecimal64:
		return util.NewErrs(validateDecimal(schema, rv))
	case yang.Yenum, yang.Yidentityref:
		if rkind != reflect.Int64 && !isValueInterfacePtrToEnum(reflect.ValueOf(value)) {
			return util.NewErrs(fmt.Errorf("bad leaf value type %v, expect Int64 for schema %s, type %v", rkind, schema.Name, ykind))
		}
		return nil
	case yang.Yunion:
		return validateUnion(schema, value)
	}
	if isIntegerType(ykind) {
		return util.NewErrs(validateInt(schema, rv))
	}
	return util.NewErrs(fmt.Errorf("unknown leaf type %v for schema %s", ykind, schema.Name))
}

/*
 validateUnion validates a union type and returns any validation errors.
 Unions have two types of possible representation in the data tree, which
 depends on the schema. The first case has alternatives with the same Go type,
 but different YANG types (possibly with different constraints):

 Name:        "address",
 Kind:        yang.Yleaf,
 Dir:         {},
 Type:        {
   Name:             "ip-address",
   Kind:             yang.Yunion,
   Type:             [
   {
           Name:             "ipv4-address",
           Kind:             yang.Ystring,
           Pattern:          [...pattern...],
   },
   {
           Name:             "ipv6-address",
           Kind:             yang.Ystring,
           Pattern:          [...pattern...],
           Type:             [],
   }]
 }

 In this case, the data tree will look like this:

 type System_Ntp_Server struct {
   Address *string `path:"address"`
 }

 The validation will check against all the schema nodes that match the YANG
 type corresponding to the Go type and return an error if none match.

 In the second case, where multiple Go types are present, the data tree has an
 additional struct layer. In this case, the struct field is compared against
 all YANG schemas that match the Go type of the selected wrapping struct e.g.

 Name:        "port",
 Kind:        yang.Yleafref,
 Dir:         {},
 Type:        {
   Name:             "port",
   Kind:             yang.Yunion,
   Type:             [
   {
           Name:             "port-string",
           Kind:             yang.Ystring,
           Pattern:          [...pattern...],
   },
   {
           Name:             "port-integer",
           Kind:             yang.Yuint16,
           Type:             [],
   }]
 }

 -- Corresponding structs data tree --

 type System_Ntp_Server struct {
   Port Port `path:"port"`
 }

 type Port interface {
   IsPort()
 }

 type Port_String struct {
   PortString *string
 }

 func (Port_String a) IsPort() {}

 type Port_Integer struct {
   PortInteger *uint16
 }

 func (Port_Integer a) IsPort() {}

 In this case, the appropriate schema is uniquely selected based on the struct
 path.

 A union may be nested. e.g. (shown as YANG schema for brevity)

 leaf foo {
   type union {
     type derived_string_type1;
     type union {
       type derived_string_type2;
       type derived_string_type3;
     }
   }
 }

 The data tree will look like this:

 type SomeContainer struct {
   Foo *string `path:"foo"`
 }

 In this case, the value for Foo would be recursively evaluated against any
 of the matching types in any contained unions.
 validateUnion supports any combination of nested union types and multiple
 choices with the same type that are not represented by a named wrapper struct.
*/
func validateUnion(schema *yang.Entry, value interface{}) util.Errors {
	if util.IsValueNil(value) {
		return nil
	}

	util.DbgPrint("validateUnion %s", schema.Name)
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Ptr:
		// The union is usually a ptr - either a struct ptr or Go value ptr like *string.
		// Enum types are also represented as a struct for union where the field
		// has the enum type.
		v = v.Elem()
	case reflect.Int64:
		// A union containing a single enumerated type would simply resolve to the
		// enum type, which represented directly by a derived Int64 type.
	default:
		return util.NewErrs(fmt.Errorf("wrong value type for union %s: got: %T, expect ptr or Int64 (enumerated type)", schema.Name, value))
	}

	// Unions of enum types are passed as ptr to interface to struct ptr.
	// Normalize to a union struct.
	// TODO(wenbli): Remove this if statement in a future PR.
	// v here is already a struct, as multi-type unions are represented as
	// interfaces within their parents' structs, *not* ptrs to interfaces.
	if util.IsValueInterface(v) {
		v = v.Elem()
		if util.IsValuePtr(v) {
			v = v.Elem()
		}
	}

	if v.Type().Kind() == reflect.Struct {
		if v.NumField() != 1 {
			return util.NewErrs(fmt.Errorf("union %s should only have one field, but has %d", schema.Name, v.NumField()))
		}
		return validateMatchingSchemas(schema, v.Field(0).Interface())
	}

	return validateMatchingSchemas(schema, value)
}

// validateMatchingSchemas validates against all schemas within the Type slice
// that match the type of passed in value. It returns nil if value is
// successfully validated against any matching schema, or a list of errors found
// during validation against each matching schema otherwise.
func validateMatchingSchemas(schema *yang.Entry, value interface{}) util.Errors {
	var errors []error
	ss := findMatchingSchemasInUnion(schema.Type, value)
	var kk []yang.TypeKind
	for _, s := range ss {
		kk = append(kk, s.Type.Kind)
	}
	util.DbgPrint("validateMatchingSchemas for value %v (%T) for schema %s with types %v", value, value, schema.Name, kk)
	if len(ss) == 0 {
		return util.NewErrs(fmt.Errorf("no types in schema %s match the type of value %v, which is %T", schema.Name, util.ValueStr(value), value))
	}
	for _, s := range ss {
		var errs []error
		if reflect.ValueOf(value).Kind() == reflect.Ptr {
			errs = validateLeaf(s, value)
		} else {
			// Unions with wrapping structs use non-ptr fields so here we need
			// to take the address of value to pass to validateLeaf, which
			// expects a ptr field.
			errs = validateLeaf(s, &value)
		}
		if errs == nil {
			return nil
		}
		errors = util.AppendErrs(errors, errs)
	}

	return errors
}

// findMatchingSchemasInUnion returns all schemas in the given union type,
// including those within nested unions, that match the Go type of value.
// value must not be nil.
func findMatchingSchemasInUnion(ytype *yang.YangType, value interface{}) []*yang.Entry {
	var matches []*yang.Entry

	util.DbgPrint("findMatchingSchemasInUnion for type %T, kind %s", value, reflect.TypeOf(value).Kind())
	for _, t := range ytype.Type {
		if t.Kind == yang.Yunion {
			// Recursively check all union types within this union.
			matches = append(matches, findMatchingSchemasInUnion(t, value)...)
			continue
		}

		ybt := yangBuiltinTypeToGoType(t.Kind)
		if reflect.ValueOf(value).Kind() == reflect.Ptr {
			ybt = ygot.ToPtr(yangBuiltinTypeToGoType(t.Kind))
		}
		if ybt == nil {
			log.Warningf("no matching Go type for type %v in union value %s", t.Kind, util.ValueStr(value))
			continue
		}
		if reflect.TypeOf(ybt).Kind() == reflect.TypeOf(value).Kind() {
			matches = append(matches, yangTypeToLeafEntry(t))
		}
	}

	return matches
}

// validateLeafSchema validates the given leaf type schema. This is a sanity
// check validation rather than a comprehensive validation against the RFC.
// It is assumed that such a validation is done when the schema is parsed from
// source YANG.
func validateLeafSchema(schema *yang.Entry) error {
	if schema == nil {
		return fmt.Errorf("leaf schema is nil")
	}
	if schema.Type == nil {
		return fmt.Errorf("leaf schema type is nil for schema %s", schema.Name)
	}
	if schema.Kind != yang.LeafEntry {
		return fmt.Errorf("case schema has wrong type %v for schema %s", schema.Kind, schema.Name)
	}
	return nil
}

// YANGEmpty is a derived type which is used to represent the YANG empty type.
type YANGEmpty bool

// Binary is a derived type which is used to represent the YANG binary type.
type Binary []byte

// unmarshalLeaf unmarshals a scalar value (determined by json.Unmarshal) into
// the parent containing the leaf.
//   schema points to the schema for the leaf type.
func unmarshalLeaf(inSchema *yang.Entry, parent interface{}, value interface{}, enc Encoding) error {
	if util.IsValueNil(value) {
		if enc == JSONEncoding {
			return nil
		}
		return fmt.Errorf("unmarshalLeaf: invalid nil value to unmarshal")
	}

	var err error
	if err := validateLeafSchema(inSchema); err != nil {
		return err
	}

	util.DbgPrint("unmarshalLeaf value %v, type %T, into parent type %T, schema name %s", util.ValueStrDebug(value), value, parent, inSchema.Name)

	fieldName, _, err := schemaToStructFieldName(inSchema, parent)
	if err != nil {
		return err
	}

	schema, err := util.ResolveIfLeafRef(inSchema)
	if err != nil {
		return err
	}

	ykind := schema.Type.Kind

	if ykind == yang.Yunion {
		return unmarshalUnion(schema, parent, fieldName, value, enc)
	}

	if ykind == yang.Ybits {
		// TODO(mostrowski)
		return nil
	}

	v, err := unmarshalScalar(parent, schema, fieldName, value, enc)
	if err != nil {
		return err
	}
	if ykind == yang.Ybinary {
		// Binary is a slice field which is treated as a scalar.
		return util.InsertIntoStruct(parent, fieldName, v)
	}

	if ykind == yang.Yempty {
		// Empty is a derived type of bool which is treated as a scalar. We
		// insert it here to avoid strict type checking against the generated
		// code.
		return util.UpdateField(parent, fieldName, v)
	}

	return util.UpdateField(parent, fieldName, v)
}

// unmarshalUnion unmarshals a union schema type with the given value into
// parent.
/*
for example, with structs schema:

type Bgp_Neighbor_RouteReflector struct {
	RouteReflectorClient    *bool                                                     `path:"config/route-reflector-client" module:"openconfig-bgp"`
	RouteReflectorClusterId Bgp_Neighbor_RouteReflector_RouteReflectorClusterId_Union `path:"config/route-reflector-cluster-id" module:"openconfig-bgp"`
}
type Bgp_Neighbor_RouteReflector_RouteReflectorClusterId_Union interface {
	Is_Bgp_Neighbor_RouteReflector_RouteReflectorClusterId_Union()
}
type Bgp_Neighbor_RouteReflector_RouteReflectorClusterId_Union_String struct {
	String string
}
func (t *Bgp_Neighbor_RouteReflector) To_Bgp_Neighbor_RouteReflector_RouteReflectorClusterId_Union(i interface{}) (Bgp_Neighbor_RouteReflector_RouteReflectorClusterId_Union, error) {

and input JSON:

{"config/route-reflector-cluster-id": "forty-two"}

the resulting Bgp_Neighbor_RouteReflector would have field
RouteReflectorClusterId set with the type Bgp_Neighbor_RouteReflector_RouteReflectorClusterId_Union_String,
with field String set to "forty-two".
*/

func unmarshalUnion(schema *yang.Entry, parent interface{}, fieldName string, value interface{}, enc Encoding) error {
	util.DbgPrint("unmarshalUnion value %v, type %T, into parent type %T field name %s, schema name %s", util.ValueStrDebug(value), value, parent, fieldName, schema.Name)
	parentV, parentT := reflect.ValueOf(parent), reflect.TypeOf(parent)
	if !util.IsTypeStructPtr(parentT) {
		return fmt.Errorf("%T is not a struct ptr in unmarshalUnion", parent)
	}

	// Get the value and type of the field to set, which may have slice or
	// interface types for leaf-list and union cases.
	destUnionFieldV := parentV.Elem().FieldByName(fieldName)
	if !destUnionFieldV.IsValid() {
		return fmt.Errorf("%s is not a valid field name in %T", fieldName, parent)
	}
	dft, _ := parentT.Elem().FieldByName(fieldName)
	destUnionFieldElemT := dft.Type

	// Possible enum types, as []reflect.Type
	ets, err := schemaToEnumTypes(schema, parentT)
	if err != nil {
		return err
	}
	// Possible YANG scalar types, as []yang.TypeKind. This discards any
	// yang.Type restrictions, since these are expected to be checked during
	// verification after unmarshal.
	sks, err := getUnionKindsNotEnums(schema)
	if err != nil {
		return err
	}

	util.DbgPrint("possible union types are enums %v or scalars %v", ets, sks)

	// Special case. If all possible union types map to a single go type, the
	// GoStruct field is that type rather than a union Interface type.
	if !util.IsTypeInterface(destUnionFieldElemT) && !util.IsTypeSliceOfInterface(destUnionFieldElemT) {
		// Is not an interface, we must have exactly one type in the union.
		var yk yang.TypeKind
		var isEnum bool
		switch {
		// That one type is either an enum or not an enum.
		case len(sks) == 1 && len(ets) == 0:
			yk = sks[0]
		case len(sks) == 0 && len(ets) == 1:
			yk = schema.Type.Type[0].Kind
			isEnum = true
		default:
			return fmt.Errorf("got %v non-enum types and %v enum types for union schema %s for type %T, expect just one type in total", sks, ets, fieldName, parent)
		}

		goValue, err := unmarshalScalar(parent, yangKindToLeafEntry(yk), fieldName, value, enc)
		if err != nil {
			return fmt.Errorf("could not unmarshal %v into type %s", value, yk)
		}

		if !util.IsTypeSlice(destUnionFieldElemT) {
			if isEnum {
				destUnionFieldV.Set(reflect.ValueOf(goValue))
			} else {
				destUnionFieldV.Set(reflect.ValueOf(ygot.ToPtr(goValue)))
			}
			return nil
		}

		// Handle the case whereby the single-type union is actually a leaf-list,
		// such that the representation in the struct is a slice, rather than a
		// scalar.
		sl := reflect.MakeSlice(destUnionFieldElemT, 0, 0)
		if !destUnionFieldV.IsNil() {
			// Ensure that we handle the case where there is an existing slice.
			sl = destUnionFieldV
		}
		destUnionFieldV.Set(reflect.Append(sl, reflect.ValueOf(goValue)))
		return nil
	}

	// For each possible union type, try to unmarshal the value. If it can be
	// unmarshaled, try to resolve the resulting type into a union struct type.
	// Note that values can resolve into more than one struct type depending on
	// the value and its range. In this case, no attempt is made to find the
	// most restrictive type.
	// Try to unmarshal to enum types first, since the case of union of string
	// and enum could unmarshal into either. Only string values can be enum
	// types.
	var valueStr string
	var ok bool
	switch enc {
	case GNMIEncoding, gNMIEncodingWithJSONTolerance:
		var sv *gpb.TypedValue_StringVal
		if sv, ok = value.(*gpb.TypedValue).GetValue().(*gpb.TypedValue_StringVal); ok {
			valueStr = sv.StringVal
		}
	case JSONEncoding:
		valueStr, ok = value.(string)
	default:
		return fmt.Errorf("unknown encoding %v", enc)
	}

	if ok {
		for _, et := range ets {
			util.DbgPrint("try to unmarshal into enum type %s", et)
			ev, err := castToEnumValue(et, valueStr)
			if err != nil {
				return err
			}
			if ev != nil {
				return setFieldWithTypedValue(parentT, destUnionFieldV, destUnionFieldElemT, ev)
			}
			util.DbgPrint("could not unmarshal %v into enum type: %s", value, err)
		}
	}

	for _, sk := range sks {
		util.DbgPrint("try to unmarshal into type %s", sk)
		sch := yangKindToLeafEntry(sk)
		gv, err := unmarshalScalar(parent, sch, fieldName, value, enc)
		if err == nil {
			return setFieldWithTypedValue(parentT, destUnionFieldV, destUnionFieldElemT, gv)
		}
		util.DbgPrint("could not unmarshal %v into type %s: %s", value, sk, err)
	}

	return fmt.Errorf("could not find suitable union type to unmarshal value %v type %T into parent struct type %T field %s", value, value, parent, fieldName)
}

// setFieldWithTypedValue sets the field destV that has type ft and the given
// parent type with v, which must be a compatible enum type.
func setFieldWithTypedValue(parentT reflect.Type, destV reflect.Value, destElemT reflect.Type, v interface{}) error {
	util.DbgPrint("setFieldWithTypedValue value %v into type %s", util.ValueStrDebug(v), destElemT)
	if destElemT.Kind() == reflect.Slice {
		// leaf-list case
		destElemT = destElemT.Elem()
	}
	mn := "To_" + destElemT.Name()
	mapMethod := reflect.New(parentT).Elem().MethodByName(mn)
	if !mapMethod.IsValid() {
		return fmt.Errorf("%s does not have a %s function", destElemT.Name(), mn)
	}
	ec := mapMethod.Call([]reflect.Value{reflect.ValueOf(v)})
	if len(ec) != 2 {
		return fmt.Errorf("%s %s function returns %d params", destElemT.Name(), mn, len(ec))
	}
	ei := ec[0].Interface()
	ee := ec[1].Interface()
	if ee != nil {
		return fmt.Errorf("unmarshaled %v type %T does not have a union type: %v", v, v, ee)
	}

	util.DbgPrint("unmarshaling %v into type %s", v, reflect.TypeOf(ei))

	eiv := reflect.ValueOf(ei)
	if destV.Type().Kind() == reflect.Slice {
		destV.Set(reflect.Append(destV, eiv))
	} else {
		destV.Set(eiv)
	}

	return nil
}

// getUnionKindsNotEnums returns all the YANG kinds under the given schema node,
// dereferencing any refs.
func getUnionKindsNotEnums(schema *yang.Entry) ([]yang.TypeKind, error) {
	var uks []yang.TypeKind
	m := make(map[yang.TypeKind]interface{})
	uts, err := getUnionTypesNotEnums(schema, schema.Type)
	if err != nil {
		return nil, err
	}
	for _, yt := range uts {
		m[yt.Kind] = nil
	}
	for k := range m {
		uks = append(uks, k)
	}
	return uks, nil
}

// getUnionTypesNotEnums returns all the non-enum YANG types under the given
// schema node, dereferencing any refs.
func getUnionTypesNotEnums(schema *yang.Entry, yt *yang.YangType) ([]*yang.YangType, error) {
	var uts []*yang.YangType
	switch yt.Kind {
	case yang.Yenum, yang.Yidentityref:
		// Enum types handled separately.
		return nil, nil
	case yang.Yleafref:
		ns, err := util.FindLeafRefSchema(schema, yt.Path)
		if err != nil {
			return nil, err
		}
		return getUnionTypesNotEnums(ns, ns.Type)
	case yang.Yunion:
		for _, t := range yt.Type {
			nt, err := getUnionTypesNotEnums(schema, t)
			if err != nil {
				return nil, err
			}
			uts = append(uts, nt...)
			if err != nil {
				return nil, err
			}
		}
	default:
		uts = []*yang.YangType{yt}
	}

	return uts, nil
}

// schemaToEnumTypes returns the actual enum types (rather than the interface
// type) for a given schema, which must be for an enum type. t is the type of
// the containing parent struct.
func schemaToEnumTypes(schema *yang.Entry, t reflect.Type) ([]reflect.Type, error) {
	enumTypesMethod := reflect.New(t).Elem().MethodByName("ΛEnumTypeMap")
	if !enumTypesMethod.IsValid() {
		return nil, fmt.Errorf("type %s does not have a ΛEnumTypesMap function", t)
	}

	ec := enumTypesMethod.Call(nil)
	if len(ec) == 0 {
		return nil, fmt.Errorf("%s ΛEnumTypes function returns empty value", t)
	}
	ei := ec[0].Interface()
	enumTypesMap, ok := ei.(map[string][]reflect.Type)
	if !ok {
		return nil, fmt.Errorf("%s ΛEnumTypes function returned wrong type %T, want map[string][]reflect.Type", t, ei)
	}

	util.DbgPrint("path is %s for schema %s", absoluteSchemaDataPath(schema), schema.Name)

	return enumTypesMap[absoluteSchemaDataPath(schema)], nil
}

// unmarshalScalar unmarshals value, which is the Go type from json.Unmarshal,
// to the corresponding value used in gostructs.
//   parent is the parent struct containing the field being unmarshaled.
//     Required if the unmarshaled type is an enum.
//   fieldName is the name of the field being unmarshaled.
//     Required if the unmarshaled type is an enum.
func unmarshalScalar(parent interface{}, schema *yang.Entry, fieldName string, value interface{}, enc Encoding) (interface{}, error) {
	if util.IsValueNil(value) {
		if enc == JSONEncoding {
			return nil, nil
		}
		return nil, fmt.Errorf("unmarshalScalar: invalid nil value to unmarshal")
	}

	if err := validateLeafSchema(schema); err != nil {
		return nil, err
	}

	util.DbgPrint("unmarshalScalar value %v, type %T, into parent type %T field %s", value, value, parent, fieldName)

	switch enc {
	case JSONEncoding:
		return sanitizeJSON(parent, schema, fieldName, value)
	case GNMIEncoding, gNMIEncodingWithJSONTolerance:
		tv, ok := value.(*gpb.TypedValue)
		if !ok {
			return nil, fmt.Errorf("got %T type, want gNMI TypedValue as value type", value)
		}
		return sanitizeGNMI(parent, schema, fieldName, tv, enc == gNMIEncodingWithJSONTolerance)
	}

	return nil, fmt.Errorf("unknown encoding mode; %v", enc)
}

// sanitizeJSON decodes the JSON encoded value into the type of corresponding
// field in GoStruct. Parent is the parent struct containing the field being
// unmarshaled. schema is *yang.Entry corresponding to the field. fieldName
// is the name of the field being written in GoStruct. value is the JSON
// encoded value.
func sanitizeJSON(parent interface{}, schema *yang.Entry, fieldName string, value interface{}) (interface{}, error) {
	ykind := schema.Type.Kind

	if ykind != yang.Yunion && reflect.ValueOf(value).Type() != yangToJSONType(ykind) {
		return nil, fmt.Errorf("got %T type for field %s, expect %v", value, schema.Name, yangToJSONType(ykind).Kind())
	}

	switch ykind {
	case yang.Ybinary:
		v, err := base64.StdEncoding.DecodeString(value.(string))
		if err != nil {
			return nil, fmt.Errorf("error in DecodeString for \n%v\n for schema %s: %v", value, schema.Name, err)
		}
		return []byte(v), nil

	case yang.Yempty:
		// If an empty leaf is included in the JSON, then we expect it to have a value of [null]. If it does not
		// this is an error.
		v, ok := value.([]interface{})
		if !ok || len(v) != 1 || v[0] != nil {
			return nil, fmt.Errorf("error parsing %v for schema %s: empty leaves must be [null]", value, schema.Name)
		}
		return true, nil

	case yang.Ybits:
		// TODO(mostrowski)
		return nil, nil

	case yang.Ybool:
		return value.(bool), nil

	case yang.Ystring:
		return value.(string), nil

	case yang.Ydecimal64:
		floatV, err := strconv.ParseFloat(value.(string), 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing %v for schema %s: %v", value, schema.Name, err)
		}

		return floatV, nil

	case yang.Yenum, yang.Yidentityref:
		return enumStringToValue(parent, fieldName, value.(string))

	case yang.Yint64:
		// TODO(b/64812268): value types are different for internal style JSON.
		intV, err := strconv.ParseInt(value.(string), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing %v for schema %s: %v", value, schema.Name, err)
		}
		return intV, nil

	case yang.Yuint64:
		uintV, err := strconv.ParseUint(value.(string), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing %v for schema %s: %v", value, schema.Name, err)
		}
		return uintV, nil

	case yang.Yint8, yang.Yint16, yang.Yint32:
		pv, err := yangFloatIntToGoType(ykind, value.(float64))
		if err != nil {
			return nil, fmt.Errorf("error parsing %v for schema %s: %v", value, schema.Name, err)
		}
		return pv, nil

	case yang.Yuint8, yang.Yuint16, yang.Yuint32:
		pv, err := yangFloatIntToGoType(ykind, value.(float64))
		if err != nil {
			return nil, fmt.Errorf("error parsing %v for schema %s: %v", value, schema.Name, err)
		}
		return pv, nil

	case yang.Yunion:
		return value, nil

	}

	return nil, fmt.Errorf("unmarshalScalar: unsupported type %v in schema node %s", ykind, schema.Name)
}

// sanitizeGNMI decodes the GNMI TypedValue encoded value into a field of the
// corresponding type in GoStruct. Parent is the parent struct containing the
// field being unmarshaled. schema is *yang.Entry corresponding to the field.
// fieldName is the name of the field being written in GoStruct. tv is the
// JSON encoded value. jsonTolerance means to allow some otherwise nonmatching
// types to match due to inconsistencies after json translation; for now, this
// just involves accepting positive ints as uints.
func sanitizeGNMI(parent interface{}, schema *yang.Entry, fieldName string, tv *gpb.TypedValue, jsonTolerance bool) (interface{}, error) {
	ykind := schema.Type.Kind

	var ok bool
	if ok = gNMIToYANGTypeMatches(ykind, tv, jsonTolerance); !ok {
		return nil, fmt.Errorf("failed to unmarshal %v into %v", tv.GetValue(), yang.TypeKindToName[ykind])
	}

	switch ykind {
	case yang.Ybool:
		return tv.GetBoolVal(), nil
	case yang.Ystring:
		return tv.GetStringVal(), nil
	case yang.Yenum, yang.Yidentityref:
		return enumStringToValue(parent, fieldName, tv.GetStringVal())
	case yang.Yint8, yang.Yint16, yang.Yint32, yang.Yint64:
		gt := reflect.TypeOf(yangBuiltinTypeToGoType(ykind))
		vs := fmt.Sprintf("%v", tv.GetIntVal())
		rv, err := StringToType(gt, vs)
		if err != nil {
			return nil, fmt.Errorf("StringToType(%q, %v) failed; %v", vs, gt, err)
		}
		return rv.Interface(), nil
	case yang.Yuint8, yang.Yuint16, yang.Yuint32, yang.Yuint64:
		gt := reflect.TypeOf(yangBuiltinTypeToGoType(ykind))
		vs := fmt.Sprintf("%v", tv.GetUintVal())
		rv, err := StringToType(gt, vs)
		if err != nil {
			return nil, fmt.Errorf("StringToType(%q, %v) failed; %v", vs, gt, err)
		}
		return rv.Interface(), nil
	case yang.Ybinary:
		bytes := tv.GetBytesVal()
		if bytes == nil {
			return nil, fmt.Errorf("received BytesVal is nil -- this is invalid")
		}
		return bytes, nil
	case yang.Ydecimal64:
		switch v := tv.GetValue().(type) {
		case *gpb.TypedValue_DecimalVal:
			if v.DecimalVal == nil {
				return nil, fmt.Errorf("received DecimalVal is nil -- this is invalid")
			}
			prec := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(v.DecimalVal.Precision)), nil)
			// Second return value indicates whether returned float64 value exactly
			// represents the division. We don't want to fail unmarshalling as float64
			// is the best type in ygot that can represent a decimal64. So, second
			// return value is just ignored.
			fv, _ := new(big.Rat).SetFrac(big.NewInt(v.DecimalVal.Digits), prec).Float64()
			return fv, nil
		case *gpb.TypedValue_FloatVal:
			return float64(v.FloatVal), nil
		}
	}
	return nil, fmt.Errorf("%v type isn't expected for GNMIEncoding", yang.TypeKindToName[ykind])
}

// gNMIToYANGTypeMatches checks whether the provided yang.TypeKind can be set
// by using the provided gNMI TypedValue, and returns the TypedValue that
// should be used to get the underlying value. gNMI TypedValue oneof fields can
// carry more than one sizes of the same type per gNMI specification:
// https://github.com/openconfig/reference/blob/master/rpc/gnmi/gnmi-specification.md#223-node-values
// jsonTolerance means to allow some otherwise nonmatching types to match due
// to inconsistencies after json translation; for now, this just involves
// accepting positive ints as uints.
func gNMIToYANGTypeMatches(ykind yang.TypeKind, tv *gpb.TypedValue, jsonTolerance bool) bool {
	var ok bool
	switch ykind {
	case yang.Ybool:
		_, ok = tv.GetValue().(*gpb.TypedValue_BoolVal)
	case yang.Ystring, yang.Yenum, yang.Yidentityref:
		_, ok = tv.GetValue().(*gpb.TypedValue_StringVal)
	case yang.Yint8, yang.Yint16, yang.Yint32, yang.Yint64:
		_, ok = tv.GetValue().(*gpb.TypedValue_IntVal)
	case yang.Yuint8, yang.Yuint16, yang.Yuint32, yang.Yuint64:
		_, ok = tv.GetValue().(*gpb.TypedValue_UintVal)
		if !ok && jsonTolerance {
			// Allow positive ints to be treated as uints.
			if v, intOk := tv.GetValue().(*gpb.TypedValue_IntVal); intOk && v.IntVal >= 0 {
				ok, tv.Value = true, &gpb.TypedValue_UintVal{UintVal: uint64(v.IntVal)}
			}
		}
	case yang.Ybinary:
		_, ok = tv.GetValue().(*gpb.TypedValue_BytesVal)
	case yang.Ydecimal64:
		_, ok = tv.GetValue().(*gpb.TypedValue_DecimalVal)
		if !ok {
			_, ok = tv.GetValue().(*gpb.TypedValue_FloatVal)
		}
	}
	return ok
}

// isValueInterfacePtrToEnum reports whether v is an interface ptr to enum type.
func isValueInterfacePtrToEnum(v reflect.Value) bool {
	if v.Kind() != reflect.Ptr {
		return false
	}
	v = v.Elem()
	if v.Kind() != reflect.Interface {
		return false
	}
	v = v.Elem()

	return v.Kind() == reflect.Int64
}
