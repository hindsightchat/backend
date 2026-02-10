package database

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"

	"gorm.io/gorm"
)

// DB is the global database connection (MYSQL, *gorm.DB)
var DB *gorm.DB

func InitDatabase() {

	dsn := os.Getenv("TIDB_DATABASE_DSN")

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})

	if err != nil {
		panic("failed to connect database:" + err.Error())
	}

	db.AutoMigrate(Schema...)

	// print every schema that exists
	for _, s := range Schema {
		fmt.Printf("Database Schema: %T\n", s)
	}

	// create base user

	basePassword, err := bcrypt.GenerateFromPassword([]byte("rmfosho123"), bcrypt.DefaultCost)
	if err != nil {
		panic("failed to hash base user password:" + err.Error())
	}

	user := User{
		Username:         "rmfosho.me",
		Password:         string(basePassword),
		Email:            "me@rmfosho.me",
		Domain:           "rmfosho.me",
		IsDomainVerified: true,
	}

	// check if user exists
	var existingUser User
	result := db.Where("email = ?", user.Email).First(&existingUser)

	if result.Error != nil && result.Error == gorm.ErrRecordNotFound {
		err = db.Create(&user).Error

		if err != nil {
			panic("failed to create base user:" + err.Error())
		}
	}

	// setup :)
	DB = db

}
