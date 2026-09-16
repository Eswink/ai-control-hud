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
