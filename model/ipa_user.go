package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/songquanpeng/one-api/common/ipa"
	"github.com/songquanpeng/one-api/common/logger"
	"gorm.io/gorm"
)

// SyncIPAUsers refreshes local IPA identities and their directory-managed roles
// and lock state.
func SyncIPAUsers() error {
	directoryUsers, err := ipa.ListUsers()
	if err != nil {
		return err
	}
	for _, directoryUser := range directoryUsers {
		if _, err = SyncIPAUser(directoryUser); err != nil {
			return err
		}
	}
	return nil
}

func SyncIPAUsersPeriodically(frequency int) {
	if frequency < 1 {
		frequency = 300
	}
	syncUsers := func() {
		if err := SyncIPAUsers(); err != nil {
			logger.SysError("FreeIPA user sync failed: " + err.Error())
		}
	}
	syncUsers()
	ticker := time.NewTicker(time.Duration(frequency) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		syncUsers()
	}
}

// SyncIPAUser mirrors a directory identity into the local account table so it
// can use the existing quota, token, and administration features.
func SyncIPAUser(directoryUser *ipa.User) (*User, error) {
	if directoryUser == nil || directoryUser.EntryUUID == "" || directoryUser.UID == "" {
		return nil, errors.New("FreeIPA 用户缺少稳定 ID 或 uid")
	}
	if ipa.IsBindUser(directoryUser) {
		return nil, errors.New("FreeIPA 绑定服务账号不能作为应用用户")
	}
	if !ipa.MatchesUID(directoryUser.UID) {
		return nil, errors.New("FreeIPA 用户不在配置的账号范围内")
	}
	user := &User{}
	err := DB.Where("ipa_entry_uuid = ?", directoryUser.EntryUUID).First(user).Error
	if err == nil {
		if user.Status == UserStatusDeleted {
			return user, nil
		}
		role := RoleCommonUser
		if ipa.IsRoot(directoryUser) {
			role = RoleRootUser
		} else if ipa.IsAdmin(directoryUser) {
			role = RoleAdminUser
		}
		if user.IPAUid == directoryUser.UID && user.DisplayName == directoryUser.Name &&
			user.Email == directoryUser.Email && user.IPAAccountLocked == directoryUser.Locked && user.Role == role {
			return user, nil
		}
		localUsername, err := getIPALocalUsername(directoryUser, user.Id)
		if err != nil {
			return nil, err
		}
		updates := map[string]interface{}{
			"username":           localUsername,
			"ipa_uid":            directoryUser.UID,
			"display_name":       directoryUser.Name,
			"email":              directoryUser.Email,
			"ipa_account_locked": directoryUser.Locked,
		}
		updates["role"] = role
		if err := DB.Model(&User{}).Where("id = ?", user.Id).Updates(updates).Error; err != nil {
			return nil, err
		}
		InvalidateUserEnabledCache(user.Id)
		return GetUserById(user.Id, false)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	localUsername, err := getIPALocalUsername(directoryUser, 0)
	if err != nil {
		return nil, err
	}
	role := RoleCommonUser
	if ipa.IsRoot(directoryUser) {
		role = RoleRootUser
	} else if ipa.IsAdmin(directoryUser) {
		role = RoleAdminUser
	}
	user = &User{
		Username:         localUsername,
		DisplayName:      directoryUser.Name,
		Email:            directoryUser.Email,
		Role:             role,
		Status:           UserStatusEnabled,
		IPAEntryUUID:     directoryUser.EntryUUID,
		IPAUid:           directoryUser.UID,
		IPAAccountLocked: directoryUser.Locked,
	}
	if user.DisplayName == "" {
		user.DisplayName = user.Username
	}
	if err := user.Insert(context.Background(), 0); err != nil {
		return nil, err
	}
	return GetUserById(user.Id, false)
}

func getIPALocalUsername(directoryUser *ipa.User, currentID int) (string, error) {
	entryUUIDSuffix := strings.ReplaceAll(directoryUser.EntryUUID, "-", "")
	if len(entryUUIDSuffix) > 12 {
		entryUUIDSuffix = entryUUIDSuffix[:12]
	}
	candidates := []string{directoryUser.UID, "ipa_" + directoryUser.UID, "ipa_" + entryUUIDSuffix}
	for _, candidate := range candidates {
		var existing User
		err := DB.Where("username = ?", candidate).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && existing.Id == currentID && currentID != 0) {
			return candidate, nil
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", err
		}
	}
	return "", fmt.Errorf("无法为 FreeIPA 用户 %q 生成唯一的本地映射名", directoryUser.UID)
}
