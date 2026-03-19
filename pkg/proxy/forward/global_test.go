package forward

import "testing"

func TestGlobalForwardManager_Singleton(t *testing.T) {
	ResetGlobalForwardManager()
	defer ResetGlobalForwardManager()

	SetGlobalConfig(PortAllocatorConfig{MinPort: 16000, MaxPort: 16100})

	gm1, err := GlobalForwardManager()
	if err != nil {
		t.Fatalf("first GlobalForwardManager: %v", err)
	}
	if gm1 == nil {
		t.Fatal("expected non-nil manager")
	}

	gm2, err := GlobalForwardManager()
	if err != nil {
		t.Fatalf("second GlobalForwardManager: %v", err)
	}

	// Should be the exact same instance
	if gm1 != gm2 {
		t.Fatal("GlobalForwardManager should return the same singleton instance")
	}
}

func TestGlobalForwardManager_Reset(t *testing.T) {
	ResetGlobalForwardManager()
	defer ResetGlobalForwardManager()

	SetGlobalConfig(PortAllocatorConfig{MinPort: 15000, MaxPort: 15100})

	gm1, _ := GlobalForwardManager()

	ResetGlobalForwardManager()

	// After reset, config can be set again
	SetGlobalConfig(PortAllocatorConfig{MinPort: 14000, MaxPort: 14100})

	gm2, _ := GlobalForwardManager()

	// Should be a different instance after reset
	if gm1 == gm2 {
		t.Fatal("after reset, GlobalForwardManager should return a new instance")
	}
}

func TestGlobalForwardManager_Functional(t *testing.T) {
	ResetGlobalForwardManager()
	defer ResetGlobalForwardManager()

	SetGlobalConfig(PortAllocatorConfig{MinPort: 13000, MaxPort: 13010})

	gm, _ := GlobalForwardManager()

	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	rule, err := gm.AddRule(ForwardRule{
		Username:   "guser@t.com",
		TargetAddr: echoAddr,
	})
	if err != nil {
		t.Fatalf("AddRule via global: %v", err)
	}
	if rule.ListenPort < 13000 || rule.ListenPort > 13010 {
		t.Errorf("port %d not in global range [13000,13010]", rule.ListenPort)
	}

	if err := gm.RemoveRule(rule.RuleKey()); err != nil {
		t.Fatalf("RemoveRule: %v", err)
	}
}
