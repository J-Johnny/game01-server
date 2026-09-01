package mongoutil

import (
	"github.com/qiniu/qmgo"
	"go.mongodb.org/mongo-driver/mongo"
)

// IsNoDoc 判断是不是不存在文档，如果不存在，则返回true，
// 若是其他错误，则返回false及其他错误
func IsNoDoc(err error) bool {
	if err == qmgo.ErrNoSuchDocuments {
		return true
	}
	return false
}

// 是否主键重复
func IsDup(err error) bool {
	return mongo.IsDuplicateKeyError(err)
}
