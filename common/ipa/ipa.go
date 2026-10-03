package ipa

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/songquanpeng/one-api/common/config"
)

// User contains the directory attributes needed to authenticate and provision
// a local One API account. Passwords are only sent to the LDAP bind operation.
type User struct {
	EntryUUID string
	UID       string
	Name      string
	Email     string
	DN        string
	MemberOf  []string
	Locked    bool
}

// BindUID reads the configured service identity instead of assuming an account name.
func BindUID() string {
	dn, err := ldap.ParseDN(config.IPABindDN)
	if err != nil || len(dn.RDNs) == 0 {
		return ""
	}
	for _, attribute := range dn.RDNs[0].Attributes {
		if strings.EqualFold(attribute.Type, "uid") {
			return attribute.Value
		}
	}
	return ""
}

func IsBindUser(user *User) bool {
	if user == nil || config.IPABindDN == "" {
		return false
	}
	if uid := BindUID(); uid != "" && strings.EqualFold(uid, user.UID) {
		return true
	}
	bindDN, bindErr := ldap.ParseDN(config.IPABindDN)
	userDN, userErr := ldap.ParseDN(user.DN)
	return bindErr == nil && userErr == nil && bindDN.EqualFold(userDN)
}

func usersFilter() string {
	parts := strings.Split(userMatchPattern(), "*")
	for index := range parts {
		parts[index] = ldap.EscapeFilter(parts[index])
	}
	filter := "(&(objectClass=posixAccount)(uid=" + strings.Join(parts, "*") + ")"
	if uid := BindUID(); uid != "" {
		filter += "(!(uid=" + ldap.EscapeFilter(uid) + "))"
	}
	return filter + ")"
}

func userMatchPattern() string {
	if pattern := strings.TrimSpace(config.IPAUserMatch); pattern != "" {
		return pattern
	}
	return "*"
}

// MatchesUID treats only * as a wildcard and compares case-insensitively.
func MatchesUID(uid string) bool {
	if uid == "" {
		return false
	}
	parts := strings.Split(userMatchPattern(), "*")
	for index := range parts {
		parts[index] = regexp.QuoteMeta(parts[index])
	}
	matched, _ := regexp.MatchString("(?i)^"+strings.Join(parts, ".*")+"$", uid)
	return matched
}

// UserMatchSQLPattern uses ! as the SQL LIKE escape character.
func UserMatchSQLPattern() string {
	parts := strings.Split(strings.ToLower(userMatchPattern()), "*")
	replacer := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	for index := range parts {
		parts[index] = replacer.Replace(parts[index])
	}
	return strings.Join(parts, "%")
}

func userBaseDN() string {
	if config.IPAUserBaseDN != "" {
		return config.IPAUserBaseDN
	}
	return config.IPABaseDN
}

func tlsConfig() (*tls.Config, error) {
	tlsConf := &tls.Config{MinVersion: tls.VersionTLS12}
	if config.IPACACert == "" {
		return tlsConf, nil
	}

	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("加载系统 CA 信任库失败: %w", err)
	}
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	certificate, err := os.ReadFile(config.IPACACert)
	if err != nil {
		return nil, fmt.Errorf("读取 FreeIPA CA 证书 %q 失败: %w", config.IPACACert, err)
	}
	if !rootCAs.AppendCertsFromPEM(certificate) {
		return nil, fmt.Errorf("FreeIPA CA 证书 %q 不包含有效的 PEM CA 证书", config.IPACACert)
	}
	tlsConf.RootCAs = rootCAs
	return tlsConf, nil
}

func connect() (*ldap.Conn, error) {
	if !config.IPAEnabled || config.IPAURL == "" || config.IPABaseDN == "" {
		return nil, errors.New("FreeIPA 尚未启用或配置不完整")
	}
	lowerURL := strings.ToLower(config.IPAURL)
	if !strings.HasPrefix(lowerURL, "ldaps://") && !(strings.HasPrefix(lowerURL, "ldap://") && config.IPAStartTLS) {
		return nil, errors.New("FreeIPA 必须使用 LDAPS 或启用 StartTLS")
	}
	if config.IPABindDN == "" || config.IPABindSecret == "" {
		return nil, errors.New("请配置只读 FreeIPA LDAP 绑定账号和密码")
	}
	tlsConfig, err := tlsConfig()
	if err != nil {
		return nil, err
	}
	conn, err := ldap.DialURL(config.IPAURL, ldap.DialWithTLSConfig(tlsConfig), ldap.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}))
	if err != nil {
		return nil, fmt.Errorf("连接 FreeIPA 失败: %w", err)
	}
	conn.SetTimeout(5 * time.Second)
	if config.IPAStartTLS && strings.HasPrefix(strings.ToLower(config.IPAURL), "ldap://") {
		if err = conn.StartTLS(tlsConfig); err != nil {
			conn.Close()
			return nil, fmt.Errorf("启动 LDAP TLS 失败: %w", err)
		}
	}
	if config.IPABindDN != "" {
		if err = conn.Bind(config.IPABindDN, config.IPABindSecret); err != nil {
			conn.Close()
			return nil, fmt.Errorf("FreeIPA 服务账号认证失败: %w", err)
		}
	}
	return conn, nil
}

func LookupByUID(uid string) (*User, error) {
	if uid == "" {
		return nil, errors.New("FreeIPA uid 为空")
	}
	entries, err := searchUsersByUID(uid)
	if err != nil {
		return nil, err
	}
	if len(entries) != 1 {
		return nil, errors.New("FreeIPA 用户不存在或不唯一")
	}
	user := userFromEntry(entries[0])
	if user.EntryUUID == "" {
		return nil, errors.New("FreeIPA 用户缺少 ipaUniqueID")
	}
	return user, nil
}

func searchUsers() ([]*ldap.Entry, error) {
	conn, err := connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	request := ldap.NewSearchRequest(
		userBaseDN(), ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 8, false,
		usersFilter(),
		[]string{"uid", "cn", "displayName", "mail", "ipaUniqueID", "memberOf", "nsaccountlock"}, nil,
	)
	result, err := conn.Search(request)
	if err != nil {
		return nil, fmt.Errorf("查询 FreeIPA 用户失败: %w", err)
	}
	return result.Entries, nil
}

func userFromEntry(entry *ldap.Entry) *User {
	name := entry.GetAttributeValue("displayName")
	if name == "" {
		name = entry.GetAttributeValue("cn")
	}
	if name == "" {
		name = entry.GetAttributeValue("uid")
	}
	return &User{
		EntryUUID: entry.GetAttributeValue("ipaUniqueID"),
		UID:       entry.GetAttributeValue("uid"),
		Name:      name,
		Email:     entry.GetAttributeValue("mail"),
		DN:        entry.DN,
		MemberOf:  entry.GetAttributeValues("memberOf"),
		Locked:    strings.EqualFold(entry.GetAttributeValue("nsaccountlock"), "true"),
	}
}

func ListUsers() ([]*User, error) {
	entries, err := searchUsers()
	if err != nil {
		return nil, err
	}
	users := make([]*User, 0, len(entries))
	for _, entry := range entries {
		user := userFromEntry(entry)
		if !MatchesUID(user.UID) || user.EntryUUID == "" || IsBindUser(user) {
			continue
		}
		users = append(users, user)
	}
	return users, nil
}

func Authenticate(username, password string) (*User, error) {
	if username == "" || password == "" {
		return nil, errors.New("IPA 用户名或密码错误")
	}
	entries, err := searchUsersByUID(username)
	if err != nil {
		if !MatchesUID(username) || IsBindUser(&User{UID: username}) {
			return nil, errors.New("IPA 用户名或密码错误")
		}
		return nil, errors.New("无法查询 FreeIPA，请检查网络连接和目录服务账号配置")
	}
	if len(entries) != 1 {
		return nil, errors.New("IPA 用户名或密码错误")
	}
	user := userFromEntry(entries[0])
	if user.EntryUUID == "" || user.Locked || IsBindUser(user) {
		return nil, errors.New("IPA 用户名或密码错误，或账号已被锁定")
	}
	tlsConfig, err := tlsConfig()
	if err != nil {
		return nil, errors.New("FreeIPA TLS 配置无效，请检查 CA 证书路径和格式")
	}
	conn, err := ldap.DialURL(config.IPAURL, ldap.DialWithTLSConfig(tlsConfig), ldap.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}))
	if err != nil {
		return nil, errors.New("无法连接 FreeIPA，请稍后重试")
	}
	defer conn.Close()
	conn.SetTimeout(5 * time.Second)
	if config.IPAStartTLS && strings.HasPrefix(strings.ToLower(config.IPAURL), "ldap://") {
		if err = conn.StartTLS(tlsConfig); err != nil {
			return nil, errors.New("无法连接 FreeIPA，请稍后重试")
		}
	}
	if err = conn.Bind(user.DN, password); err != nil {
		return nil, errors.New("IPA 用户名或密码错误")
	}
	return user, nil
}

func searchUsersByUID(uid string) ([]*ldap.Entry, error) {
	if !MatchesUID(uid) {
		return nil, errors.New("FreeIPA 用户不在配置的账号范围内")
	}
	if IsBindUser(&User{UID: uid}) {
		return nil, errors.New("FreeIPA 绑定服务账号不能作为应用用户")
	}
	conn, err := connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	request := ldap.NewSearchRequest(
		userBaseDN(), ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 8, false,
		"(&(objectClass=posixAccount)(uid="+ldap.EscapeFilter(uid)+"))",
		[]string{"uid", "cn", "displayName", "mail", "ipaUniqueID", "memberOf", "nsaccountlock"}, nil,
	)
	result, err := conn.Search(request)
	if err != nil {
		return nil, err
	}
	return result.Entries, nil
}

func IsAdmin(user *User) bool {
	if user == nil {
		return false
	}
	for _, groupDN := range user.MemberOf {
		if (config.IPAAdminGroupDN != "" && strings.EqualFold(groupDN, config.IPAAdminGroupDN)) ||
			(config.IPARootGroupDN != "" && strings.EqualFold(groupDN, config.IPARootGroupDN)) {
			return true
		}
	}
	return false
}

func IsRoot(user *User) bool {
	if user == nil || config.IPARootGroupDN == "" {
		return false
	}
	for _, groupDN := range user.MemberOf {
		if strings.EqualFold(groupDN, config.IPARootGroupDN) {
			return true
		}
	}
	return false
}
