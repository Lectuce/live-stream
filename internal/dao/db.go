package dao

import (
	"fmt"
	"live-stream/internal/config"
	"live-stream/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var SC *model.StorageContainer

func Initdb() {
	cfg := config.GlobalConfig
	dsn := cfg.Mysql.DSN()
	// fmt.Println("===调试 DSN:", dsn)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	SC = &model.StorageContainer{
		Db: db,
	}
	if err != nil {
		panic(fmt.Errorf("unable to connect to mysql server, %v", err))
	}

	// if db.Migrator().HasTable(&model.Room{}) {
	// 	if err := db.Migrator().DropTable(&model.Room{}); err != nil {
	// 		panic(fmt.Errorf("数据库表 Room 迁移失败: %v", err))
	// 	}
	// }

	// if db.Migrator().HasTable(&model.User{}) {
	// 	if err := db.Migrator().DropTable(&model.User{}); err != nil {
	// 		panic(fmt.Errorf("数据库表 User 迁移失败: %v", err))
	// 	}
	// }

	// if db.Migrator().HasTable(&model.Danma{}) {
	// 	if err := db.Migrator().DropTable(&model.Danma{}); err != nil {
	// 		panic(fmt.Errorf("数据库表 Danma 迁移失败: %v", err))
	// 	}
	// }

	if err := db.AutoMigrate(&model.Room{}); err != nil {
		panic(fmt.Errorf("Database table Room migration failed: %v", err))
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		panic(fmt.Errorf("Database table User migration failed: %v", err))
	}
	if err := db.AutoMigrate(&model.Danma{}); err != nil {
		panic(fmt.Errorf("Database table Danma migration failed: %v", err))
	}
	if err := db.AutoMigrate(&model.LikeBatch{}); err != nil {
		panic(fmt.Errorf("Database table LikeBatch migration failed: %v", err))
	}
	if err := db.AutoMigrate(&model.LikeAggregationCheckpoint{}); err != nil {
		panic(fmt.Errorf("Database table LikeAggregationCheckpoint migration failed: %v", err))
	}

	if err := db.AutoMigrate(&model.Video{}); err != nil {
		panic(fmt.Errorf("Database table video migration failed: %v", err))
	}

	if err := migrateDanmaSequences(db); err != nil {
		panic(fmt.Errorf("Database danma sequence migration failed: %v", err))
	}

	fmt.Println("Connect success!")

}
