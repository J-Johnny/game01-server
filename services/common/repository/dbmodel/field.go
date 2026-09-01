package dbmodel

import (
	"github.com/qiniu/qmgo/field"
)

type DefaultField struct {
	Id           string `bson:"_id"`
	CreateTimeAt int64  `bson:"createTimeAt"`
	UpdateTimeAt int64  `bson:"updateTimeAt"`
}

func (u *DefaultField) CustomFields() field.CustomFieldsBuilder {
	return field.NewCustom().SetCreateAt("CreateTimeAt").SetUpdateAt("UpdateTimeAt").SetId("Id")
}
