package response

import (
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNormalizeJSONValueKeepsNilDataNil(t *testing.T) {
	if normalizeJSONValue(nil) != nil {
		t.Fatal("expected nil to stay nil")
	}
}

func TestNormalizeJSONValueNilSliceAsEmptyArray(t *testing.T) {
	var list []string
	body, err := json.Marshal(normalizeJSONValue(list))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "[]" {
		t.Fatalf("expected nil slice to marshal as [], got %s", body)
	}
}

func TestNormalizeJSONValueNestedNilSlicesAsEmptyArrays(t *testing.T) {
	var list []gin.H
	var data []int
	body, err := json.Marshal(normalizeJSONValue(gin.H{
		"list": list,
		"data": data,
		"none": nil,
	}))
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["list"]) != "[]" {
		t.Fatalf("expected list to marshal as [], got %s", got["list"])
	}
	if string(got["data"]) != "[]" {
		t.Fatalf("expected data to marshal as [], got %s", got["data"])
	}
	if string(got["none"]) != "null" {
		t.Fatalf("expected none to marshal as null, got %s", got["none"])
	}
}
