package secretstore

import "testing"

func TestRecordValidate(t *testing.T) {
	valid := Record{ProviderID: "command", BaseURL: "https://api.commandcode.ai/provider/v1", APIKey: "secret"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, record := range map[string]Record{
		"missing provider": {BaseURL: valid.BaseURL, APIKey: valid.APIKey},
		"missing key":      {ProviderID: valid.ProviderID, BaseURL: valid.BaseURL},
		"http endpoint":    {ProviderID: valid.ProviderID, BaseURL: "http://api.commandcode.ai", APIKey: valid.APIKey},
	} {
		t.Run(name, func(t *testing.T) {
			if err := record.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestHubRecordValidate(t *testing.T) {
	for _, baseURL := range []string{
		"https://hub.example.test",
		"http://100.64.0.10:8787",
		"http://127.0.0.1:8787",
		HubAutoBaseURL,
		"auto",
	} {
		record := HubRecord{AgentID: "desktop-main", BaseURL: baseURL, Token: "secret"}
		if err := record.Validate(); err != nil {
			t.Fatalf("baseURL=%s: %v", baseURL, err)
		}
	}

	valid := HubRecord{AgentID: "desktop-main", BaseURL: "https://hub.example.test", Token: "secret"}
	for name, record := range map[string]HubRecord{
		"missing agent": {BaseURL: valid.BaseURL, Token: valid.Token},
		"missing token": {AgentID: valid.AgentID, BaseURL: valid.BaseURL},
		"relative URL":  {AgentID: valid.AgentID, BaseURL: "/hub", Token: valid.Token},
		"ftp URL":       {AgentID: valid.AgentID, BaseURL: "ftp://hub.example.test", Token: valid.Token},
		"user info":     {AgentID: valid.AgentID, BaseURL: "https://user@hub.example.test", Token: valid.Token},
		"query":         {AgentID: valid.AgentID, BaseURL: "https://hub.example.test?x=1", Token: valid.Token},
		"fragment":      {AgentID: valid.AgentID, BaseURL: "https://hub.example.test#x", Token: valid.Token},
	} {
		t.Run(name, func(t *testing.T) {
			if err := record.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
