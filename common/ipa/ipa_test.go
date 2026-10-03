package ipa

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/songquanpeng/one-api/common/config"
)

func TestBindAccountExclusion(t *testing.T) {
	previous := config.IPABindDN
	previousMatch := config.IPAUserMatch
	config.IPAUserMatch = "*"
	config.IPABindDN = "UID=app_bind_oneapi,cn=users,dc=example,dc=com"
	t.Cleanup(func() { config.IPABindDN, config.IPAUserMatch = previous, previousMatch })
	for _, test := range []struct {
		name string
		user *User
		want bool
	}{
		{"configured_uid", &User{UID: "app_bind_oneapi"}, true},
		{"case_insensitive_uid", &User{UID: "APP_BIND_ONEAPI"}, true},
		{"configured_dn", &User{DN: "uid=app_bind_oneapi,cn=users,dc=example,dc=com"}, true},
		{"normal_user", &User{UID: "person", DN: "uid=person,cn=users,dc=example,dc=com"}, false},
		{"ipa_admin", &User{UID: "admin"}, false},
		{"nil_user", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsBindUser(test.user); got != test.want {
				t.Fatalf("IsBindUser = %v, want %v", got, test.want)
			}
		})
	}
	if _, err := searchUsersByUID("app_bind_oneapi"); err == nil {
		t.Fatal("service account lookup must be rejected before LDAP access")
	}
	if _, err := Authenticate("app_bind_oneapi", "unused-test-password"); err == nil {
		t.Fatal("service account login must be rejected")
	}
	config.IPABindDN = "uid=app(bind)*,cn=users,dc=example,dc=com"
	if !strings.Contains(usersFilter(), `(!(uid=app\28bind\29\2a))`) {
		t.Fatalf("bind UID must be escaped in LDAP filter: %s", usersFilter())
	}
}

func TestIPAUserMatch(t *testing.T) {
	previousMatch, previousBind := config.IPAUserMatch, config.IPABindDN
	config.IPABindDN = ""
	t.Cleanup(func() { config.IPAUserMatch, config.IPABindDN = previousMatch, previousBind })
	for _, test := range []struct {
		pattern, uid string
		want         bool
	}{
		{"*", "admin", true}, {"h*", "h320001", true}, {"h*", "H320001", true},
		{"h*", "admin", false}, {"h*", "otherh320001", false},
		{"h320001", "h320001", true}, {"h320001", "h320001x", false},
		{"*001", "h320001", true}, {"h*001", "h320001", true},
		{"h_*", "h_100", true}, {"h_*", "hX100", false},
		{"h%*", "h%100", true}, {"h%*", "h100", false},
		{"h?*", "h100", false}, {"", "admin", true}, {"*", "", false},
	} {
		t.Run(test.pattern+"/"+test.uid, func(t *testing.T) {
			config.IPAUserMatch = test.pattern
			if got := MatchesUID(test.uid); got != test.want {
				t.Fatalf("MatchesUID = %v, want %v", got, test.want)
			}
		})
	}
	config.IPAUserMatch = "h*"
	if usersFilter() != "(&(objectClass=posixAccount)(uid=h*))" {
		t.Fatalf("unexpected prefix LDAP filter: %s", usersFilter())
	}
	if _, err := searchUsersByUID("admin"); err == nil {
		t.Fatal("out-of-scope lookup must fail before LDAP access")
	}
	config.IPAUserMatch = "h*)(uid=*)"
	if !strings.Contains(usersFilter(), `(uid=h*\29\28uid=*\29)`) {
		t.Fatal("pattern literals must not inject LDAP filter clauses")
	}
}

func TestAuthenticateReportsDirectoryFailure(t *testing.T) {
	previousEnabled, previousURL, previousBase := config.IPAEnabled, config.IPAURL, config.IPABaseDN
	previousBind, previousSecret, previousCA, previousMatch := config.IPABindDN, config.IPABindSecret, config.IPACACert, config.IPAUserMatch
	t.Cleanup(func() {
		config.IPAEnabled, config.IPAURL, config.IPABaseDN = previousEnabled, previousURL, previousBase
		config.IPABindDN, config.IPABindSecret, config.IPACACert, config.IPAUserMatch = previousBind, previousSecret, previousCA, previousMatch
	})
	config.IPAEnabled, config.IPAURL, config.IPABaseDN = true, "ldaps://127.0.0.1:1", "dc=example,dc=com"
	config.IPABindDN, config.IPABindSecret = "uid=reader,cn=users,dc=example,dc=com", "unused-test-secret"
	config.IPACACert, config.IPAUserMatch = filepath.Join(t.TempDir(), "missing-ca.crt"), "*"
	_, err := Authenticate("person", "unused-test-password")
	if err == nil || !strings.Contains(err.Error(), "无法查询 FreeIPA") {
		t.Fatalf("directory failure must be distinguishable from a user password error: %v", err)
	}
}

func TestTLSConfigUsesSystemTrustWhenCACertIsUnset(t *testing.T) {
	previous := config.IPACACert
	config.IPACACert = ""
	t.Cleanup(func() { config.IPACACert = previous })

	tlsConfig, err := tlsConfig()
	if err != nil {
		t.Fatalf("tlsConfig() error = %v", err)
	}
	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %d, want TLS 1.2 or newer", tlsConfig.MinVersion)
	}
	if tlsConfig.RootCAs != nil {
		t.Fatal("RootCAs should remain nil to use the system trust store")
	}
}

func TestTLSConfigAppendsConfiguredCACert(t *testing.T) {
	certificatePath := filepath.Join(t.TempDir(), "ipa-ca.crt")
	if err := os.WriteFile(certificatePath, makeTestCACertificate(t), 0600); err != nil {
		t.Fatal(err)
	}
	previous := config.IPACACert
	config.IPACACert = certificatePath
	t.Cleanup(func() { config.IPACACert = previous })

	tlsConfig, err := tlsConfig()
	if err != nil {
		t.Fatalf("tlsConfig() error = %v", err)
	}
	if tlsConfig.RootCAs == nil {
		t.Fatal("RootCAs is nil; configured CA was not loaded")
	}
	if len(tlsConfig.RootCAs.Subjects()) == 0 {
		t.Fatal("RootCAs has no subjects after loading configured CA")
	}
}

func TestTLSConfigRejectsMissingOrInvalidCACert(t *testing.T) {
	previous := config.IPACACert
	t.Cleanup(func() { config.IPACACert = previous })

	config.IPACACert = filepath.Join(t.TempDir(), "missing-ca.crt")
	if _, err := tlsConfig(); err == nil {
		t.Fatal("tlsConfig() succeeded with a missing CA file")
	}

	invalidPath := filepath.Join(t.TempDir(), "invalid-ca.crt")
	if err := os.WriteFile(invalidPath, []byte("not a PEM certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	config.IPACACert = invalidPath
	if _, err := tlsConfig(); err == nil {
		t.Fatal("tlsConfig() succeeded with invalid PEM data")
	}
}

func makeTestCACertificate(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate})
}
