package pgschema_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"wscyber/native/pgschema"
)

const fakePassword = "s3cr3t-pw-should-not-leak"

var fakeDSN = "postgres://wscyber_app:" + fakePassword + "@db.example.internal:6543/wscyber_prod?sslmode=disable"

func writeFile(t *testing.T, mode os.FileMode, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.url")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func noEnv(string) string { return "" }

func TestDatabaseURLFileModes(t *testing.T) {
	for _, tc := range []struct {
		mode os.FileMode
		ok   bool
	}{
		{0o600, true}, {0o400, true}, {0o700, true},
		{0o644, false}, {0o640, false}, {0o604, false}, {0o660, false}, {0o610, false}, {0o601, false}, {0o200, false}, {0o000, false},
	} {
		t.Run(fmt.Sprintf("%04o", tc.mode), func(t *testing.T) {
			path := writeFile(t, tc.mode, fakeDSN+"\n")
			u, err := pgschema.ResolveDatabaseURL(path, noEnv)
			if tc.ok {
				if err != nil {
					t.Fatalf("期望接受，得到 %v", err)
				}
				if u.DSN() != fakeDSN || u.Host() != "db.example.internal" || u.Port() != 6543 || u.Database() != "wscyber_prod" || u.User() != "wscyber_app" {
					t.Fatalf("解析结果不对: %s", u)
				}
				if u.Source() != "--database-url-file" {
					t.Fatalf("来源 %q", u.Source())
				}
				return
			}
			if err == nil {
				t.Fatalf("期望拒绝 %04o", tc.mode)
			}
			if strings.Contains(err.Error(), fakePassword) {
				t.Fatal("错误信息泄露口令")
			}
		})
	}
}

func TestDatabaseURLFileRejectsSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, 0o600, fakeDSN)
	link := filepath.Join(dir, "link.url")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := pgschema.ReadDatabaseURLFile(link); err == nil || !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("符号链接应被拒绝: %v", err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := pgschema.ReadDatabaseURLFile(sub); err == nil || !strings.Contains(err.Error(), "普通文件") {
		t.Fatalf("目录应被拒绝: %v", err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pgschema.ReadDatabaseURLFile(fifo); err == nil || !strings.Contains(err.Error(), "普通文件") {
		t.Fatalf("FIFO 应被拒绝: %v", err)
	}
	if _, err := pgschema.ReadDatabaseURLFile(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("不存在的文件应报错")
	}
}

func TestDatabaseURLFileContent(t *testing.T) {
	for name, content := range map[string]string{"空": "", "空白": " \n\t\n", "多行": fakeDSN + "\n" + fakeDSN + "\n"} {
		t.Run(name, func(t *testing.T) {
			path := writeFile(t, 0o600, content)
			if _, err := pgschema.ResolveDatabaseURL(path, noEnv); err == nil {
				t.Fatal("应拒绝")
			}
		})
	}
	path := writeFile(t, 0o600, "postgres://u:"+fakePassword+"@host:notaport/db")
	_, err := pgschema.ResolveDatabaseURL(path, noEnv)
	if err == nil || strings.Contains(err.Error(), fakePassword) || strings.Contains(err.Error(), "notaport") {
		t.Fatalf("无法解析的连接串应报错且不回显原文: %v", err)
	}
}

func TestResolveDatabaseURLPriority(t *testing.T) {
	envDSN := "host=127.0.0.1 port=5432 dbname=from_env user=env_user password=" + fakePassword
	env := func(key string) string {
		if key == pgschema.EnvDatabaseURL {
			return envDSN
		}
		return ""
	}
	path := writeFile(t, 0o600, fakeDSN)
	u, err := pgschema.ResolveDatabaseURL(path, env)
	if err != nil || u.Database() != "wscyber_prod" {
		t.Fatalf("文件应优先于环境变量: %v %s", err, u)
	}
	// 文件给了但不合格时不得静默回退到环境变量。
	bad := writeFile(t, 0o644, fakeDSN)
	if _, err := pgschema.ResolveDatabaseURL(bad, env); err == nil {
		t.Fatal("文件权限不合格时应失败，不回退环境变量")
	}
	u, err = pgschema.ResolveDatabaseURL("", env)
	if err != nil || u.Database() != "from_env" || u.Source() != pgschema.EnvDatabaseURL {
		t.Fatalf("应回退环境变量: %v %s", err, u)
	}
	_, err = pgschema.ResolveDatabaseURL("", noEnv)
	if !errors.Is(err, pgschema.ErrNoDatabaseURL) || !strings.Contains(err.Error(), "--database-url-file") ||
		!strings.Contains(err.Error(), pgschema.EnvDatabaseURL) || !strings.Contains(err.Error(), pgschema.EnvDatabaseURLFile) {
		t.Fatalf("都没有时应说明三种方式: %v", err)
	}
	// WSCYBER_DATABASE_URL_FILE 位于旗标之后、WSCYBER_DATABASE_URL 之前；不合格时不回退。
	envFile := writeFile(t, 0o600, fakeDSN)
	both := func(key string) string {
		switch key {
		case pgschema.EnvDatabaseURLFile:
			return envFile
		case pgschema.EnvDatabaseURL:
			return envDSN
		}
		return ""
	}
	u, err = pgschema.ResolveDatabaseURL("", both)
	if err != nil || u.Database() != "wscyber_prod" || u.Source() != pgschema.EnvDatabaseURLFile {
		t.Fatalf("环境变量文件应优先于环境变量连接串: %v %s", err, u)
	}
	if err := os.Chmod(envFile, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := pgschema.ResolveDatabaseURL("", both); err == nil {
		t.Fatal("环境变量文件不合格时应失败，不回退")
	}
}

func TestDatabaseURLNeverPrintsPassword(t *testing.T) {
	for _, dsn := range []string{fakeDSN, "host=h port=1 dbname=d user=u password=" + fakePassword} {
		u, err := pgschema.ParseDatabaseURL(dsn)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{u.String(), u.GoString(), fmt.Sprintf("%v", u), fmt.Sprintf("%+v", u), fmt.Sprintf("%#v", u), fmt.Sprint(u), fmt.Sprintf("%s", &u)} {
			if strings.Contains(s, fakePassword) {
				t.Fatalf("输出泄露口令: %s", s)
			}
		}
		if !strings.Contains(u.String(), "host=") || !strings.Contains(u.String(), "user=") {
			t.Fatalf("应包含 host/user: %s", u)
		}
	}
}

func TestDatabaseURLWithDatabase(t *testing.T) {
	for _, dsn := range []string{fakeDSN, "host=h port=1 dbname=d user=u password='" + fakePassword + "'"} {
		u, err := pgschema.ParseDatabaseURL(dsn)
		if err != nil {
			t.Fatal(err)
		}
		other, err := u.WithDatabase("wscyber_demo")
		if err != nil {
			t.Fatal(err)
		}
		if other.Database() != "wscyber_demo" || other.Host() != u.Host() || other.User() != u.User() || other.Port() != u.Port() {
			t.Fatalf("换库名失败: %s → %s", u, other)
		}
		if !strings.Contains(other.DSN(), fakePassword) {
			t.Fatal("换库名后应保留口令")
		}
	}
}
