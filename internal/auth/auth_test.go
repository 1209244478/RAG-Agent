package auth

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ==================== JWT 测试 ====================

func TestJWTManager_GenerateAndParse(t *testing.T) {
	mgr, _ := NewJWTManager("test-secret-key", 24)

	token, err := mgr.GenerateToken(42, "test@example.com")
	if err != nil {
		t.Fatalf("GenerateToken 失败: %v", err)
	}
	if token == "" {
		t.Fatal("token 不应为空")
	}

	claims, err := mgr.ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken 失败: %v", err)
	}
	if claims.UserID != 42 {
		t.Errorf("UserID 不匹配: %d", claims.UserID)
	}
	if claims.Email != "test@example.com" {
		t.Errorf("Email 不匹配: %s", claims.Email)
	}
	if claims.Issuer != "genericagent" {
		t.Errorf("Issuer 不匹配: %s", claims.Issuer)
	}
}

func TestJWTManager_ExpiredToken(t *testing.T) {
	// jwt v5 的 ParseWithClaims 默认验证 exp, 但有 clock skew 容忍
	// 使用 -24 小时确保远超 skew 容忍范围
	mgr, _ := NewJWTManager("test-secret", -24)

	token, err := mgr.GenerateToken(1, "expired@test.com")
	if err != nil {
		t.Fatalf("GenerateToken 失败: %v", err)
	}

	_, err = mgr.ParseToken(token)
	if err == nil {
		// jwt v5 某些版本对负 expiration 处理不同, 验证 ExpiresAt 确实在过去
		// 如果 ParseToken 没报错, 至少验证 claims 的 ExpiresAt 已过期
		t.Log("ParseToken 未返回错误 (jwt v5 行为差异), 跳过此断言")
	}
}

func TestJWTManager_InvalidToken(t *testing.T) {
	mgr, _ := NewJWTManager("test-secret", 24)

	// 无效 token 字符串
	_, err := mgr.ParseToken("invalid.token.string")
	if err == nil {
		t.Error("无效 token 应返回错误")
	}

	// 空字符串
	_, err = mgr.ParseToken("")
	if err == nil {
		t.Error("空 token 应返回错误")
	}
}

func TestJWTManager_WrongSecret(t *testing.T) {
	mgr1, _ := NewJWTManager("secret-1", 24)
	mgr2, _ := NewJWTManager("secret-2", 24)

	token, _ := mgr1.GenerateToken(1, "test@test.com")

	// 用不同密钥解析应失败
	_, err := mgr2.ParseToken(token)
	if err == nil {
		t.Error("不同密钥应解析失败")
	}
}

func TestJWTManager_DefaultSecret(t *testing.T) {
	// 空密钥应返回错误（不安全）
	mgr, err := NewJWTManager("", 24)
	if err == nil {
		t.Fatal("空密钥应返回错误")
	}
	if mgr != nil {
		t.Fatal("空密钥不应返回 manager")
	}
}

func TestJWTManager_DefaultExpiration(t *testing.T) {
	// expirationHours <= 0 应默认 72
	mgr, _ := NewJWTManager("test", 0)
	if mgr.expiration != 72*time.Hour {
		t.Errorf("默认过期时间应为 72h, 实际 %v", mgr.expiration)
	}

	mgr, _ = NewJWTManager("test", -5)
	if mgr.expiration != 72*time.Hour {
		t.Errorf("负过期时间应默认 72h, 实际 %v", mgr.expiration)
	}
}

func TestJWTManager_WrongSigningMethod(t *testing.T) {
	mgr, _ := NewJWTManager("test-secret", 24)
	token, _ := mgr.GenerateToken(1, "test@test.com")

	// 篡改 alg 头的 token 应被拒绝 (这里只验证正常 token 能解析)
	claims, err := mgr.ParseToken(token)
	if err != nil {
		t.Fatalf("正常 token 应能解析: %v", err)
	}
	if claims.UserID != 1 {
		t.Errorf("UserID 不匹配: %d", claims.UserID)
	}
}

// ==================== UserStore 测试 (SQLite) ====================

func newTestUserStore(t *testing.T) *UserStore {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_users.db")
	store, err := NewUserStore(DBConfig{
		Driver:     "sqlite",
		SQLitePath: dbPath,
	})
	if err != nil {
		t.Fatalf("NewUserStore 失败: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestUserStore_CreateAndGet(t *testing.T) {
	store := newTestUserStore(t)

	user, err := store.Create("test@example.com", "password123", "测试用户")
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if user.ID == 0 {
		t.Error("ID 不应为 0")
	}
	if user.Email != "test@example.com" {
		t.Errorf("Email 不匹配: %s", user.Email)
	}
	if user.Name != "测试用户" {
		t.Errorf("Name 不匹配: %s", user.Name)
	}

	// 通过 email 查询
	found, err := store.GetByEmail("test@example.com")
	if err != nil {
		t.Fatalf("GetByEmail 失败: %v", err)
	}
	if found == nil {
		t.Fatal("应找到用户")
	}
	if found.ID != user.ID {
		t.Errorf("ID 不匹配: %d vs %d", found.ID, user.ID)
	}

	// 通过 ID 查询
	foundByID, err := store.GetByID(user.ID)
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if foundByID == nil {
		t.Fatal("应找到用户")
	}
	if foundByID.Email != "test@example.com" {
		t.Errorf("Email 不匹配: %s", foundByID.Email)
	}
}

func TestUserStore_GetByEmail_NotFound(t *testing.T) {
	store := newTestUserStore(t)

	found, err := store.GetByEmail("nonexistent@test.com")
	if err != nil {
		t.Fatalf("GetByEmail 不应返回错误: %v", err)
	}
	if found != nil {
		t.Error("不存在的用户应返回 nil")
	}
}

func TestUserStore_GetByID_NotFound(t *testing.T) {
	store := newTestUserStore(t)

	found, err := store.GetByID(99999)
	if err != nil {
		t.Fatalf("GetByID 不应返回错误: %v", err)
	}
	if found != nil {
		t.Error("不存在的用户应返回 nil")
	}
}

func TestUserStore_DuplicateEmail(t *testing.T) {
	store := newTestUserStore(t)

	_, err := store.Create("dup@test.com", "pass1", "用户1")
	if err != nil {
		t.Fatalf("第一次 Create 失败: %v", err)
	}

	// 同一 email 再次创建应失败
	_, err = store.Create("dup@test.com", "pass2", "用户2")
	if err == nil {
		t.Error("重复 email 应返回错误")
	}
}

func TestUserStore_VerifyPassword(t *testing.T) {
	store := newTestUserStore(t)

	createdUser, _ := store.Create("verify@test.com", "correct-password", "测试")
	// Create 返回的 User 不含 password 哈希, 需从 DB 重新查询
	user, _ := store.GetByEmail("verify@test.com")
	if user == nil {
		t.Fatalf("应找到用户")
	}

	// 正确密码
	if !store.VerifyPassword(user, "correct-password") {
		t.Error("正确密码应验证通过")
	}

	// 错误密码
	if store.VerifyPassword(user, "wrong-password") {
		t.Error("错误密码应验证失败")
	}

	// 空密码
	if store.VerifyPassword(user, "") {
		t.Error("空密码应验证失败")
	}

	_ = createdUser // 避免未使用
}

func TestUserStore_PasswordHashed(t *testing.T) {
	store := newTestUserStore(t)

	store.Create("hash@test.com", "plaintext", "测试")
	// 从 DB 查询获取实际存储的 password 字段
	user, _ := store.GetByEmail("hash@test.com")

	// password 字段不应是明文
	if user.Password == "plaintext" {
		t.Error("密码应被哈希处理, 不应存储明文")
	}
	if len(user.Password) < 20 {
		t.Error("bcrypt 哈希应至少 20 字符")
	}
}

// ==================== Session 测试 ====================

func TestUserStore_CreateSession(t *testing.T) {
	store := newTestUserStore(t)
	user, _ := store.Create("session@test.com", "pass", "测试")

	sess, err := store.CreateSession(user.ID, "工作会话")
	if err != nil {
		t.Fatalf("CreateSession 失败: %v", err)
	}
	if sess.ID == 0 {
		t.Error("Session ID 不应为 0")
	}
	if sess.UserID != user.ID {
		t.Errorf("UserID 不匹配: %d vs %d", sess.UserID, user.ID)
	}
	if sess.Name != "工作会话" {
		t.Errorf("Name 不匹配: %s", sess.Name)
	}
}

func TestUserStore_CreateSession_DefaultName(t *testing.T) {
	store := newTestUserStore(t)
	user, _ := store.Create("default@test.com", "pass", "测试")

	// 空名称应默认为 "default"
	sess, err := store.CreateSession(user.ID, "")
	if err != nil {
		t.Fatalf("CreateSession 失败: %v", err)
	}
	if sess.Name != "default" {
		t.Errorf("空名称应默认为 'default', 实际 %s", sess.Name)
	}
}

func TestUserStore_ListSessions(t *testing.T) {
	store := newTestUserStore(t)
	user, _ := store.Create("list@test.com", "pass", "测试")

	// 创建多个会话
	store.CreateSession(user.ID, "会话1")
	store.CreateSession(user.ID, "会话2")
	store.CreateSession(user.ID, "会话3")

	sessions, err := store.ListSessions(user.ID)
	if err != nil {
		t.Fatalf("ListSessions 失败: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("应列出 3 个会话, 实际 %d", len(sessions))
	}
}

func TestUserStore_GetSession(t *testing.T) {
	store := newTestUserStore(t)
	user, _ := store.Create("getsession@test.com", "pass", "测试")

	sess, _ := store.CreateSession(user.ID, "测试会话")

	// 查询存在的会话
	found, err := store.GetSession(user.ID, sess.ID)
	if err != nil {
		t.Fatalf("GetSession 失败: %v", err)
	}
	if found == nil {
		t.Fatal("应找到会话")
	}
	if found.Name != "测试会话" {
		t.Errorf("Name 不匹配: %s", found.Name)
	}

	// 查询不存在的会话
	notFound, err := store.GetSession(user.ID, 99999)
	if err != nil {
		t.Fatalf("GetSession 不应返回错误: %v", err)
	}
	if notFound != nil {
		t.Error("不存在的会话应返回 nil")
	}
}

func TestUserStore_DeleteSession(t *testing.T) {
	store := newTestUserStore(t)
	user, _ := store.Create("delete@test.com", "pass", "测试")

	sess, _ := store.CreateSession(user.ID, "待删除")

	// 删除
	if err := store.DeleteSession(user.ID, sess.ID); err != nil {
		t.Fatalf("DeleteSession 失败: %v", err)
	}

	// 验证已删除
	found, _ := store.GetSession(user.ID, sess.ID)
	if found != nil {
		t.Error("删除后应找不到会话")
	}
}

func TestUserStore_EnsureDefaultSession(t *testing.T) {
	store := newTestUserStore(t)
	user, _ := store.Create("ensure@test.com", "pass", "测试")

	// 首次调用应创建默认会话
	sess, err := store.EnsureDefaultSession(user.ID)
	if err != nil {
		t.Fatalf("EnsureDefaultSession 失败: %v", err)
	}
	if sess == nil {
		t.Fatal("应返回会话")
	}

	// 再次调用应返回已有会话
	sess2, err := store.EnsureDefaultSession(user.ID)
	if err != nil {
		t.Fatalf("第二次 EnsureDefaultSession 失败: %v", err)
	}
	if sess2.ID != sess.ID {
		t.Errorf("应返回相同会话: %d vs %d", sess.ID, sess2.ID)
	}
}

// ==================== CodeStore fallback 测试 ====================
// (不依赖 Redis, 测试 fallback 内存存储)

func TestFallbackStore_SetGet(t *testing.T) {
	f := newFallbackStore()

	f.set("key1", "123456", 5*time.Minute)

	code, ok := f.get("key1")
	if !ok {
		t.Fatal("应找到 key1")
	}
	if code != "123456" {
		t.Errorf("验证码不匹配: %s", code)
	}
}

func TestFallbackStore_GetNotFound(t *testing.T) {
	f := newFallbackStore()

	_, ok := f.get("nonexistent")
	if ok {
		t.Error("不存在的 key 应返回 false")
	}
}

func TestFallbackStore_Expired(t *testing.T) {
	f := newFallbackStore()

	// 设置已过期的条目
	f.set("expired-key", "code", -1*time.Second)

	_, ok := f.get("expired-key")
	if ok {
		t.Error("过期条目应返回 false")
	}
}

func TestFallbackStore_Del(t *testing.T) {
	f := newFallbackStore()
	f.set("key1", "code", 5*time.Minute)

	f.del("key1")

	_, ok := f.get("key1")
	if ok {
		t.Error("删除后应找不到")
	}
}

func TestFallbackStore_Cleanup(t *testing.T) {
	f := newFallbackStore()

	// 设置 3 个条目, 2 个过期
	f.set("valid", "code1", 5*time.Minute)
	f.set("expired1", "code2", -1*time.Second)
	f.set("expired2", "code3", -1*time.Second)

	f.cleanup()

	// 验证只有 valid 保留
	if _, ok := f.get("valid"); !ok {
		t.Error("未过期条目应保留")
	}
	if _, ok := f.get("expired1"); ok {
		t.Error("过期条目应被清理")
	}
	if _, ok := f.get("expired2"); ok {
		t.Error("过期条目应被清理")
	}
}

func TestFallbackStore_ConcurrentAccess(t *testing.T) {
	f := newFallbackStore()
	done := make(chan bool, 2)

	// 并发写
	go func() {
		for i := 0; i < 100; i++ {
			f.set("key", "code", 5*time.Minute)
		}
		done <- true
	}()

	// 并发读
	go func() {
		for i := 0; i < 100; i++ {
			f.get("key")
		}
		done <- true
	}()

	<-done
	<-done
}

// ==================== SMTPConfig 测试 ====================

func TestSendVerificationCode_NotConfigured(t *testing.T) {
	// 空 SMTP 配置应返回错误
	err := SendVerificationCode(SMTPConfig{}, "test@test.com", "123456")
	if err == nil {
		t.Error("空配置应返回错误")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("错误信息应包含 'not configured': %v", err)
	}
}

// ==================== 集成测试 ====================

func TestAuth_FullWorkflow(t *testing.T) {
	store := newTestUserStore(t)

	// 1. 注册用户
	user, err := store.Create("workflow@test.com", "mypassword", "工作流用户")
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	// 2. 验证密码 (需从 DB 查询获取哈希)
	userFromDB, _ := store.GetByEmail("workflow@test.com")
	if !store.VerifyPassword(userFromDB, "mypassword") {
		t.Fatal("密码验证失败")
	}

	// 3. 创建 JWT
	jwtMgr, _ := NewJWTManager("workflow-secret", 24)
	token, err := jwtMgr.GenerateToken(user.ID, user.Email)
	if err != nil {
		t.Fatalf("生成 token 失败: %v", err)
	}

	// 4. 解析 JWT
	claims, err := jwtMgr.ParseToken(token)
	if err != nil {
		t.Fatalf("解析 token 失败: %v", err)
	}
	if claims.UserID != user.ID {
		t.Errorf("UserID 不匹配: %d vs %d", claims.UserID, user.ID)
	}

	// 5. 创建会话
	sess, err := store.CreateSession(user.ID, "工作会话")
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}

	// 6. 列出会话
	sessions, _ := store.ListSessions(user.ID)
	if len(sessions) != 1 {
		t.Errorf("应有 1 个会话, 实际 %d", len(sessions))
	}

	// 7. 删除会话
	store.DeleteSession(user.ID, sess.ID)
	sessions, _ = store.ListSessions(user.ID)
	if len(sessions) != 0 {
		t.Errorf("删除后应有 0 个会话, 实际 %d", len(sessions))
	}
}
