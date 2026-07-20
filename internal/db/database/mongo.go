package database

import (
	"log"

	"api-node/internal/config"

	"github.com/zergolf1994/goose"

	"go.mongodb.org/mongo-driver/mongo"
)

// Connect establishes a connection to MongoDB via goose ODM.
//
// ⚠ ไม่มี EnsureIndexes — เวอร์ชันเก่าเคยสร้าง unique index บน
// video_process.fileId (schema เก่า: 1 ไฟล์ = 1 งาน) ซึ่ง "ผิด" กับคิวใหม่
// ที่หนึ่งไฟล์มีได้หลายงานต่อ processType (download/transfer/transcode/...)
// — index ของคิวใหม่จัดการโดย vdohide-service ฝั่งเดียวเท่านั้น
func Connect() error {
	return goose.Connect(config.AppConfig.MongoURI)
}

// Disconnect closes the MongoDB connection.
func Disconnect() {
	if goose.Client() != nil {
		if err := goose.Close(); err != nil {
			log.Printf("⚠️ Error disconnecting from MongoDB: %v", err)
		} else {
			log.Println("🔌 Disconnected from MongoDB")
		}
	}
}

// DB returns the database instance (delegates to goose).
func DB() *mongo.Database {
	return goose.DB()
}
