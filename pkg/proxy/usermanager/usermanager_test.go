package usermanager

import (
	"strings"
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

func (m *mockForwardManager) Close() error {
	return nil
}

func TestNewUserManager(t *testing.T) {
	um := NewUserManager(nil)
	if um == nil {
		t.Error("NewUserManager returned nil")
	}
}

func TestAddUser_Success(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM)

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
	if user.Password != "testpass" {
		t.Errorf("expected password testpass, got %v", user.Password)
	}
}

func TestAddUser_ValidationError(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

	// Add a user first
	req := AddUserRequest{
		Username: "testuser",
		Password: "testpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Add a bind port to the user
	user, _ := um.GetUser("testuser")
	user.BindPorts = append(user.BindPorts, 12345)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

	_, err := um.GetUser("nonexistent")
	if err == nil {
		t.Error("expected error when getting nonexistent user")
	}
}

func TestListUsers(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

	// Add a user
	req := AddUserRequest{
		Username: "testuser",
		Password: "oldpass",
	}
	err := um.AddUser(req)
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Update password
	updateReq := UpdateUserPasswordRequest{
		Username:    "testuser",
		NewPassword: "newpass",
	}
	err = um.UpdateUserPassword(updateReq)
	if err != nil {
		t.Errorf("UpdateUserPassword returned error: %v", err)
	}

	// Verify password was updated
	user, _ := um.GetUser("testuser")
	if user.Password != "newpass" {
		t.Errorf("expected password newpass, got %v", user.Password)
	}
}

func TestUpdateUserPassword_NotFound(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(nil)

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
	um := NewUserManager(nil)

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
type mockStatsForwardManager struct {
	records []forward.ForwardTrafficRecord
}

func (m *mockStatsForwardManager) GetAllTrafficRecords(reset bool) []forward.ForwardTrafficRecord {
	return m.records
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
func (m *mockStatsForwardManager) Close() error {
	return nil
}

func TestUserManager_StartTrafficStats(t *testing.T) {
	um := NewUserManager(nil)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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

// TestReleaseBindPort_FinalizeDelete tests that ReleaseBindPort triggers physical deletion
// when user is in deleting state and has no more rules.
// Note: Current implementation calls RemoveRulesByUser which removes ALL rules for the user,
// so finalization happens on the first ReleaseBindPort call when user is in deleting state.
func TestReleaseBindPort_FinalizeDelete(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM)

	// Add forward rule to the mock
	mockFM.rules["xray:inbound:12345"] = &forward.ForwardRule{
		Username:   "testuser",
		InboundTag: "inbound",
		ListenPort: 12345,
		TargetAddr: "127.0.0.1:443",
	}

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

	// Mark user for deletion (soft delete)
	err = um.RemoveUser(RemoveUserRequest{Username: "testuser"})
	if err != nil {
		t.Fatalf("RemoveUser failed: %v", err)
	}

	// Verify user is still in map but marked as deleting
	deletingUser := um.GetUserIncludingDeleting("testuser")
	if deletingUser == nil {
		t.Fatal("expected user to still exist in map")
	}
	if !deletingUser.IsDeleting() {
		t.Error("expected user to be marked as deleting")
	}

	// Release the port - should finalize because there are no more rules
	err = um.ReleaseBindPort(ReleaseBindPortRequest{Username: "testuser", BindPort: 12345})
	if err != nil {
		t.Errorf("ReleaseBindPort returned error: %v", err)
	}

	// User should be physically deleted now because:
	// 1. User is in "deleting" state
	// 2. ReleaseBindPort calls RemoveRulesByUser which removes ALL rules for the user
	// 3. GetRulesByUser returns 0 rules, so finalization happens
	deletingUser = um.GetUserIncludingDeleting("testuser")
	if deletingUser != nil {
		t.Error("expected user to be physically deleted after releasing port (finalization)")
	}
}

// TestReleaseBindPort_IdempotentWithDeleteState tests ReleaseBindPort idempotency with delete state.
func TestReleaseBindPort_IdempotentWithDeleteState(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
		// The error should include remaining rules info from forward manager
		if !strings.Contains(errMsg, "remaining rules") {
			t.Errorf("expected error to mention remaining rules, got: %v", errMsg)
		}
	}
}

// TestUserManager_SetUserBandwidthLimit tests setting user bandwidth limit via usermanager.
func TestUserManager_SetUserBandwidthLimit(t *testing.T) {
	mockFM := newMockForwardManager()
	um := NewUserManager(mockFM)

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
	um := NewUserManager(mockFM)

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
	um, err := NewUserManagerWithStore(nil, storeMgr)
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
	if users[0].Password != "pass" {
		t.Errorf("expected password=pass, got %q", users[0].Password)
	}
}

// TestUserManager_WithStore_RemoveUser verifies RemoveUser physically deletes from store.
func TestUserManager_WithStore_RemoveUser(t *testing.T) {
	storeMgr := openTestStoreManager(t)
	um, err := NewUserManagerWithStore(nil, storeMgr)
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}

	if err := um.AddUser(AddUserRequest{Username: "bob", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if err := um.RemoveUser(RemoveUserRequest{Username: "bob"}); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}

	users, err := storeMgr.UserStore().Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users in store after remove, got %d", len(users))
	}
}

// TestUserManager_WithStore_UpdatePassword verifies UpdateUserPassword persists the new password.
func TestUserManager_WithStore_UpdatePassword(t *testing.T) {
	storeMgr := openTestStoreManager(t)
	um, err := NewUserManagerWithStore(nil, storeMgr)
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
	if users[0].Password != "new" {
		t.Errorf("expected password=new, got %q", users[0].Password)
	}
}

// TestUserManager_NilStore_Backward verifies NewUserManager (no store) works identically to before.
func TestUserManager_NilStore_Backward(t *testing.T) {
	um := NewUserManager(nil) // pure in-memory, no store

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
