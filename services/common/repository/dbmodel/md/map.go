package md

import (
	"encoding/json"
	"fmt"
	"reflect"
	"server/common/util/panicutil"
	"strings"

	"github.com/mohae/deepcopy"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
)

// Map 内存放了map数据结构的数据，虽然类型为 Map[K]T  ，但是实际上内部存储的是 Map[K]*Node[T]，
// Map 内存放的 Node 存在相同的父节点，并且 path 皆为key
// 通过使用该数据结构声明模型，可以实现只更新 Map 下的一个 Node 子文档，提升节约流量，提升性能
type Map[K comparable, T any] struct {
	data   map[K]*Node[T]
	parent Noder
	schema *ModelNodeSchema
}

func NewMap[K comparable, T any]() *Map[K, T] {
	return &Map[K, T]{
		data: map[K]*Node[T]{},
	}
}

// Foreach 遍历 Map 若需要中断遍历，则返回 isBreak == true
func (mp *Map[K, T]) Foreach(fn func(key K) (isBreak bool)) {
	for k := range mp.data {
		if fn(k) {
			break
		}
	}
}

// Foreach2 遍历 Map 若需要中断遍历，则返回 isBreak == true
func (mp *Map[K, T]) Foreach2(fn func(key K, value *Node[T]) (isBreak bool)) {
	for k := range mp.data {
		if fn(k, mp.data[k]) {
			break
		}
	}
}

// Set 设置Key的Value，Set会用 Node 对 v 进行包装，并初始化其数据
func (mp *Map[K, T]) Set(k K, v T) {
	if mp.data == nil {
		mp.data = map[K]*Node[T]{}
	}
	node := NewNode(v)
	mp.data[k] = node
	if mp.schema != nil {
		mp.initNode(k, node)
		node.Save()
	}
}

// Delete 删除Key
func (mp *Map[K, T]) Delete(k K) {
	delete(mp.data, k)
}

// Clear 清空Map
func (mp *Map[K, T]) Clear() {
	mp.data = map[K]*Node[T]{}
}

// Get 根据key获取Value
func (mp *Map[K, T]) Get(k K) *Node[T] {
	return mp.data[k]
}

// Get2 根据Key获取Value，并告知是否存在Key
func (mp *Map[K, T]) Get2(k K) (*Node[T], bool) {
	d, ok := mp.data[k]
	return d, ok
}

func (mp *Map[K, T]) Data() map[K]*Node[T] {
	return mp.data
}

func (mp *Map[K, T]) Data2() map[K]T {
	result := make(map[K]T, len(mp.data))
	for k, n := range mp.data {
		result[k] = n.Data()
	}
	return result
}

// Len 当前Map的长度
func (mp *Map[K, T]) Len() int {
	return len(mp.data)
}

// GetKeys 以slice形式获取所有的Key，注意是乱序的
func (mp *Map[K, T]) GetKeys() []K {
	length := mp.Len()
	res := make([]K, 0, length)
	mp.Foreach(func(key K) (isBreak bool) {
		res = append(res, key)
		return false
	})
	return res
}

// GetValueSlice 以slice形式获取所有的值，注意是乱序的
func (mp *Map[K, T]) GetValueSlice() []T {
	length := mp.Len()
	res := make([]T, 0, length)
	mp.Foreach2(func(key K, value *Node[T]) (isBreak bool) {
		res = append(res, value.Data())
		return false
	})
	return res
}

func (mp *Map[K, T]) initNode(k K, node *Node[T]) {
	path := strings.Join([]string{mp.schema.BsonTagName, fmt.Sprint(k)}, ".")
	err := initChild(mp.parent, node, path, mp.schema)
	panicutil.Must(err)
}
func (mp *Map[K, T]) privateMap() {}

func (mp *Map[K, T]) MarshalBSONValue() (bsontype.Type, []byte, error) {
	if mp == nil || reflect.ValueOf(mp.data).IsZero() || len(mp.data) == 0 {
		typ, b, err := bson.MarshalValue(bson.M{})
		return typ, b, err
	}
	typ, b, err := bson.MarshalValue(mp.data)
	return typ, b, err
}
func (mp *Map[K, T]) UnmarshalBSONValue(b bsontype.Type, bytes []byte) error {
	mp.data = map[K]*Node[T]{}
	datas := make(map[K]T, 0)
	err := bson.Unmarshal(bytes, &datas)
	if err != nil {
		return err
	}
	for k := range datas {
		mp.data[k] = NewNode(datas[k])
	}
	return nil
}

func (mp *Map[K, T]) UnmarshalJSON(bytes []byte) error {
	mp.data = map[K]*Node[T]{}
	datas := make(map[K]T, 0)
	err := json.Unmarshal(bytes, &datas)
	if err != nil {
		return err
	}
	for k := range datas {
		mp.data[k] = NewNode(datas[k])
	}
	return nil
}

func (mp *Map[K, T]) MarshalJSON() ([]byte, error) {
	if mp == nil || mp.data == nil {
		return json.Marshal(map[K]T{})
	}
	return json.Marshal(mp.data)
}

func (mp *Map[K, T]) GenericType() []reflect.Type {
	var (
		k K
		t T
	)
	return []reflect.Type{reflect.TypeOf(k), reflect.TypeOf(t)}
}

func (mp *Map[K, T]) init(parent Noder, schema *ModelNodeSchema) {
	mp.parent = parent
	mp.schema = schema
	for k := range mp.data {
		node := mp.data[k]
		mp.initNode(k, node)
	}
}

func (mp *Map[K, T]) ipath() string {
	return mp.schema.BsonTagName
}

func (mp *Map[K, T]) DeepCopy() interface{} {
	return &Map[K, T]{
		data: deepcopy.Copy(mp.data).(map[K]*Node[T]),
	}
}
