package usermanager

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
)

// mockForwardManager is a mock implementation of forward.ForwardManager.
type mockForwardManager struct {
	rules      map[string]*forward.ForwardRule
	connLimits map[string]int
}

func newMockForwardManager() *mockForwardManager {
	return &mockForwardManager{
		rules:      make(map[string]*forward.ForwardRule),
		connLimits: make(map[string]int),
	}
}

func (m *mockForwardManager) AddRule(rule forward.ForwardRule) (*forward.ForwardRule, error) {
	// Auto-allocate port if not specified
	if rule.ListenPort == 0 {
		rule.ListenPort = uint32(10000 + len(m.rules) + 1)
	}
	m.rules[rule.Username] = &rule
	return &rule, nil
}

func (m *mockForwardManager) RemoveRule(ruleKey string) error {
	delete(m.rules, ruleKey)
	return nil
}

func (m *mockForwardManager) RemoveRulesByInbound(inboundTag string) error {
	return nil
}

func (m *mockForwardManager) RemoveRulesByUser(userEmail string) error {
	// Remove rules that match the username
	keysToRemove := make([]string, 0)
	for key, rule := range m.rules {
		if rule.Username == userEmail {
			keysToRemove = append(keysToRemove, key)
		}
	}
	for _, key := range keysToRemove {
		delete(m.rules, key)
	}
	return nil
}

func (m *mockForwardManager) GetRule(ruleKey string) *forward.ForwardRule {
	return m.rules[ruleKey]
}

func (m *mockForwardManager) GetRulesByInbound(inboundTag string) []*forward.ForwardRule {
	return nil
}

func (m *mockForwardManager) GetRulesByUser(userEmail string) []*forward.ForwardRule {
	var result []*forward.ForwardRule
	for _, rule := range m.rules {
		if rule.Username == userEmail {
			result = append(result, rule)
		}
	}
	return result
}

func (m *mockForwardManager) GetAllRules() []*forward.ForwardRule {
	result := make([]*forward.ForwardRule, 0, len(m.rules))
	for _, rule := range m.rules {
		result = append(result, rule)
	}
	return result
}

func (m *mockForwardManager) GetTraffic(ruleKey string, reset bool) (*forward.TrafficSnapshot, error) {
	return nil, nil
}

func (m *mockForwardManager) GetAllTraffic(reset bool) *forward.ForwardManagerStats {
	return nil
}

func (m *mockForwardManager) GetAllTrafficRecords(reset bool) []forward.ForwardTrafficRecord {
	// Return empty records by default
	return nil
}

func (m *mockForwardManager) QueryTrafficStats(query forward.TrafficQuery) forward.TrafficQueryResult {
	return forward.TrafficQueryResult{}
}

func (m *mockForwardManager) UpdateRateLimit(ruleKey string, uploadBPS, downloadBPS int64) error {
	return nil
}

func (m *mockForwardManager) SetUserBandwidthLimit(username string, kind forward.BandwidthLimitKind, bytesPerSec int64) error {
	return nil
}

func (m *mockForwardManager) GetUserBandwidthLimit(username string, kind forward.BandwidthLimitKind) (int64, bool) {
	return 0, false
}

func (m *mockForwardManager) SetUserConnectionLimit(username string, limit int) error {
	m.connLimits[username] = limit
	return nil
}

func (m *mockForwardManager) GetUserConnectionLimit(username string) (int, bool) {
	v, ok := m.connLimits[username]
	return v, ok
}

func (m *mockForwardManager) SetUserClientLimitConfig(username string, config forward.ClientLimitConfig) error {
	return nil
}

func (m *mockForwardManager) GetUserClientLimitConfig(username string) (forward.ClientLimitConfig, bool) {
	return forward.ClientLimitConfig{}, false
}

func (m *mockForwardManager) AllocatePort() (uint32, error) {
	return 0, nil
}

func (m *mockForwardManager) ReleasePort(port uint32) {}

func (m *mockForwardManager) Close() error {
	return nil
}

func (m *mockForwardManager) DropUser(username string) bool {
	return false
}

func TestNewUserManager(t *testing.T) {
	um := NewUserManager(nil, "test-node")
	if um == nil {
		t.Error("NewUserManager returned nil")
	}
}

func TestAddUser_Success(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
		TTL:      time.Hour,
	}

	err := um.AddUser(req)
	if err != nil {
		t.Errorf("AddUser returned error: %v", err)
	}

	// Verify user was added
	user, err := um.GetUser("testuser")
	if err != nil {
		t.Errorf("GetUser returned error: %v", err)
	}
	if user.Username != "testuser" {
		t.Errorf("expected username testuser, got %s", user.Username)
	}
	if user.AuthToken == "" {
		t.Error("expected non-empty AuthToken after AddUser")
	}
}

func TestAddUser_ValidationError(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// User with empty username
	req := AddUserRequest{
		Username: "",
		Password: "testpass",
	}

	err := um.AddUser(req)
	if err == nil {
		t.Error("expected error for empty username")
	}
}

func TestAddUser_AlreadyExists(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}

	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("first AddUser failed: %v", err)
	}

	// Try to add same user again
	err = um.AddUser(req)
	if err == nil {
		t.Error("expected error when adding duplicate user")
	}
}

func TestAddUser_DeletingUserRejected(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user first
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Add a forward rule so the user won't be finalized immediately on RemoveUser
	mockFM.rules["testuser"] = &forward.ForwardRule{Username: "testuser", ListenPort: 12345}

	// Mark user for deletion (soft delete)
	err = um.RemoveUser(RemoveUserRequest{Username: "testuser"})
	if err != nil {
		t.Fatalf("RemoveUser failed: %v", err)
	}

	// Try to add the same user again - should be rejected with deletion in progress error
	err = um.AddUser(req)
	if err == nil {
		t.Error("expected error when adding user in deleting state")
	}

	// Verify the error mentions deletion in progress
	if err != nil && !strings.Contains(err.Error(), "being deleted") {
		t.Errorf("expected error to mention deletion in progress, got: %v", err)
	}
}

func TestRemoveUser_Success(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user first
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Remove the user
	removeReq := RemoveUserRequest{
		Username: "testuser",
	}
	err = um.RemoveUser(removeReq)
	if err != nil {
		t.Errorf("RemoveUser returned error: %v", err)
	}

	// Verify user was removed
	_, err = um.GetUser("testuser")
	if err == nil {
		t.Error("expected error when getting removed user")
	}
}

func TestRemoveUser_Idempotent(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	removeReq := RemoveUserRequest{
		Username: "nonexistent",
	}
	// Calling remove on nonexistent user should not error (idempotent)
	err := um.RemoveUser(removeReq)
	if err != nil {
		t.Errorf("RemoveUser should be idempotent: %v", err)
	}
}

func TestRemoveUser_SoftDelete(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user with a port binding
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Manually add a bind port to simulate GetBindPort
	user, _ := um.GetUser("testuser")
	user.BindPorts = append(user.BindPorts, 12345)

	// Remove the user - should soft delete (mark as deleting)
	removeReq := RemoveUserRequest{
		Username: "testuser",
	}
	err = um.RemoveUser(removeReq)
	if err != nil {
		t.Errorf("RemoveUser returned error: %v", err)
	}

	// Verify user is marked as deleting
	if !user.IsDeleting() {
		t.Error("expected user to be marked as deleting")
	}

	// Verify port bindings are NOT cleared immediately (soft delete)
	if len(user.BindPorts) == 0 {
		t.Error("expected bind ports to be preserved for cleanup")
	}

	// Verify user is not visible via GetUser (filtered out)
	_, err = um.GetUser("testuser")
	if err == nil {
		t.Error("expected GetUser to fail for deleting user")
	}

	// Verify user is not visible via ListUsers
	users := um.ListUsers()
	for _, u := range users {
		if u.Username == "testuser" {
			t.Error("expected deleting user to be filtered from ListUsers")
		}
	}
}

func TestGetUser_Success(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
		TTL:      time.Hour,
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Get the user
	user, err := um.GetUser("testuser")
	if err != nil {
		t.Errorf("GetUser returned error: %v", err)
	}
	if user.Username != "testuser" {
		t.Errorf("expected username testuser, got %s", user.Username)
	}
}

func TestGetUser_NotFound(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	_, err := um.GetUser("nonexistent")
	if err == nil {
		t.Error("expected error when getting nonexistent user")
	}
}

func TestListUsers(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add multiple users
	users := []AddUserRequest{
		{Username: "user1", Password: "pass1"},
		{Username: "user2", Password: "pass2"},
		{Username: "user3", Password: "pass3"},
	}

	for _, u := range users {
		err := um.AddUser(u)
		if err != nil {
			t.Fatalf("AddUser failed: %v", err)
		}
	}

	// List all users
	list := um.ListUsers()
	if len(list) != 3 {
		t.Errorf("expected 3 users, got %d", len(list))
	}
}

func TestUpdateUserPassword(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user
	req := AddUserRequest{
		Username: "testuser",
		Password: "oldpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Capture AuthToken before update (it should not change)
	userBefore, _ := um.GetUser("testuser")
	authTokenBefore := userBefore.AuthToken

	// Update password
	updateReq := UpdateUserPasswordRequest{
		Username:    "testuser",
		NewPassword: "newpass",
	}
	err = um.UpdateUserPassword(updateReq)
	if err != nil {
		t.Errorf("UpdateUserPassword returned error: %v", err)
	}

	// Verify AuthToken is unchanged (password updates only affect LoginPassword)
	user, _ := um.GetUser("testuser")
	if user.AuthToken != authTokenBefore {
		t.Errorf("expected AuthToken to be unchanged, got %q, want %q", user.AuthToken, authTokenBefore)
	}
}

func TestUpdateUserPassword_NotFound(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	updateReq := UpdateUserPasswordRequest{
		Username:    "nonexistent",
		NewPassword: "newpass",
	}
	err := um.UpdateUserPassword(updateReq)
	if err == nil {
		t.Error("expected error when updating nonexistent user")
	}
}

func TestUserWithTTL(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user with very short TTL
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
		TTL:      time.Millisecond, // 1ms TTL
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Wait for TTL to expire
	time.Sleep(10 * time.Millisecond)

	// Try to get the user (should fail because expired)
	_, err = um.GetUser("testuser")
	if err == nil {
		t.Error("expected error for expired user")
	}
}

func TestGetBindPort_AllocatesPort(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Get a bind port
	bindPortReq := GetBindPortRequest{
		Username:      "testuser",
		TargetPort:    443,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "test-inbound",
	}
	bindPort, err := um.GetBindPort(bindPortReq)
	if err != nil {
		t.Errorf("GetBindPort returned error: %v", err)
	}
	if bindPort == 0 {
		t.Error("expected non-zero bind port")
	}

	// Verify user has the port in BindPorts
	user, _ := um.GetUser("testuser")
	if len(user.BindPorts) != 1 || user.BindPorts[0] != bindPort {
		t.Errorf("expected user to have bind port %d, got %v", bindPort, user.BindPorts)
	}
}

func TestGetBindPort_EmptyContainerType(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user first
	addReq := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(addReq)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Try to get bind port with empty ContainerType
	bindPortReq := GetBindPortRequest{
		Username:      "testuser",
		TargetPort:    443,
		ContainerType: "", // Empty - should fail
		InboundTag:    "test-inbound",
	}
	_, err = um.GetBindPort(bindPortReq)
	if err == nil {
		t.Error("expected error for empty ContainerType")
	}
}

func TestGetBindPort_EmptyInboundTag(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user first
	addReq := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(addReq)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Try to get bind port with empty InboundTag
	bindPortReq := GetBindPortRequest{
		Username:      "testuser",
		TargetPort:    443,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "", // Empty - should fail
	}
	_, err = um.GetBindPort(bindPortReq)
	if err == nil {
		t.Error("expected error for empty InboundTag")
	}
}

func TestGetBindPort_EmptyBoth(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user first
	addReq := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(addReq)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Try to get bind port with both empty
	bindPortReq := GetBindPortRequest{
		Username:      "testuser",
		TargetPort:    443,
		ContainerType: "", // Empty
		InboundTag:    "", // Empty - should fail
	}
	_, err = um.GetBindPort(bindPortReq)
	if err == nil {
		t.Error("expected error for empty ContainerType and InboundTag")
	}
}

func TestGetBindPort_ExpiredUser(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add an expired user
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
		TTL:      time.Nanosecond,
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Wait for TTL to expire
	time.Sleep(time.Millisecond)

	// Try to get bind port (should fail)
	bindPortReq := GetBindPortRequest{
		Username:      "testuser",
		TargetPort:    443,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "test-inbound",
	}
	_, err = um.GetBindPort(bindPortReq)
	if err == nil {
		t.Error("expected error for expired user")
	}
}

func TestReleaseBindPort_Idempotent(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Try to release a port that doesn't exist - should be idempotent
	releaseReq := ReleaseBindPortRequest{
		Username: "testuser",
		BindPort: 99999,
	}
	err = um.ReleaseBindPort(releaseReq)
	if err != nil {
		t.Errorf("ReleaseBindPort should be idempotent: %v", err)
	}
}

func TestGetUserPort_WithBoundPort(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user first
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Bind a port to the user
	bindReq := GetBindPortRequest{
		Username:      "testuser",
		TargetPort:    443,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "test-inbound",
	}
	boundPort, err := um.GetBindPort(bindReq)
	if err != nil {
		t.Fatalf("GetBindPort failed: %v", err)
	}
	if boundPort == 0 {
		t.Fatal("bound port should not be 0")
	}

	// Now test GetUserPort
	port, found := um.GetUserPort("testuser")
	if !found {
		t.Error("GetUserPort should return true for existing user with port")
	}
	if port != boundPort {
		t.Errorf("GetUserPort returned %d, want %d", port, boundPort)
	}
}

func TestGetUserPort_NoBoundPort(t *testing.T) {
	um := NewUserManager(nil, "test-node")

	// Add a user without binding any port
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Test GetUserPort for user without bound port
	port, found := um.GetUserPort("testuser")
	if found {
		t.Error("GetUserPort should return false for user without bound port")
	}
	if port != 0 {
		t.Error("port should be 0 when not found")
	}
}

func TestGetUserPort_NonExistentUser(t *testing.T) {
	um := NewUserManager(nil, "test-node")

	// Test GetUserPort for non-existent user
	port, found := um.GetUserPort("nonexistent")
	if found {
		t.Error("GetUserPort should return false for non-existent user")
	}
	if port != 0 {
		t.Error("port should be 0 when not found")
	}
}

// ============ Traffic Stats Tests (Forward-Only) ============

// mockStatsForwardManager is a mock forward manager for testing forward-only stats.
// Reads and writes of records are serialised with mu so tests that mutate the
// fixture concurrently with the statsCollector's collect() goroutine do not
// trip the race detector.
type mockStatsForwardManager struct {
	mu      sync.RWMutex
	records []forward.ForwardTrafficRecord
}

func (m *mockStatsForwardManager) GetAllTrafficRecords(reset bool) []forward.ForwardTrafficRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.records == nil {
		return nil
	}
	out := make([]forward.ForwardTrafficRecord, len(m.records))
	copy(out, m.records)
	return out
}

// setRecordTraffic atomically updates the counters on the i-th record.
// Used by tests that simulate the forward layer producing additional traffic.
func (m *mockStatsForwardManager) setRecordTraffic(i int, up, down int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[i].UplinkBytes = up
	m.records[i].DownlinkBytes = down
}

func (m *mockStatsForwardManager) QueryTrafficStats(query forward.TrafficQuery) forward.TrafficQueryResult {
	return forward.TrafficQueryResult{}
}

// Ensure mockStatsForwardManager implements the full ForwardManager interface
var _ forward.ForwardManager = (*mockStatsForwardManager)(nil)

func (m *mockStatsForwardManager) AddRule(rule forward.ForwardRule) (*forward.ForwardRule, error) {
	return nil, nil
}
func (m *mockStatsForwardManager) RemoveRule(ruleKey string) error {
	return nil
}
func (m *mockStatsForwardManager) RemoveRulesByUser(userEmail string) error {
	return nil
}
func (m *mockStatsForwardManager) RemoveRulesByInbound(inboundTag string) error {
	return nil
}
func (m *mockStatsForwardManager) GetRule(ruleKey string) *forward.ForwardRule {
	return nil
}
func (m *mockStatsForwardManager) GetRulesByUser(userEmail string) []*forward.ForwardRule {
	return nil
}
func (m *mockStatsForwardManager) GetAllRules() []*forward.ForwardRule {
	return nil
}
func (m *mockStatsForwardManager) GetTraffic(ruleKey string, reset bool) (*forward.TrafficSnapshot, error) {
	return nil, nil
}
func (m *mockStatsForwardManager) GetAllTraffic(reset bool) *forward.ForwardManagerStats {
	return nil
}
func (m *mockStatsForwardManager) UpdateRateLimit(ruleKey string, uploadBPS, downloadBPS int64) error {
	return nil
}
func (m *mockStatsForwardManager) SetUserBandwidthLimit(username string, kind forward.BandwidthLimitKind, bytesPerSec int64) error {
	return nil
}
func (m *mockStatsForwardManager) GetUserBandwidthLimit(username string, kind forward.BandwidthLimitKind) (int64, bool) {
	return 0, false
}
func (m *mockStatsForwardManager) SetUserConnectionLimit(username string, limit int) error {
	return nil
}
func (m *mockStatsForwardManager) GetUserConnectionLimit(username string) (int, bool) {
	return 0, false
}
func (m *mockStatsForwardManager) SetUserClientLimitConfig(username string, config forward.ClientLimitConfig) error {
	return nil
}
func (m *mockStatsForwardManager) GetUserClientLimitConfig(username string) (forward.ClientLimitConfig, bool) {
	return forward.ClientLimitConfig{}, false
}
func (m *mockStatsForwardManager) AllocatePort() (uint32, error) {
	return 0, nil
}
func (m *mockStatsForwardManager) ReleasePort(port uint32) {}
func (m *mockStatsForwardManager) Close() error {
	return nil
}
func (m *mockStatsForwardManager) DropUser(username string) bool {
	return false
}

func TestUserManager_StartTrafficStats(t *testing.T) {
	um := NewUserManager(nil, "test-node")

	// Should not panic
	um.StartTrafficStats(time.Second)

	// Should be able to stop
	um.StopTrafficStats()
}

func TestUserManager_GetUserTrafficStats_ForwardOnly(t *testing.T) {
	// Create mock forward manager with traffic records
	mockFM := &mockStatsForwardManager{
		records: []forward.ForwardTrafficRecord{
			{
				RuleKey:       "xray:ss-demo-new:demo@v2raymg.local",
				Username:      "demo@v2raymg.local",
				ContainerType: contracts.ContainerXray,
				InboundTag:    "ss-demo-new",
				ListenPort:    10000,
				TargetAddr:    "127.0.0.1:10105",
				UplinkBytes:   100,
				DownlinkBytes: 200,
			},
		},
	}
	um := NewUserManager(mockFM, "test-node")

	// Start stats collection
	um.StartTrafficStats(time.Second)
	defer um.StopTrafficStats()

	// Force collection
	um.ForceTrafficStatsCollection()
	time.Sleep(50 * time.Millisecond)

	// Check user stats
	stats, found := um.GetUserTrafficStats("demo@v2raymg.local")
	if !found {
		allStats := um.GetTrafficStats()
		t.Logf("All user stats: %+v", allStats.ByUser)
		t.Error("expected to find user stats")
		return
	}
	if stats.DeltaUplink != 100 || stats.DeltaDownlink != 200 {
		t.Errorf("expected uplink=100, downlink=200, got uplink=%d, downlink=%d", stats.DeltaUplink, stats.DeltaDownlink)
	}
}

func TestUserManager_GetContainerTrafficStats_ForwardOnly(t *testing.T) {
	mockFM := &mockStatsForwardManager{
		records: []forward.ForwardTrafficRecord{
			{
				RuleKey:       "xray:ss-demo-new:demo@v2raymg.local",
				Username:      "demo@v2raymg.local",
				ContainerType: contracts.ContainerXray,
				InboundTag:    "ss-demo-new",
				UplinkBytes:   100,
				DownlinkBytes: 200,
			},
		},
	}
	um := NewUserManager(mockFM, "test-node")

	um.StartTrafficStats(time.Second)
	defer um.StopTrafficStats()

	// Force collection
	um.ForceTrafficStatsCollection()
	time.Sleep(50 * time.Millisecond)

	// Check container stats
	stats, found := um.GetContainerTrafficStats(contracts.ContainerXray)
	if !found {
		t.Error("expected to find container stats")
	}
	if stats.DeltaUplink != 100 || stats.DeltaDownlink != 200 {
		t.Errorf("expected uplink=100, downlink=200, got uplink=%d, downlink=%d", stats.DeltaUplink, stats.DeltaDownlink)
	}
}

func TestUserManager_GetGlobalTrafficStats_ForwardOnly(t *testing.T) {
	mockFM := &mockStatsForwardManager{
		records: []forward.ForwardTrafficRecord{
			{
				RuleKey:       "xray:ss-demo-new:user1@test.com",
				Username:      "user1@test.com",
				ContainerType: contracts.ContainerXray,
				InboundTag:    "ss-demo-new",
				UplinkBytes:   100,
				DownlinkBytes: 200,
			},
			{
				RuleKey:       "xray:ss-demo-new:user2@test.com",
				Username:      "user2@test.com",
				ContainerType: contracts.ContainerXray,
				InboundTag:    "ss-demo-new",
				UplinkBytes:   150,
				DownlinkBytes: 250,
			},
		},
	}
	um := NewUserManager(mockFM, "test-node")

	um.StartTrafficStats(time.Second)
	defer um.StopTrafficStats()

	// Force collection
	um.ForceTrafficStatsCollection()
	time.Sleep(50 * time.Millisecond)

	// Check global stats
	stats := um.GetGlobalTrafficStats()
	if stats.Total.DeltaUplink != 250 || stats.Total.DeltaDownlink != 450 {
		t.Errorf("expected uplink=250, downlink=450, got uplink=%d, downlink=%d", stats.Total.DeltaUplink, stats.Total.DeltaDownlink)
	}
}

// TestReleaseBindPort_DeletingUserStaysAsTombstone tests that ReleaseBindPort
// cleans up forward rules but keeps the tombstone in memory.
func TestReleaseBindPort_DeletingUserStaysAsTombstone(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add forward rule to the mock
	mockFM.rules["xray:inbound:12345"] = &forward.ForwardRule{
		Username:   "testuser",
		InboundTag: "inbound",
		ListenPort: 12345,
		TargetAddr: "127.0.0.1:443",
	}

	// Add a user
	err := um.AddUser(AddUserRequest{Username: "testuser", Password: "testpass"})
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Mark user for deletion
	err = um.RemoveUser(RemoveUserRequest{Username: "testuser"})
	if err != nil {
		t.Fatalf("RemoveUser failed: %v", err)
	}

	// Release port
	err = um.ReleaseBindPort(ReleaseBindPortRequest{Username: "testuser", BindPort: 12345})
	if err != nil {
		t.Errorf("ReleaseBindPort returned error: %v", err)
	}

	// Tombstone should still be in memory
	deletingUser := um.GetUserIncludingDeleting("testuser")
	if deletingUser == nil {
		t.Fatal("expected tombstone to still exist in map")
	}
	if !deletingUser.IsDeleting() {
		t.Error("expected user to still be marked as deleting")
	}

	// Re-adding should succeed since no forward rules remain
	err = um.AddUser(AddUserRequest{Username: "testuser", Password: "newpass"})
	if err != nil {
		t.Errorf("expected re-add to succeed after rules cleaned up, got: %v", err)
	}
	user, _ := um.GetUser("testuser")
	if user == nil {
		t.Fatal("expected user to exist after re-add")
	}
	if user.AuthToken == "" {
		t.Error("expected non-empty AuthToken after re-add")
	}
}

// TestReleaseBindPort_IdempotentWithDeleteState tests ReleaseBindPort idempotency with delete state.
func TestReleaseBindPort_IdempotentWithDeleteState(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Get the user and add bind port
	user, _ := um.GetUser("testuser")
	user.BindPorts = append(user.BindPorts, 12345)

	// Release the same port twice - should be idempotent
	err = um.ReleaseBindPort(ReleaseBindPortRequest{Username: "testuser", BindPort: 12345})
	if err != nil {
		t.Errorf("first ReleaseBindPort returned error: %v", err)
	}

	err = um.ReleaseBindPort(ReleaseBindPortRequest{Username: "testuser", BindPort: 12345})
	if err != nil {
		t.Errorf("second ReleaseBindPort returned error: %v", err)
	}

	// Release non-existent port - should also be idempotent
	err = um.ReleaseBindPort(ReleaseBindPortRequest{Username: "testuser", BindPort: 99999})
	if err != nil {
		t.Errorf("ReleaseBindPort for non-existent port returned error: %v", err)
	}
}

// TestAddUser_DeletingUserWithRemainingRules tests that AddUser error includes remaining rules info.
func TestAddUser_DeletingUserWithRemainingRules(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Get the user and add bind ports
	user, _ := um.GetUser("testuser")
	user.BindPorts = append(user.BindPorts, 12345)

	// Add a forward rule to the mock (simulating existing rule)
	mockFM.rules["xray:inbound:12345"] = &forward.ForwardRule{
		Username:   "testuser",
		InboundTag: "inbound",
		ListenPort: 12345,
		TargetAddr: "127.0.0.1:443",
	}

	// Mark user for deletion (soft delete)
	err = um.RemoveUser(RemoveUserRequest{Username: "testuser"})
	if err != nil {
		t.Fatalf("RemoveUser failed: %v", err)
	}

	// Try to add the same user again - should be rejected with deletion in progress
	err = um.AddUser(req)
	if err == nil {
		t.Error("expected error when adding user in deleting state")
	}

	// Verify the error mentions deletion and remaining rules
	if err != nil {
		errMsg := err.Error()
		if !strings.Contains(errMsg, "being deleted") {
			t.Errorf("expected error to mention deletion, got: %v", errMsg)
		}
		// The error should include forward rules info
		if !strings.Contains(errMsg, "forward rules") {
			t.Errorf("expected error to mention forward rules, got: %v", errMsg)
		}
	}
}

// TestUserManager_SetUserBandwidthLimit tests setting user bandwidth limit via usermanager.
func TestUserManager_SetUserBandwidthLimit(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Set upload bandwidth limit
	err = um.SetUserBandwidthLimit("testuser", forward.BandwidthUpload, 1024)
	if err != nil {
		t.Errorf("SetUserBandwidthLimit failed: %v", err)
	}

	// Verify the limit is stored in user
	user, _ := um.GetUser("testuser")
	if user.BandwidthUploadBps != 1024 {
		t.Errorf("expected BandwidthUploadBps=1024, got %d", user.BandwidthUploadBps)
	}

	// Set download bandwidth limit
	err = um.SetUserBandwidthLimit("testuser", forward.BandwidthDownload, 2048)
	if err != nil {
		t.Errorf("SetUserBandwidthLimit failed: %v", err)
	}

	// Verify
	user, _ = um.GetUser("testuser")
	if user.BandwidthDownloadBps != 2048 {
		t.Errorf("expected BandwidthDownloadBps=2048, got %d", user.BandwidthDownloadBps)
	}
}

// TestUserManager_GetUserBandwidthLimit tests getting user bandwidth limit.
func TestUserManager_GetUserBandwidthLimit(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")

	// Add a user
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Initially no limit
	_, ok := um.GetUserBandwidthLimit("testuser", forward.BandwidthUpload)
	if ok {
		t.Error("expected no limit initially")
	}

	// Set limit
	err = um.SetUserBandwidthLimit("testuser", forward.BandwidthUpload, 1024)
	if err != nil {
		t.Fatalf("SetUserBandwidthLimit failed: %v", err)
	}

	// Get should return the limit
	rate, ok := um.GetUserBandwidthLimit("testuser", forward.BandwidthUpload)
	if !ok {
		t.Error("expected to get limit")
	}
	if rate != 1024 {
		t.Errorf("expected rate=1024, got %d", rate)
	}
}

// ============ Persistence Tests ============

// TestUserManager_WithStore_AddUser verifies AddUser persists to store.
func TestUserManager_WithStore_AddUser(t *testing.T) {
	storeMgr := openTestStoreManager(t)
	um, err := NewUserManagerWithStore(nil, storeMgr, "test-node")
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}

	if err := um.AddUser(AddUserRequest{Username: "alice", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	users, err := storeMgr.UserStore().Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user in store, got %d", len(users))
	}
	if users[0].Username != "alice" {
		t.Errorf("expected username=alice, got %q", users[0].Username)
	}
	if users[0].AuthToken == "" {
		t.Error("expected non-empty AuthToken in store after AddUser")
	}
}

// TestUserManager_WithStore_RemoveUser verifies RemoveUser keeps tombstone in store.
func TestUserManager_WithStore_RemoveUser(t *testing.T) {
	storeMgr := openTestStoreManager(t)
	um, err := NewUserManagerWithStore(nil, storeMgr, "test-node")
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}

	if err := um.AddUser(AddUserRequest{Username: "bob", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if err := um.RemoveUser(RemoveUserRequest{Username: "bob"}); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}

	// Tombstone should remain in store
	users, err := storeMgr.UserStore().Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 tombstone in store after remove, got %d", len(users))
	}
	if !users[0].IsDeleting() {
		t.Error("expected user in store to be marked as deleting")
	}
}

// TestUserManager_WithStore_UpdatePassword verifies UpdateUserPassword persists the new password.
func TestUserManager_WithStore_UpdatePassword(t *testing.T) {
	storeMgr := openTestStoreManager(t)
	um, err := NewUserManagerWithStore(nil, storeMgr, "test-node")
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}

	if err := um.AddUser(AddUserRequest{Username: "carol", Password: "old"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if err := um.UpdateUserPassword(UpdateUserPasswordRequest{Username: "carol", NewPassword: "new"}); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}

	users, err := storeMgr.UserStore().Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user in store, got %d", len(users))
	}
	if users[0].AuthToken == "" {
		t.Error("expected non-empty AuthToken in store after UpdateUserPassword")
	}
}

// TestUserManager_NilStore_Backward verifies NewUserManager (no store) works identically to before.
func TestUserManager_NilStore_Backward(t *testing.T) {
	um := NewUserManager(nil, "test-node") // pure in-memory, no store

	if err := um.AddUser(AddUserRequest{Username: "dave", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	users := um.ListUsers()
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if err := um.UpdateUserPassword(UpdateUserPasswordRequest{Username: "dave", NewPassword: "newpass"}); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}
	if err := um.RemoveUser(RemoveUserRequest{Username: "dave"}); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}
}

// --- Group filtering tests ---

// memNodeGroupsStore is an in-memory mock of NodeGroupsStore for testing.
type memNodeGroupsStore struct {
	groups []string
}

func (s *memNodeGroupsStore) List() ([]string, error) { return s.groups, nil }
func (s *memNodeGroupsStore) Set(groups []string) error {
	s.groups = groups
	return nil
}

// addUserWithGroup adds a user and directly sets TargetGroup, bypassing stampVersion.
// This is fine for group filtering tests which don't check hash consistency.
func addUserWithGroup(t *testing.T, um *UserManager, username, group string) {
	t.Helper()
	if err := um.AddUser(AddUserRequest{Username: username, Password: "pass"}); err != nil {
		t.Fatalf("AddUser(%s): %v", username, err)
	}
	if group != "" {
		um.mu.Lock()
		um.users[username].TargetGroup = group
		um.mu.Unlock()
	}
}

func TestListUsers_NoCluster_ReturnsAll(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")
	addUserWithGroup(t, um, "u1", "asia")
	addUserWithGroup(t, um, "u2", "europe")

	users := um.ListUsers()
	if len(users) != 2 {
		t.Errorf("expected 2 users without cluster, got %d", len(users))
	}
}

func TestListUsers_ClusterEnabled_NoGroups_ReturnsAll(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")
	um.EnableClusterSync("default", &memNodeGroupsStore{groups: []string{}})

	addUserWithGroup(t, um, "u1", "asia")
	addUserWithGroup(t, um, "u2", "europe")

	users := um.ListUsers()
	if len(users) != 2 {
		t.Errorf("expected 2 users with empty groups (fail-open), got %d", len(users))
	}
}

func TestListUsers_ClusterEnabled_FiltersByGroup(t *testing.T) {
	ngs := &memNodeGroupsStore{groups: []string{"asia", "europe"}}
	um := NewUserManager(newMockForwardManager(), "test-node")
	um.EnableClusterSync("default", ngs)

	addUserWithGroup(t, um, "u-asia", "asia")
	addUserWithGroup(t, um, "u-europe", "europe")
	addUserWithGroup(t, um, "u-us", "us")

	users := um.ListUsers()
	names := make(map[string]bool)
	for _, u := range users {
		names[u.Username] = true
	}

	if !names["u-asia"] {
		t.Error("expected u-asia to be visible")
	}
	if !names["u-europe"] {
		t.Error("expected u-europe to be visible")
	}
	if names["u-us"] {
		t.Error("expected u-us to be hidden")
	}
	if len(users) != 2 {
		t.Errorf("expected 2 visible users, got %d", len(users))
	}
}

func TestListUsersWithPasswd_FiltersByGroup(t *testing.T) {
	ngs := &memNodeGroupsStore{groups: []string{"asia"}}
	um := NewUserManager(newMockForwardManager(), "test-node")
	um.EnableClusterSync("default", ngs)

	addUserWithGroup(t, um, "u-asia", "asia")
	addUserWithGroup(t, um, "u-us", "us")

	result := um.ListUsersWithPasswd()
	if _, ok := result["u-asia"]; !ok {
		t.Error("expected u-asia in password map")
	}
	if _, ok := result["u-us"]; ok {
		t.Error("expected u-us to be filtered from password map")
	}
}

func TestSetNodeGroups_UpdatesCache(t *testing.T) {
	ngs := &memNodeGroupsStore{groups: []string{"asia"}}
	um := NewUserManager(newMockForwardManager(), "test-node")
	um.EnableClusterSync("default", ngs)

	addUserWithGroup(t, um, "u-asia", "asia")
	addUserWithGroup(t, um, "u-us", "us")

	// Initially only asia visible
	if len(um.ListUsers()) != 1 {
		t.Fatalf("expected 1 user with group=asia, got %d", len(um.ListUsers()))
	}

	// Change groups to include us
	if err := um.SetNodeGroups([]string{"asia", "us"}); err != nil {
		t.Fatalf("SetNodeGroups: %v", err)
	}
	if len(um.ListUsers()) != 2 {
		t.Errorf("expected 2 users after adding us group, got %d", len(um.ListUsers()))
	}
}

func TestListDigests_NotFilteredByGroup(t *testing.T) {
	ngs := &memNodeGroupsStore{groups: []string{"asia"}}
	um := NewUserManager(newMockForwardManager(), "test-node")
	um.EnableClusterSync("default", ngs)

	addUserWithGroup(t, um, "u-asia", "asia")
	addUserWithGroup(t, um, "u-us", "us")

	digests := um.ListDigests()
	if len(digests) != 2 {
		t.Errorf("expected 2 digests (unfiltered), got %d", len(digests))
	}
}

func TestGetUserForSync_NotFilteredByGroup(t *testing.T) {
	ngs := &memNodeGroupsStore{groups: []string{"asia"}}
	um := NewUserManager(newMockForwardManager(), "test-node")
	um.EnableClusterSync("default", ngs)

	addUserWithGroup(t, um, "u-us", "us")

	u := um.GetUserForSync("u-us")
	if u == nil {
		t.Error("expected GetUserForSync to return user outside node's groups")
	}
}

// ============ AuthToken Tests ============

func TestFindUserByToken(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	// Add two users
	if err := um.AddUser(AddUserRequest{Username: "alice", Password: "pass1"}); err != nil {
		t.Fatalf("AddUser(alice): %v", err)
	}
	if err := um.AddUser(AddUserRequest{Username: "bob", Password: "pass2"}); err != nil {
		t.Fatalf("AddUser(bob): %v", err)
	}

	alice, _ := um.GetUser("alice")
	bob, _ := um.GetUser("bob")

	// Find by valid token
	found := um.FindUserByToken(alice.AuthToken)
	if found == nil {
		t.Fatal("expected to find alice by token")
	}
	if found.Username != "alice" {
		t.Errorf("expected alice, got %s", found.Username)
	}

	found = um.FindUserByToken(bob.AuthToken)
	if found == nil {
		t.Fatal("expected to find bob by token")
	}
	if found.Username != "bob" {
		t.Errorf("expected bob, got %s", found.Username)
	}

	// Empty token returns nil
	if um.FindUserByToken("") != nil {
		t.Error("expected nil for empty token")
	}

	// Nonexistent token returns nil
	if um.FindUserByToken("deadbeef12345678deadbeef12345678") != nil {
		t.Error("expected nil for nonexistent token")
	}
}

func TestFindUserByToken_DeletedUser(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	if err := um.AddUser(AddUserRequest{Username: "alice", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	alice, _ := um.GetUser("alice")
	token := alice.AuthToken

	// Remove user
	if err := um.RemoveUser(RemoveUserRequest{Username: "alice"}); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}

	// Deleted user should not be findable by token
	if um.FindUserByToken(token) != nil {
		t.Error("expected nil for deleted user's token")
	}
}

func TestFindUserByToken_ExpiredUser(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	if err := um.AddUser(AddUserRequest{Username: "alice", Password: "pass", TTL: time.Nanosecond}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	alice := um.GetUserIncludingDeleting("alice")
	token := alice.AuthToken

	time.Sleep(time.Millisecond)

	// Expired user should not be findable by token
	if um.FindUserByToken(token) != nil {
		t.Error("expected nil for expired user's token")
	}
}

func TestResetAuthToken(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	if err := um.AddUser(AddUserRequest{Username: "alice", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	alice, _ := um.GetUser("alice")
	oldToken := alice.AuthToken

	// Reset token
	newToken, err := um.ResetAuthToken("alice", "")
	if err != nil {
		t.Fatalf("ResetAuthToken: %v", err)
	}
	if newToken == "" {
		t.Fatal("expected non-empty new token")
	}
	if newToken == oldToken {
		t.Error("expected new token to differ from old token")
	}

	// Verify user has the new token
	alice, _ = um.GetUser("alice")
	if alice.AuthToken != newToken {
		t.Errorf("expected AuthToken=%s, got %s", newToken, alice.AuthToken)
	}

	// Old token should no longer work
	if um.FindUserByToken(oldToken) != nil {
		t.Error("expected old token to no longer find user")
	}

	// New token should work
	found := um.FindUserByToken(newToken)
	if found == nil || found.Username != "alice" {
		t.Error("expected new token to find alice")
	}
}

func TestResetAuthToken_NonexistentUser(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	_, err := um.ResetAuthToken("nonexistent", "")
	if err == nil {
		t.Error("expected error for nonexistent user")
	}
}

func TestResetAuthToken_WithSpecificToken(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	if err := um.AddUser(AddUserRequest{Username: "bob", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	specificToken := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	newToken, err := um.ResetAuthToken("bob", specificToken)
	if err != nil {
		t.Fatalf("ResetAuthToken with specific token: %v", err)
	}
	if newToken != specificToken {
		t.Errorf("expected token=%s, got %s", specificToken, newToken)
	}

	bob, _ := um.GetUser("bob")
	if bob.AuthToken != specificToken {
		t.Errorf("expected AuthToken=%s, got %s", specificToken, bob.AuthToken)
	}

	found := um.FindUserByToken(specificToken)
	if found == nil || found.Username != "bob" {
		t.Error("expected specific token to find bob")
	}
}

func TestResetAuthToken_InvalidUUID(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	if err := um.AddUser(AddUserRequest{Username: "carol", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	carol, _ := um.GetUser("carol")
	oldToken := carol.AuthToken

	_, err := um.ResetAuthToken("carol", "not-a-valid-uuid")
	if err == nil {
		t.Fatal("expected error for invalid UUID")
	}

	// Token should be unchanged after failed reset.
	carol, _ = um.GetUser("carol")
	if carol.AuthToken != oldToken {
		t.Errorf("expected token unchanged, got %s (was %s)", carol.AuthToken, oldToken)
	}
}

func TestResetAuthToken_TokenConflict(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	if err := um.AddUser(AddUserRequest{Username: "dave", Password: "pass"}); err != nil {
		t.Fatalf("AddUser dave: %v", err)
	}
	if err := um.AddUser(AddUserRequest{Username: "eve", Password: "pass"}); err != nil {
		t.Fatalf("AddUser eve: %v", err)
	}

	dave, _ := um.GetUser("dave")
	daveToken := dave.AuthToken

	// Try to set eve's token to dave's token — should fail.
	_, err := um.ResetAuthToken("eve", daveToken)
	if err == nil {
		t.Fatal("expected error when setting token that conflicts with another user")
	}

	// Eve's token should be unchanged.
	eve, _ := um.GetUser("eve")
	if eve.AuthToken == daveToken {
		t.Error("eve's token should not have been changed to dave's token")
	}
}

// ---------------------------------------------------------------------------
// setAuthTokenLocked — direct unit tests
// ---------------------------------------------------------------------------

func TestSetAuthTokenLocked_EmptyAutoGenerates(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")
	if err := um.AddUser(AddUserRequest{Username: "u1", Password: "p"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	u1, _ := um.GetUser("u1")
	if u1.AuthToken == "" {
		t.Fatal("AddUser should auto-generate a token via setAuthTokenLocked")
	}
	if !isValidUUIDv4(u1.AuthToken) {
		t.Errorf("auto-generated token should be valid UUID v4, got %s", u1.AuthToken)
	}
}

func TestSetAuthTokenLocked_AllUsersGetUniqueTokens(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")
	names := []string{"a", "b", "c", "d", "e"}
	for _, n := range names {
		if err := um.AddUser(AddUserRequest{Username: n, Password: "p"}); err != nil {
			t.Fatalf("AddUser(%s): %v", n, err)
		}
	}
	seen := map[string]string{} // token -> username
	for _, n := range names {
		u, _ := um.GetUser(n)
		if prev, dup := seen[u.AuthToken]; dup {
			t.Errorf("token collision: %s and %s share token %s", prev, n, u.AuthToken)
		}
		seen[u.AuthToken] = n
	}
}

func TestSetAuthTokenLocked_RejectInvalidUUID(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")
	if err := um.AddUser(AddUserRequest{Username: "u1", Password: "p"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	oldToken, _ := um.GetUser("u1")

	// Invalid UUID should be rejected.
	_, err := um.ResetAuthToken("u1", "not-a-uuid")
	if err == nil {
		t.Fatal("expected error for invalid UUID")
	}

	// Token should be unchanged.
	u1, _ := um.GetUser("u1")
	if u1.AuthToken != oldToken.AuthToken {
		t.Error("token should not change on invalid UUID")
	}
}

func TestSetAuthTokenLocked_RejectConflict(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")
	if err := um.AddUser(AddUserRequest{Username: "u1", Password: "p"}); err != nil {
		t.Fatalf("AddUser u1: %v", err)
	}
	if err := um.AddUser(AddUserRequest{Username: "u2", Password: "p"}); err != nil {
		t.Fatalf("AddUser u2: %v", err)
	}

	u1, _ := um.GetUser("u1")

	// Setting u2's token to u1's token should fail.
	_, err := um.ResetAuthToken("u2", u1.AuthToken)
	if err == nil {
		t.Fatal("expected error for token conflict")
	}
}

func TestSetAuthTokenLocked_SameTokenOnSameUser(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")
	if err := um.AddUser(AddUserRequest{Username: "u1", Password: "p"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	u1, _ := um.GetUser("u1")
	// Re-setting a user's own token should succeed (no self-conflict).
	_, err := um.ResetAuthToken("u1", u1.AuthToken)
	if err != nil {
		t.Fatalf("expected no error when re-setting own token, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// SyncUpsertUser — token uniqueness paths
// ---------------------------------------------------------------------------

func TestSyncUpsertUser_NewUserValidToken(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	incoming := &contracts.User{
		Username:    "remote-user",
		AuthToken:   "11111111-1111-4111-8111-111111111111",
		UpdatedAtUs: 100,
		OriginNode:  "node-b",
	}
	applied, err := um.SyncUpsertUser(incoming)
	if err != nil {
		t.Fatalf("SyncUpsertUser: %v", err)
	}
	if !applied {
		t.Fatal("expected record to be applied")
	}

	u, err := um.GetUser("remote-user")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.AuthToken != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("expected token preserved, got %s", u.AuthToken)
	}
}

func TestSyncUpsertUser_NewUserInvalidTokenRegenerated(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	incoming := &contracts.User{
		Username:    "remote-user",
		AuthToken:   "legacy-plaintext-password",
		UpdatedAtUs: 100,
		OriginNode:  "node-b",
	}
	applied, err := um.SyncUpsertUser(incoming)
	if err != nil {
		t.Fatalf("SyncUpsertUser: %v", err)
	}
	if !applied {
		t.Fatal("expected record to be applied")
	}

	u, _ := um.GetUser("remote-user")
	if u.AuthToken == "legacy-plaintext-password" {
		t.Error("invalid token should have been regenerated")
	}
	if !isValidUUIDv4(u.AuthToken) {
		t.Errorf("regenerated token should be valid UUID v4, got %s", u.AuthToken)
	}
}

func TestSyncUpsertUser_NewUserConflictingTokenRegenerated(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	// Create a local user first.
	if err := um.AddUser(AddUserRequest{Username: "local", Password: "p"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	local, _ := um.GetUser("local")

	// Incoming remote user has the same token as the local user.
	incoming := &contracts.User{
		Username:    "remote",
		AuthToken:   local.AuthToken,
		UpdatedAtUs: 100,
		OriginNode:  "node-b",
	}
	applied, err := um.SyncUpsertUser(incoming)
	if err != nil {
		t.Fatalf("SyncUpsertUser: %v", err)
	}
	if !applied {
		t.Fatal("expected record to be applied")
	}

	remote, _ := um.GetUser("remote")
	if remote.AuthToken == local.AuthToken {
		t.Error("conflicting token should have been regenerated")
	}
	if !isValidUUIDv4(remote.AuthToken) {
		t.Errorf("regenerated token should be valid UUID v4, got %s", remote.AuthToken)
	}
}

func TestSyncUpsertUser_UpdateConflictingTokenRegenerated(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	// Create two local users.
	if err := um.AddUser(AddUserRequest{Username: "u1", Password: "p"}); err != nil {
		t.Fatalf("AddUser u1: %v", err)
	}
	if err := um.AddUser(AddUserRequest{Username: "u2", Password: "p"}); err != nil {
		t.Fatalf("AddUser u2: %v", err)
	}
	u1, _ := um.GetUser("u1")
	u2, _ := um.GetUser("u2")
	u2OldToken := u2.AuthToken

	// Sync u2 with u1's token from a "newer" remote version — should regenerate.
	incoming := &contracts.User{
		Username:    "u2",
		AuthToken:   u1.AuthToken,
		UpdatedAtUs: u2.UpdatedAtUs + 1000,
		OriginNode:  "node-b",
	}
	applied, err := um.SyncUpsertUser(incoming)
	if err != nil {
		t.Fatalf("SyncUpsertUser: %v", err)
	}
	if !applied {
		t.Fatal("expected record to be applied")
	}

	u2After, _ := um.GetUser("u2")
	if u2After.AuthToken == u1.AuthToken {
		t.Error("conflicting token should have been regenerated")
	}
	if u2After.AuthToken == u2OldToken {
		// It's OK if it happens to be different from old; just must not be u1's.
		// This check is a no-op assertion but documents intent.
	}
	if !isValidUUIDv4(u2After.AuthToken) {
		t.Errorf("regenerated token should be valid UUID v4, got %s", u2After.AuthToken)
	}
}

// TestGetBindPort_NetworkPassthrough verifies that GetBindPortRequest.Network
// is propagated to the ForwardRule unchanged (empty → tcp default path,
// "udp" → udp).
func TestGetBindPort_NetworkPassthrough(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM, "test-node")
	if err := um.AddUser(AddUserRequest{Username: "netuser", Password: "pw"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Default empty → stored as empty on the rule; ForwardRule.ResolvedNetwork()
	// maps empty to tcp, so downstream dispatch works.
	if _, err := um.GetBindPort(GetBindPortRequest{
		Username:      "netuser",
		TargetPort:    443,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "tcp-in",
	}); err != nil {
		t.Fatalf("GetBindPort tcp: %v", err)
	}
	stored := mockFM.rules["netuser"]
	if stored == nil {
		t.Fatal("rule not recorded")
	}
	if stored.Network != "" {
		t.Errorf("default Network = %q, want empty string", stored.Network)
	}
	if stored.ResolvedNetwork() != forward.NetworkTCP {
		t.Errorf("ResolvedNetwork = %q, want tcp", stored.ResolvedNetwork())
	}

	// Second call for the same user with Network="udp" on a different inbound
	// should propagate through to the mock FM.
	if _, err := um.GetBindPort(GetBindPortRequest{
		Username:      "netuser",
		TargetPort:    5060,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "udp-in",
		Network:       "udp",
	}); err != nil {
		t.Fatalf("GetBindPort udp: %v", err)
	}
	// mockFM stores by username, so the last AddRule overwrote the first.
	storedUDP := mockFM.rules["netuser"]
	if storedUDP == nil {
		t.Fatal("udp rule not recorded")
	}
	if storedUDP.Network != "udp" {
		t.Errorf("Network = %q, want %q", storedUDP.Network, "udp")
	}
	if storedUDP.ResolvedNetwork() != forward.NetworkUDP {
		t.Errorf("ResolvedNetwork = %q, want udp", storedUDP.ResolvedNetwork())
	}
}
