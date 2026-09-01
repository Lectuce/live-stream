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
	// fmt.Println("===DEBUG DSN:", dsn)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	SC = &model.StorageContainer{
		Db: db,
	}
	if err != nil {
		panic(fmt.Errorf("unable to connect to mysql server, %v", err))
	}

	// if db.Migrator().HasTable(&model.Room{}) {
	// 	if err := db.Migrator().DropTable(&model.Room{}); err != nil {
	// 		panic(fmt.Errorf("Database table Room migration failed: %v", err))
	// 	}
	// }

	// if db.Migrator().HasTable(&model.User{}) {
	// 	if err := db.Migrator().DropTable(&model.User{}); err != nil {
	// 		panic(fmt.Errorf("Database table User migration failed: %v", err))
	// 	}
	// }

	// if db.Migrator().HasTable(&model.Danma{}) {
	// 	if err := db.Migrator().DropTable(&model.Danma{}); err != nil {
	// 		panic(fmt.Errorf("Database table Danma migration failed: %v", err))
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
	if err := migrateDanmaSequences(db); err != nil {
		panic(fmt.Errorf("Database danma sequence migration failed: %v", err))
	}

	fmt.Println("Connect success!")

}
