package hashtags

import (
	"reflect"
	"testing"
)

func TestExtract(t *testing.T) {
	body := "# Heading\n\nA #Go post about #web-dev and #Go again.\n" +
		"```\n#not-a-tag\n```\nhttps://example.test/#fragment\n"
	got := Extract(body)
	want := []string{"go", "web-dev"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Extract() = %#v, want %#v", got, want)
	}
}
