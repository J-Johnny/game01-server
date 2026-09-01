package md

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	errors2 "github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
)

// NodeRoot mongodb的数据库文档可以通过匿名组合该结构体，来搭配 Node 进行组合管理
type NodeRoot struct {
	dirtyNode map[string]interface{}
}

func (r *NodeRoot) ipath() string {
	return ""
}

func (r *NodeRoot) ResetDirty() {
	r.dirtyNode = map[string]interface{}{}
}

func (r *NodeRoot) DirtyData() bson.M {
	return r.dirtyNode
}

func (r *NodeRoot) addDirty(path string, data interface{}) {
	if path == "" {
		panic("why empty path?")
	}
	toDeletePath := make([]string, 0)
	for p := range r.dirtyNode {
		if p == path {
			toDeletePath = append(toDeletePath, p)
			continue
		}
		if strings.HasPrefix(path, fmt.Sprintf("%s.", p)) {
			return
		}
		if strings.HasPrefix(p, fmt.Sprintf("%s.", path)) {
			toDeletePath = append(toDeletePath, p)
		}
	}
	for _, p := range toDeletePath {
		delete(r.dirtyNode, p)
	}
	r.dirtyNode[path] = data
}

func (r *NodeRoot) DeepCopy() interface{} {
	return &NodeRoot{dirtyNode: map[string]interface{}{}}
}

func Init2[T any](model T) error {
	schema, err := GlobalSchemaManager.Parse(model)
	if err != nil {
		return errors2.Wrap(err, "parse schema")
	}
	err = Init(model, schema)
	if err != nil {
		return errors2.Wrap(err, "init")
	}
	return nil
}

// 初始化
func Init(model interface{}, schema *ModelRootSchema) error {
	rvPtr := reflect.ValueOf(model)
	if rvPtr.Kind() != reflect.Ptr {
		return errors.New("root must be struct ptr")
	}
	rv := rvPtr.Elem()
	if rv.Kind() != reflect.Struct {
		return errors.New("root must be struct ptr")
	}
	rootRv := rv.Field(schema.RootField.Index[0])
	rootRv.Set(reflect.ValueOf(&NodeRoot{}))
	root := rootRv.Interface().(*NodeRoot)
	root.ResetDirty()

	var err error
	for bsonName := range schema.Nodes {
		nodeSchema := schema.Nodes[bsonName]
		fieldValue := rv.Field(nodeSchema.FieldType.Index[0])
		err = setupNode(root, "", fieldValue, nodeSchema)
		if err != nil {
			return errors2.Wrap(err, "init NodeRoot")
		}
	}

	return nil
}

func setupNode(parent NodeParent, parentPath string, fieldValue reflect.Value, schema *ModelNodeSchema) error {
	var (
		err error
	)
	bsonPath := make([]string, 0)
	if parentPath != "" {
		bsonPath = append(bsonPath, parentPath)
	}

	bsonPath = append(bsonPath, schema.BsonTagName)

	switch schema.ContainerType {
	case ContainerTypeList:
		if fieldValue.IsZero() {
			fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
		}
		node := fieldValue.Interface().(ilist)
		node.init(parent.(Noder), schema)
	case ContainerTypeMap:
		if fieldValue.IsZero() {
			fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
		}
		node := fieldValue.Interface().(imap)
		node.init(parent.(Noder), schema)
	default:
		node := fieldValue.Interface().(Noder)
		err = initChild(parent, node, schema.BsonTagName, schema)
		if err != nil {
			return err
		}
	}
	return nil
}

func initChild(parent NodeParent, node Noder, path string, schema *ModelNodeSchema) error {
	var err error
	node.init(parent, path)
	rvPtr := reflect.ValueOf(node.idata())
	if rvPtr.IsNil() {
		rvPtr.Set(reflect.New(rvPtr.Type().Elem()))
	}
	rv := rvPtr.Elem()
	for bsonName := range schema.Children {
		childSchema := schema.Children[bsonName]
		fieldValue := rv.Field(childSchema.FieldType.Index[0])
		err = setupNode(node, path, fieldValue, childSchema)
		if err != nil {
			return err
		}
	}
	return nil
}
