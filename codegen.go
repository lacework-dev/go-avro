/* Licensed to the Apache Software Foundation (ASF) under one or more
contributor license agreements.  See the NOTICE file distributed with
this work for additional information regarding copyright ownership.
The ASF licenses this file to You under the Apache License, Version 2.0
(the "License"); you may not use this file except in compliance with
the License.  You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License. */

package avro

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"strconv"
	"strings"
)

// CodeGenerator is a code generation tool for structs from given Avro schemas.
type CodeGenerator struct {
	rawSchemas []string

	structs           map[string]*bytes.Buffer
	codeSnippets      []*bytes.Buffer
	schemaDefinitions *bytes.Buffer
}

// Creates a new CodeGenerator for given Avro schemas.
func NewCodeGenerator(schemas []string) *CodeGenerator {
	return &CodeGenerator{
		rawSchemas:        schemas,
		structs:           make(map[string]*bytes.Buffer),
		codeSnippets:      make([]*bytes.Buffer, 0),
		schemaDefinitions: &bytes.Buffer{},
	}
}

type recordSchemaInfo struct {
	schema        *RecordSchema
	typeName      string
	schemaVarName string
	schemaErrName string
}

func newRecordSchemaInfo(schema *RecordSchema) (*recordSchemaInfo, error) {
	if schema.Name == "" {
		return nil, errors.New("Name not set.")
	}

	typeName := strings.ToUpper(schema.Name[:1]) + schema.Name[1:]

	return &recordSchemaInfo{
		schema:        schema,
		typeName:      typeName,
		schemaVarName: "_" + typeName + "_schema",
		schemaErrName: "_" + typeName + "_schema_err",
	}, nil
}

type enumSchemaInfo struct {
	schema   *EnumSchema
	typeName string
}

func newEnumSchemaInfo(schema *EnumSchema) (*enumSchemaInfo, error) {
	if schema.Name == "" {
		return nil, errors.New("Name not set.")
	}

	return &enumSchemaInfo{
		schema:   schema,
		typeName: strings.ToUpper(schema.Name[:1]) + schema.Name[1:],
	}, nil
}

// Generates source code for Avro schemas specified on creation.
// The ouput is Go formatted source code that contains struct definitions for all given schemas.
// May return an error if code generation fails, e.g. due to unparsable schema.
func (this *CodeGenerator) Generate() (string, error) {
	for index, rawSchema := range this.rawSchemas {
		parsedSchema, err := ParseSchema(rawSchema)
		if err != nil {
			return "", err
		}

		schema, ok := parsedSchema.(*RecordSchema)
		if !ok {
			return "", errors.New("Not a Record schema.")
		}
		schemaInfo, err := newRecordSchemaInfo(schema)
		if err != nil {
			return "", err
		}

		buffer := &bytes.Buffer{}
		this.codeSnippets = append(this.codeSnippets, buffer)

		// write package and import only once
		if index == 0 {
			this.writePackageName(schemaInfo)

			this.writeImportStatement()
		}

		err = this.writeStruct(schemaInfo)
		if err != nil {
			return "", err
		}
	}

	formatted, err := format.Source([]byte(this.collectResult()))
	if err != nil {
		return "", err
	}

	return string(formatted), nil
}

func (this *CodeGenerator) collectResult() string {
	results := make([]string, len(this.codeSnippets)+1)
	for i, snippet := range this.codeSnippets {
		results[i] = snippet.String()
	}
	results[len(results)-1] = this.schemaDefinitions.String()

	return strings.Join(results, "\n")
}

func (this *CodeGenerator) writePackageName(info *recordSchemaInfo) {
	buffer := this.codeSnippets[0]
	buffer.WriteString("package ")
	if info.schema.Namespace == "" {
		info.schema.Namespace = "avro"
	}

	packages := strings.Split(info.schema.Namespace, ".")
	buffer.WriteString(packages[len(packages)-1] + "\n\n")
}

func (this *CodeGenerator) writeStruct(info *recordSchemaInfo) error {
	buffer := &bytes.Buffer{}
	if _, exists := this.structs[info.typeName]; exists {
		return nil
	} else {
		this.codeSnippets = append(this.codeSnippets, buffer)
		this.structs[info.typeName] = buffer
	}

	this.writeStructSchemaVar(info)

	this.writeDoc("", info.schema.Doc, buffer)

	err := this.writeStructDefinition(info, buffer)
	if err != nil {
		return err
	}

	buffer.WriteString("\n\n")

	err = this.writeStructConstructor(info, buffer)
	if err != nil {
		return err
	}

	buffer.WriteString("\n\n")

	this.writeSchemaGetter(info, buffer)

	return nil
}

func (this *CodeGenerator) writeEnum(info *enumSchemaInfo) error {
	buffer := &bytes.Buffer{}
	if _, exists := this.structs[info.typeName]; exists {
		return nil
	} else {
		this.codeSnippets = append(this.codeSnippets, buffer)
		this.structs[info.typeName] = buffer
	}

	err := this.writeEnumConstants(info, buffer)
	if err != nil {
		return err
	}

	return nil
}

func (this *CodeGenerator) writeEnumConstants(info *enumSchemaInfo, buffer *bytes.Buffer) error {
	if len(info.schema.Symbols) == 0 {
		return nil
	}

	buffer.WriteString("// Enum values for " + info.typeName + "\n")
	buffer.WriteString("const (")
	for index, symbol := range info.schema.Symbols {
		buffer.WriteString(info.typeName + "_" + symbol + " int32 = " + strconv.FormatInt(int64(index), 10) + "\n")
	}
	buffer.WriteString(")")
	return nil
}

func (this *CodeGenerator) writeImportStatement() {
	buffer := this.codeSnippets[0]
	buffer.WriteString(`import "github.com/stealthly/go-avro"`)
	buffer.WriteString("\n")
}

func (this *CodeGenerator) writeStructSchemaVar(info *recordSchemaInfo) {
	buffer := this.schemaDefinitions
	buffer.WriteString("// Generated by codegen. Please do not modify.\n")
	buffer.WriteString("var " + info.schemaVarName + ", " + info.schemaErrName + " = avro.ParseSchema(`" + info.schema.String() + "`)\n\n")
}

func (this *CodeGenerator) writeDoc(prefix string, doc string, buffer *bytes.Buffer) {
	if doc == "" {
		return
	}

	buffer.WriteString(prefix + "/* " + doc + " */\n")
}

func (this *CodeGenerator) writeStructDefinition(info *recordSchemaInfo, buffer *bytes.Buffer) error {
	buffer.WriteString("type " + info.typeName + " struct {\n")

	for i := 0; i < len(info.schema.Fields); i++ {
		err := this.writeStructField(info.schema.Fields[i], buffer)
		if err != nil {
			return err
		}
	}

	buffer.WriteString("}")

	return nil
}

func (this *CodeGenerator) writeStructField(field *SchemaField, buffer *bytes.Buffer) error {
	this.writeDoc("\t", field.Doc, buffer)
	if field.Name == "" {
		return errors.New("Empty field name.")
	}

	buffer.WriteString("\t" + strings.ToUpper(field.Name[:1]) + field.Name[1:] + " ")

	err := this.writeStructFieldType(field.Type, buffer)
	if err != nil {
		return err
	}

	buffer.WriteString("\n")

	return nil
}

func (this *CodeGenerator) writeStructFieldType(schema Schema, buffer *bytes.Buffer) error {
	switch schema.Type() {
	case Null:
		buffer.WriteString("interface{}")
	case Boolean:
		buffer.WriteString("bool")
	case String:
		buffer.WriteString("string")
	case Int:
		buffer.WriteString("int32")
	case Long:
		buffer.WriteString("int64")
	case Float:
		buffer.WriteString("float32")
	case Double:
		buffer.WriteString("float64")
	case Bytes:
		buffer.WriteString("[]byte")
	case Array:
		{
			buffer.WriteString("[]")
			err := this.writeStructFieldType(schema.(*ArraySchema).Items, buffer)
			if err != nil {
				return err
			}
		}
	case Map:
		{
			buffer.WriteString("map[string]")
			err := this.writeStructFieldType(schema.(*MapSchema).Values, buffer)
			if err != nil {
				return err
			}
		}
	case Enum:
		{
			enumSchema := schema.(*EnumSchema)
			info, err := newEnumSchemaInfo(enumSchema)
			if err != nil {
				return err
			}

			buffer.WriteString("*avro.GenericEnum")

			return this.writeEnum(info)
		}
	case Union:
		{
			err := this.writeStructUnionType(schema.(*UnionSchema), buffer)
			if err != nil {
				return err
			}
		}
	case Fixed:
		buffer.WriteString("[]byte")
	case Record:
		{
			buffer.WriteString("*")
			recordSchema := schema.(*RecordSchema)

			schemaInfo, err := newRecordSchemaInfo(recordSchema)
			if err != nil {
				return err
			}

			buffer.WriteString(schemaInfo.typeName)

			return this.writeStruct(schemaInfo)
		}
	case Recursive:
		{
			buffer.WriteString("*")
			buffer.WriteString(schema.(*RecursiveSchema).GetName())
		}
	}

	return nil
}

func (this *CodeGenerator) writeStructUnionType(schema *UnionSchema, buffer *bytes.Buffer) error {
	var unionType Schema
	if schema.Types[0].Type() == Null {
		unionType = schema.Types[1]
	} else if schema.Types[1].Type() == Null {
		unionType = schema.Types[0]
	}

	if unionType != nil && this.isNullable(unionType) {
		return this.writeStructFieldType(unionType, buffer)
	}

	buffer.WriteString("interface{}")
	return nil
}

func (this *CodeGenerator) isNullable(schema Schema) bool {
	switch schema.(type) {
	case *BooleanSchema, *IntSchema, *LongSchema, *FloatSchema, *DoubleSchema, *StringSchema:
		return false
	default:
		return true
	}
}

func (this *CodeGenerator) writeStructConstructor(info *recordSchemaInfo, buffer *bytes.Buffer) error {
	buffer.WriteString("func New" + info.typeName + "() *" + info.typeName + " {\n\treturn &" + info.typeName + "{\n")

	for i := 0; i < len(info.schema.Fields); i++ {
		err := this.writeStructConstructorField(info, info.schema.Fields[i], buffer)
		if err != nil {
			return err
		}
	}

	buffer.WriteString("\t}\n}")

	return nil
}

func (this *CodeGenerator) writeStructConstructorField(info *recordSchemaInfo, field *SchemaField, buffer *bytes.Buffer) error {
	if !this.needWriteField(field) {
		return nil
	}

	this.writeStructConstructorFieldName(field, buffer)
	this.writeStructConstructorFieldValue(info, field, buffer)

	buffer.WriteString(",\n")

	return nil
}

func (this *CodeGenerator) writeStructConstructorFieldValue(info *recordSchemaInfo, field *SchemaField, buffer *bytes.Buffer) error {
	switch field.Type.(type) {
	case *NullSchema:
		buffer.WriteString("nil")
	case *BooleanSchema:
		buffer.WriteString(fmt.Sprintf("%t", field.Default))
	case *StringSchema:
		{
			buffer.WriteString(`"` + field.Default.(string) + `"`)
		}
	case *IntSchema:
		{
			defaultValue, ok := field.Default.(float64)
			if !ok {
				return fmt.Errorf("Invalid default value for %s field of type %s", field.Name, field.Type.GetName())
			}
			buffer.WriteString("int32(" + strconv.FormatInt(int64(defaultValue), 10) + ")")
		}
	case *LongSchema:
		{
			defaultValue, ok := field.Default.(float64)
			if !ok {
				return fmt.Errorf("Invalid default value for %s field of type %s", field.Name, field.Type.GetName())
			}
			buffer.WriteString("int64(" + strconv.FormatInt(int64(defaultValue), 10) + ")")
		}
	case *FloatSchema:
		{
			defaultValue, ok := field.Default.(float64)
			if !ok {
				return fmt.Errorf("Invalid default value for %s field of type %s", field.Name, field.Type.GetName())
			}
			buffer.WriteString(fmt.Sprintf("float32(%f)", float32(defaultValue)))
		}
	case *DoubleSchema:
		{
			defaultValue, ok := field.Default.(float64)
			if !ok {
				return fmt.Errorf("Invalid default value for %s field of type %s", field.Name, field.Type.GetName())
			}
			buffer.WriteString(fmt.Sprintf("float64(%f)", defaultValue))
		}
	case *BytesSchema:
		buffer.WriteString("[]byte{}")
	case *ArraySchema:
		{
			buffer.WriteString("make(")
			err := this.writeStructFieldType(field.Type, buffer)
			if err != nil {
				return err
			}
			buffer.WriteString(", 0)")
		}
	case *MapSchema:
		{
			buffer.WriteString("make(")
			err := this.writeStructFieldType(field.Type, buffer)
			if err != nil {
				return err
			}
			buffer.WriteString(")")
		}
	case *EnumSchema:
		{
			buffer.WriteString("avro.NewGenericEnum([]string{")
			enum := field.Type.(*EnumSchema)
			for _, symbol := range enum.Symbols {
				buffer.WriteString(`"`)
				buffer.WriteString(symbol)
				buffer.WriteString(`",`)
			}
			buffer.WriteString("})")
		}
	case *UnionSchema:
		{
			union := field.Type.(*UnionSchema)
			unionField := &SchemaField{}
			*unionField = *field
			unionField.Type = union.Types[0]
			return this.writeStructConstructorFieldValue(info, unionField, buffer)
		}
	case *FixedSchema:
		{
			buffer.WriteString("make([]byte, " + strconv.FormatInt(int64(field.Type.(*FixedSchema).Size), 10) + ")")
		}
	case *RecordSchema:
		{
			info, err := newRecordSchemaInfo(field.Type.(*RecordSchema))
			if err != nil {
				return err
			}
			buffer.WriteString("New" + info.typeName + "()")
		}
	}

	return nil
}

func (this *CodeGenerator) needWriteField(field *SchemaField) bool {
	if field.Default != nil {
		return true
	}

	switch field.Type.(type) {
	case *BytesSchema, *ArraySchema, *MapSchema, *EnumSchema, *FixedSchema, *RecordSchema:
		return true
	}

	return false
}

func (this *CodeGenerator) writeStructConstructorFieldName(field *SchemaField, buffer *bytes.Buffer) {
	buffer.WriteString("\t\t" + strings.ToUpper(field.Name[:1]) + field.Name[1:] + ": ")
}

func (this *CodeGenerator) writeSchemaGetter(info *recordSchemaInfo, buffer *bytes.Buffer) {
	buffer.WriteString("func (this *" + info.typeName + ") Schema() avro.Schema {\n\tif " + info.schemaErrName + " != nil {\n\t\tpanic(" + info.schemaErrName + ")\n\t}\n\treturn " + info.schemaVarName + "\n}")
}
