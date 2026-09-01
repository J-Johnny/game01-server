package md

import (
	"errors"
	"fmt"
	"reflect"
	"server/common/util/panicutil"
	"server/common/util/reflectutil"

	errors2 "github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson/bsoncodec"
)

// 由于golang的
type ilist interface {
	reflectutil.GenericTyper
	init(parent Noder, schema *ModelNodeSchema)
	privateList()
}

type imap interface {
	reflectutil.GenericTyper
	init(parent Noder, schema *ModelNodeSchema)
	privateMap()
}

func ParseSchema(model interface{}) (*ModelRootSchema, error) {
	p := &schemaParser{}
	err := p.parse(model)
	if err != nil {
		return nil, err
	}
	return p.schema, nil
}

type schemaParser struct {
	schema *ModelRootSchema
}

func (parser *schemaParser) parse(model interface{}) (err error) {
	defer func() {
		if err != nil {
			err = errors2.Wrap(err, "parse model schema failed")
		}
	}()
	var (
		rt reflect.Type
	)
	rt, err = reflectutil.GetStructTyp(reflect.TypeOf(model))
	if err != nil {
		return
	}
	parser.schema = &ModelRootSchema{
		RootField: reflect.StructField{},
		Nodes:     map[string]*ModelNodeSchema{},
	}
	parser.schema.RootField, err = findModelNodeRootField(rt)
	if err != nil {
		return
	}

	var (
		isNode          bool
		modelNodeSchema *ModelNodeSchema
	)
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		modelNodeSchema, isNode, err = parser.parseStructField(field)
		if err != nil {
			return
		}
		if !isNode {
			continue
		}
		parser.schema.Nodes[modelNodeSchema.BsonTagName] = modelNodeSchema
	}

	return nil
}

func (parser *schemaParser) parseStructField(field reflect.StructField) (schema *ModelNodeSchema, isNode bool, err error) {
	defer func() {
		if err != nil {
			err = errors2.Wrap(err, "parse mode node schema failed")
		}
	}()
	if !field.IsExported() {
		return
	}

	var (
		nodeType     = field.Type
		ok           bool
		genericTypes []reflect.Type
	)

	ok = nodeType.Implements(inoderType)
	if !ok {
		return
	}

	bsonTag, err := bsoncodec.DefaultStructTagParser(field)
	panicutil.Must(err)
	if bsonTag.Skip {
		return
	}

	schema = &ModelNodeSchema{}
	schema.FieldType = field
	schema.BsonTagName = bsonTag.Name
	schema.Children = map[string]*ModelNodeSchema{}
	//schema.NodeId = parser.nextNodeId

	genericTypes, err = reflectutil.GetStructGenericType(nodeType)
	if err != nil {
		return
	}

	if nodeType.Implements(imapType) {
		schema.ContainerType = ContainerTypeMap

		schema.ContainerMapKey = genericTypes[0]
		err = checkMapKey(schema.ContainerMapKey.Kind())
		if err != nil {
			return
		}

		genericTypes = genericTypes[1:]

	} else if nodeType.Implements(ilistType) {
		schema.ContainerType = ContainerTypeList
	}
	schema.DataStructPtrType = genericTypes[0]

	dataStruct := schema.DataStructPtrType.Elem()
	var (
		Schema2 *ModelNodeSchema
		isNode2 bool
		err2    error
	)
	for i := 0; i < dataStruct.NumField(); i++ {
		field2 := dataStruct.Field(i)
		Schema2, isNode2, err2 = parser.parseStructField(field2)
		if err2 != nil {
			err = errors2.Wrap(err2, fmt.Sprint("parse child failed: ", field2.Name))
			return
		}
		if !isNode2 {
			continue
		}
		//Schema2.FatherSchema = schema
		schema.Children[Schema2.BsonTagName] = Schema2
	}
	return schema, true, nil
}

var inoderType = reflect.TypeOf((*inoder)(nil)).Elem()
var ilistType = reflect.TypeOf((*ilist)(nil)).Elem()
var imapType = reflect.TypeOf((*imap)(nil)).Elem()
var rootType = reflect.TypeOf(&NodeRoot{})

func findModelNodeRootField(rt reflect.Type) (reflect.StructField, error) {
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.Type == rootType {
			return field, nil
		}
	}
	return reflect.StructField{}, errors.New("no model root field")
}

func checkMapKey(kind reflect.Kind) error {
	switch kind {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return nil
	default:
		return errors.New("map key must be int/uint or string")
	}
}
