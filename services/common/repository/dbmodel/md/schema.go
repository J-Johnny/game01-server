package md

import (
	"fmt"
	"reflect"
	"server/common/util/panicutil"
	"server/common/util/reflectutil"

	"github.com/pkg/errors"
)

var GlobalSchemaManager = newDBModelManager()

type SchemaManager struct {
	schemas map[reflect.Type]*ModelRootSchema
}

func newDBModelManager() *SchemaManager {
	return &SchemaManager{
		schemas: map[reflect.Type]*ModelRootSchema{},
	}
}

func (mgr *SchemaManager) Parse(model interface{}) (*ModelRootSchema, error) {
	rt, err := reflectutil.GetStructTyp(reflect.TypeOf(model))
	if err != nil {
		return nil, errors.Wrap(err, "SchemaManager")
	}
	schema, ok := mgr.schemas[rt]
	if !ok {
		return nil, errors.New(fmt.Sprint("not register this model: ", rt.Name()))
	}

	return schema, nil
}

func (mgr *SchemaManager) Register(model interface{}) {
	rt, err := reflectutil.GetStructTyp(reflect.TypeOf(model))
	panicutil.Must(err)

	schema, err := ParseSchema(model)
	panicutil.Must(err)

	mgr.schemas[rt] = schema
}

type ContainerType uint8

const (
	ContainerTypeMap  ContainerType = 1
	ContainerTypeList ContainerType = 2
)

type ModelNodeSchema struct {
	//FatherSchema *ModelNodeSchema

	// 当前node在父节点Map容器中key的类型
	ContainerMapKey reflect.Type
	ContainerType   ContainerType

	// 当前node在父节点中的字段类型
	FieldType reflect.StructField
	// 当前node持有的struct ptr类型
	DataStructPtrType reflect.Type
	// 当前node在父节点中的bson tag名
	BsonTagName string
	// 当前node的子node
	Children map[string]*ModelNodeSchema
}

type ModelRootSchema struct {
	// ModelRoot字段
	RootField reflect.StructField
	Nodes     map[string]*ModelNodeSchema
}
