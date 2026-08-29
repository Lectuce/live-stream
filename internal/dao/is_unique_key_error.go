package dao

import (
	"errors"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// 判断是否为唯一键冲突错误
func IsUniqueKeyError(err error) bool {
	if err == nil {
		return false
	}
	// 解 GORM 包装的错误
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		// MySQL 唯一键冲突错误码 1062
		return mysqlErr.Number == 1062
	}
	// 兼容原始错误链
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	return false
}
