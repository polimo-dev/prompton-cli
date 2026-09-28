package cmd

import (
	"encoding/json"
	"testing"
)

func TestCommitRetainsToolsAndNativeMessages(t *testing.T) {
	raw := []byte(`{"engine":"raw","messages":[{"type":"slot","name":"history"},{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"id\":9007199254740993}"}}],"reasoning_details":[{"signature":"opaque"}]},{"role":"tool","tool_call_id":"call-1","content":[{"type":"text","text":"{{literal}}"}]}],"tools":{"definitions":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}},"output_examples":[{"id":9007199254740993}]}]}}`)
	for _, format := range []string{"auto", "messages"} {
		req, err := buildCommit(raw, format, "prompt.json")
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		var original, actual map[string]json.RawMessage
		_ = json.Unmarshal(raw, &original)
		_ = json.Unmarshal(encoded, &actual)
		for _, key := range []string{"messages", "tools", "engine"} {
			var compactOriginal, compactActual any
			_ = json.Unmarshal(original[key], &compactOriginal)
			_ = json.Unmarshal(actual[key], &compactActual)
			a, _ := json.Marshal(compactOriginal)
			b, _ := json.Marshal(compactActual)
			if string(a) != string(b) {
				t.Fatalf("%s lost %s: %s", format, key, actual[key])
			}
		}
		if req.Engine != "raw" {
			t.Fatal("engine was not retained")
		}
	}
}

func TestCommitRejectsNonObjectToolContract(t *testing.T) {
	_, err := buildCommit([]byte(`{"messages":[{"role":"user","content":"Hello"}],"tools":[]}`), "messages", "prompt.json")
	if err == nil {
		t.Fatal("invalid tools accepted")
	}
}
