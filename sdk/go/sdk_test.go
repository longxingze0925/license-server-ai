package license

import (
	"fmt"
	"testing"
	"time"
)

const (
	TestServerURL = "http://localhost:8080"
	TestAppKey    = "test-app-key"
)

// TestClientCreation tests basic client creation
func TestClientCreation(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithAppVersion("1.0.0"),
		WithOfflineGraceDays(7),
		WithSkipVerify(true),
		WithTimeout(30*time.Second),
		WithMaxRetries(3),
		WithEncryptCache(true),
	)

	if client == nil {
		t.Fatal("Failed to create client")
	}

	// Test getters
	if client.GetServerURL() != TestServerURL {
		t.Errorf("Expected server URL %s, got %s", TestServerURL, client.GetServerURL())
	}

	if client.GetAppKey() != TestAppKey {
		t.Errorf("Expected app key %s, got %s", TestAppKey, client.GetAppKey())
	}

	machineID := client.GetMachineID()
	if machineID == "" {
		t.Error("Machine ID should not be empty")
	}
	if len(machineID) != 32 {
		t.Errorf("Machine ID should be 32 characters, got %d", len(machineID))
	}

	fmt.Printf("  Client created successfully\n")
	fmt.Printf("  Server URL: %s\n", client.GetServerURL())
	fmt.Printf("  App Key: %s\n", client.GetAppKey())
	fmt.Printf("  Machine ID: %s\n", machineID)

	client.Close()
}

// TestSecureClient tests secure client wrapper
func TestSecureClient(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
	)

	secureClient := NewSecureClient(client)
	if secureClient == nil {
		t.Fatal("Failed to create secure client")
	}

	// Test security token generation
	token := secureClient.GetSecurityToken()
	if token == "" {
		t.Error("Security token should not be empty")
	}
	if len(token) != 32 {
		t.Errorf("Security token should be 32 characters, got %d", len(token))
	}

	fmt.Printf("  Secure client created successfully\n")
	fmt.Printf("  Security token: %s\n", token)

	secureClient.Close()
}

// TestAntiDebug tests anti-debug functionality
func TestAntiDebug(t *testing.T) {
	ad := &AntiDebug{}

	// This test may vary depending on environment
	isDebugger := ad.IsDebuggerPresent()
	fmt.Printf("  Debugger detected: %v\n", isDebugger)
}

// TestTimeChecker tests time rollback detection
func TestTimeChecker(t *testing.T) {
	tc := NewTimeChecker(".")

	// First check should pass
	if !tc.Check() {
		t.Error("First time check should pass")
	}

	// Second check should also pass (no rollback)
	if !tc.Check() {
		t.Error("Second time check should pass")
	}

	fmt.Printf("  Time checker working correctly\n")
}

// TestIntegrityChecker tests integrity verification
func TestIntegrityChecker(t *testing.T) {
	ic := NewIntegrityChecker()

	// Verify should return true (no modification)
	if !ic.Verify() {
		t.Error("Integrity verification should pass")
	}

	checksum := ic.GetChecksum()
	fmt.Printf("  Integrity checksum: %s\n", checksum)
}

// TestEnvironmentChecker tests environment detection
func TestEnvironmentChecker(t *testing.T) {
	ec := &EnvironmentChecker{}

	isVM := ec.IsVirtualMachine()
	fmt.Printf("  Virtual machine detected: %v\n", isVM)
}

// TestDistributedValidator tests distributed validation
func TestDistributedValidator(t *testing.T) {
	dv := NewDistributedValidator()

	// Register some validators
	dv.Register("always_true", func() bool { return true })
	dv.Register("also_true", func() bool { return true })

	// All should pass
	if !dv.ValidateAll() {
		t.Error("All validators should pass")
	}

	token := dv.GetValidationToken()
	if token == "" {
		t.Error("Validation token should not be empty")
	}

	fmt.Printf("  Distributed validator working correctly\n")
	fmt.Printf("  Validation token: %s\n", token)
}

// TestCheckEnvironment tests environment check helper
func TestCheckEnvironment(t *testing.T) {
	result := CheckEnvironment()

	fmt.Printf("  Environment check results:\n")
	for k, v := range result {
		fmt.Printf("    %s: %v\n", k, v)
	}
}

// TestDataSyncClient tests data sync client creation
func TestDataSyncClient(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
	)

	syncClient := client.NewDataSyncClient()
	if syncClient == nil {
		t.Fatal("Failed to create data sync client")
	}

	// Test last sync time
	syncClient.SetLastSyncTime("test_table", time.Now().Unix())
	lastSync := syncClient.GetLastSyncTime("test_table")
	if lastSync == 0 {
		t.Error("Last sync time should not be 0")
	}

	fmt.Printf("  Data sync client created successfully\n")
	fmt.Printf("  Last sync time for test_table: %d\n", lastSync)

	client.Close()
}

// TestHotUpdateManager tests hot update manager creation
func TestHotUpdateManager(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
	)

	manager := NewHotUpdateManager(client, "1.0.0",
		WithUpdateDir("./.test_updates"),
		WithBackupDir("./.test_backups"),
		WithAutoCheck(false, time.Hour),
		WithUpdateCallback(func(status HotUpdateStatus, progress float64, err error) {
			// callback for testing
		}),
	)

	if manager == nil {
		t.Fatal("Failed to create hot update manager")
	}

	// Test version
	if manager.GetCurrentVersion() != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", manager.GetCurrentVersion())
	}

	manager.SetCurrentVersion("1.0.1")
	if manager.GetCurrentVersion() != "1.0.1" {
		t.Errorf("Expected version 1.0.1, got %s", manager.GetCurrentVersion())
	}

	// Test updating status
	if manager.IsUpdating() {
		t.Error("Should not be updating initially")
	}

	fmt.Printf("  Hot update manager created successfully\n")
	fmt.Printf("  Current version: %s\n", manager.GetCurrentVersion())

	client.Close()
}

// TestSecureScriptManager tests secure script manager creation
func TestSecureScriptManager(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
	)

	manager := NewSecureScriptManager(client,
		WithAppSecret("test-secret"),
		WithExecuteCallback(func(scriptID string, status string, err error) {
			// callback for testing
		}),
	)

	if manager == nil {
		t.Fatal("Failed to create secure script manager")
	}

	// Test cache operations
	manager.ClearCache()
	cached := manager.GetCachedScript("non-existent")
	if cached != nil {
		t.Error("Should return nil for non-existent script")
	}

	fmt.Printf("  Secure script manager created successfully\n")

	client.Close()
}

// TestWSClient tests WebSocket client creation
func TestWSClient(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
	)

	wsClient := NewWSClient(client,
		WithReconnect(true, 5*time.Second),
		WithConnectCallback(func() {
			// connect callback for testing
		}),
		WithDisconnectCallback(func(err error) {
			// disconnect callback for testing
		}),
		WithErrorCallback(func(err error) {
			// error callback for testing
		}),
	)

	if wsClient == nil {
		t.Fatal("Failed to create WebSocket client")
	}

	// Test initial state
	if wsClient.IsConnected() {
		t.Error("Should not be connected initially")
	}

	sessionID := wsClient.GetSessionID()
	if sessionID != "" {
		t.Error("Session ID should be empty initially")
	}

	// Test handler registration
	wsClient.RegisterHandler("test", func(inst *Instruction) (interface{}, error) {
		return "ok", nil
	})

	wsClient.RegisterHandlers(map[string]InstructionHandler{
		"test2": func(inst *Instruction) (interface{}, error) {
			return "ok2", nil
		},
	})

	fmt.Printf("  WebSocket client created successfully\n")

	client.Close()
}

// TestConvertSQLiteRows tests SQLite row conversion
func TestConvertSQLiteRows(t *testing.T) {
	columns := []string{"id", "name", "value"}
	rows := [][]interface{}{
		{1, "test1", 100},
		{2, "test2", 200},
	}

	result := ConvertSQLiteRows(columns, rows)

	if len(result) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(result))
	}

	if result[0]["id"] != 1 {
		t.Error("First row id should be 1")
	}

	if result[1]["name"] != "test2" {
		t.Error("Second row name should be test2")
	}

	fmt.Printf("  SQLite row conversion working correctly\n")
}

// TestApplySyncRecordToMap tests sync record conversion
func TestApplySyncRecordToMap(t *testing.T) {
	record := SyncRecord{
		ID: "test-id",
		Data: map[string]interface{}{
			"field1": "value1",
			"field2": 123,
		},
		Version:   1,
		IsDeleted: false,
	}

	result := ApplySyncRecordToMap(record)

	if result["field1"] != "value1" {
		t.Error("field1 should be value1")
	}

	if result["field2"] != 123 {
		t.Error("field2 should be 123")
	}

	fmt.Printf("  Sync record conversion working correctly\n")
}

// TestAutoSyncManager tests auto sync manager creation
func TestAutoSyncManager(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
	)

	syncClient := client.NewDataSyncClient()
	manager := syncClient.NewAutoSyncManager([]string{"table1", "table2"}, time.Minute)

	if manager == nil {
		t.Fatal("Failed to create auto sync manager")
	}

	manager.OnPull(func(tableName string, records []SyncRecord, deletes []string) error {
		// pull callback for testing
		return nil
	})

	manager.OnConflict(func(tableName string, result SyncResult) error {
		// conflict callback for testing
		return nil
	})

	fmt.Printf("  Auto sync manager created successfully\n")

	client.Close()
}

// TestPasswordHashing tests password hashing
func TestPasswordHashing(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey)

	hash1 := client.hashPassword("password123", "test@example.com")
	hash2 := client.hashPassword("password123", "test@example.com")
	hash3 := client.hashPassword("password123", "other@example.com")

	// Same password and email should produce same hash
	if hash1 != hash2 {
		t.Error("Same password and email should produce same hash")
	}

	// Different email should produce different hash
	if hash1 == hash3 {
		t.Error("Different email should produce different hash")
	}

	// Hash should be 64 characters (SHA256 hex)
	if len(hash1) != 64 {
		t.Errorf("Hash should be 64 characters, got %d", len(hash1))
	}

	fmt.Printf("  Password hashing working correctly\n")
	fmt.Printf("  Hash length: %d\n", len(hash1))

	client.Close()
}

// TestCertificateFingerprint tests fingerprint normalization
func TestCertificateFingerprint(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey)

	// Test various formats
	fp1 := client.normalizeFingerprint("SHA256:AB:CD:EF:12:34")
	fp2 := client.normalizeFingerprint("ABCDEF1234")
	fp3 := client.normalizeFingerprint("sha256:ab:cd:ef:12:34")

	if fp1 != "abcdef1234" {
		t.Errorf("Expected abcdef1234, got %s", fp1)
	}

	if fp2 != "abcdef1234" {
		t.Errorf("Expected abcdef1234, got %s", fp2)
	}

	if fp3 != "abcdef1234" {
		t.Errorf("Expected abcdef1234, got %s", fp3)
	}

	fmt.Printf("  Certificate fingerprint normalization working correctly\n")

	client.Close()
}

// TestSignatureVerificationOptions tests signature verification configuration
func TestSignatureVerificationOptions(t *testing.T) {
	// Test without public key - should not require signature
	client1 := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
	)
	if client1.IsSignatureEnabled() {
		t.Error("Signature should not be enabled without public key")
	}
	client1.Close()

	// Test with public key - should enable signature verification
	testPublicKey := `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0Z3VS5JJcds3xfn/ygWyf8sKZLk
-----END PUBLIC KEY-----`

	client2 := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
		WithServerPublicKey(testPublicKey),
	)
	if !client2.IsSignatureEnabled() {
		t.Error("Signature should be enabled with public key")
	}
	client2.Close()

	// Test with signature time window
	client3 := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
		WithSignatureTimeWindow(600),
	)
	if client3.signatureTimeWindow != 600 {
		t.Errorf("Expected signature time window 600, got %d", client3.signatureTimeWindow)
	}
	client3.Close()

	// Test require signature without public key
	client4 := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
		WithRequireSignature(true),
	)
	if !client4.requireSignature {
		t.Error("requireSignature should be true")
	}
	client4.Close()

	fmt.Printf("  Signature verification options working correctly\n")
}

// TestSignatureVerificationErrors tests signature verification error handling
func TestSignatureVerificationErrors(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
		WithRequireSignature(true),
	)

	// Test missing public key error
	testData := map[string]interface{}{
		"valid": true,
	}
	err := client.verifyResponseSignature(testData, "some_signature")
	if err != ErrInvalidPublicKey {
		t.Errorf("Expected ErrInvalidPublicKey, got %v", err)
	}

	// Test missing signature error (with require signature but no public key set yet)
	client.publicKeyPEM = "invalid"
	err = client.verifyResponseSignature(testData, "")
	if err != ErrSignatureMissing {
		t.Errorf("Expected ErrSignatureMissing, got %v", err)
	}

	client.Close()
	fmt.Printf("  Signature verification error handling working correctly\n")
}

// TestCanonicalJSON tests canonical JSON generation
func TestCanonicalJSON(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey)

	data := map[string]interface{}{
		"zebra":  1,
		"apple":  2,
		"banana": 3,
	}

	jsonBytes, err := client.canonicalJSON(data)
	if err != nil {
		t.Fatalf("canonicalJSON failed: %v", err)
	}

	// Keys should be sorted alphabetically
	expected := `{"apple":2,"banana":3,"zebra":1}`
	if string(jsonBytes) != expected {
		t.Errorf("Expected %s, got %s", expected, string(jsonBytes))
	}

	fmt.Printf("  Canonical JSON generation working correctly\n")
	client.Close()
}

// TestSetPublicKey tests dynamic public key setting
func TestSetPublicKey(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
	)

	if client.IsSignatureEnabled() {
		t.Error("Signature should not be enabled initially")
	}

	testPublicKey := `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0Z3VS5JJcds3xfn/ygWyf8sKZLk
-----END PUBLIC KEY-----`

	client.SetPublicKey(testPublicKey)

	if !client.IsSignatureEnabled() {
		t.Error("Signature should be enabled after SetPublicKey")
	}

	if client.publicKeyPEM != testPublicKey {
		t.Error("Public key not set correctly")
	}

	fmt.Printf("  Dynamic public key setting working correctly\n")
	client.Close()
}

// ==================== 高级安全模块测试 ====================

// TestAdvancedSecureClient tests advanced secure client
func TestAdvancedSecureClient(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
	)

	advClient := NewAdvancedSecureClient(client)
	if advClient == nil {
		t.Fatal("Failed to create advanced secure client")
	}

	// Test obfuscated validation
	result := advClient.IsValidObfuscated()
	if result == nil {
		t.Error("Obfuscated result should not be nil")
	}

	// Verify the obfuscated result
	verified := advClient.VerifyObfuscatedResult(result)
	// Result should be false since we haven't activated
	fmt.Printf("  Advanced secure client created successfully\n")
	fmt.Printf("  Obfuscated result verified: %v\n", verified)

	advClient.Close()
}

// TestRuntimeIntegrityChecker tests runtime integrity checking
func TestRuntimeIntegrityChecker(t *testing.T) {
	ric := NewRuntimeIntegrityChecker()
	if ric == nil {
		t.Fatal("Failed to create runtime integrity checker")
	}

	// Register a test function
	testFunc := func() bool { return true }
	ric.RegisterFunction("testFunc", testFunc)

	// Check integrity
	if !ric.CheckIntegrity() {
		t.Error("Integrity check should pass for unmodified functions")
	}

	fmt.Printf("  Runtime integrity checker working correctly\n")
}

func TestRuntimeIntegrityCheckerStartStopIsIdempotentAndRestartable(t *testing.T) {
	ric := NewRuntimeIntegrityChecker()
	ric.checkInterval = time.Hour

	ric.StartPeriodicCheck()
	firstStopCh := ric.stopChan
	if firstStopCh == nil {
		t.Fatal("StartPeriodicCheck should create a stop channel")
	}

	ric.StartPeriodicCheck()
	if ric.stopChan != firstStopCh {
		t.Fatal("duplicate StartPeriodicCheck should not replace the running checker")
	}

	ric.Stop()
	ric.Stop()
	if ric.stopChan != nil {
		t.Fatal("Stop should clear the stop channel")
	}

	ric.StartPeriodicCheck()
	if ric.stopChan == nil {
		t.Fatal("StartPeriodicCheck should restart after Stop")
	}
	ric.Stop()
}

// TestObfuscatedValidator tests obfuscated validation
func TestObfuscatedValidator(t *testing.T) {
	secretKey := []byte("test-secret-key-for-validation")
	ov := NewObfuscatedValidator(secretKey)
	if ov == nil {
		t.Fatal("Failed to create obfuscated validator")
	}

	// Create valid result
	validResult := ov.CreateResult(true)
	if validResult == nil {
		t.Error("Valid result should not be nil")
	}

	// Verify valid result
	if !ov.VerifyResult(validResult) {
		t.Error("Valid result should verify successfully")
	}

	// Create invalid result
	invalidResult := ov.CreateResult(false)
	if invalidResult == nil {
		t.Error("Invalid result should not be nil")
	}

	// Invalid result should not verify as valid
	if ov.VerifyResult(invalidResult) {
		t.Error("Invalid result should not verify as valid")
	}

	fmt.Printf("  Obfuscated validator working correctly\n")
}

// TestRandomVerificationScheduler tests random verification scheduling
func TestRandomVerificationScheduler(t *testing.T) {
	rvs := NewRandomVerificationScheduler(100*time.Millisecond, 200*time.Millisecond)
	if rvs == nil {
		t.Fatal("Failed to create random verification scheduler")
	}

	verifyCount := 0
	rvs.SetVerifyFunc(func() bool {
		verifyCount++
		return true
	})

	failureCount := 0
	rvs.SetFailureHandler(func() {
		failureCount++
	})

	// Get stats
	vCount, fCount := rvs.GetStats()
	if vCount != 0 || fCount != 0 {
		t.Error("Initial stats should be 0")
	}

	fmt.Printf("  Random verification scheduler working correctly\n")
}

func TestRandomVerificationSchedulerStartStopIsIdempotentAndRestartable(t *testing.T) {
	rvs := NewRandomVerificationScheduler(time.Hour, time.Hour)

	rvs.Start()
	firstStopCh := rvs.stopChan
	if firstStopCh == nil {
		t.Fatal("Start should create a stop channel")
	}

	rvs.Start()
	if rvs.stopChan != firstStopCh {
		t.Fatal("duplicate Start should not replace the running scheduler")
	}

	rvs.Stop()
	rvs.Stop()
	if rvs.stopChan != nil {
		t.Fatal("Stop should clear the stop channel")
	}

	rvs.Start()
	if rvs.stopChan == nil {
		t.Fatal("Start should restart after Stop")
	}
	rvs.Stop()
}

// TestHoneypotDetector tests honeypot detection
func TestHoneypotDetector(t *testing.T) {
	hd := NewHoneypotDetector()
	if hd == nil {
		t.Fatal("Failed to create honeypot detector")
	}

	// Record some calls
	hd.RecordCall("testFunc")
	time.Sleep(2 * time.Millisecond)
	hd.RecordCall("testFunc")
	time.Sleep(2 * time.Millisecond)
	hd.RecordCall("testFunc")

	// Should not be compromised yet
	if hd.IsCompromised() {
		t.Error("Should not be compromised with normal calls")
	}

	// Check call frequency
	if !hd.CheckCallFrequency("testFunc", 1000) {
		t.Error("Call frequency should be within limits")
	}

	fmt.Printf("  Honeypot detector working correctly\n")
}

// TestChallengeSolver tests challenge-response solving
func TestChallengeSolver(t *testing.T) {
	secretKey := []byte("test-secret-key-for-challenge")
	cs := NewChallengeSolver(secretKey)
	if cs == nil {
		t.Fatal("Failed to create challenge solver")
	}

	// Create a test challenge
	challenge := &ChallengeResponse{
		ChallengeID: "test-challenge-1",
		Challenge:   "random-challenge-data",
		Algorithm:   "hmac-sha256",
		Difficulty:  0,
		ExpiresAt:   time.Now().Unix() + 300,
	}

	// Solve the challenge
	answer, err := cs.SolveChallenge(challenge)
	if err != nil {
		t.Fatalf("Failed to solve challenge: %v", err)
	}

	if answer == nil {
		t.Error("Answer should not be nil")
	}

	if answer.ChallengeID != challenge.ChallengeID {
		t.Error("Challenge ID should match")
	}

	if answer.Answer == "" {
		t.Error("Answer should not be empty")
	}

	fmt.Printf("  Challenge solver working correctly\n")
	fmt.Printf("  Challenge ID: %s\n", answer.ChallengeID)
	fmt.Printf("  Answer: %s\n", answer.Answer[:16]+"...")
}

// TestProtectedBool tests protected boolean values
func TestProtectedBool(t *testing.T) {
	// Test true value
	pbTrue := NewProtectedBool(true)
	if pbTrue == nil {
		t.Fatal("Failed to create protected bool")
	}

	if !pbTrue.Get() {
		t.Error("Protected bool should return true")
	}

	if !pbTrue.IsIntact() {
		t.Error("Protected bool should be intact")
	}

	// Test false value
	pbFalse := NewProtectedBool(false)
	if pbFalse.Get() {
		t.Error("Protected bool should return false")
	}

	// Test setting value
	pbTrue.Set(false)
	if pbTrue.Get() {
		t.Error("Protected bool should return false after Set")
	}

	fmt.Printf("  Protected bool working correctly\n")
}

// TestCallStackChecker tests call stack checking
func TestCallStackChecker(t *testing.T) {
	csc := NewCallStackChecker()
	if csc == nil {
		t.Fatal("Failed to create call stack checker")
	}

	// Check caller (should pass in normal test environment)
	if !csc.CheckCaller(2) {
		t.Error("Call stack check should pass in normal environment")
	}

	// Get call stack
	stack := csc.GetCallStack(2)
	if len(stack) == 0 {
		t.Error("Call stack should not be empty")
	}

	fmt.Printf("  Call stack checker working correctly\n")
	fmt.Printf("  Stack depth: %d\n", len(stack))
}

// TestSecureRandom tests secure random number generation
func TestSecureRandom(t *testing.T) {
	sr := GetSecureRandom()
	if sr == nil {
		t.Fatal("Failed to get secure random")
	}

	// Test Uint32
	u32 := sr.Uint32()
	fmt.Printf("  Random Uint32: %d\n", u32)

	// Test Uint64
	u64 := sr.Uint64()
	fmt.Printf("  Random Uint64: %d\n", u64)

	// Test Bytes
	bytes := sr.Bytes(16)
	if len(bytes) != 16 {
		t.Errorf("Expected 16 bytes, got %d", len(bytes))
	}

	// Test Hex
	hex := sr.Hex(8)
	if len(hex) != 16 { // 8 bytes = 16 hex chars
		t.Errorf("Expected 16 hex chars, got %d", len(hex))
	}

	// Test IntN
	intN := sr.IntN(100)
	if intN < 0 || intN >= 100 {
		t.Errorf("IntN should be in range [0, 100), got %d", intN)
	}

	// Test Duration
	dur := sr.Duration(time.Second, 2*time.Second)
	if dur < time.Second || dur >= 2*time.Second {
		t.Errorf("Duration should be in range [1s, 2s), got %v", dur)
	}

	fmt.Printf("  Secure random working correctly\n")
}

// ==================== 强化安全模块测试 ====================

// TestHardenedDistributedValidator tests hardened distributed validation
func TestHardenedDistributedValidator(t *testing.T) {
	hdv := NewHardenedDistributedValidator("test-machine-id", "test-app-key")
	if hdv == nil {
		t.Fatal("Failed to create hardened distributed validator")
	}

	// Create valid result
	validResult := hdv.CreateDistributedResult(true)
	if validResult == nil {
		t.Error("Valid result should not be nil")
	}

	// Verify all tokens
	if !hdv.VerifyAll(validResult) {
		t.Error("All tokens should verify for valid result")
	}

	// Verify individual tokens
	for i := 0; i < 4; i++ {
		if !hdv.VerifyToken(validResult, i) {
			t.Errorf("Token %d should verify for valid result", i)
		}
	}

	// Create invalid result
	invalidResult := hdv.CreateDistributedResult(false)
	if hdv.VerifyAll(invalidResult) {
		t.Error("Invalid result should not verify")
	}

	fmt.Printf("  Hardened distributed validator working correctly\n")
}

// TestPublicKeyProtector tests public key protection
func TestPublicKeyProtector(t *testing.T) {
	pkp := NewPublicKeyProtector("test-machine-id")
	if pkp == nil {
		t.Fatal("Failed to create public key protector")
	}

	testKey := `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0Z3VS5JJcds3xfn/ygWy
f8sKZLkABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890abcdefghijklmnopqrstuv
-----END PUBLIC KEY-----`

	// Protect the key
	pkp.ProtectPublicKey(testKey)

	// Retrieve the key
	retrievedKey := pkp.GetPublicKey()
	if retrievedKey != testKey {
		t.Error("Retrieved key should match original")
	}

	fmt.Printf("  Public key protector working correctly\n")
}

// TestOpaquePredicates tests opaque predicates
func TestOpaquePredicates(t *testing.T) {
	op := NewOpaquePredicates()
	if op == nil {
		t.Fatal("Failed to create opaque predicates")
	}

	// AlwaysTrue should always return true
	for i := 0; i < 10; i++ {
		if !op.AlwaysTrue() {
			t.Error("AlwaysTrue should always return true")
		}
	}

	// AlwaysFalse should always return false
	for i := 0; i < 10; i++ {
		if op.AlwaysFalse() {
			t.Error("AlwaysFalse should always return false")
		}
	}

	// RandomLooking with expectedTrue should return true
	if !op.RandomLooking(true) {
		t.Error("RandomLooking(true) should return true")
	}

	// RandomLooking with expectedFalse should return false
	if op.RandomLooking(false) {
		t.Error("RandomLooking(false) should return false")
	}

	// ConfusingBranch should execute the real check
	result := op.ConfusingBranch(func() bool { return true })
	if !result {
		t.Error("ConfusingBranch should return true when realCheck returns true")
	}

	fmt.Printf("  Opaque predicates working correctly\n")
}

// TestEnhancedAntiDebug tests enhanced anti-debug detection
func TestEnhancedAntiDebug(t *testing.T) {
	ead := NewEnhancedAntiDebug()
	if ead == nil {
		t.Fatal("Failed to create enhanced anti-debug")
	}

	// Check if debugger is present (may vary by environment)
	isDebugger := ead.IsDebuggerPresent()
	fmt.Printf("  Enhanced debugger detection: %v\n", isDebugger)

	// Check WasDetected (should be false initially)
	if ead.WasDetected() {
		t.Error("Should not have detected debugger initially")
	}

	fmt.Printf("  Enhanced anti-debug working correctly\n")
}

func TestEnhancedAntiDebugStartStopIsIdempotentAndRestartable(t *testing.T) {
	ead := NewEnhancedAntiDebug()
	ead.checkInterval = time.Hour

	ead.StartContinuousCheck(func() {})
	firstStopCh := ead.stopChan
	if firstStopCh == nil {
		t.Fatal("StartContinuousCheck should create a stop channel")
	}

	ead.StartContinuousCheck(func() {})
	if ead.stopChan != firstStopCh {
		t.Fatal("duplicate StartContinuousCheck should not replace the running checker")
	}

	ead.Stop()
	ead.Stop()
	if ead.stopChan != nil {
		t.Fatal("Stop should clear the stop channel")
	}

	ead.StartContinuousCheck(func() {})
	if ead.stopChan == nil {
		t.Fatal("StartContinuousCheck should restart after Stop")
	}
	ead.Stop()
}

// TestHardenedSecureClient tests hardened secure client
func TestHardenedSecureClient(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
	)

	hsc := NewHardenedSecureClient(client)
	if hsc == nil {
		t.Fatal("Failed to create hardened secure client")
	}

	// Test distributed validation
	result := hsc.IsValidDistributed()
	if result == nil {
		t.Error("Distributed result should not be nil")
	}

	// Verify distributed token
	verified := hsc.VerifyDistributedToken(result, 0)
	fmt.Printf("  Distributed token 0 verified: %v\n", verified)

	// Get security status
	status := hsc.GetSecurityStatus()
	if status == nil {
		t.Error("Security status should not be nil")
	}

	fmt.Printf("  Hardened secure client created successfully\n")
	fmt.Printf("  Security status: %v\n", status)

	hsc.Close()
}

// TestScriptManager tests script manager
func TestScriptManager(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
	)

	sm := client.NewScriptManager()
	if sm == nil {
		t.Fatal("Failed to create script manager")
	}

	fmt.Printf("  Script manager created successfully\n")

	client.Close()
}

// TestCacheEncryption tests cache encryption
func TestCacheEncryption(t *testing.T) {
	client := NewClient(TestServerURL, TestAppKey,
		WithSkipVerify(true),
		WithEncryptCache(true),
	)

	// Test that cache encryption is enabled
	if !client.encryptCache {
		t.Error("Cache encryption should be enabled")
	}

	fmt.Printf("  Cache encryption working correctly\n")

	client.Close()
}

// TestValidationToken tests validation token creation and verification
func TestValidationToken(t *testing.T) {
	secretKey := []byte("test-secret-key-for-token")
	ov := NewObfuscatedValidator(secretKey)

	// Create token for valid state
	token := ov.CreateToken(true, 300)
	if token == nil {
		t.Error("Token should not be nil")
	}

	if token.Token == "" {
		t.Error("Token string should not be empty")
	}

	if token.Nonce == "" {
		t.Error("Token nonce should not be empty")
	}

	// Verify token
	if !ov.VerifyToken(token, true) {
		t.Error("Valid token should verify successfully")
	}

	// Token with wrong expected value should fail
	if ov.VerifyToken(token, false) {
		t.Error("Token should not verify with wrong expected value")
	}

	fmt.Printf("  Validation token working correctly\n")
	fmt.Printf("  Token: %s...\n", token.Token[:16])
}
