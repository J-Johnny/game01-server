package md

import (
	"encoding/json"
	"reflect"
	"server/common/util/panicutil"
	"sort"
	"strconv"
	"strings"

	"github.com/mohae/deepcopy"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
	"go.mongodb.org/mongo-driver/x/bsonx/bsoncore"
)

// List 内存放了切片数据结构的数据，虽然类型为 List[T] ，但是实际上内部存储的是 *Node[T]，
// List 内存放的Node存在相同的父节点，并且 path 皆为索引数字（如0，1，2）
// 通过使用该数据结构声明模型，可以实现只更新 List 下的一个 Node 子文档，提升节约流量，提升性能
type List[T any] struct {
	nodes  []*Node[T]
	parent Noder
	schema *ModelNodeSchema
}

func NewList[T any]() *List[T] {
	return &List[T]{
		nodes: make([]*Node[T], 0),
	}
}

// Foreach 遍历, 若需要终断遍历，则返回 isBreak == true
func (list *List[T]) Foreach(fn func(i int) (isBreak bool)) {
	for idx := range list.nodes {
		if fn(idx) {
			break
		}
	}
}

// Foreach2 遍历（带成员数据）,若需要终断遍历，则返回 isBreak == true
func (list *List[T]) Foreach2(fn func(i int, v *Node[T]) (isBreak bool)) {
	for idx := range list.nodes {
		if fn(idx, list.nodes[idx]) {
			break
		}
	}
}

func (list *List[T]) Update(idx int, data T) {
	node := NewNode(data)
	if list.schema != nil {
		list.initNode(idx, node)
		node.Save()
	}
	list.nodes[idx] = node
}

// Find 查找, 若idx < 0 则找不到
func (list *List[T]) Find(fn func(v *Node[T]) bool) (idx int, n *Node[T]) {
	for i := range list.nodes {
		if fn(list.nodes[i]) {
			return i, list.nodes[i]
		}
	}
	return -1, nil
}

// Append 在List中追加新成员
func (list *List[T]) Append(datas ...T) {
	maxIndex := len(list.nodes) - 1
	for i := range datas {
		node := NewNode(datas[i])
		if list.schema != nil {
			list.initNode(maxIndex+i+1, node)
			node.Save()
		}
		list.nodes = append(list.nodes, node)
	}
}

// Get 获取索引位置的Node
func (list *List[T]) Get(idx int) *Node[T] {
	return list.nodes[idx]
}

// Len 当前List的长度
func (list *List[T]) Len() int {
	return len(list.nodes)
}

func (list *List[T]) Data() []*Node[T] {
	return list.nodes
}

func (list *List[T]) Data2() []T {
	result := make([]T, len(list.nodes))
	for i, n := range list.nodes {
		result[i] = n.Data()
	}
	return result
}

func (list *List[T]) ipath() string {
	return list.schema.BsonTagName
}

func (list *List[T]) init(parent Noder, schema *ModelNodeSchema) {
	list.parent = parent
	list.schema = schema
	for i := range list.nodes {
		node := list.nodes[i]
		list.initNode(i, node)
	}
}
func (list *List[T]) initNode(idx int, node *Node[T]) {
	path := strings.Join([]string{list.schema.BsonTagName, strconv.Itoa(idx)}, ".")
	err := initChild(list.parent, node, path, list.schema)
	panicutil.Must(err)
}
func (list *List[T]) privateList() {}

func (list *List[T]) UnmarshalJSON(bytes []byte) error {
	datas := make([]T, 0)
	err := json.Unmarshal(bytes, &datas)
	if err != nil {
		return err
	}
	list.nodes = make([]*Node[T], len(datas))
	for i := range datas {
		list.nodes[i] = NewNode(datas[i])
		// list.initNode()
	}
	return nil
}

func (list *List[T]) MarshalJSON() ([]byte, error) {
	if list == nil || list.nodes == nil {
		return json.Marshal([]T{})
	}
	return json.Marshal(list.nodes)
}

func (list *List[T]) MarshalBSONValue() (bsontype.Type, []byte, error) {
	if list == nil || reflect.ValueOf(list.nodes).IsZero() || len(list.nodes) == 0 {
		return bson.MarshalValue(make([]T, 0))
	}
	return bson.MarshalValue(list.nodes)
}

func (list *List[T]) UnmarshalBSONValue(b bsontype.Type, bytes []byte) error {
	list.nodes = make([]*Node[T], 0)

	elements, err := bsoncore.Document(bytes).Elements()
	if err != nil {
		return err
	}

	if elements == nil {
		return nil
	}

	for _, elem := range elements {
		rawVal := elem.Value()
		t := reflect.New(list.GenericType()[0].Elem()).Interface().(T)
		err = bson.Unmarshal(rawVal.Data, t)
		// err = val.UnmarshalBSONValue(rawVal.Type, rawVal.Data)
		if err != nil {
			return err
		}
		list.nodes = append(list.nodes, NewNode(t))
	}
	return nil
}

func (list *List[T]) GenericType() []reflect.Type {
	var t T
	return []reflect.Type{reflect.TypeOf(t)}
}

func (list *List[T]) DeepCopy() interface{} {
	return &List[T]{
		nodes: deepcopy.Copy(list.nodes).([]*Node[T]),
	}
}

func (list *List[T]) Delete(idx int) {
	list.nodes = append(list.nodes[:idx], list.nodes[idx+1:]...)
}

// DeleteMore 删除多条数据，确保索引唯一，否则可能会删除正常数据
func (list *List[T]) DeleteMore(idxs []int) {
	sort.Slice(idxs, func(i, j int) bool {
		// 这里为了直接得到逆序，使用!less，可以按照id从大到小批量删除数据
		return idxs[i] > idxs[j]
	})
	for _, idx := range idxs {
		list.Delete(idx)
	}
}
