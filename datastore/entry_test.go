package datastore

import (
	"bufio"
	"bytes"
	"testing"
)

func TestEntry_Encode(t *testing.T) {
	e := entry{"key", "value"}
	e.Decode(e.Encode())
	if e.key != "key" {
		t.Error("incorrect key")
	}
	if e.value != "value" {
		t.Error("incorrect value")
	}
}

func TestReadValue(t *testing.T) {
	var (
		a, b entry
	)
	a = entry{"key", "test-value"}
	originalBytes := a.Encode()

	b.Decode(originalBytes)
	t.Log("encode/decode", a, b)
	if a != b {
		t.Error("Encode/Decode mismatch")
	}

	b = entry{}
	n, err := b.DecodeFromReader(bufio.NewReader(bytes.NewReader(originalBytes)))
	if err != nil {
		t.Fatal(err)
	}
	t.Log("encode/decodeFromReader", a, b)
	if a != b {
		t.Error("Encode/DecodeFromReader mismatch")
	}
	if n != len(originalBytes) {
		t.Errorf("DecodeFromReader() read %d bytes, expected %d", n, len(originalBytes))
	}
}
