package main

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestMapToKVs(t *testing.T) {
	if out := mapToKVs(nil); out != nil {
		t.Fatalf("expected nil for nil map")
	}

	out := mapToKVs(map[string]string{
		"b": "2",
		"a": "1",
	})
	if len(out) != 2 {
		t.Fatalf("expected 2 kv pairs, got %d", len(out))
	}
	if out[0].K != "a" || out[0].V != "1" {
		t.Fatalf("unexpected first kv: %#v", out[0])
	}
	if out[1].K != "b" || out[1].V != "2" {
		t.Fatalf("unexpected second kv: %#v", out[1])
	}
}

func TestTagsToKVs(t *testing.T) {
	if out := tagsToKVs(nil); out != nil {
		t.Fatalf("expected nil for nil tags")
	}

	out := tagsToKVs([]types.Tag{
		{Key: aws.String("z"), Value: aws.String("last")},
		{Key: aws.String("a"), Value: aws.String("first")},
	})
	if len(out) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(out))
	}
	if out[0].K != "a" || out[0].V != "first" {
		t.Fatalf("unexpected first tag: %#v", out[0])
	}
	if out[1].K != "z" || out[1].V != "last" {
		t.Fatalf("unexpected second tag: %#v", out[1])
	}
}
