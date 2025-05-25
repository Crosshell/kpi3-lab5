package datastore

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"testing"
)

func TestEntry_EncodeWithChecksum(t *testing.T) {
	e := entry{key: "key", value: "value"}
	encoded := e.Encode()

	var decoded entry
	decoded.Decode(encoded)

	if decoded.key != "key" {
		t.Error("incorrect key")
	}
	if decoded.value != "value" {
		t.Error("incorrect value")
	}

	expectedChecksum := sha1.Sum([]byte("value"))
	if decoded.checksum != expectedChecksum {
		t.Error("incorrect checksum")
	}

	if err := decoded.verifyChecksum(); err != nil {
		t.Errorf("checksum verification failed: %v", err)
	}
}

func TestEntry_ChecksumVerification(t *testing.T) {
	e := entry{key: "testkey", value: "testvalue"}

	e.checksum = e.calculateChecksum()
	if err := e.verifyChecksum(); err != nil {
		t.Errorf("Expected valid checksum to pass, got error: %v", err)
	}

	e.checksum = sha1.Sum([]byte("corrupted_value"))
	if err := e.verifyChecksum(); err == nil {
		t.Error("Expected invalid checksum to fail, but it passed")
	}
}

func TestReadValueWithChecksum(t *testing.T) {
	e := entry{key: "key", value: "value"}
	data := e.Encode()

	v, err := readValue(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		t.Fatal(err)
	}
	if v != "value" {
		t.Errorf("incorrect value [%s]", v)
	}
}

func TestReadValueWithCorruptedChecksum(t *testing.T) {
	e := entry{key: "key", value: "value"}
	data := e.Encode()

	data[len(data)-1] = data[len(data)-1] ^ 0xFF

	_, err := readValue(bufio.NewReader(bytes.NewReader(data)))
	if err == nil {
		t.Error("Expected checksum verification to fail with corrupted data, but it passed")
	}

	if !bytes.Contains([]byte(err.Error()), []byte("checksum mismatch")) {
		t.Errorf("Expected checksum mismatch error, got: %v", err)
	}
}

func TestReadValueWithCorruptedData(t *testing.T) {
	e := entry{key: "key", value: "original_value"}
	data := e.Encode()

	valueStart := 4 + 4 + len(e.key) + 4
	if valueStart < len(data)-20 {
		data[valueStart] = data[valueStart] ^ 0xFF
	}

	_, err := readValue(bufio.NewReader(bytes.NewReader(data)))
	if err == nil {
		t.Error("Expected checksum verification to fail with corrupted value data, but it passed")
	}

	if !bytes.Contains([]byte(err.Error()), []byte("checksum mismatch")) {
		t.Errorf("Expected checksum mismatch error, got: %v", err)
	}
}

func TestEntry_ChecksumConsistency(t *testing.T) {
	testCases := []struct {
		key   string
		value string
	}{
		{"", ""},
		{"key", ""},
		{"", "value"},
		{"simple", "test"},
		{"unicode", "тест 🌟"},
		{"long_key_with_underscores", "very long value with multiple words and special characters !@#$%^&*()"},
	}

	for _, tc := range testCases {
		t.Run(tc.key+"_"+tc.value, func(t *testing.T) {
			e := entry{key: tc.key, value: tc.value}

			encoded := e.Encode()
			var decoded entry
			decoded.Decode(encoded)

			if decoded.key != tc.key {
				t.Errorf("Key mismatch: expected %s, got %s", tc.key, decoded.key)
			}
			if decoded.value != tc.value {
				t.Errorf("Value mismatch: expected %s, got %s", tc.value, decoded.value)
			}

			if err := decoded.verifyChecksum(); err != nil {
				t.Errorf("Checksum verification failed: %v", err)
			}

			v, err := readValue(bufio.NewReader(bytes.NewReader(encoded)))
			if err != nil {
				t.Fatalf("readValue failed: %v", err)
			}
			if v != tc.value {
				t.Errorf("readValue returned wrong value: expected %s, got %s", tc.value, v)
			}
		})
	}
}
