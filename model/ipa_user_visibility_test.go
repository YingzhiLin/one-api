package model

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/ipa"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestIPAUserVisibility(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "visibility.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	originalDB, originalEnabled, originalOnly, originalBind := DB, config.IPAEnabled, config.IPAOnly, config.IPABindDN
	originalRedis := common.RedisEnabled
	originalMatch := config.IPAUserMatch
	config.IPAUserMatch = "*"
	DB, config.IPAEnabled, config.IPABindDN = db, true, "uid=app_bind_oneapi,cn=users,dc=example,dc=com"
	sqlDB := setDBConns(db)
	t.Cleanup(func() {
		DB, config.IPAEnabled, config.IPAOnly, config.IPABindDN = originalDB, originalEnabled, originalOnly, originalBind
		common.RedisEnabled = originalRedis
		config.IPAUserMatch = originalMatch
		_ = sqlDB.Close()
	})
	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatal(err)
	}
	config.IPAOnly = true
	if err := CreateRootAccountIfNeed(); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&User{}).Count(&count)
	if count != 0 {
		t.Fatal("IPA-only initialization must not create a local root")
	}
	fixtures := []User{
		{Username: "root", Role: RoleRootUser},
		{Username: "app_bind_oneapi"}, // A local namesake is still visible in mixed mode.
		{Username: "person", IPAEntryUUID: "ipa-person", IPAUid: "person"},
		{Username: "ipa_app_bind", IPAEntryUUID: "ipa-bind", IPAUid: "app_bind_oneapi"},
		{Username: "admin", IPAEntryUUID: "ipa-admin", IPAUid: "admin"},
	}
	for index := range fixtures {
		fixtures[index].Status = UserStatusEnabled
		fixtures[index].AccessToken = fmt.Sprintf("test-access-%d", index)
		fixtures[index].AffCode = fmt.Sprintf("test-aff-%d", index)
		if err := db.Create(&fixtures[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&User{}).Where("id = ?", fixtures[0].Id).Update("ipa_entry_uuid", nil).Error; err != nil {
		t.Fatal(err)
	}
	for _, only := range []bool{false, true} {
		t.Run(fmt.Sprintf("ipa_only_%v", only), func(t *testing.T) {
			config.IPAOnly = only
			wantCount := 4
			if only {
				wantCount = 2
			}
			users, err := GetAllUsers(0, 100, "")
			if err != nil || len(users) != wantCount {
				t.Fatalf("listed users count = %d, want %d, err = %v", len(users), wantCount, err)
			}
			if total, err := CountListedUsers(); err != nil || total != int64(wantCount) {
				t.Fatalf("filtered total=%d, want %d, err=%v", total, wantCount, err)
			}
			for _, user := range users {
				if user.Id == fixtures[3].Id || (only && user.IPAEntryUUID == "") {
					t.Fatalf("excluded identity appeared in list: %s", user.Username)
				}
			}
			page, err := GetAllUsers(1, 1, "")
			if err != nil || len(page) != 1 || page[0].Username != "person" {
				t.Fatalf("filter must be applied before pagination: %v", err)
			}
			for _, keyword := range []string{"", "app_bind", "root", "4", "admin"} {
				matches, err := SearchUsers(keyword)
				if err != nil {
					t.Fatal(err)
				}
				for _, user := range matches {
					if user.Id == fixtures[3].Id || (only && user.IPAEntryUUID == "") {
						t.Fatalf("excluded identity appeared in search %q", keyword)
					}
				}
				if keyword == "admin" && (len(matches) != 1 || matches[0].Username != "admin") {
					t.Fatal("IPA admin must remain visible")
				}
			}
		})
	}
	for index, uid := range []string{"h100", "s100", "h_100", "hX100"} {
		user := User{Username: uid, IPAUid: uid, IPAEntryUUID: "match-" + uid, Status: UserStatusEnabled,
			AccessToken: fmt.Sprintf("match-access-%d", index), AffCode: fmt.Sprintf("match-aff-%d", index)}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	config.IPAOnly = true
	for _, test := range []struct {
		pattern string
		count   int
	}{{"h*", 3}, {"h_*", 1}, {"h%*", 0}, {"h100", 1}, {"*100", 4}} {
		config.IPAUserMatch = test.pattern
		users, err := GetAllUsers(0, 100, "")
		if err != nil || len(users) != test.count {
			t.Fatalf("pattern %q list count=%d, want %d, err=%v", test.pattern, len(users), test.count, err)
		}
		if total, err := CountListedUsers(); err != nil || total != int64(test.count) {
			t.Fatalf("pattern %q filtered total=%d, want %d, err=%v", test.pattern, total, test.count, err)
		}
		matches, err := SearchUsers("")
		if err != nil || len(matches) != test.count {
			t.Fatalf("pattern %q search count=%d, want %d, err=%v", test.pattern, len(matches), test.count, err)
		}
	}
	config.IPAUserMatch = "h*"
	if enabled, err := IsUserEnabled(fixtures[4].Id); err != nil || enabled {
		t.Fatalf("out-of-scope IPA identity must be unusable: enabled=%v err=%v", enabled, err)
	}
	config.IPAUserMatch = "*"
	common.RedisEnabled = true // Even an old cached enabled state must not admit the bind account.
	if enabled, err := CacheIsUserEnabled(fixtures[3].Id); err != nil || enabled {
		t.Fatalf("bind account must be unusable: enabled=%v err=%v", enabled, err)
	}
	if _, err := SyncIPAUser(&ipa.User{EntryUUID: "ipa-bind", UID: "app_bind_oneapi"}); err == nil {
		t.Fatal("bind identity must not be synchronized as an app account")
	}
}
