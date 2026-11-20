package db

import (
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestCommentMigrationPreservesExistingRows(t *testing.T) {
	d, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	type oldComment struct {
		ID        uint `gorm:"primaryKey;autoIncrement"`
		Name      string
		IsGuest   bool
		Content   string
		CreatedAt time.Time
	}
	if err := d.Table("comments").AutoMigrate(&oldComment{}); err != nil {
		t.Fatal(err)
	}
	if err := d.Table("comments").Create(&oldComment{Name: "alice", Content: "old comment"}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := d.Table("comments").AutoMigrate(&model.Comment{}); err != nil {
			t.Fatal(err)
		}
	}
	var comment model.Comment
	if err := d.Table("comments").First(&comment).Error; err != nil {
		t.Fatal(err)
	}
	if comment.Name != "alice" || comment.Content != "old comment" || comment.IP != "" {
		t.Fatalf("unexpected migrated comment: %+v", comment)
	}
	if !d.Migrator().HasColumn("comments", "ip") {
		t.Fatal("IP column was not added")
	}
}
