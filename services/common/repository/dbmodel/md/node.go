package md

import (
	"encoding/json"
	"reflect"
	"server/common/util/reflectutil"
	"strings"

	"github.com/mohae/deepcopy"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
)

type inoder interface {
	reflectutil.GenericTyper
	ipath() string
}

type NodeParent interface {
	ipath() string
	addDirty(path string, data interface{})
}

type Noder interface {
	inoder
	idata() interface{}
	init(parent NodeParent, path string)
	addDirty(path string, data interface{})
	Save()
}

// Node 为Mongodb 文档数据结构的节点，
// 每个节点持有了mongodb子文档的数据（data），并记录该子文档在文档中的路径和父节点。
type Node[T any] struct {
	data T `bson:"-"`
	// 父节点
	parent NodeParent `bson:"-"`
	// 该节点的路径
	path string `bson:"-" fake:"{sentence:2}"`
}

func NewNode[T any](data T) *Node[T] {
	return &Node[T]{
		data: data,
	}
}

// Save 每次 Node 内挂载的 data 发生数据变更时，都要调用该方法，
// 该方法将数据标记需要保存，待当前事务完成后，会一并将数据提交到mongodb
// 同一个dbmodel可以调用 Save 多次，性能开销可以忽略不计
func (n *Node[T]) Save() {
	// 将自己上报到根节点
	n.parent.addDirty(n.path, n.data)
}

func (n *Node[T]) Data() T {
	return n.data
}

func (n *Node[T]) idata() interface{} {
	return n.data
}

func (n *Node[T]) ipath() string {
	return n.path
}

func (n *Node[T]) addDirty(path string, data interface{}) {
	path = strings.Join([]string{n.path, path}, ".")
	n.parent.addDirty(path, data)
}

func (n *Node[T]) init(parent NodeParent, path string) {
	n.parent = parent
	//n.schema = schema
	n.path = path
}

func (n *Node[T]) DeepCopy() interface{} {
	return &Node[T]{
		data: deepcopy.Copy(n.data).(T),
	}
}

func (n *Node[T]) GenericType() []reflect.Type {
	var t T
	return []reflect.Type{reflect.TypeOf(t)}
}

func (n *Node[T]) UnmarshalBSON(bytes []byte) error {
	n.data = reflect.New(n.GenericType()[0].Elem()).Interface().(T)
	return bson.Unmarshal(bytes, n.data)
}

func (n *Node[T]) MarshalBSONValue() (bsontype.Type, []byte, error) {
	if n == nil || reflect.ValueOf(n.data).IsZero() {
		return bsontype.Null, nil, nil
	}
	b, err := bson.Marshal(n.data)
	return bsontype.EmbeddedDocument, b, err
}

func (n *Node[T]) UnmarshalJSON(bytes []byte) error {
	n.data = reflect.New(n.GenericType()[0].Elem()).Interface().(T)
	return json.Unmarshal(bytes, n.data)
}

func (n *Node[T]) MarshalJSON() ([]byte, error) {
	if n == nil {
		return json.Marshal(nil)
	}
	return json.Marshal(n.data)
}
