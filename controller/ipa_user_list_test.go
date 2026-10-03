package controller

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestIPAUserListReadsLocalPagesWithoutDirectorySync(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "users.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	originalDB, originalIPAEnabled, originalIPAURL := model.DB, config.IPAEnabled, config.IPAURL
	originalMatch, originalBind := config.IPAUserMatch, config.IPABindDN
	config.IPAUserMatch, config.IPABindDN = "*", ""
	model.DB, config.IPAEnabled, config.IPAURL = db, true, ""
	t.Cleanup(func() {
		model.DB, config.IPAEnabled, config.IPAURL = originalDB, originalIPAEnabled, originalIPAURL
		config.IPAUserMatch, config.IPABindDN = originalMatch, originalBind
		_ = sqlDB.Close()
	})
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < config.ItemsPerPage*2+1; index++ {
		user := model.User{
			Username: fmt.Sprintf("person%03d", index), IPAUid: fmt.Sprintf("person%03d", index),
			IPAEntryUUID: fmt.Sprintf("ipa-%d", index), AccessToken: fmt.Sprintf("access-%d", index),
			AffCode: fmt.Sprintf("aff-%d", index), Status: model.UserStatusEnabled,
		}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	// An unavailable directory must not force a full sync from read handlers.
	for _, test := range []struct {
		name, url string
		handler   gin.HandlerFunc
		count     int
		firstID   int
	}{
		{"first_page", "/api/user/?p=0", GetAllUsers, config.ItemsPerPage, config.ItemsPerPage*2 + 1},
		{"second_page", "/api/user/?p=1", GetAllUsers, config.ItemsPerPage, config.ItemsPerPage + 1},
		{"last_page", "/api/user/?p=2", GetAllUsers, 1, 1},
		{"search", "/api/user/search?keyword=person000", SearchUsers, 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest("GET", test.url, nil)
			test.handler(context)
			var response struct {
				Success bool         `json:"success"`
				Message string       `json:"message"`
				Data    []model.User `json:"data"`
				Total   int64        `json:"total"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if !response.Success {
				t.Fatalf("read handler attempted directory sync: %s", response.Message)
			}
			if len(response.Data) != test.count || response.Data[0].Id != test.firstID {
				t.Fatalf("incorrect page or search results: %+v", response.Data)
			}
			if test.name != "search" && response.Total != int64(config.ItemsPerPage*2+1) {
				t.Fatalf("incorrect total user count: %d", response.Total)
			}
		})
	}
}
