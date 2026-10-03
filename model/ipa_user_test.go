package model

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/ipa"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSQLiteConcurrentIPASync(t *testing.T) {
	// A short lock timeout exposes competing SQLite connections quickly.
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ipa.db")+"?_busy_timeout=1"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	originalDB := DB
	originalRedisEnabled := common.RedisEnabled
	originalMatch, originalBind := config.IPAUserMatch, config.IPABindDN
	config.IPAUserMatch, config.IPABindDN = "*", ""
	common.RedisEnabled = false
	DB = db
	sqlDB := setDBConns(db)
	t.Cleanup(func() {
		DB = originalDB
		common.RedisEnabled = originalRedisEnabled
		config.IPAUserMatch, config.IPABindDN = originalMatch, originalBind
		_ = sqlDB.Close()
	})
	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasColumn(&User{}, "ipa_account_locked") {
		t.Fatal("IPA lock column was not migrated with its expected name")
	}
	if sqlDB.Stats().MaxOpenConnections != 1 {
		t.Fatal("SQLite pool must serialize access")
	}

	directoryUser := &ipa.User{EntryUUID: "test-ipa-id", UID: "ipa-test", Name: "IPA Test", Locked: true}
	user := User{Username: directoryUser.UID, IPAEntryUUID: directoryUser.EntryUUID, Status: UserStatusDisabled, Password: ""}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	var workers sync.WaitGroup
	errors := make(chan error, 8)
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for iteration := 0; iteration < 25; iteration++ {
				changedUser := *directoryUser
				changedUser.Name = fmt.Sprintf("IPA Test %d", iteration)
				if _, err := SyncIPAUser(&changedUser); err != nil {
					errors <- err
					return
				}
				enabled, err := IsUserEnabled(user.Id)
				if err != nil {
					errors <- err
					return
				}
				if enabled {
					errors <- fmt.Errorf("disabled IPA user became enabled")
					return
				}
			}
		}()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	updated, err := GetUserById(user.Id, false)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.IPAAccountLocked || updated.Status != UserStatusDisabled {
		t.Fatal("sync must preserve local disable state and update IPA lock state")
	}
	if _, err := SyncIPAUser(directoryUser); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("test:unexpected_update", func(db *gorm.DB) {
		t.Error("unchanged directory account must not trigger a database update")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncIPAUser(directoryUser); err != nil {
		t.Fatal(err)
	}
}
